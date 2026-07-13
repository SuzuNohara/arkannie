package scheduler

import (
	"fmt"
	"strings"

	"arkannie/internal/ann"
	"arkannie/internal/checkpoint"
	"arkannie/internal/config"
	"arkannie/internal/dispatch"
	"arkannie/internal/envelope"
	"arkannie/internal/output"
	"arkannie/internal/ram"
	"arkannie/internal/registry"
	"arkannie/internal/spawn"
)

// Scheduler is the sequential+concurrent executor for one Ann program.
type Scheduler struct {
	Reg              *registry.Registry
	Cfg              *config.Config
	Sp               spawn.Spawner
	MemDir           string
	PersonalitiesDir string
	InvokerCwd       string
	Consent          spawn.Consent
	Notices          []string // notify/clarify/Class A messages for the report
}

// RunResult is the aggregated outcome of a program run.
type RunResult struct {
	Status envelope.Status
	Report string
	Esc    *Escalation
}

// New builds a Scheduler with its collaborators injected.
func New(reg *registry.Registry, cfg *config.Config, sp spawn.Spawner,
	memDir, personalitiesDir, invokerCwd string, consent spawn.Consent) *Scheduler {
	return &Scheduler{
		Reg: reg, Cfg: cfg, Sp: sp,
		MemDir: memDir, PersonalitiesDir: personalitiesDir,
		InvokerCwd: invokerCwd, Consent: consent,
	}
}

// execState is the mutable per-run context threaded through execution.
type execState struct {
	ram          *ram.RAM
	runID        string
	programPath  string
	report       strings.Builder
	status       envelope.Status
	stop         bool
	seq          int
	loopDepth    int            // >0 while inside a foreach/loop/each body
	returnCounts map[string]int // per-[return] --id run counter for loop numbering
}

// preparedDispatch is a dispatch resolved up to (but not including) the spawn.
type preparedDispatch struct {
	did         string
	runID       string
	d           *ann.Dispatch
	a           *registry.Agent
	opName      string
	prompt      string
	runDir      string
	spec        spawn.RunSpec
	timeoutSecs int
}

// Run executes prog to completion and returns the aggregated result.
// programPath non-empty selects program mode: checkpoints (§10) apply.
func (s *Scheduler) Run(prog *ann.Program, runID, programPath string) *RunResult {
	st := &execState{
		ram:          ram.New(),
		runID:        runID,
		programPath:  programPath,
		status:       envelope.StatusSuccess,
		returnCounts: map[string]int{},
	}
	start := s.resume(st)
	esc := s.runStatements(st, prog.Statements, start, true)
	return s.finish(st, esc)
}

// resume restores a checkpoint (§10.4) in program mode, returning the index
// of the first statement still to execute.
func (s *Scheduler) resume(st *execState) int {
	if st.programPath == "" {
		return 0
	}
	cp, ok := checkpoint.Load(s.MemDir, st.programPath)
	if !ok {
		return 0
	}
	for name, v := range cp.Bindings {
		_ = st.ram.Set(name, v) // names validated at parse time
	}
	return cp.LastCompletedStep + 1
}

// finish assembles the final RunResult, sanitizing the report and cleaning
// the checkpoint only on a successful program (§10.5).
func (s *Scheduler) finish(st *execState, esc *Escalation) *RunResult {
	if esc != nil {
		return &RunResult{
			Status: envelope.StatusError,
			Report: output.Sanitize(esc.Format()),
			Esc:    esc,
		}
	}
	if st.programPath != "" && st.status == envelope.StatusSuccess {
		_ = checkpoint.Clean(s.MemDir, st.programPath) // best effort
	}
	return &RunResult{Status: st.status, Report: output.Sanitize(s.assembleReport(st))}
}

// assembleReport joins payload output with accumulated notices.
func (s *Scheduler) assembleReport(st *execState) string {
	var b strings.Builder
	b.WriteString(st.report.String())
	if len(s.Notices) > 0 {
		b.WriteString("\n## Notices\n\n")
		for _, n := range s.Notices {
			b.WriteString("- " + n + "\n")
		}
	}
	return b.String()
}

