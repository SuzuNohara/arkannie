package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arkannie/internal/ann"
	"arkannie/internal/config"
	"arkannie/internal/interpreter"
	"arkannie/internal/output"
	"arkannie/internal/registry"
	"arkannie/internal/scheduler"
	"arkannie/internal/spawn"
)

// runExecute performs a blocking run: resolve the runID, execute under a
// panic guard, always write .output/<runID>.md and print its absolute path.
func (a *App) runExecute(args parsedArgs) int {
	runID := args.runID
	if runID == "" {
		runID = output.NewRunID(args.id)
	}
	started := a.now()
	res := a.guardedExecute(args, runID)
	finished := a.now()
	outputDir := filepath.Join(a.Root, ".output")
	outputID := output.SanitizeLabel(args.id)
	path, err := output.Write(outputDir, outputID, frontmatterAgent(res, args), args.input, res, started, finished)
	if err != nil {
		fmt.Fprintln(a.Stderr, "writing output: "+err.Error())
		return 1
	}
	fmt.Fprintln(a.Stdout, path)
	return output.ExitCode(res.Status)
}

// guardedExecute runs execute under a recover() so any panic (R17) becomes an
// error Result instead of crashing; an output file is always produced.
func (a *App) guardedExecute(args parsedArgs, runID string) (res output.Result) {
	defer func() {
		if r := recover(); r != nil {
			res = output.Result{Status: "error", Body: fmt.Sprintf("panic during execution: %v\n", r)}
		}
	}()
	return a.execute(args, runID)
}

// execute health-checks claude, then runs either program mode (a .ann file,
// each dispatch resolving its own agent) or prompt mode (free text against a
// single --agent). Program mode does not require --agent.
func (a *App) execute(args parsedArgs, runID string) output.Result {
	reg, _ := registry.Load(filepath.Join(a.Root, ".agents"))
	if _, err := config.Check(a.Cfg, filepath.Join(a.Root, ".mem")); err != nil {
		return errResult("[class B] claude healthcheck failed: %v", err)
	}
	if strings.HasSuffix(args.input, ".ann") {
		return a.runProgram(reg, args, runID)
	}
	ag, ok := reg.Resolve(args.agent)
	if !ok {
		return errResult("unknown agent [%s]: not registered under .agents/", strings.Trim(args.agent, "[]"))
	}
	return a.runPrompt(reg, ag, args, runID)
}

// frontmatterAgent chooses the agent label for the output frontmatter: the
// label the run computed (single agent for prompt mode, comma-joined list for
// program mode), falling back to --agent and finally "(program)".
func frontmatterAgent(res output.Result, args parsedArgs) string {
	if res.Agent != "" {
		return res.Agent
	}
	if a := strings.Trim(args.agent, "[]"); a != "" {
		return a
	}
	return "(program)"
}

// runProgram reads and parses a .ann file (repairing it via --interpret on a
// parse error) and runs the resulting program through the scheduler.
func (a *App) runProgram(reg *registry.Registry, args parsedArgs, runID string) output.Result {
	src, err := os.ReadFile(args.input)
	if err != nil {
		return errResult("program file %q could not be read: %v", args.input, err)
	}
	prog, perr := ann.Parse(src, ann.ProgramMode)
	var fixed []byte
	if perr != nil {
		repaired, fixedSrc, done, res := a.repairOrFail(args, src, perr)
		if done {
			return res
		}
		prog = repaired
		fixed = fixedSrc
	}
	res := a.runScheduler(reg, prog, runID, args.input, consentFrom(args))
	res.Agent = programAgents(prog)
	// R14: when --interpret repaired the program, include the corrected source
	// verbatim in the output body. The normal (no-repair) path is untouched.
	if fixed != nil {
		res.Body = withCorrectedProgram(fixed, res.Body)
	}
	return res
}

