package ann

import "fmt"

// Category classifies a parse error per spec §7.1.
type Category int

// Parse error categories (§7.1).
const (
	Syntax Category = iota
	UnknownCommand
	Type
	VersionMismatch
)

// String returns the human-readable category name.
func (c Category) String() string {
	switch c {
	case Syntax:
		return "syntax error"
	case UnknownCommand:
		return "unknown command"
	case Type:
		return "type error"
	case VersionMismatch:
		return "version mismatch"
	default:
		return "unknown category"
	}
}

// ParseError is the single error type returned by Parse and
// ValidateCommands. Line and Col are 1-based.
type ParseError struct {
	Line, Col int
	Category  Category
	Msg       string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s: %s", e.Line, e.Col, e.Category, e.Msg)
}

// Class maps the error category to its escalation class per §7.3:
// Type errors (including loop limit ≤ 0) are Class A; Syntax,
// UnknownCommand and VersionMismatch are Class B.
func (e *ParseError) Class() byte {
	if e.Category == Type {
		return 'A'
	}
	return 'B'
}

// perrf builds a ParseError at an explicit position.
func perrf(line, col int, cat Category, format string, args ...any) *ParseError {
	return &ParseError{Line: line, Col: col, Category: cat, Msg: fmt.Sprintf(format, args...)}
}

// errAt builds a ParseError at a token's position.
func errAt(t token, cat Category, format string, args ...any) *ParseError {
	return perrf(t.line, t.col, cat, format, args...)
}

// builtinCommands are always registered regardless of the registry (§3):
// they may appear as Dispatch commands and are always known.
var builtinCommands = map[string]bool{
	"ask-user": true,
	"notify":   true,
	"clarify":  true,
}

// ValidateCommands walks the AST and checks that every Dispatch.Command
// satisfies known. Built-ins ask-user, notify and clarify are always
// accepted. Returns the first unknown command with its line (§3, §7.1).
func (p *Program) ValidateCommands(known func(name string) bool) *ParseError {
	return validateStmts(p.Statements, known)
}

func validateStmts(stmts []Stmt, known func(string) bool) *ParseError {
	for _, s := range stmts {
		if err := validateStmt(s, known); err != nil {
			return err
		}
	}
	return nil
}

func validateStmt(s Stmt, known func(string) bool) *ParseError {
	switch st := s.(type) {
	case *Dispatch:
		return validateDispatch(st, known)
	case *Assign:
		if d, ok := st.Expr.(*Dispatch); ok {
			return validateDispatch(d, known)
		}
	case *Parallel:
		for i := range st.Dispatches {
			if err := validateDispatch(&st.Dispatches[i], known); err != nil {
				return err
			}
		}
		return validateStmts(st.Each, known)
	case *Foreach:
		return validateStmts(st.Body, known)
	case *Loop:
		return validateStmts(st.Body, known)
	case *If:
		if err := validateStmts(st.Then, known); err != nil {
			return err
		}
		return validateStmts(st.Else, known)
	}
	return nil
}

func validateDispatch(d *Dispatch, known func(string) bool) *ParseError {
	if !builtinCommands[d.Command] && !known(d.Command) {
		return &ParseError{
			Line:     d.Line,
			Col:      1,
			Category: UnknownCommand,
			Msg:      fmt.Sprintf("unknown command [%s]", d.Command),
		}
	}
	for _, status := range []Status{StatusSuccess, StatusError, StatusInfo} {
		if body, ok := d.Handlers[status]; ok {
			if err := validateStmts(body, known); err != nil {
				return err
			}
		}
	}
	return nil
}