// runStatements executes stmts[start:] in order. topLevel enables the
// checkpoint trigger (§10.2). It stops on the first escalation or on a
// terminal ask-user.
func (s *Scheduler) runStatements(st *execState, stmts []ann.Stmt, start int, topLevel bool) *Escalation {
	for i := start; i < len(stmts); i++ {
		if topLevel {
			s.maybeCheckpoint(st, stmts, i)
		}
		if esc := s.execStmt(st, stmts[i]); esc != nil {
			return esc
		}
		if st.stop {
			return nil
		}
	}
	return nil
}

// execStmt dispatches one statement to its handler.
func (s *Scheduler) execStmt(st *execState, stmt ann.Stmt) *Escalation {
	switch v := stmt.(type) {
	case *ann.Dispatch:
		return s.execDispatch(st, v)
	case *ann.Assign:
		return s.execAssign(st, v)
	case *ann.Parallel:
		return s.execParallel(st, v)
	case *ann.Foreach:
		return s.execForeach(st, v)
	case *ann.Loop:
		return s.execLoop(st, v)
	case *ann.If:
		return s.execIf(st, v)
	default:
		return nil
	}
}

// execDispatch runs a bare dispatch or a native keyword and routes handlers.
func (s *Scheduler) execDispatch(st *execState, d *ann.Dispatch) *Escalation {
	if isKeyword(d.Command) {
		s.execKeyword(st, d)
		return nil
	}
	env, esc := s.dispatch(st, d)
	if esc != nil {
		return esc
	}
	if env == nil {
		return nil // Class A pre-dispatch skip
	}
	return s.routeDispatch(st, d, env)
}

// isKeyword reports whether cmd is a native batch keyword (§3).
func isKeyword(cmd string) bool {
	return cmd == "ask-user" || cmd == "notify" || cmd == "clarify" || cmd == "return"
}

// execKeyword handles ask-user (terminal info question), notify/clarify
// (report notices) and return (explicit output emission) in batch mode.
func (s *Scheduler) execKeyword(st *execState, d *ann.Dispatch) {
	if d.Command == "return" {
		s.execReturn(st, d)
		return
	}
	text := d.Context
	if text == "" && len(d.Args) > 0 {
		text = strings.Join(d.Args, " ")
	}
	switch d.Command {
	case "ask-user":
		st.report.WriteString("## Question\n\n" + text + "\n")
		st.status = envelope.StatusInfo
		st.stop = true
	case "notify", "clarify":
		s.Notices = append(s.Notices, text)
	}
}

// execReturn emits one value to the output body (the Ann output indicator).
// The operand is a $binding (resolved from RAM) or a string literal. The
// section title comes from --id; inside a loop it is suffixed with the run
// number (--id-N); a single non-loop return without --id has no title. The
// parser guarantees --id is present whenever the run count can exceed one.
// An unbound binding is a Class A notice + skip.
func (s *Scheduler) execReturn(st *execState, d *ann.Dispatch) {
	if len(d.Args) == 0 {
		s.Notices = append(s.Notices, "[class A] [return] has no operand — skipped")
		return
	}
	op := d.Args[0]
	var val ram.Value
	if strings.HasPrefix(op, "$") {
		name := op[1:]
		v, ok := st.ram.Resolve(name)
		if !ok {
			s.Notices = append(s.Notices, fmt.Sprintf("[class A] [return] $%s: unbound binding — skipped", name))
			return
		}
		val = v
	} else {
		val = ram.Value{Kind: ram.KString, Str: op}
	}
	label := d.ID
	if st.loopDepth > 0 {
		st.returnCounts[d.ID]++
		label = fmt.Sprintf("%s-%d", d.ID, st.returnCounts[d.ID])
	}
	st.report.WriteString(renderValue(label, val))
}

// dispatch prepares and invokes a single wave, applying the single retry
// (R10). (nil, nil) means a Class A pre-dispatch skip.
func (s *Scheduler) dispatch(st *execState, d *ann.Dispatch) (*envelope.Envelope, *Escalation) {
	prep, esc, skip := s.prepare(st, d)
	if esc != nil {
		return nil, esc
	}
	if skip {
		return nil, nil
	}
	return s.invoke(prep, true)
}

// prepare resolves agent, operation, context_block, prompt and run spec.
// Returns (prep,nil,false) on success, (nil,esc,false) on Class B/C, or
// (nil,nil,true) on a Class A skip.
func (s *Scheduler) prepare(st *execState, d *ann.Dispatch) (*preparedDispatch, *Escalation, bool) {
	did := d.ID
	if did == "" {
		did = fmt.Sprintf("s%d", st.seq)
		st.seq++
	}
	a, ok := s.Reg.Resolve(d.Command)
	if !ok {
		return nil, escUnknownCommand(d, did), false
	}
	op, opName, err := dispatch.SelectOperation(a, d)
	if err != nil {
		return s.predispatch(err, d, "", did)
	}
	return s.buildPrep(st, d, a, op, opName, did)
}

