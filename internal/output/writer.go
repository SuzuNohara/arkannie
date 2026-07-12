// Package output writes trinary run results to the output directory:
// timestamp-based run IDs, collision-safe file creation (O_EXCL) and
// redaction of credential-shaped content before anything is persisted.
package output

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Result is the aggregated outcome of a program run, ready to persist.
type Result struct {
	Status string // "success" | "error" | "info"
	Body   string // markdown already aggregated by the scheduler
	Agent  string // agent label for the frontmatter (single agent or a list)
}

// maxCollisionRetries is how many suffixed names are tried after the
// unsuffixed run ID collides with an existing file.
const maxCollisionRetries = 10

const redactionMarker = "[REDACTED — potential credential]"

// credentialPatterns are applied in order; the complete BEGIN/END key
// block must run before the bare BEGIN header so full blocks collapse
// into a single redaction marker.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*KEY-----.*?-----END [A-Z ]*KEY-----`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*KEY-----`),
	regexp.MustCompile(`[a-z+]+://[^\s/]+:[^\s@]+@`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\b(?:sk-|AIza|AKIA|xoxb-|ghp_|ghs_)[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`),
}

// NewRunID returns a run identifier: UTC "20060102T150405" + "." +
// microseconds (%06d) + "Z", plus "-" + sanitized label when label is
// non-empty. The result matches ^\d{8}T\d{6}\.\d{6}Z(-[a-z0-9-]+)?$.
func NewRunID(label string) string {
	now := time.Now().UTC()
	id := fmt.Sprintf("%s.%06dZ", now.Format("20060102T150405"), now.Nanosecond()/1000)
	if label != "" {
		id += "-" + sanitizeLabel(label)
	}
	return id
}

// SanitizeLabel exposes sanitizeLabel so CLI callers can derive an output
// file id from a user-supplied --id using the same rules as run-ID labels.
func SanitizeLabel(label string) string {
	return sanitizeLabel(label)
}

// sanitizeLabel lowercases the label and maps every character outside
// [a-z0-9-] to "-".
func sanitizeLabel(label string) string {
	var b strings.Builder
	b.Grow(len(label))
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ExitCode maps a run status to the trinary process exit code.
func ExitCode(status string) int {
	switch status {
	case "success":
		return 0
	case "error":
		return 1
	case "info":
		return 2
	default:
		return 1
	}
}

// Sanitize replaces credential-shaped substrings (private key blocks,
// connection strings with passwords, JWTs, known API key prefixes and
// high-entropy hex) with a redaction marker. Normal text is untouched.
func Sanitize(s string) string {
	for _, re := range credentialPatterns {
		s = re.ReplaceAllString(s, redactionMarker)
	}
	return s
}

// Write persists a run result to <outputDir>/<outputID>.md, creating
// outputDir when missing. The newest run always keeps the clean
// <outputID>.md name: on collision the previous file is renamed to
// <outputID>-N.md (smallest free N ≥ 1) before the new content is written.
// It returns the path of the file actually written (always the clean name).
func Write(outputDir, outputID, agent, input string, res Result, started, finished time.Time) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output dir %s: %w", outputDir, err)
	}
	return writeNewest(outputDir, outputID, render(outputID, agent, input, res, started, finished))
}

// render builds the frontmatter + sanitized body content of the file.
func render(outputID, agent, input string, res Result, started, finished time.Time) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", outputID)
	fmt.Fprintf(&b, "agent: %s\n", agent)
	fmt.Fprintf(&b, "status: %s\n", res.Status)
	fmt.Fprintf(&b, "started: %s\n", started.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "finished: %s\n", finished.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "input: %s\n", Sanitize(input))
	b.WriteString("---\n")
	body := Sanitize(res.Body)
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

// writeNewest writes content to <dir>/<outputID>.md so the newest run always
// holds the clean name. When that file already exists it is first archived to
// <outputID>-N.md, then the new content is created with O_EXCL.
func writeNewest(dir, outputID, content string) (string, error) {
	canonical := filepath.Join(dir, outputID+".md")
	if err := archivePrevious(dir, outputID, canonical); err != nil {
		return "", err
	}
	f, err := os.OpenFile(canonical, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating output file %s: %w", canonical, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close() // best effort; write error takes precedence
		return "", fmt.Errorf("writing output file %s: %w", canonical, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing output file %s: %w", canonical, err)
	}
	return canonical, nil
}

// archivePrevious renames an existing <outputID>.md to <outputID>-N.md using
// the smallest free N in [1, maxCollisionRetries], leaving the clean name free
// for the new (newest) run. It is a no-op when the clean name is unused.
func archivePrevious(dir, outputID, canonical string) error {
	if _, err := os.Stat(canonical); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat output file %s: %w", canonical, err)
	}
	for n := 1; n <= maxCollisionRetries; n++ {
		archived := filepath.Join(dir, fmt.Sprintf("%s-%d.md", outputID, n))
		if _, err := os.Stat(archived); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(canonical, archived); err != nil {
				return fmt.Errorf("archiving previous output %s -> %s: %w", canonical, archived, err)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("stat output file %s: %w", archived, err)
		}
	}
	return fmt.Errorf("output file collision: %s and %d suffixed variants already exist",
		canonical, maxCollisionRetries)
}
