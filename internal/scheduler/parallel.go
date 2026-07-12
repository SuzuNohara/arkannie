package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkannie/internal/ann"
	"arkannie/internal/dispatch"
	"arkannie/internal/envelope"
	"arkannie/internal/registry"
	"arkannie/internal/spawn"
)

var refToken = regexp.MustCompile(`\$[A-Za-z0-9_]+`)

// parResult carries one parallel dispatch outcome back from its goroutine.
type parResult struct {
	did string
	env *envelope.Envelope
	esc *Escalation
}

// execParallel runs a parallel {} block (§6): all dispatches concurrently
// (bounded by MaxConcurrency), then each -> {} serially in arrival order.
func (s *Scheduler) execParallel(st *execState, p *ann.Parallel) *Escalation {
	preps, esc := s.prepareAll(st, p.Dispatches)
	if esc != nil {
		return esc
	}
	results := s.runConcurrent(preps)
	return s.processResults(st, p, preps, results)
}

// prepareAll resolves every dispatch sequentially (RAM reads happen before
// any goroutine starts). Class B stops the block; Class A skips one dispatch.
func (s *Scheduler) prepareAll(st *execState, ds []ann.Dispatch) ([]*preparedDispatch, *Escalation) {
	preps := make([]*preparedDispatch, 0, len(ds))
	for i := range ds {
		prep, esc, skip := s.prepare(st, &ds[i])
		if esc != nil {
			return nil, esc
		}
		if skip {
			continue
		}
		preps = append(preps, prep)
	}
	return preps, nil
}

// runConcurrent spawns every prepared dispatch under a MaxConcurrency
// semaphore and collects results in completion (arrival) order.
func (s *Scheduler) runConcurrent(preps []*preparedDispatch) []parResult {
	sem := make(chan struct{}, s.maxConcurrency())
	out := make(chan parResult, len(preps))
	var wg sync.WaitGroup
	for _, prep := range preps {
		wg.Add(1)
		go func(pr *preparedDispatch) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			env, esc := s.invoke(pr, false)
			out <- parResult{did: pr.did, env: env, esc: esc}
		}(prep)
	}
	go func() { wg.Wait(); close(out) }()
	results := make([]parResult, 0, len(preps))
	for r := range out {
		results = append(results, r)
	}
	return results
}

// maxConcurrency returns the configured bound, never below 1.
func (s *Scheduler) maxConcurrency() int {
	if s.Cfg != nil && s.Cfg.MaxConcurrency > 0 {
		return s.Cfg.MaxConcurrency
	}
	return 1
}

// processResults correlates envelopes by id (§3) and routes each -> {}
// serially, or escalates unhandled errors after all complete (§6.8).
func (s *Scheduler) processResults(st *execState, p *ann.Parallel, preps []*preparedDispatch, results []parResult) *Escalation {
	reg := make(map[string]*preparedDispatch, len(preps))
	for _, pr := range preps {
		reg[pr.did] = pr
	}
	hasEach := p.Each != nil
	if hasEach {
		// each runs once per dispatch, so returns inside it are numbered.
		st.loopDepth++
		defer func() { st.loopDepth-- }()
	}
	seen := map[string]bool{}
	var firstEsc *Escalation
	var errored []string
	for _, r := range results {
		if esc := s.handleOneResult(st, p, reg, seen, r, hasEach, &errored); esc != nil && firstEsc == nil {
			firstEsc = esc
		}
	}
	if firstEsc != nil {
		return firstEsc
	}
	if !hasEach && len(errored) > 0 {
		return escParallelErrors(errored)
	}
	return nil
}

// handleOneResult correlates and routes a single parallel result.
func (s *Scheduler) handleOneResult(st *execState, p *ann.Parallel, reg map[string]*preparedDispatch,
	seen map[string]bool, r parResult, hasEach bool, errored *[]string) *Escalation {
	if r.esc != nil {
		return r.esc
	}
	id := r.env.ID
	if _, ok := reg[id]; !ok {
		return escOrphan(id)
	}
	if seen[id] {
		return escDuplicate(id)
	}
	seen[id] = true
	if hasEach {
		return s.runHandler(st, p.Each, r.env)
	}
	if r.env.Status == envelope.StatusError {
		*errored = append(*errored, id)
	}
	return nil
}

// producesReferencedBinding reports whether stmts[i] assigns a dispatch
// result that a later top-level statement reads (§10.2).
func producesReferencedBinding(stmts []ann.Stmt, i int) bool {
	as, ok := stmts[i].(*ann.Assign)
	if !ok {
		return false
	}
	if _, isDispatch := as.Expr.(*ann.Dispatch); !isDispatch {
		return false
	}
	for j := i + 1; j < len(stmts); j++ {
		if referencesBinding(stmts[j], as.Name) {
			return true
		}
	}
	return false
}

