package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Notices          []string            // notify/clarify/Class A messages for the report
	sleep            func(time.Duration) // backoff pause (R13); default time.Sleep, replaced in tests
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
		sleep: time.Sleep,
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
	callDepth    int            // nesting level of `call` (0 = top-level program)
	callSeq      int            // 1-based counter of calls executed (run-dir namespacing)
	returns      []capturedReturn
}

// capturedReturn records a [return]'s raw value and its --id, so a called module
// can reduce its returns to the value the call expression yields without going
// through the markdown report.
type capturedReturn struct {
	id  string
	val ram.Value
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
	case *ann.ParallelForeach:
		return s.execParallelForeach(st, v)
	case *ann.Foreach:
		return s.execForeach(st, v)
	case *ann.Loop:
		return s.execLoop(st, v)
	case *ann.If:
		return s.execIf(st, v)
	case *ann.Call:
		_, esc := s.execCall(st, v)
		return esc
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
	text = ram.Unescape(text)
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
		val = ram.Value{Kind: ram.KString, Str: ram.Unescape(op)}
	}
	st.returns = append(st.returns, capturedReturn{id: d.ID, val: val})
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
		_ = st.ram.Set(as.Name, ram.Value{Kind: ram.KString, Str: ram.Unescape(e.Value)}) // name validated at parse
		return nil
	case ann.ListLit:
		_ = st.ram.Set(as.Name, s.listValue(st, e)) // name validated at parse
		return nil
	case ann.MapLit:
		_ = st.ram.Set(as.Name, s.mapValue(st, e)) // name validated at parse
		return nil
	case *ann.Concat:
		_ = st.ram.Set(as.Name, s.concatValue(st, e)) // name validated at parse
		return nil
	case *ann.Dispatch:
		return s.assignDispatch(st, as.Name, e)
	case *ann.Call:
		val, esc := s.execCall(st, e)
		if esc != nil {
			return esc
		}
		_ = st.ram.Set(as.Name, val) // name validated at parse
		return nil
	default:
		return nil
	}
}

// listValue resolves a list() literal recursively (§2.6). Scalars stay strings,
// nested list()/map() build nested composites, and $refs are resolved from RAM.
// An unresolvable ref is a Class A notice and the element is OMITTED (v0.3: no
// longer a silent empty string).
func (s *Scheduler) listValue(st *execState, l ann.ListLit) ram.Value {
	items := make([]ram.Value, 0, len(l.Elems))
	for _, e := range l.Elems {
		if v, ok := s.elemValue(st, e); ok {
			items = append(items, v)
		}
	}
	return ram.Value{Kind: ram.KList, List: items}
}

// concatValue evaluates a concat() into a single list, flattening exactly ONE
// level: a list argument contributes its items, a non-list argument contributes
// itself, and an unresolvable ref is a Class A notice + omission (§2.6, v0.3).
func (s *Scheduler) concatValue(st *execState, c *ann.Concat) ram.Value {
	items := []ram.Value{}
	for _, e := range c.Args {
		v, ok := s.elemValue(st, e)
		if !ok {
			continue
		}
		if v.Kind == ram.KList {
			items = append(items, v.List...)
			continue
		}
		items = append(items, v)
	}
	return ram.Value{Kind: ram.KList, List: items}
}

// elemValue evaluates one list/concat element. The bool is false when an
// unresolvable $ref was found (a Class A notice is recorded and the caller
// omits the element).
func (s *Scheduler) elemValue(st *execState, e ann.Elem) (ram.Value, bool) {
	switch {
	case e.List != nil:
		return s.listValue(st, *e.List), true
	case e.Map != nil:
		return s.mapValue(st, *e.Map), true
	case e.IsRef:
		v, ok := st.ram.Resolve(e.Str)
		if !ok {
			s.Notices = append(s.Notices,
				fmt.Sprintf("[class A] $%s: unbound binding in list/concat — element omitted", e.Str))
			return ram.Value{}, false
		}
		return v, true
	default:
		return ram.Value{Kind: ram.KString, Str: ram.Unescape(e.Str)}, true
	}
}

