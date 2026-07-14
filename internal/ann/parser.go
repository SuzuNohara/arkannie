package ann

import (
	"strconv"
	"strings"
)

// Mode selects the parsing mode (§1.0).
type Mode int

const (
	// ProgramMode requires the version header on the first non-comment line.
	ProgramMode Mode = iota
	// PromptMode is interactive: the version header is optional and ignored.
	PromptMode
)

type parser struct {
	lines []string
	pos   int // index of the next line to read (0-based)
}

// Parse compiles Ann source into a Program. It is stop-on-first-error
// (§7.2): on error it returns (nil, err) and nothing partial.
func Parse(src []byte, mode Mode) (*Program, *ParseError) {
	normalized := strings.ReplaceAll(string(src), "\r\n", "\n")
	p := &parser{lines: strings.Split(normalized, "\n")}
	start, err := checkHeader(p.lines, mode)
	if err != nil {
		return nil, err
	}
	p.pos = start
	stmts, err := p.parseBlock(0, false)
	if err != nil {
		return nil, err
	}
	if err := validateReturns(stmts); err != nil {
		return nil, err
	}
	return &Program{Statements: stmts}, nil
}

// retInfo records a [return] statement and whether it sits inside an
// iteration body (foreach/loop/each), where each run emits its own section.
type retInfo struct {
	d      *Dispatch
	inLoop bool
}

// validateReturns enforces the [return] output-indicator rules: a return
// inside a loop needs --id (its sections are numbered per run); if the
// program has two or more returns every one needs --id; and all --id values
// must be unique. A single non-loop return may omit --id (unlabeled section).
func validateReturns(stmts []Stmt) *ParseError {
	var rets []retInfo
	collectReturns(stmts, false, &rets)
	multiple := len(rets) >= 2
	seen := map[string]int{}
	for _, r := range rets {
		if r.d.ID == "" {
			if r.inLoop {
				return perrf(r.d.Line, 1, Syntax, "[return] inside a loop requires --id")
			}
			if multiple {
				return perrf(r.d.Line, 1, Syntax, "[return] requires --id when the program has multiple returns")
			}
			continue
		}
		if prev, dup := seen[r.d.ID]; dup {
			return perrf(r.d.Line, 1, Syntax, "duplicate [return] --id=%s (first used at line %d)", r.d.ID, prev)
		}
		seen[r.d.ID] = r.d.Line
	}
	return nil
}

// collectReturns walks stmts recording every [return], propagating inLoop
// through foreach/loop bodies and parallel each handlers.
func collectReturns(stmts []Stmt, inLoop bool, out *[]retInfo) {
	for _, st := range stmts {
		switch v := st.(type) {
		case *Dispatch:
			if v.Command == "return" {
				*out = append(*out, retInfo{d: v, inLoop: inLoop})
			}
			for _, body := range v.Handlers {
				collectReturns(body, inLoop, out)
			}
		case *Assign:
			if d, ok := v.Expr.(*Dispatch); ok {
				for _, body := range d.Handlers {
					collectReturns(body, inLoop, out)
				}
			}
		case *Parallel:
			collectReturns(v.Each, true, out)
		case *Foreach:
			collectReturns(v.Body, true, out)
		case *Loop:
			collectReturns(v.Body, true, out)
		case *If:
			collectReturns(v.Then, inLoop, out)
			collectReturns(v.Else, inLoop, out)
		}
	}
}

// parseBlock parses statements until the closing '}' (nested) or EOF.
func (p *parser) parseBlock(openLine int, nested bool) ([]Stmt, *ParseError) {
	stmts := []Stmt{}
	for p.pos < len(p.lines) {
		toks, err := p.lexNext()
		if err != nil {
			return nil, err
		}
		if toks == nil {
			continue // blank or comment-only line
		}
		if toks[0].kind == tkRBrace {
			if !nested {
				return nil, errAt(toks[0], Syntax, "unexpected '}' outside a block")
			}
			return stmts, nil
		}
		st, err := p.parseStatement(toks)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, st)
	}
	if nested {
		return nil, perrf(openLine, 1, Syntax, "unclosed block opened at line %d", openLine)
	}
	return stmts, nil
}

// lexNext tokenizes the current line and advances; (nil, nil) for blank or
// comment-only lines.
func (p *parser) lexNext() ([]token, *ParseError) {
	toks, err := lexLine(p.lines[p.pos], p.pos+1)
	if err != nil {
		return nil, err
	}
	p.pos++
	if len(toks) == 0 {
		return nil, nil
	}
	return toks, nil
}