// referencesBinding reports whether stmt reads $name anywhere.
func referencesBinding(stmt ann.Stmt, name string) bool {
	found := false
	walkRefs(stmt, func(n string) {
		if n == name {
			found = true
		}
	})
	return found
}

// walkRefs visits every $binding reference reachable from stmt.
func walkRefs(stmt ann.Stmt, fn func(string)) {
	switch v := stmt.(type) {
	case *ann.Dispatch:
		dispatchRefs(v, fn)
	case *ann.Assign:
		exprRefs(v.Expr, fn)
	case *ann.Parallel:
		for i := range v.Dispatches {
			dispatchRefs(&v.Dispatches[i], fn)
		}
		walkAll(v.Each, fn)
	case *ann.Foreach:
		fn(v.List)
		walkAll(v.Body, fn)
	case *ann.Loop:
		walkAll(v.Body, fn)
	}
}

// walkAll visits references across a statement list.
func walkAll(stmts []ann.Stmt, fn func(string)) {
	for _, s := range stmts {
		walkRefs(s, fn)
	}
}

// dispatchRefs visits references in a dispatch's args, context and handlers.
func dispatchRefs(d *ann.Dispatch, fn func(string)) {
	for _, a := range d.Args {
		if strings.HasPrefix(a, "$") {
			fn(a[1:])
		}
	}
	for _, m := range refToken.FindAllString(d.Context, -1) {
		fn(m[1:])
	}
	for _, body := range d.Handlers {
		walkAll(body, fn)
	}
}

// exprRefs visits references in a binding right-hand side.
func exprRefs(e ann.Expr, fn func(string)) {
	switch v := e.(type) {
	case *ann.Dispatch:
		dispatchRefs(v, fn)
	case ann.ListLit:
		for _, el := range v.Elems {
			if strings.HasPrefix(el, "$") {
				fn(el[1:])
			}
		}
	case ann.StrLit:
		for _, m := range refToken.FindAllString(v.Value, -1) {
			fn(m[1:])
		}
	}
}

// buildPrep completes preparation once the operation is selected.
func (s *Scheduler) buildPrep(st *execState, d *ann.Dispatch, a *registry.Agent,
	op *registry.Operation, opName, did string) (*preparedDispatch, *Escalation, bool) {
	res, err := dispatch.ResolveFlags(a, op, opName, d)
	if err != nil {
		return s.predispatch(err, d, opName, did)
	}
	cb, err := dispatch.BuildContextBlock(op, opName, res.Data, st.ram)
	if err != nil {
		return s.predispatch(err, d, opName, did)
	}
	pre, post := dispatch.RenderDirectives(a, op, res)
	prompt := dispatch.AssemblePrompt(a, cb, pre, post, did)
	runDir, err := dispatch.MaterializeRunDir(s.MemDir, st.runID, did, prompt)
	if err != nil {
		return nil, escInternal(d, opName, did, err), false
	}
	spec, err := spawn.BuildRunSpec(a, op, filepath.Join(runDir, "prompt.md"),
		runDir, s.InvokerCwd, s.Consent, timeoutFlag(d), s.Cfg)
	if err != nil {
		return s.predispatch(err, d, opName, did)
	}
	return &preparedDispatch{
		did: did, runID: st.runID, d: d, a: a, opName: opName,
		prompt: prompt, runDir: runDir, spec: spec,
		timeoutSecs: int(spec.Timeout / time.Second),
	}, nil, false
}

// predispatch turns a pre-dispatch error into a Class A notice+skip or a
// Class B/C escalation.
func (s *Scheduler) predispatch(err error, d *ann.Dispatch, opName, did string) (*preparedDispatch, *Escalation, bool) {
	class, msg := classify(err)
	if class == 'A' {
		s.Notices = append(s.Notices, fmt.Sprintf("[class A] %s: %s", brackets(d.Command), msg))
		return nil, nil, true
	}
	return nil, escPredispatch(d, opName, did, msg, class), false
}

// classify extracts the class and message from a pre-dispatch error.
func classify(err error) (byte, string) {
	var pde *spawn.PreDispatchError
	if errors.As(err, &pde) {
		return pde.Class, pde.Msg
	}
	var dpe *dispatch.PreDispatchError
	if errors.As(err, &dpe) {
		return dpe.Class, dpe.Msg
	}
	return 'B', err.Error()
}

