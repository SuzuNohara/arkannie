// Package ann implements the lexer and recursive-descent parser for the
// Ann v0.1 command language (spec/ann-lang.md §1–§3, §7, §8).
//
// The package is pure: no I/O, no external dependencies. Parse compiles a
// source buffer into an AST and stops on the first error (§7.2).
package ann

import "strings"

// Program is the root of a parsed Ann source.
type Program struct{ Statements []Stmt }

// Stmt is the sealed statement interface, implemented by *Dispatch,
// *Assign, *Parallel, *Foreach and *Loop.
type Stmt interface{ stmt() }

// Expr is the sealed expression interface for binding right-hand sides,
// implemented by *Dispatch, StrLit and ListLit.
type Expr interface{ expr() }

// Status identifies a trinary handler key (§2.2).
type Status string

// Trinary handler statuses.
const (
	StatusSuccess Status = "success"
	StatusError   Status = "error"
	StatusInfo    Status = "info"
)

// Dispatch is a command atom (§2.1), optionally with context text (§2.7)
// and trinary handlers (§2.2).
type Dispatch struct {
	Command  string            // "seeker" (without brackets)
	Args     []string          // positional args; $refs keep their $ prefix
	Flags    map[string]string // boolean flag → ""
	Context  string            // verbatim text after ": " (multi-line joined with \n)
	ID       string            // value of --id ("" when absent)
	Handlers map[Status][]Stmt // keys: success | error | info
	Line     int
}

// Assign binds the result of Expr to a RAM name (§2.3).
type Assign struct {
	Name string
	Expr Expr
	Line int
}

// Parallel is a concurrent dispatch block with an optional each handler (§2.4, §6).
type Parallel struct {
	Dispatches []Dispatch
	Each       []Stmt
	Line       int
}

// Foreach iterates sequentially over a list binding (§2.4). List holds the
// binding name without the $ prefix.
type Foreach struct {
	List string
	Body []Stmt
	Line int
}

// Loop executes Body up to Limit times (§2.4, §6.7).
type Loop struct {
	Limit int
	Body  []Stmt
	Line  int
}

// StrLit is a string literal expression. Template slots ({{ }}) and $refs
// are kept verbatim — the parser never resolves templates (§2.5).
type StrLit struct{ Value string }

// ListLit is a list() constructor (§2.6). Elements are literal strings or
// $refs (kept with their $ prefix).
type ListLit struct{ Elems []string }

// keywords are reserved per §1.3 and cannot be used as binding names.
var keywords = map[string]bool{
	"parallel": true, "foreach": true, "loop": true,
	"success": true, "error": true, "info": true,
	"each": true, "limit": true,
	"ask-user": true, "notify": true, "clarify": true, "null": true,
	"return": true,
}

// addFlag records a lexed flag ("name" or "name=value"); --id mirrors to ID.
func (d *Dispatch) addFlag(text string) {
	name, value := text, ""
	if i := strings.IndexByte(text, '='); i >= 0 {
		name, value = text[:i], text[i+1:]
	}
	if d.Flags == nil {
		d.Flags = map[string]string{}
	}
	d.Flags[name] = value
	if name == "id" {
		d.ID = value
	}
}

func (*Dispatch) stmt() {}
func (*Assign) stmt()   {}
func (*Parallel) stmt() {}
func (*Foreach) stmt()  {}
func (*Loop) stmt()     {}

func (*Dispatch) expr() {}
func (StrLit) expr()    {}
func (ListLit) expr()   {}