func (p *parser) parseStatement(toks []token) (Stmt, *ParseError) {
	t := toks[0]
	switch {
	case t.kind == tkCommand && (t.text == "if" || t.text == "while"):
		return nil, errTrinary(t, "["+t.text+"]")
	case t.kind == tkCommand:
		d, err := p.parseDispatch(toks)
		if err != nil {
			return nil, err
		}
		if err := p.parseHandlers(d); err != nil {
			return nil, err
		}
		return d, nil
	case t.kind == tkBinding:
		return p.parseAssign(toks)
	case t.kind == tkIdent:
		return p.parseKeywordStmt(toks)
	default:
		return nil, errAt(t, Syntax, "unexpected token at start of statement")
	}
}

func (p *parser) parseKeywordStmt(toks []token) (Stmt, *ParseError) {
	t := toks[0]
	switch t.text {
	case "parallel":
		return p.parseParallel(toks)
	case "foreach":
		return p.parseForeach(toks)
	case "loop":
		return p.parseLoop(toks)
	case "if":
		return p.parseIf(toks)
	case "while":
		return nil, errTrinary(t, t.text)
	case "else":
		return nil, errAt(t, Syntax, "else without a matching if")
	case "success", "error", "info", "each":
		return nil, errAt(t, Syntax, "%s handler without a preceding dispatch", t.text)
	default:
		return nil, errAt(t, Syntax, "unexpected token %q", t.text)
	}
}

// errTrinary rejects unsupported conditionals (§8).
func errTrinary(t token, form string) *ParseError {
	return errAt(t, Syntax, "%s is not supported in Ann v0.2 — use trinary handlers", form)
}

// parseDispatch parses a command atom (§2.1): args, flags and context.
func (p *parser) parseDispatch(toks []token) (*Dispatch, *ParseError) {
	d := &Dispatch{Command: toks[0].text, Line: toks[0].line}
	for i := 1; i < len(toks); {
		t := toks[i]
		switch t.kind {
		case tkIdent, tkString:
			d.Args = append(d.Args, t.text)
			i++
		case tkBinding:
			var path string
			path, i = refPath(toks, i)
			d.Args = append(d.Args, "$"+path)
		case tkFlag:
			d.addFlag(t.text)
			i++
		case tkContext:
			d.Context = t.text
			i++
		case tkContextOpen:
			d.Context, p.pos = collectContext(p.lines, p.pos)
			i++
		default:
			return nil, errAt(t, Syntax, "unexpected token in dispatch")
		}
	}
	return d, nil
}

// parseHandlers attaches 0–3 trinary handlers to a dispatch (§2.2).
func (p *parser) parseHandlers(d *Dispatch) *ParseError {
	for {
		toks, ok, err := p.nextHandlerLine()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		status := Status(toks[0].text)
		if _, dup := d.Handlers[status]; dup {
			return errAt(toks[0], Syntax, "duplicate %s handler", status)
		}
		body, err := p.parseHandlerBody(toks)
		if err != nil {
			return err
		}
		if d.Handlers == nil {
			d.Handlers = map[Status][]Stmt{}
		}
		d.Handlers[status] = body
	}
}

// nextHandlerLine consumes and returns the next line when it starts a
// success/error/info handler; otherwise it consumes nothing.
func (p *parser) nextHandlerLine() ([]token, bool, *ParseError) {
	i := nextContent(p.lines, p.pos)
	if i >= len(p.lines) || !isHandlerLine(p.lines[i], "success", "error", "info") {
		return nil, false, nil
	}
	toks, err := lexLine(p.lines[i], i+1)
	if err != nil {
		return nil, false, err
	}
	p.pos = i + 1
	return toks, true, nil
}

// parseHandlerBody parses "name -> {" (body until '}') or "name -> {}".
func (p *parser) parseHandlerBody(toks []token) ([]Stmt, *ParseError) {
	if len(toks) < 3 || toks[1].kind != tkArrow || toks[2].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "%s handler must be followed by '-> {'", toks[0].text)
	}
	if len(toks) == 4 && toks[3].kind == tkRBrace {
		return []Stmt{}, nil
	}
	if len(toks) > 3 {
		return nil, errAt(toks[3], Syntax, "unexpected tokens after '{'")
	}
	return p.parseBlock(toks[0].line, true)
}