// routeDispatch applies the §2.2 trinary handlers of a bare dispatch.
func (s *Scheduler) routeDispatch(st *execState, d *ann.Dispatch, env *envelope.Envelope) *Escalation {
	handler, has := d.Handlers[ann.Status(env.Status)]
	switch env.Status {
	case envelope.StatusSuccess:
		if has {
			return s.runHandler(st, handler, env)
		}
		// Success payloads are no longer auto-dumped: the body is defined
		// explicitly by [return] directives. The payload stays in RAM only.
		return nil
	case envelope.StatusError:
		if has {
			return s.runHandler(st, handler, env)
		}
		return escUnhandledError(d, env)
	default:
		if has {
			return s.runHandler(st, handler, env)
		}
		s.infoDefault(st, env)
		return nil
	}
}

// runHandler executes a handler body in a fresh scope with $result bound.
func (s *Scheduler) runHandler(st *execState, body []ann.Stmt, env *envelope.Envelope) *Escalation {
	st.ram.Push()
	defer st.ram.Pop()
	bindResult(st.ram, env)
	return s.runStatements(st, body, 0, false)
}

// infoDefault applies §2.2/§2.7.1: discard info unless payload.missing_field
// is present, in which case surface it and set program status to info.
func (s *Scheduler) infoDefault(st *execState, env *envelope.Envelope) {
	m, ok := env.Payload.(map[string]any)
	if !ok {
		return
	}
	if _, ok := m["missing_field"]; !ok {
		return
	}
	st.report.WriteString(renderPayload(env))
	st.status = envelope.StatusInfo
}

// execAssign binds a string literal, a list(), or a dispatch result (§2.3).
func (s *Scheduler) execAssign(st *execState, as *ann.Assign) *Escalation {
	switch e := as.Expr.(type) {
	case ann.StrLit:
		_ = st.ram.Set(as.Name, ram.Value{Kind: ram.KString, Str: e.Value}) // name validated at parse
		return nil
	case ann.ListLit:
		_ = st.ram.Set(as.Name, s.listValue(st, e)) // name validated at parse
		return nil
	case *ann.Dispatch:
		return s.assignDispatch(st, as.Name, e)
	default:
		return nil
	}
}

// listValue resolves a list() literal, substituting $refs from RAM (§2.6).
func (s *Scheduler) listValue(st *execState, l ann.ListLit) ram.Value {
	items := make([]ram.Value, 0, len(l.Elems))
	for _, e := range l.Elems {
		if strings.HasPrefix(e, "$") {
			if v, ok := st.ram.Resolve(e[1:]); ok {
				items = append(items, v)
				continue
			}
			items = append(items, ram.Value{Kind: ram.KString})
			continue
		}
		items = append(items, ram.Value{Kind: ram.KString, Str: e})
	}
	return ram.Value{Kind: ram.KList, List: items}
}

// assignDispatch stores a success payload as the binding (§2.3); an error
// leaves the binding unset and escalates through the handler rules.
func (s *Scheduler) assignDispatch(st *execState, name string, d *ann.Dispatch) *Escalation {
	env, esc := s.dispatch(st, d)
	if esc != nil {
		return esc
	}
	if env == nil {
		return nil // Class A skip: binding not set
	}
	switch env.Status {
	case envelope.StatusSuccess:
		_ = st.ram.Set(name, payloadValue(env.Payload)) // name validated at parse
		if h, ok := d.Handlers[ann.StatusSuccess]; ok {
			return s.runHandler(st, h, env)
		}
		return nil
	case envelope.StatusError:
		if h, ok := d.Handlers[ann.StatusError]; ok {
			return s.runHandler(st, h, env)
		}
		return escUnhandledError(d, env)
	default:
		if h, ok := d.Handlers[ann.StatusInfo]; ok {
			return s.runHandler(st, h, env)
		}
		s.infoDefault(st, env)
		return nil
	}
}