// programAgents returns the distinct, sorted agent commands dispatched by the
// program (excluding native keywords), joined by ", " for the frontmatter.
// Empty program (or only keywords) yields "(program)".
func programAgents(prog *ann.Program) string {
	seen := map[string]bool{}
	collectAgents(prog.Statements, seen)
	if len(seen) == 0 {
		return "(program)"
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// collectAgents walks statements (including nested handlers and blocks),
// recording every dispatch command that is a real agent, not a native keyword.
func collectAgents(stmts []ann.Stmt, seen map[string]bool) {
	for _, st := range stmts {
		switch v := st.(type) {
		case *ann.Dispatch:
			addAgent(v, seen)
		case *ann.Assign:
			if d, ok := v.Expr.(*ann.Dispatch); ok {
				addAgent(d, seen)
			}
		case *ann.Parallel:
			for i := range v.Dispatches {
				addAgent(&v.Dispatches[i], seen)
			}
			collectAgents(v.Each, seen)
		case *ann.Foreach:
			collectAgents(v.Body, seen)
		case *ann.Loop:
			collectAgents(v.Body, seen)
		}
	}
}

// addAgent records a dispatch's command and recurses into its handlers,
// skipping native keywords (ask-user, notify, clarify, return).
func addAgent(d *ann.Dispatch, seen map[string]bool) {
	switch d.Command {
	case "ask-user", "notify", "clarify", "return":
	default:
		seen[d.Command] = true
	}
	for _, body := range d.Handlers {
		collectAgents(body, seen)
	}
}

// withCorrectedProgram prepends a clearly labeled block containing the
// interpreter-repaired program (verbatim) to the run report body (R14).
func withCorrectedProgram(fixed []byte, body string) string {
	return "## Programa corregido por el intérprete (--interpret)\n\n" +
		"```ann\n" + string(fixed) + "\n```\n\n---\n\n" + body
}

// repairOrFail applies the --interpret fallback. It returns done=true with a
// terminal Result when the run cannot proceed, or a repaired program plus its
// corrected source (fixed) when it can. Without --interpret a parse error is
// reported and claude is never run.
func (a *App) repairOrFail(args parsedArgs, src []byte, perr *ann.ParseError) (*ann.Program, []byte, bool, output.Result) {
	if !args.interpret {
		return nil, nil, true, errResult("parse error at %d:%d [%s]: %s", perr.Line, perr.Col, perr.Category, perr.Msg)
	}
	fixed, giveUp, err := interpreter.TryRepair(a.Exec, a.Cfg, a.Root, src, perr)
	if err != nil {
		return nil, nil, true, errResult("interpreter: %v", err)
	}
	if giveUp != nil {
		msg := ""
		if m, ok := giveUp.Payload.(map[string]any); ok {
			msg, _ = m["message"].(string)
		}
		return nil, nil, true, output.Result{Status: "info", Body: "## Interpreter could not repair the program\n\n" + msg + "\n"}
	}
	prog, perr2 := ann.Parse(fixed, ann.ProgramMode)
	if perr2 != nil {
		return nil, nil, true, errResult("parse error after repair at %d:%d [%s]: %s",
			perr2.Line, perr2.Col, perr2.Category, perr2.Msg)
	}
	return prog, fixed, false, output.Result{}
}

// runPrompt builds a single dispatch to the --agent's default operation with
// the prompt as verbatim context.text and runs it through the same pipeline.
func (a *App) runPrompt(reg *registry.Registry, ag *registry.Agent, args parsedArgs, runID string) output.Result {
	if ag.DefaultOperation == "" {
		return errResult("agent [%s] has no default_operation; available operations: %s",
			strings.Trim(ag.Command, "[]"), strings.Join(sortedOps(ag), ", "))
	}
	// Prompt mode has no .ann to carry a [return], so synthesize the unified
	// output path: bind the single dispatch, then emit it via [return]. This
	// keeps prompt mode from producing an empty body under the F0 model.
	cmd := strings.Trim(ag.Command, "[]")
	prog := &ann.Program{Statements: []ann.Stmt{
		&ann.Assign{Name: "__out", Expr: &ann.Dispatch{Command: cmd, Context: args.input}},
		&ann.Dispatch{Command: "return", Args: []string{"$__out"}}, // single, unlabeled
	}}
	res := a.runScheduler(reg, prog, runID, "", consentFrom(args))
	res.Agent = cmd
	return res
}

// consentFrom maps the parsed argv into the spawn consent contract. Bare
// --allow-layer (no list) grants every layer agent; a list restricts consent
// to the named agents. Workspace and layer are independent surfaces.
func consentFrom(args parsedArgs) spawn.Consent {
	return spawn.Consent{
		Workspace: args.allowWorkspace,
		LayerAll:  args.allowLayer && len(args.allowLayerList) == 0,
		LayerList: args.allowLayerList,
	}
}

// runScheduler executes prog and folds the RunResult into an output.Result.
// programPath non-empty selects program mode (checkpoints apply).
func (a *App) runScheduler(reg *registry.Registry, prog *ann.Program, runID, programPath string, consent spawn.Consent) output.Result {
	invoker := a.InvokerCwd
	if invoker == "" {
		invoker = a.Root
	}
	sched := scheduler.New(reg, a.Cfg, a.Spawner,
		filepath.Join(a.Root, ".mem"),
		filepath.Join(a.Root, ".agents", ".personalities"),
		invoker, consent)
	rr := sched.Run(prog, runID, programPath)
	return output.Result{Status: string(rr.Status), Body: rr.Report}
}

// sortedOps returns an agent's operation names in deterministic order.
func sortedOps(ag *registry.Agent) []string {
	names := make([]string, 0, len(ag.Operations))
	for name := range ag.Operations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// errResult builds a status-error Result with a formatted body.
func errResult(format string, args ...any) output.Result {
	return output.Result{Status: "error", Body: fmt.Sprintf(format, args...) + "\n"}
}