// parseAssign parses a binding assignment (§2.3). Names are alphanumeric
// plus '_' (enforced by the lexer); keywords are rejected here.
func (p *parser) parseAssign(toks []token) (Stmt, *ParseError) {
	name := toks[0]
	if keywords[name.text] {
		return nil, errAt(name, Syntax, "binding name $%s is a reserved keyword", name.text)
	}
	if len(toks) < 2 || toks[1].kind != tkAssign {
		return nil, errAt(name, Syntax, "invalid binding name or missing '=' in assignment")
	}
	if len(toks) < 3 {
		return nil, errAt(name, Syntax, "missing expression after '='")
	}
	expr, err := p.parseExpr(toks[2:])
	if err != nil {
		return nil, err
	}
	return &Assign{Name: name.text, Expr: expr, Line: name.line}, nil
}

// parseExpr parses a binding right-hand side: dispatch, string or list().
func (p *parser) parseExpr(toks []token) (Expr, *ParseError) {
	switch toks[0].kind {
	case tkCommand:
		d, err := p.parseDispatch(toks)
		if err != nil {
			return nil, err
		}
		if err := p.parseHandlers(d); err != nil {
			return nil, err
		}
		return d, nil
	case tkString:
		if len(toks) > 1 {
			return nil, errAt(toks[1], Syntax, "unexpected tokens after string literal")
		}
		return StrLit{Value: toks[0].text}, nil
	case tkListOpen:
		return parseList(toks)
	case tkConcatOpen:
		return parseConcat(toks)
	default:
		return nil, errAt(toks[0], Syntax, "expected [command], string literal, list() or concat() after '='")
	}
}

// parseList parses a top-level list(...) expression and rejects trailing
// tokens. Elements are strings, $refs (with optional dot-path) or nested
// list() constructors (§2.6, v0.3).
func parseList(toks []token) (Expr, *ParseError) {
	ll, next, err := parseListAt(toks, 0)
	if err != nil {
		return nil, err
	}
	if next != len(toks) {
		return nil, errAt(toks[next], Syntax, "unexpected tokens after list()")
	}
	return ll, nil
}

// parseConcat parses a top-level concat(...) expression and rejects trailing
// tokens. Arguments share the element grammar of list(); concat() with no args
// and a single argument are both valid (§2.6, v0.3).
func parseConcat(toks []token) (Expr, *ParseError) {
	args, next, err := parseElems(toks, 1, toks[0], "concat")
	if err != nil {
		return nil, err
	}
	if next != len(toks) {
		return nil, errAt(toks[next], Syntax, "unexpected tokens after concat()")
	}
	return &Concat{Args: args, Line: toks[0].line}, nil
}

// parseListAt parses a list(...) starting at toks[i] (a tkListOpen). It returns
// the ListLit and the index just past the closing ')'.
func parseListAt(toks []token, i int) (ListLit, int, *ParseError) {
	open := toks[i]
	elems, next, err := parseElems(toks, i+1, open, "list")
	if err != nil {
		return ListLit{}, 0, err
	}
	return ListLit{Elems: elems, Line: open.line}, next, nil
}

// parseElems parses comma-separated elements from toks[i] until the closing
// ')'. opener anchors the "unclosed" error and name labels it. It returns the
// elements and the index just past ')'.
func parseElems(toks []token, i int, opener token, name string) ([]Elem, int, *ParseError) {
	elems := []Elem{}
	for i < len(toks) && toks[i].kind != tkRParen {
		e, next, err := parseElem(toks, i)
		if err != nil {
			return nil, 0, err
		}
		elems = append(elems, e)
		i = next
		if i < len(toks) && toks[i].kind == tkComma {
			i++
		}
	}
	if i >= len(toks) {
		return nil, 0, errAt(opener, Syntax, "unclosed %s()", name)
	}
	return elems, i + 1, nil
}

// parseElem parses one list/concat element at toks[i]: a string literal, a $ref
// (with optional dot-path), or a nested list() constructor. It returns the
// element and the index just past it.
func parseElem(toks []token, i int) (Elem, int, *ParseError) {
	switch toks[i].kind {
	case tkString:
		return Elem{Str: toks[i].text}, i + 1, nil
	case tkBinding:
		path, next := refPath(toks, i)
		return Elem{IsRef: true, Str: path}, next, nil
	case tkListOpen:
		ll, next, err := parseListAt(toks, i)
		if err != nil {
			return Elem{}, 0, err
		}
		return Elem{List: &ll}, next, nil
	default:
		return Elem{}, 0, errAt(toks[i], Syntax,
			"list elements must be string literals, $bindings or nested list()")
	}
}

// parseParallel parses "parallel { dispatches } [each -> { ... }]" (§6.1).
func (p *parser) parseParallel(toks []token) (Stmt, *ParseError) {
	if len(toks) != 2 || toks[1].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "parallel must be followed by '{'")
	}
	par := &Parallel{Line: toks[0].line}
	if err := p.parseParallelBody(par); err != nil {
		return nil, err
	}
	if err := checkParallelIDs(par); err != nil {
		return nil, err
	}
	if err := p.parseEach(par); err != nil {
		return nil, err
	}
	return par, nil
}

