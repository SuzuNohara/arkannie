package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"arkannie/internal/registry"
)

// runValidate implements `arkannie validate [--agent=<n>]`: it loads the registry
// and reports validation errors. Exit 0 when clean, 1 on VAL violations, 64
// when a named agent does not exist.
func (a *App) runValidate(args parsedArgs) int {
	agentsDir := filepath.Join(a.Root, ".agents")
	reg, errs := registry.Load(agentsDir)
	if args.agent != "" {
		return a.validateOne(reg, errs, args.agent)
	}
	return a.validateAll(reg, errs)
}

// validateAll reports every load error, or an OK line with the valid count.
func (a *App) validateAll(reg *registry.Registry, errs []error) int {
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(a.Stderr, e)
		}
		return 1
	}
	fmt.Fprintf(a.Stdout, "OK: %d agent(s) valid\n", len(reg.Names()))
	return 0
}

// validateOne validates a single named agent: OK when it resolved, exit 1 when
// its contract carries violations, exit 64 when it does not exist at all.
func (a *App) validateOne(reg *registry.Registry, errs []error, agent string) int {
	bare := strings.Trim(agent, "[]")
	if _, ok := reg.Resolve(bare); ok {
		fmt.Fprintln(a.Stdout, "OK: 1 agent(s) valid")
		return 0
	}
	marker := string(filepath.Separator) + bare + string(filepath.Separator)
	var mine []error
	for _, e := range errs {
		if strings.Contains(e.Error(), marker) {
			mine = append(mine, e)
		}
	}
	if len(mine) > 0 {
		for _, e := range mine {
			fmt.Fprintln(a.Stderr, e)
		}
		return 1
	}
	fmt.Fprintf(a.Stderr, "usage error: unknown agent %q\n", agent)
	return 64
}
