package main

import (
	"fmt"
	"path/filepath"

	"arkannie/internal/registry"
)

// runCatalog implements `arkannie --catalog[=agent]`: it loads the registry and
// prints the agent capability catalog — the calling card of every valid agent —
// so the orchestrator can discover and select agents. Load errors are reported
// to stderr but do not suppress the valid agents. Exit 0 on success; 64 when a
// named agent is not registered.
func (a *App) runCatalog(args parsedArgs) int {
	agentsDir := filepath.Join(a.Root, ".agents")
	reg, errs := registry.Load(agentsDir)
	for _, e := range errs {
		fmt.Fprintln(a.Stderr, e)
	}
	out, ok := reg.Catalog(args.catalogAgent)
	if !ok {
		fmt.Fprintf(a.Stderr, "usage error: unknown agent %q\n", args.catalogAgent)
		return 64
	}
	fmt.Fprint(a.Stdout, out)
	return 0
}
