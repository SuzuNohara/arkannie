// Package ram implements binding storage for Ann v0.2 as a stack of
// scopes (spec/ann-lang.md §4). Every block {} pushes a scope; leaving
// it pops the scope and destroys its bindings. Values returned by Get
// and Snapshot are deep copies: callers may mutate them freely without
// affecting stored state.
package ram

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// RefToken is the single definition of a binding reference token: a `$`
// followed by a name and zero or more dotted segments (§4). It is the
// canonical matcher consumed across packages; Resolve receives the path
// with the leading `$` already stripped.
var RefToken = regexp.MustCompile(`\$[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*`)

// Kind discriminates the shape of a Value.
type Kind int

// Value kinds supported by Ann v0.2 bindings.
const (
	KString Kind = iota
	KList
	KMap
)

// Value is a single binding value: a string, a list, or a map.
// Exactly one of Str, List, or Map is meaningful according to Kind.
type Value struct {
	Kind Kind             `yaml:"kind"`
	Str  string           `yaml:"str,omitempty"`
	List []Value          `yaml:"list,omitempty"`
	Map  map[string]Value `yaml:"map,omitempty"`
}

// ErrInvalidName reports a binding name outside [A-Za-z0-9_]+.
var ErrInvalidName = errors.New("invalid binding name")

// RAM is a stack of scopes. The bottom scope is the program root and
// is never destroyed; Push/Pop bracket every inner block.
type RAM struct {
	scopes []map[string]Value
}

// New returns a RAM with only the root scope.
func New() *RAM {
	return &RAM{scopes: []map[string]Value{{}}}
}

// Push enters a block {}: a fresh innermost scope.
func (r *RAM) Push() {
	r.scopes = append(r.scopes, map[string]Value{})
}

// Pop leaves the current block, destroying its bindings. Popping the
// root scope is a no-op: the program scope lives for the whole run (§4.3).
func (r *RAM) Pop() {
	if len(r.scopes) > 1 {
		r.scopes = r.scopes[:len(r.scopes)-1]
	}
}

// Set creates or overwrites name in the CURRENT scope. The stored
// value is a deep copy, so later mutations by the caller are not
// observable. Names must match [A-Za-z0-9_]+.
func (r *RAM) Set(name string, v Value) error {
	if !validName(name) {
		return fmt.Errorf("setting binding %q: %w", name, ErrInvalidName)
	}
	r.scopes[len(r.scopes)-1][name] = deepCopy(v)
	return nil
}

// Get resolves name from the current scope outwards (§4.2). The
// returned Value is a deep copy.
func (r *RAM) Get(name string) (Value, bool) {
	for i := len(r.scopes) - 1; i >= 0; i-- {
		if v, ok := r.scopes[i][name]; ok {
			return deepCopy(v), true
		}
	}
	return Value{}, false
}

// Resolve walks a dotted path (without the leading `$`, e.g. "x.a.b").
// The first segment is resolved like Get; each remaining segment indexes
// into a KMap. Any unresolvable step returns (zero, false). A path with
// no dot is exactly Get. The returned Value is a deep copy.
func (r *RAM) Resolve(path string) (Value, bool) {
	segs := strings.Split(path, ".")
	v, ok := r.Get(segs[0])
	if !ok {
		return Value{}, false
	}
	for _, seg := range segs[1:] {
		if v.Kind != KMap {
			return Value{}, false
		}
		next, ok := v.Map[seg]
		if !ok {
			return Value{}, false
		}
		v = next
	}
	return v, true
}

// Snapshot returns every binding visible from the current scope, with
// inner bindings shadowing outer ones. All values are deep copies.
func (r *RAM) Snapshot() map[string]Value {
	snap := make(map[string]Value)
	for _, scope := range r.scopes {
		for name, v := range scope {
			snap[name] = deepCopy(v)
		}
	}
	return snap
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alnum && c != '_' {
			return false
		}
	}
	return true
}

func deepCopy(v Value) Value {
	out := Value{Kind: v.Kind, Str: v.Str}
	if v.List != nil {
		out.List = make([]Value, len(v.List))
		for i, e := range v.List {
			out.List[i] = deepCopy(e)
		}
	}
	if v.Map != nil {
		out.Map = make(map[string]Value, len(v.Map))
		for k, e := range v.Map {
			out.Map[k] = deepCopy(e)
		}
	}
	return out
}