// mapValue evaluates a map() literal into a KMap. Entries whose value is an
// unresolvable $ref are omitted (Class A), mirroring listValue (§2.6, v0.3).
func (s *Scheduler) mapValue(st *execState, m ann.MapLit) ram.Value {
	out := make(map[string]ram.Value, len(m.Entries))
	for _, e := range m.Entries {
		if v, ok := s.elemValue(st, e.Val); ok {
			out[e.Key] = v
		}
	}
	return ram.Value{Kind: ram.KMap, Map: out}
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

// execLoop runs the body up to Limit times (§6.7). A non-nil Until is a §8
// post-condition guard evaluated after each body and BEFORE that iteration's
// scope is popped, so it sees the bindings the body created: the loop breaks
// early once the guard holds. A composite operand is a Class A notice treated
// as unmet, so the loop still runs to Limit and the program continues.
func (s *Scheduler) execLoop(st *execState, l *ann.Loop) *Escalation {
	st.loopDepth++
	defer func() { st.loopDepth-- }()
	for i := 0; i < l.Limit; i++ {
		st.ram.Push()
		esc := s.runStatements(st, l.Body, 0, false)
		done := esc == nil && !st.stop && s.untilMet(st, l.Until)
		st.ram.Pop()
		if esc != nil {
			return esc
		}
		if st.stop || done {
			return nil
		}
	}
	return nil
}

// untilMet evaluates a loop's until post-condition against the current RAM
// scope (call before the iteration Pop). A nil guard is never met. A composite
// operand is not comparable: a Class A notice, treated as unmet (§8, R9).
func (s *Scheduler) untilMet(st *execState, g *ann.Guard) bool {
	if g == nil {
		return false
	}
	met, skipKind := compareOperands(st.ram, g.Left, g.Op, g.Right)
	if skipKind != "" {
		s.Notices = append(s.Notices, fmt.Sprintf(
			"[class A] loop until guard: operand resolved to a %s value, not comparable — treated as unmet", skipKind))
		return false
	}
	return met
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
	return compareOperands(st.ram, ifs.Left, ifs.Op, ifs.Right)
}

// compareOperands applies the deterministic ==/!= comparison shared by if
// conditions and loop until post-conditions (§8). It returns the guard result
// and a skip kind: a non-empty kind ("map"/"list") means a composite operand
// was found and the caller must treat the guard as not comparable. Null
// resolves for an irresolvable ref; null==null is true.
func compareOperands(r *ram.RAM, left ann.Operand, op string, right ann.Operand) (bool, string) {
	l := resolveOperand(r, left)
	rt := resolveOperand(r, right)
	if l.compound != "" {
		return false, l.compound
	}
	if rt.compound != "" {
		return false, rt.compound
	}
	eq := (l.isNull && rt.isNull) || (!l.isNull && !rt.isNull && l.str == rt.str)
	if op == "!=" {
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

// maxCallDepth is the `call` nesting limit (v0.4): a called module may not
// itself call another module.
const maxCallDepth = 1

// execCall runs `call "module.ann"` with FUNCTION semantics: the module executes
// in total RAM isolation, with no checkpointing (the call is an atomic statement
// of the parent — on child failure the parent escalates and a resume re-executes
// the whole call), and its [return]s reduce to the value the call yields. It
// returns that value (KString for one return, KMap keyed by --id for several) or
// a Class B escalation. The child's [return]s never touch the parent report.
func (s *Scheduler) execCall(st *execState, c *ann.Call) (ram.Value, *Escalation) {
	if st.callDepth >= maxCallDepth {
		return ram.Value{}, escCallDepth(c)
	}
	path, esc := resolveCallPath(st.programPath, c)
	if esc != nil {
		return ram.Value{}, esc
	}
	prog, esc := loadCallProgram(c, path)
	if esc != nil {
		return ram.Value{}, esc
	}
	st.callSeq++
	return s.runChild(st, prog)
}

// resolveCallPath resolves c.Path relative to the parent program's directory and
// enforces (Clean + prefix check, .langs §5) that it stays inside that tree. A
// path escaping the program directory is a Class B stop.
func resolveCallPath(programPath string, c *ann.Call) (string, *Escalation) {
	dir := filepath.Dir(programPath)
	target := filepath.Clean(filepath.Join(dir, c.Path))
	absDir, err1 := filepath.Abs(dir)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return "", escCallPath(c)
	}
	if absTarget != absDir && !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) {
		return "", escCallPath(c)
	}
	return target, nil
}

// loadCallProgram reads and parses a called module in program mode. A read
// failure or a parse error (including a wrong version header) is a Class B stop
// carrying the call site's line.
func loadCallProgram(c *ann.Call, path string) (*ann.Program, *Escalation) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, escCallLoad(c, fmt.Sprintf("module %q could not be read: %v", c.Path, err))
	}
	prog, perr := ann.Parse(src, ann.ProgramMode)
	if perr != nil {
		return nil, escCallLoad(c, fmt.Sprintf("module %q parse error at %d:%d [%s]: %s",
			c.Path, perr.Line, perr.Col, perr.Category, perr.Msg))
	}
	return prog, nil
}

