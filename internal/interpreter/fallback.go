// Package interpreter implements the fallback interpreter (Rol 1): when a
// .ann program fails to compile and the caller passed --interpret, Claude is
// invoked at arkannie's root for exactly ONE minimal repair. The runtime always
// recompiles the returned program with ann.Parse — this package never
// executes anything itself and never retries.
package interpreter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"arkannie/internal/ann"
	"arkannie/internal/config"
	"arkannie/internal/envelope"
)

// ExecFunc abstracts running claude so tests inject a stub and production uses
// os/exec. It returns the child's stdout, its exit code, and any infrastructure
// error (binary missing, context cancelled).
type ExecFunc func(ctx context.Context, bin string, args []string, cwd string) (stdout []byte, exitCode int, err error)

// repairInstruction is the exact directive appended to every repair prompt.
const repairInstruction = "Devuelve ÚNICAMENTE un bloque cercado ```ann con el programa " +
	"corregido COMPLETO, o si no puedes corregirlo con confianza responde con una sola línea " +
	"`GIVEUP: <qué debe arreglar el invocador, específico y accionable>`. " +
	"No añadas comandos, agentes ni lógica nuevos."

// TryRepair attempts ONE minimal repair of a broken .ann program. Exactly one
// of {fixed, giveUp} is non-nil when err == nil:
//   - fixed  != nil → corrected .ann program (caller recompiles it with ann.Parse)
//   - giveUp != nil → info envelope whose message tells the caller what to fix
//   - err    != nil → infrastructure failure or unintelligible claude output
//
// No recursion, no second attempt.
func TryRepair(exec ExecFunc, cfg *config.Config, arkannieRoot string, src []byte, perr *ann.ParseError) (fixed []byte, giveUp *envelope.Envelope, err error) {
	prompt := buildRepairPrompt(src, perr)
	argv := []string{
		"-p", prompt,
		"--model", "sonnet",
		"--add-dir", filepath.Join(arkannieRoot, "spec"),
		"--output-format", "json",
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutDefault)*time.Second)
	defer cancel()

	stdout, exitCode, execErr := exec(ctx, cfg.ClaudeBin, argv, arkannieRoot)
	if execErr != nil {
		return nil, nil, fmt.Errorf("interpreter: running claude: %w", execErr)
	}
	if exitCode != 0 {
		return nil, nil, fmt.Errorf("interpreter: claude exited with code %d", exitCode)
	}
	result, err := resultText(stdout)
	if err != nil {
		return nil, nil, err
	}
	if block, ok := annBlock(result); ok {
		return []byte(block), nil, nil
	}
	if msg, ok := giveUpMessage(result); ok {
		return nil, &envelope.Envelope{
			Status:  envelope.StatusInfo,
			Payload: map[string]any{"message": msg},
		}, nil
	}
	return nil, nil, fmt.Errorf("interpreter: unintelligible claude output: %q", truncate(result, 200))
}

// categoryName maps a parse-error category to the exact token the repair
// prompt must show (Syntax/UnknownCommand/Type/VersionMismatch).
func categoryName(c ann.Category) string {
	switch c {
	case ann.Syntax:
		return "Syntax"
	case ann.UnknownCommand:
		return "UnknownCommand"
	case ann.Type:
		return "Type"
	case ann.VersionMismatch:
		return "VersionMismatch"
	default:
		return "Unknown"
	}
}

// buildRepairPrompt assembles the deterministic repair prompt: the broken .ann
// verbatim, the ParseError (line, category, message), then the exact directive.
func buildRepairPrompt(src []byte, perr *ann.ParseError) string {
	var b strings.Builder
	b.WriteString("El siguiente programa .ann no compila:\n\n```ann\n")
	b.Write(src)
	b.WriteString("\n```\n\n")
	fmt.Fprintf(&b, "Error de compilación:\n- Línea: %d\n- Categoría: %s\n- Mensaje: %s\n\n",
		perr.Line, categoryName(perr.Category), perr.Msg)
	b.WriteString(repairInstruction)
	return b.String()
}

// resultText deserializes claude's outer JSON and returns its string result
// field, without depending on envelope.Extract.
func resultText(stdout []byte) (string, error) {
	var outer map[string]any
	if err := json.Unmarshal(stdout, &outer); err != nil {
		return "", fmt.Errorf("interpreter: stdout is not valid claude JSON: %w", err)
	}
	result, ok := outer["result"].(string)
	if !ok {
		return "", errors.New(`interpreter: claude JSON has no string "result" field`)
	}
	return result, nil
}

// annBlock returns the contents of the first fenced ```ann block in s, without
// the fences and without the trailing newline preceding the closing fence.
func annBlock(s string) (string, bool) {
	const open = "```ann"
	start := strings.Index(s, open)
	if start == -1 {
		return "", false
	}
	rest := s[start+len(open):]
	nl := strings.IndexByte(rest, '\n')
	if nl == -1 {
		return "", false
	}
	body := rest[nl+1:]
	end := strings.Index(body, "```")
	if end == -1 {
		return "", false
	}
	return strings.TrimSuffix(body[:end], "\n"), true
}

// giveUpMessage returns the actionable text after a `GIVEUP:` marker, trimmed.
func giveUpMessage(s string) (string, bool) {
	const marker = "GIVEUP:"
	idx := strings.Index(s, marker)
	if idx == -1 {
		return "", false
	}
	rest := s[idx+len(marker):]
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		rest = rest[:nl]
	}
	msg := strings.TrimSpace(rest)
	if msg == "" {
		return "", false
	}
	return msg, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
