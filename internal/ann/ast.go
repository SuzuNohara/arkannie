// Package ann implements the lexer and recursive-descent parser for the
// Ann v0.2 command language (spec/ann-lang.md §1–§3, §7, §8).
//
// The package is pure: no I/O, no external dependencies. Parse compiles a
// source buffer into an AST and stops on the first error (§7.2).
package ann

// Program is the root of a parsed Ann source.
type Program struct{ Statements []Stmt }

// Stmt is the sealed statement interface, implemented by *Dispatch,
// *Assign, *Parallel, *Foreach, *Loop and *If.
type Stmt interface{ stmt() }

// Expr is the sealed expression interface for binding right-hand sides,
// implemented by *Dispatch, StrLit, ListLit, *Concat and MapLit.
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

// ParallelForeach is the dynamic fan-out form `parallel foreach $list --id=<base>
// { <template> } [each -> {...}]` (§6, R9/R11). One copy of Template runs per list
// item, concurrently and bounded by MaxConcurrency, with a synthetic id
// `<base>-<n>` per item; Each, when present, runs once per item in index order.
// List holds the list ref path without the $ prefix (dot-path preserved); BaseID
// is the reserved id prefix; Template carries no --id of its own (the runtime
// synthesizes it).
type ParallelForeach struct {
	List     string
	BaseID   string
	Template Dispatch
	Each     []Stmt
	Line     int
}

// Loop executes Body up to Limit times (§2.4, §6.7). Until, when non-nil,
// is a post-condition guard: iteration stops early once it holds (§8). The
// guard is parsed here; its evaluation belongs to a later execution stage.
type Loop struct {
	Limit int
	Until *Guard
	Body  []Stmt
	Line  int
}

// Guard is a deterministic comparison `Left Op Right` shared by If conditions
// and loop `until` post-conditions (§8). Op is "==" or "!="; compound
// operators and arithmetic are out of scope in Ann v0.2.
type Guard struct {
	Left  Operand
	Op    string
	Right Operand
}

// Operand is one side of an If comparison (§8). It is exactly one of: a $ref
// path (IsRef, Text holds the path without the $ prefix, e.g. "x.status"), the
// null literal (IsNull), or a string literal (Text holds the verbatim value).
type Operand struct {
	IsRef  bool
	IsNull bool
	Text   string
}

// If is a deterministic conditional statement (§8): it runs Then when
// Left Op Right holds and Else otherwise. Op is "==" or "!="; compound
// operators and arithmetic are out of scope in Ann v0.2. Else is nil when no
// else block is present.
type If struct {
	Left  Operand
	Op    string
	Right Operand
	Then  []Stmt
	Else  []Stmt
	Line  int
}

// StrLit is a string literal expression. Template slots ({{ }}) and $refs
// are kept verbatim — the parser never resolves templates (§2.5).
type StrLit struct{ Value string }

// Elem is one element of a list() or concat() constructor (§2.6, v0.3). It is
// exactly one of: a $ref path (IsRef, Str holds the path without the $ prefix,
// e.g. "x.a"), a nested list() (List), a nested map() (Map), or a verbatim
// string literal (Str, IsRef false, List/Map nil).
type Elem struct {
	Str   string
	IsRef bool
	List  *ListLit
	Map   *MapLit
}

// ListLit is a list() constructor (§2.6). Elements are strings, $refs, or
// nested list()/map() constructors.
type ListLit struct {
	Elems []Elem
	Line  int
}

// Concat is a concat(...) constructor (§2.6, v0.3): it appends its arguments
// into a single list, flattening exactly one level (a list argument contributes
// its items; a non-list argument contributes itself). Its arguments share the
// element grammar of list().
type Concat struct {
	Args []Elem
	Line int
}

// MapLit is a map() constructor (§2.6, v0.3): an ordered list of key/value
// entries. Keys are bare identifiers; values share the element grammar of
// list(). It is a binding right-hand side (Expr) and may nest inside Elem.Map.
type MapLit struct {
	Entries []MapEntry
	Line    int
}

// MapEntry is one key/value pair of a MapLit; Val follows the element grammar.
type MapEntry struct {
	Key string
	Val Elem
}

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
	name, value := splitFlag(text)
	if d.Flags == nil {
		d.Flags = map[string]string{}
	}
	d.Flags[name] = value
	if name == "id" {
		d.ID = value
	}
}

func (*Dispatch) stmt()        {}
func (*Assign) stmt()          {}
func (*Parallel) stmt()        {}
func (*ParallelForeach) stmt() {}
func (*Foreach) stmt()         {}
func (*Loop) stmt()            {}
func (*If) stmt()              {}

func (*Dispatch) expr() {}
func (StrLit) expr()    {}
func (ListLit) expr()   {}
func (*Concat) expr()   {}
func (MapLit) expr()    {}
