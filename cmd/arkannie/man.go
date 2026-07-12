package main

import (
	"fmt"
	"path/filepath"

	"arkannie/internal/registry"
)

// runMan implements `arkannie --man[=agent]`: it loads the registry and prints the
// per-agent execution manual — enough detail (dispatch rule, per-operation
// contract, personalities, examples) to drive an agent end to end. Like
// --catalog it is derived purely from the loaded contract: no LLM spawn, no
// program execution. Load errors are reported to stderr but do not suppress the
// valid agents. Exit 0 on success; 64 when a named agent is not registered.
func (a *App) runMan(args parsedArgs) int {
	agentsDir := filepath.Join(a.Root, ".agents")
	reg, errs := registry.Load(agentsDir)
	for _, e := range errs {
		fmt.Fprintln(a.Stderr, e)
	}
	out, ok := reg.Manual(args.manAgent)
	if !ok {
		fmt.Fprintf(a.Stderr, "usage error: unknown agent %q\n", args.manAgent)
		return 64
	}
	fmt.Fprint(a.Stdout, out)
	return 0
}
