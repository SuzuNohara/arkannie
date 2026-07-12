// Package checkpoint persists RAM snapshots for .ann program mode
// (spec/ann-lang.md §10). One checkpoint file is kept per program at
// <memDir>/checkpoints/<sanitized-program>-latest.yaml; Write
// overwrites it, Load never deletes, and only Clean removes it — the
// runtime calls Clean exclusively on successful completion (§10.5).
package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"arkannie/internal/ram"
)

// Checkpoint is the on-disk schema defined in §10.3.
type Checkpoint struct {
	Program           string               `yaml:"program"`
	Timestamp         string               `yaml:"timestamp"`           // ISO8601 UTC
	LastCompletedStep int                  `yaml:"last_completed_step"` // 0-indexed
	Bindings          map[string]ram.Value `yaml:"bindings"`
}

// Write persists a checkpoint for program, overwriting any previous
// one for the same program. Missing directories are created (0o755).
func Write(memDir, program string, step int, snap map[string]ram.Value) error {
	dir := filepath.Join(memDir, "checkpoints")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating checkpoint dir: %w", err)
	}
	cp := Checkpoint{
		Program:           program,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		LastCompletedStep: step,
		Bindings:          snap,
	}
	data, err := yaml.Marshal(&cp)
	if err != nil {
		return fmt.Errorf("marshaling checkpoint for %s: %w", program, err)
	}
	if err := os.WriteFile(path(memDir, program), data, 0o644); err != nil {
		return fmt.Errorf("writing checkpoint for %s: %w", program, err)
	}
	return nil
}

// Load reads the current checkpoint for program. It returns
// (nil, false) when no readable, well-formed checkpoint exists.
// Load never deletes checkpoint files.
func Load(memDir, program string) (*Checkpoint, bool) {
	data, err := os.ReadFile(path(memDir, program))
	if err != nil {
		return nil, false
	}
	var cp Checkpoint
	if err := yaml.Unmarshal(data, &cp); err != nil {
		return nil, false
	}
	return &cp, true
}

// Clean removes the checkpoint for program. Removing a checkpoint
// that does not exist is not an error. The runtime calls Clean only
// on successful program completion (§10.5).
func Clean(memDir, program string) error {
	if err := os.Remove(path(memDir, program)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleaning checkpoint for %s: %w", program, err)
	}
	return nil
}

func path(memDir, program string) string {
	return filepath.Join(memDir, "checkpoints", sanitize(program)+"-latest.yaml")
}

// sanitize maps a program name to [a-z0-9-]: runs of any other
// characters collapse to a single dash, with no leading or trailing
// dashes. The result is safe as a file name (no path traversal).
func sanitize(program string) string {
	var b strings.Builder
	b.Grow(len(program))
	pendingDash := false
	for _, c := range strings.ToLower(program) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(c)
			pendingDash = false
			continue
		}
		pendingDash = true
	}
	if b.Len() == 0 {
		return "program"
	}
	return b.String()
}
