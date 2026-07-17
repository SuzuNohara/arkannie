package ann

import (
	"fmt"
	"sort"
	"strings"
)

// dumpProgram renders a deterministic textual form of the AST for golden
// comparison. Map-backed fields (flags, handlers) print in sorted/fixed order.
func dumpProgram(p *Program) string {
	var b strings.Builder
	b.WriteString("Program\n")
	dumpStmts(&b, p.Statements, 1)
	return b.String()
}

func dumpStmts(b *strings.Builder, stmts []Stmt, depth int) {
	for _, s := range stmts {
		dumpStmt(b, s, depth)
	}
}

func writeIndent(b *strings.Builder, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
}

func dumpStmt(b *strings.Builder, s Stmt, depth int) {
	switch st := s.(type) {
	case *Dispatch:
		dumpDispatch(b, st, depth)
	case *Assign:
		writeIndent(b, depth)
		fmt.Fprintf(b, "Assign line=%d name=%s\n", st.Line, st.Name)
		dumpExpr(b, st.Expr, depth+1)
	case *Parallel:
		dumpParallel(b, st, depth)
	case *ParallelForeach:
		dumpParallelForeach(b, st, depth)
	case *Foreach:
		writeIndent(b, depth)
		fmt.Fprintf(b, "Foreach line=%d list=$%s\n", st.Line, st.List)
		dumpStmts(b, st.Body, depth+1)
	case *Loop:
		writeIndent(b, depth)
		fmt.Fprintf(b, "Loop line=%d limit=%d", st.Line, st.Limit)
		if st.Until != nil {
			fmt.Fprintf(b, " until op=%s left=%s right=%s",
				st.Until.Op, dumpOperand(st.Until.Left), dumpOperand(st.Until.Right))
		}
		b.WriteString("\n")
		dumpStmts(b, st.Body, depth+1)
	case *If:
		dumpIf(b, st, depth)
	case *Call:
		writeIndent(b, depth)
		fmt.Fprintf(b, "Call line=%d path=%q\n", st.Line, st.Path)
	}
}

func dumpIf(b *strings.Builder, st *If, depth int) {
	writeIndent(b, depth)
	fmt.Fprintf(b, "If line=%d op=%s left=%s right=%s\n",
		st.Line, st.Op, dumpOperand(st.Left), dumpOperand(st.Right))
	writeIndent(b, depth+1)
	b.WriteString("Then\n")
	dumpStmts(b, st.Then, depth+2)
	if st.Else != nil {
		writeIndent(b, depth+1)
		b.WriteString("Else\n")
		dumpStmts(b, st.Else, depth+2)
	}
}

func dumpOperand(o Operand) string {
	switch {
	case o.IsNull:
		return "null"
	case o.IsRef:
		return "$" + o.Text
	default:
		return fmt.Sprintf("%q", o.Text)
	}
}

func dumpParallel(b *strings.Builder, st *Parallel, depth int) {
	writeIndent(b, depth)
	fmt.Fprintf(b, "Parallel line=%d\n", st.Line)
	for i := range st.Dispatches {
		dumpDispatch(b, &st.Dispatches[i], depth+1)
	}
	if st.Each != nil {
		writeIndent(b, depth+1)
		b.WriteString("Each\n")
		dumpStmts(b, st.Each, depth+2)
	}
}

func dumpParallelForeach(b *strings.Builder, st *ParallelForeach, depth int) {
	writeIndent(b, depth)
	fmt.Fprintf(b, "ParallelForeach line=%d list=$%s base=%s\n", st.Line, st.List, st.BaseID)
	dumpDispatch(b, &st.Template, depth+1)
	if st.Each != nil {
		writeIndent(b, depth+1)
		b.WriteString("Each\n")
		dumpStmts(b, st.Each, depth+2)
	}
}

func dumpDispatch(b *strings.Builder, d *Dispatch, depth int) {
	writeIndent(b, depth)
	fmt.Fprintf(b, "Dispatch line=%d cmd=%s args=%q id=%q ctx=%q flags=%s\n",
		d.Line, d.Command, d.Args, d.ID, d.Context, dumpFlags(d.Flags))
	for _, status := range []Status{StatusSuccess, StatusError, StatusInfo} {
		body, ok := d.Handlers[status]
		if !ok {
			continue
		}
		writeIndent(b, depth+1)
		fmt.Fprintf(b, "Handler %s\n", status)
		dumpStmts(b, body, depth+2)
	}
}

func dumpFlags(flags map[string]string) string {
	if len(flags) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(flags))
	for k := range flags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, flags[k]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func dumpExpr(b *strings.Builder, e Expr, depth int) {
	switch ex := e.(type) {
	case *Dispatch:
		dumpDispatch(b, ex, depth)
	case StrLit:
		writeIndent(b, depth)
		fmt.Fprintf(b, "StrLit %q\n", ex.Value)
	case ListLit:
		writeIndent(b, depth)
		parts := make([]string, len(ex.Elems))
		for i, el := range ex.Elems {
			parts[i] = elemSrc(el)
		}
		fmt.Fprintf(b, "ListLit %q\n", parts)
	case MapLit:
		writeIndent(b, depth)
		parts := make([]string, len(ex.Entries))
		for i, ent := range ex.Entries {
			parts[i] = ent.Key + ": " + elemSrc(ent.Val)
		}
		fmt.Fprintf(b, "MapLit %q\n", parts)
	case *Concat:
		writeIndent(b, depth)
		parts := make([]string, len(ex.Args))
		for i, el := range ex.Args {
			parts[i] = elemSrc(el)
		}
		fmt.Fprintf(b, "Concat %q\n", parts)
	case *Call:
		writeIndent(b, depth)
		fmt.Fprintf(b, "Call path=%q\n", ex.Path)
	}
}