// runChild executes a module in a fresh execState — isolated RAM, checkpointing
// off (programPath ""), depth+1, run dirs namespaced under <runID>/call-<n> —
// while sharing the parent's Spawner/config/registry. It returns the reduced
// call value or the child's escalation, which propagates to the parent.
func (s *Scheduler) runChild(st *execState, prog *ann.Program) (ram.Value, *Escalation) {
	child := &execState{
		ram:          ram.New(),
		runID:        fmt.Sprintf("%s/call-%d", st.runID, st.callSeq),
		programPath:  "",
		status:       envelope.StatusSuccess,
		returnCounts: map[string]int{},
		callDepth:    st.callDepth + 1,
	}
	if esc := s.runStatements(child, prog.Statements, 0, false); esc != nil {
		return ram.Value{}, esc
	}
	return s.callReturnValue(child), nil
}

// callReturnValue reduces a module's captured [return]s to the call value: one
// return → its value; two or more → a KMap keyed by --id; none → the empty
// string (with a Class A notice).
func (s *Scheduler) callReturnValue(child *execState) ram.Value {
	switch len(child.returns) {
	case 0:
		s.Notices = append(s.Notices, "[class A] call: module produced no [return] — bound the empty string")
		return ram.Value{Kind: ram.KString}
	case 1:
		return child.returns[0].val
	default:
		out := make(map[string]ram.Value, len(child.returns))
		for _, r := range child.returns {
			out[r.id] = r.val
		}
		return ram.Value{Kind: ram.KMap, Map: out}
	}
}

// escCallDepth reports a `call` nested past the depth-1 limit as Class B.
func escCallDepth(c *ann.Call) *Escalation {
	return &Escalation{
		Class: 'B',
		Title: "call depth exceeded",
		Detail: fmt.Sprintf("call %q at line %d exceeds the maximum nesting depth of %d: "+
			"a called module may not itself call another.", c.Path, c.Line, maxCallDepth),
		Command:  "[call]",
		Proposal: "Flatten the module graph so no called module contains its own call.",
	}
}

// escCallPath reports a module path escaping the program tree as Class B.
func escCallPath(c *ann.Call) *Escalation {
	return &Escalation{
		Class: 'B',
		Title: "call path escapes the program tree",
		Detail: fmt.Sprintf("call %q at line %d resolves outside the program directory; "+
			"a module path must stay within the program tree.", c.Path, c.Line),
		Command:  "[call]",
		Proposal: "Use a module path inside the program's directory tree.",
	}
}

// escCallLoad reports a module read/parse failure as Class B, carrying the call
// site's line in the detail.
func escCallLoad(c *ann.Call, detail string) *Escalation {
	return &Escalation{
		Class:    'B',
		Title:    "call module load failed",
		Detail:   fmt.Sprintf("%s (call at line %d)", detail, c.Line),
		Command:  "[call]",
		Proposal: "Fix the module path or its contents and re-run the program.",
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