// timeoutFlag parses --timeout=N; absent → 0, non-integer → -1 (which
// BuildRunSpec rejects as Class A).
func timeoutFlag(d *ann.Dispatch) int {
	v, ok := d.Flags["timeout"]
	if !ok || v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

// invoke runs the prepared dispatch with a single corrective retry (R10).
func (s *Scheduler) invoke(prep *preparedDispatch, strict bool) (*envelope.Envelope, *Escalation) {
	env, viol, esc := s.spawnAttempt(prep, prep.prompt, strict, false)
	if esc != nil {
		return nil, esc
	}
	if viol == nil {
		return env, nil
	}
	retry := prep.prompt + correctiveSuffix(viol, prep.did, prep.a.Scope == "executor")
	env2, viol2, esc2 := s.spawnAttempt(prep, retry, strict, true)
	if esc2 != nil {
		return nil, esc2
	}
	if viol2 == nil {
		return env2, nil
	}
	if viol2.Kind == envelope.ViolationSchema {
		// Decision D: a schema mismatch that survives the corrective retry
		// surfaces as a catchable error envelope, not a Class B escalation.
		return envelope.SchemaError(prep.did, viol2.Msg), nil
	}
	return nil, escMalformed(prep, env2, viol2)
}

// validateSchema checks a success or info payload against the operation's
// declared output_schema (decision B). It returns a schema-kind Violation on
// mismatch, or nil; error envelopes are covered by structural check 5.
func (s *Scheduler) validateSchema(prep *preparedDispatch, env *envelope.Envelope) *envelope.Violation {
	op, ok := prep.a.Operations[prep.opName]
	if !ok {
		return nil
	}
	var schema *registry.PayloadSchema
	switch env.Status {
	case envelope.StatusSuccess:
		schema = op.SuccessSchema
	case envelope.StatusInfo:
		schema = op.InfoSchema
	default:
		return nil
	}
	if schema == nil {
		return nil
	}
	if reason := schema.Match(env.Payload, env.Status == envelope.StatusSuccess); reason != "" {
		return &envelope.Violation{Check: 6, Msg: reason, Kind: envelope.ViolationSchema}
	}
	return nil
}

// spawnAttempt performs one spawn: run, timeout synthesis (§4.3), extract
// and validate. A returned violation triggers the caller's retry.
func (s *Scheduler) spawnAttempt(prep *preparedDispatch, prompt string, strict, isRetry bool) (*envelope.Envelope, *envelope.Violation, *Escalation) {
	spec, err := s.specFor(prep, prompt, isRetry)
	if err != nil {
		return nil, nil, escInternal(prep.d, prep.opName, prep.did, err)
	}
	res, err := s.Sp.Run(context.Background(), spec)
	if err != nil {
		return nil, nil, escSpawn(prep, err)
	}
	if res.TimedOut {
		return envelope.Timeout(prep.did, prep.timeoutSecs), nil, nil
	}
	env, exErr := envelope.Extract(res.Stdout)
	if exErr != nil {
		return &envelope.Envelope{Raw: string(res.Stdout)}, &envelope.Violation{Msg: exErr.Error()}, nil
	}
	wantID := prep.did
	if !strict {
		wantID = env.ID
	}
	if v := envelope.Validate(env, wantID); v != nil {
		return env, v, nil
	}
	return env, s.validateSchema(prep, env), nil
}

// specFor returns the run spec for an attempt; a retry materializes a new
// run dir carrying the corrective prompt.
func (s *Scheduler) specFor(prep *preparedDispatch, prompt string, isRetry bool) (spawn.RunSpec, error) {
	if !isRetry {
		return prep.spec, nil
	}
	dir, err := dispatch.MaterializeRunDir(s.MemDir, prep.runID, prep.did+"-retry", prompt)
	if err != nil {
		return spawn.RunSpec{}, err
	}
	spec := prep.spec
	spec.PromptFile = filepath.Join(dir, "prompt.md")
	if prep.a.Scope != "executor" {
		spec.Cwd = dir
		return spec, nil
	}
	// R10 is a corrective retry (fix the envelope), not a re-execution. An
	// executor already applied its side effects on attempt 1; re-running its
	// write/execute tools would double-apply them and let the retry report
	// provenance for a workspace it did not itself mutate. Demote the retry to
	// read-only — physically unable to write or run commands — so it can only
	// inspect the workspace attempt 1 left behind and report it faithfully. The
	// cwd stays the invoker's workspace (unlike agnostic) so it sees those edits.
	spec.AllowedTools = spawn.WithoutSideEffects(spec.AllowedTools)
	spec.DisallowedTools = spawn.PlusSideEffects(spec.DisallowedTools)
	return spec, nil
}

// correctiveSuffix is the §2 retry instruction appended to the prompt. For an
// executor the retry runs read-only over the workspace attempt 1 already
// mutated, so it is told to report rather than redo.
func correctiveSuffix(v *envelope.Violation, did string, executor bool) string {
	extra := ""
	if executor {
		extra = "Un intento anterior de este MISMO dispatch ya se ejecutó y sus efectos en el workspace ya están aplicados; " +
			"en este reintento tienes acceso de SOLO LECTURA (no puedes escribir archivos ni ejecutar comandos). " +
			"NO rehagas el trabajo: inspecciona el estado actual del workspace y reporta lo que ya se logró (p. ej. los archivos que ahora difieren). "
	}
	return fmt.Sprintf("\n\n---\nTu retorno anterior violó el protocolo: %s. %s"+
		"Devuelve ÚNICAMENTE el envelope YAML {id, status, payload} con id=%s.", v.Msg, extra, did)
}
