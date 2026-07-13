package main

import (
	"fmt"
	"os"

	"arkannie/internal/ann"
)

// runCheck implements `arkannie --check <program.ann>`: a syntax-only parse of the
// program with zero side effects. It never loads the registry, never runs the
// claude healthcheck, and never writes .output/ or .mem/. A clean parse prints
// an OK line with an explicit "syntax only" disclaimer and exits 0; a parse
// error is reported to stderr in the canonical `parse error at L:C [category]:
// msg` form and exits 1. Argument validation (a required .ann input, mutual
// exclusion with the execution flags) happens earlier in parseArgs.
func (a *App) runCheck(args parsedArgs) int {
	src, err := os.ReadFile(args.input)
	if err != nil {
		fmt.Fprintln(a.Stderr, "program file could not be read: "+err.Error())
		return 1
	}
	if _, perr := ann.Parse(src, ann.ProgramMode); perr != nil {
		fmt.Fprintf(a.Stderr, "parse error at %d:%d [%s]: %s\n",
			perr.Line, perr.Col, perr.Category, perr.Msg)
		return 1
	}
	fmt.Fprintf(a.Stdout, "OK: %s parses (syntax only — no agents were run)\n", args.input)
	return 0
}
