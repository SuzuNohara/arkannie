package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkannie/internal/ann"
)

// TestProgramAgentsIncludesCalledModules pins T6.10: the .output frontmatter
// agent list folds in the agents dispatched by called modules (parse at depth 1),
// resolved relative to the parent program's directory.
func TestProgramAgentsIncludesCalledModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.ann"),
		[]byte("# ann v0.3\n[seeker] find\n[return] \"r\"\n"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	src := "# ann v0.3\n[echo] --id=e : hi\n$x = call \"child.ann\"\n"
	prog, perr := ann.Parse([]byte(src), ann.ProgramMode)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	got := programAgents(prog, dir)
	if !strings.Contains(got, "echo") {
		t.Errorf("frontmatter %q missing parent agent echo", got)
	}
	if !strings.Contains(got, "seeker") {
		t.Errorf("frontmatter %q missing called-module agent seeker", got)
	}
}