// parseParallelBody accepts only dispatch atoms; nesting is a Syntax error (§8).
func (p *parser) parseParallelBody(par *Parallel) *ParseError {
	for p.pos < len(p.lines) {
		toks, err := p.lexNext()
		if err != nil {
			return err
		}
		if toks == nil {
			continue
		}
		switch {
		case toks[0].kind == tkRBrace:
			return nil
		case toks[0].kind == tkIdent && toks[0].text == "parallel":
			return errAt(toks[0], Syntax, "nested parallel blocks are not supported in Ann v0.2")
		case toks[0].kind == tkCommand:
			d, err := p.parseDispatch(toks)
			if err != nil {
				return err
			}
			par.Dispatches = append(par.Dispatches, *d)
		default:
			return errAt(toks[0], Syntax, "only [command] dispatches are allowed inside parallel {}")
		}
	}
	return perrf(par.Line, 1, Syntax, "unclosed block opened at line %d", par.Line)
}

// checkParallelIDs enforces §6.1: --id required and unique per block.
func checkParallelIDs(par *Parallel) *ParseError {
	seen := make(map[string]int, len(par.Dispatches))
	for i := range par.Dispatches {
		d := &par.Dispatches[i]
		if d.ID == "" {
			return perrf(d.Line, 1, Syntax, "dispatch [%s] inside parallel {} requires --id", d.Command)
		}
		if prev, dup := seen[d.ID]; dup {
			return perrf(d.Line, 1, Syntax,
				"duplicate --id=%s in parallel {} (first used at line %d)", d.ID, prev)
		}
		seen[d.ID] = d.Line
	}
	return nil
}

// parseEach attaches the optional "each -> { ... }" handler (§6.2).
func (p *parser) parseEach(par *Parallel) *ParseError {
	i := nextContent(p.lines, p.pos)
	if i >= len(p.lines) || !isHandlerLine(p.lines[i], "each") {
		return nil
	}
	toks, err := lexLine(p.lines[i], i+1)
	if err != nil {
		return err
	}
	p.pos = i + 1
	body, err := p.parseHandlerBody(toks)
	if err != nil {
		return err
	}
	par.Each = body
	return nil
}

// parseForeach parses "foreach $list { body }" (§6.6).
func (p *parser) parseForeach(toks []token) (Stmt, *ParseError) {
	if len(toks) < 3 || toks[1].kind != tkBinding {
		return nil, errAt(toks[0], Syntax, "foreach must be 'foreach $list {'")
	}
	path, next := refPath(toks, 1)
	if next != len(toks)-1 || toks[next].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "foreach must be 'foreach $list {'")
	}
	body, err := p.parseBlock(toks[0].line, true)
	if err != nil {
		return nil, err
	}
	return &Foreach{List: path, Body: body, Line: toks[0].line}, nil
}

// parseLoop parses "loop limit=N [until Guard] { body }"; non-integer or
// N ≤ 0 is a Type error, Class A (§6.7, §7.3). The optional until clause is a
// post-condition guard between the limit and the '{' (§8); it is reserved only
// in this header position (R7) — elsewhere "until" stays free text.
func (p *parser) parseLoop(toks []token) (Stmt, *ParseError) {
	if len(toks) < 5 || toks[1].kind != tkIdent || toks[1].text != "limit" ||
		toks[2].kind != tkAssign || toks[3].kind != tkIdent ||
		toks[len(toks)-1].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "loop must be 'loop limit=N [until <guard>] {'")
	}
	n, convErr := strconv.Atoi(toks[3].text)
	if convErr != nil {
		return nil, errAt(toks[3], Type, "loop limit must be an integer, got %q", toks[3].text)
	}
	if n <= 0 {
		return nil, errAt(toks[3], Type, "loop limit must be a positive integer, got %d", n)
	}
	until, err := parseLoopUntil(toks)
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock(toks[0].line, true)
	if err != nil {
		return nil, err
	}
	return &Loop{Limit: n, Until: until, Body: body, Line: toks[0].line}, nil
}