// execForeach iterates a list binding sequentially, binding $item (§6.6). A
// non-list binding is a runtime type error → Class A notice + skip (§7.3).
func (s *Scheduler) execForeach(st *execState, f *ann.Foreach) *Escalation {
	v, ok := st.ram.Resolve(f.List)
	if !ok || v.Kind != ram.KList {
		s.Notices = append(s.Notices, fmt.Sprintf("[class A] foreach $%s: not a list binding — skipped", f.List))
		return nil
	}
	st.loopDepth++
	defer func() { st.loopDepth-- }()
	for _, item := range v.List {
		st.ram.Push()
		_ = st.ram.Set("item", item) // 'item' is a valid binding name
		esc := s.runStatements(st, f.Body, 0, false)
		st.ram.Pop()
		if esc != nil {
			return esc
		}
		if st.stop {
			return nil
		}
	}
	return nil
}

// execLoop runs the body up to Limit times (§6.7); no implicit break.
func (s *Scheduler) execLoop(st *execState, l *ann.Loop) *Escalation {
	st.loopDepth++
	defer func() { st.loopDepth-- }()
	for i := 0; i < l.Limit; i++ {
		st.ram.Push()
		esc := s.runStatements(st, l.Body, 0, false)
		st.ram.Pop()
		if esc != nil {
			return esc
		}
		if st.stop {
			return nil
		}
	}
	return nil
}

// guardVal is one operand of an if guard reduced to its comparable form:
// exactly one of null, a scalar string, or a composite (map/list) kind. A
// composite operand is not comparable and forces a Class A skip (§8).
type guardVal struct {
	isNull   bool
	str      string
	compound string // "" if null/scalar; else "map" or "list"
}

// execIf evaluates an if guard (§8) and runs the selected branch in its own
// RAM scope (Push/Pop), so bindings created inside a branch die on exit and
// the other branch never executes. A composite operand is a Class A notice
// that skips the whole statement — no branch runs and the program continues.
func (s *Scheduler) execIf(st *execState, ifs *ann.If) *Escalation {
	result, skipKind := s.evalGuard(st, ifs)
	if skipKind != "" {
		s.Notices = append(s.Notices, fmt.Sprintf(
			"[class A] if guard: operand resolved to a %s value, not comparable — statement skipped", skipKind))
		return nil
	}
	branch := ifs.Then
	if !result {
		branch = ifs.Else
	}
	if len(branch) == 0 {
		return nil
	}
	st.ram.Push()
	defer st.ram.Pop()
	return s.runStatements(st, branch, 0, false)
}

// evalGuard resolves both operands and applies the deterministic ==/!=
// comparison. It returns the guard result and a skip kind: a non-empty kind
// ("map"/"list") means a composite operand was found and the caller must skip
// the statement. Null resolves for an irresolvable ref; null==null is true.
func (s *Scheduler) evalGuard(st *execState, ifs *ann.If) (bool, string) {
	l := resolveOperand(st.ram, ifs.Left)
	r := resolveOperand(st.ram, ifs.Right)
	if l.compound != "" {
		return false, l.compound
	}
	if r.compound != "" {
		return false, r.compound
	}
	eq := (l.isNull && r.isNull) || (!l.isNull && !r.isNull && l.str == r.str)
	if ifs.Op == "!=" {
		eq = !eq
	}
	return eq, ""
}

// resolveOperand reduces an operand to its guardVal: the null literal and an
// irresolvable ref both become null; a ref to a string is that string; a ref
// to a map/list is flagged composite; a string literal is its verbatim value.
func resolveOperand(r *ram.RAM, op ann.Operand) guardVal {
	if op.IsNull {
		return guardVal{isNull: true}
	}
	if !op.IsRef {
		return guardVal{str: op.Text}
	}
	v, ok := r.Resolve(op.Text)
	if !ok {
		return guardVal{isNull: true}
	}
	switch v.Kind {
	case ram.KMap:
		return guardVal{compound: "map"}
	case ram.KList:
		return guardVal{compound: "list"}
	default:
		return guardVal{str: v.Str}
	}
}

// maybeCheckpoint writes a RAM snapshot before a top-level dispatch whose
// binding a later statement references (§10.2), in program mode.
func (s *Scheduler) maybeCheckpoint(st *execState, stmts []ann.Stmt, i int) {
	if st.programPath == "" || !producesReferencedBinding(stmts, i) {
		return
	}
	_ = checkpoint.Write(s.MemDir, st.programPath, i-1, st.ram.Snapshot()) // best effort
}