// parseLoopUntil parses the optional "until Guard" clause sitting between the
// limit and the trailing '{'. It returns nil when the clause is absent.
func parseLoopUntil(toks []token) (*Guard, *ParseError) {
	mid := toks[4 : len(toks)-1]
	if len(mid) == 0 {
		return nil, nil
	}
	if mid[0].kind != tkIdent || mid[0].text != "until" {
		return nil, errAt(mid[0], Syntax, "loop header allows only 'until <guard>' before '{'")
	}
	if len(mid) == 1 {
		return nil, errAt(mid[0], Syntax, "until requires a guard condition")
	}
	g, err := parseGuard(mid[1:], mid[0])
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// parseIf parses "if Operand (==|!=) Operand {" plus its Then block and an
// optional "else {" block (§8). Guards are deterministic: no compound
// operators or arithmetic. Malformed guards are Syntax errors with the token's
// line:column. The else block, when present, is on its own line.
func (p *parser) parseIf(toks []token) (Stmt, *ParseError) {
	if toks[len(toks)-1].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "if condition must end with '{'")
	}
	g, err := parseGuard(toks[1:len(toks)-1], toks[0])
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock(toks[0].line, true)
	if err != nil {
		return nil, err
	}
	els, err := p.parseElse()
	if err != nil {
		return nil, err
	}
	return &If{Left: g.Left, Op: g.Op, Right: g.Right, Then: then, Else: els, Line: toks[0].line}, nil
}

// parseGuard parses a deterministic comparison `Operand (==|!=) Operand` from
// the tokens between a header keyword and its '{'. It is shared by if
// conditions and loop until post-conditions (§8). at anchors the position of
// errors reported before the operator is found.
func parseGuard(toks []token, at token) (Guard, *ParseError) {
	opIdx := -1
	for i := 0; i < len(toks); i++ {
		if toks[i].kind == tkEq || toks[i].kind == tkNe {
			opIdx = i
			break
		}
	}
	if opIdx < 0 {
		return Guard{}, errAt(at, Syntax, "condition requires '==' or '!='")
	}
	left, err := parseOperand(toks[:opIdx], at)
	if err != nil {
		return Guard{}, err
	}
	right, err := parseOperand(toks[opIdx+1:], toks[opIdx])
	if err != nil {
		return Guard{}, err
	}
	op := "=="
	if toks[opIdx].kind == tkNe {
		op = "!="
	}
	return Guard{Left: left, Op: op, Right: right}, nil
}

// parseOperand builds an Operand from the tokens between keywords/operators:
// a $ref path (binding plus optional dotted idents), a string literal, or
// null (§8). Anything else is a Syntax error.
func parseOperand(toks []token, at token) (Operand, *ParseError) {
	if len(toks) == 0 {
		return Operand{}, errAt(at, Syntax, "missing operand in if condition")
	}
	first := toks[0]
	switch first.kind {
	case tkString:
		if len(toks) != 1 {
			return Operand{}, errAt(toks[1], Syntax, "unexpected token after string operand")
		}
		return Operand{Text: first.text}, nil
	case tkBinding:
		path, next := refPath(toks, 0)
		if next != len(toks) {
			return Operand{}, errAt(toks[next], Syntax, "invalid reference path in if operand")
		}
		return Operand{IsRef: true, Text: path}, nil
	case tkIdent:
		if first.text == "null" && len(toks) == 1 {
			return Operand{IsNull: true}, nil
		}
		return Operand{}, errAt(first, Syntax, "invalid operand %q; expected $ref, string or null", first.text)
	default:
		return Operand{}, errAt(first, Syntax, "invalid operand in if condition")
	}
}

// refPath reconstructs a $ref path from the $binding token at toks[i] plus any
// immediately following dotted-ident tokens. The lexer splits `$x.a.b` into
// [$x, .a, .b] (it stops the binding at '.'), so every position that admits a
// reference rejoins them into a single path ("x.a.b", without the $). It
// returns the path and the index just past the last token consumed.
func refPath(toks []token, i int) (string, int) {
	path := toks[i].text
	i++
	for i < len(toks) && toks[i].kind == tkIdent && strings.HasPrefix(toks[i].text, ".") {
		path += toks[i].text
		i++
	}
	return path, i
}

// parseElse consumes and parses an "else {" block when the next content line
// starts with the else keyword; otherwise it consumes nothing and returns nil.
func (p *parser) parseElse() ([]Stmt, *ParseError) {
	i := nextContent(p.lines, p.pos)
	if i >= len(p.lines) || !isElseLine(p.lines[i]) {
		return nil, nil
	}
	toks, err := lexLine(p.lines[i], i+1)
	if err != nil {
		return nil, err
	}
	p.pos = i + 1
	if len(toks) != 2 || toks[1].kind != tkLBrace {
		return nil, errAt(toks[0], Syntax, "else must be written as 'else {' on its own line")
	}
	return p.parseBlock(toks[0].line, true)
}
