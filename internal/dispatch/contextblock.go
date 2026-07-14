// Package dispatch assembles what a wave agent receives: the operation
// selected from the dispatch atom, the canonical context_block
// (spec/ann-lang.md §9) and the final rendered prompt materialized to an
// ephemeral run directory.
package dispatch

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"arkannie/internal/ann"
	"arkannie/internal/ram"
	"arkannie/internal/registry"
)

// PreDispatchError is a failure detected before the wave is ever spawned
// (spec/agent-protocol.md §5). Class is the escalation class, 'A' or 'B'.
type PreDispatchError struct {
	Class byte
	Msg   string
}

func (e *PreDispatchError) Error() string {
	return fmt.Sprintf("pre-dispatch failure [class %c]: %s", e.Class, e.Msg)
}

func classB(format string, args ...any) error {
	return &PreDispatchError{Class: 'B', Msg: fmt.Sprintf(format, args...)}
}

// FlagResolution is the outcome of classifying a dispatch's flags into their
// roles. Data is a clone of the dispatch carrying only the data flags (fed to
// BuildContextBlock); the directive flags are consumed into Groups (group ->
// chosen option), Personality (chosen value, "" = default) and Modifiers.
type FlagResolution struct {
	Data        *ann.Dispatch
	Groups      map[string]string
	Personality string
	Modifiers   []string
}

// ResolveFlags classifies every dispatch flag using the operation's declared
// groups/modifiers and the agent-level personality. It enforces group
// exclusivity, rejects a value on a boolean directive flag, an unknown flag,
// or an undeclared personality value — all Class B. Directive flags are
// consumed out of Data so only data flags reach the context_block.
func ResolveFlags(a *registry.Agent, op *registry.Operation, opName string, d *ann.Dispatch) (*FlagResolution, error) {
	res := &FlagResolution{Groups: map[string]string{}}
	data := map[string]string{}
	for _, name := range sortedKeys(d.Flags) {
		value := d.Flags[name]
		if excludedFlag(name, value, opName) { // id, timeout, operation selector
			data[name] = value
			continue
		}
		if name == "personality" && a.Personality != nil {
			if value == "" {
				return nil, classB("flag --personality requires a value (one of the declared values)")
			}
			if _, ok := a.Personality.Values[value]; !ok {
				return nil, classB("--personality=%s is not a declared personality value", value)
			}
			res.Personality = value
			continue
		}
		role, group := op.ClassifyFlag(name)
		switch role {
		case registry.RoleGroupOption:
			if value != "" {
				return nil, classB("group option --%s takes no value", name)
			}
			if prev, ok := res.Groups[group]; ok {
				return nil, classB("group %q: --%s and --%s are mutually exclusive", group, prev, name)
			}
			res.Groups[group] = name
		case registry.RoleModifier:
			if value != "" {
				return nil, classB("modifier --%s takes no value", name)
			}
			res.Modifiers = append(res.Modifiers, name)
		case registry.RoleData:
			data[name] = value
		default: // RoleUnknown
			return nil, classB("flag --%s is not declared by operation %q", name, opName)
		}
	}
	clone := *d
	clone.Flags = data
	res.Data = &clone
	return res, nil
}

// SelectOperation resolves which operation of agent a the dispatch atom d
// selects. A boolean flag whose name matches an operation selects it; more
// than one match is ambiguous (Class B). With no match the agent's
// default_operation applies; without one the dispatch is unresolvable and
// the error lists the available operations.
func SelectOperation(a *registry.Agent, d *ann.Dispatch) (*registry.Operation, string, error) {
	var matches []string
	for _, name := range sortedKeys(d.Flags) {
		if d.Flags[name] != "" {
			continue // only boolean flags select operations
		}
		if _, ok := a.Operations[name]; ok {
			matches = append(matches, name)
		}
	}
	if len(matches) > 1 {
		return nil, "", classB("dispatch %s selects more than one operation: --%s",
			a.Command, strings.Join(matches, ", --"))
	}
	name := a.DefaultOperation
	if len(matches) == 1 {
		name = matches[0]
	}
	if name == "" {
		return nil, "", classB("dispatch %s selects no operation and %s has no default_operation; available operations: %s",
			a.Command, a.Command, strings.Join(sortedKeys(a.Operations), ", "))
	}
	op := a.Operations[name]
	return &op, name, nil
}

// BuildContextBlock serializes the canonical context_block (§9) for one
// dispatch: fixed key order operation, context, flags, output_schema, with
// $bindings resolved from RAM and op.OutputSchema copied verbatim.
func BuildContextBlock(op *registry.Operation, opName string, d *ann.Dispatch, r *ram.RAM) (string, error) {
	if op.OutputSchema == "" {
		return "", classB("operation %q has no output_schema (§9.4)", opName)
	}
	text, extras, err := resolveBindings(d.Context, r)
	if err != nil {
		return "", err
	}
	if err := checkFlags(op, opName, d); err != nil {
		return "", err
	}
	populateFields(op, d, extras)
	if err := checkRequired(op, opName, extras); err != nil {
		return "", err
	}
	return marshalBlock(opName, text, d.Context != "", extras, effectiveFlags(op, opName, d), op.OutputSchema)
}

// resolveBindings applies §9.3/§9.6 to the context text using the single
// canonical ram.RefToken matcher: a KString path is inlined; a KList/KMap path
// becomes a context.<field> entry (keyed by the last dotted segment) and the
// token is replaced by that bare name. Dotted paths (`$x.a.b`) resolve a field
// over KMaps; an unresolvable path is Class B (§7.3), naming the base binding
// and the failing segment.
func resolveBindings(text string, r *ram.RAM) (string, map[string]*yaml.Node, error) {
	extras := map[string]*yaml.Node{}
	var firstErr error
	masked := ram.EscapePlaceholder(text)
	resolved := ram.RefToken.ReplaceAllStringFunc(masked, func(tok string) string {
		v, ok := r.Resolve(tok[1:])
		if !ok {
			if firstErr == nil {
				firstErr = unresolvablePath(r, tok)
			}
			return tok
		}
		if v.Kind == ram.KString {
			return v.Str
		}
		key := lastSegment(tok[1:])
		extras[key] = valueNode(v)
		return key
	})
	if firstErr != nil {
		return "", nil, firstErr
	}
	return ram.RestoreEscapes(resolved), extras, nil
}

// lastSegment returns the final dotted segment of a path — the field name that
// keys a KList/KMap value inlined as a context field.
func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// unresolvablePath reconstructs why r.Resolve(tok[1:]) failed so the Class B
// error names the base binding and the failing segment (§7.3). A path that
// tries to descend into a non-map suggests separating the dot from the
// reference — the dot was most likely meant as literal text.
func unresolvablePath(r *ram.RAM, tok string) error {
	segs := strings.Split(tok[1:], ".")
	base := segs[0]
	cur, ok := r.Get(base)
	if !ok {
		return classB("unresolvable binding %s in context_block render (§7.3)", tok)
	}
	for _, seg := range segs[1:] {
		if cur.Kind != ram.KMap {
			return classB("binding $%s in %s is not a map, so .%s cannot index it; "+
				"if the dot is literal text, separate it from the reference (§7.3)", base, tok, seg)
		}
		next, ok := cur.Map[seg]
		if !ok {
			return classB("binding $%s has no field %q for path %s (§7.3)", base, seg, tok)
		}
		cur = next
	}
	return classB("unresolvable binding %s in context_block render (§7.3)", tok)
}

// checkFlags enforces §9.2 / agent-protocol §5.1 Type-3: every dispatch
// flag except --id, --timeout and the operation selector must be declared
// by the operation.
func checkFlags(op *registry.Operation, opName string, d *ann.Dispatch) error {
	for _, name := range sortedKeys(d.Flags) {
		if excludedFlag(name, d.Flags[name], opName) {
			continue
		}
		if _, ok := op.Flags[name]; !ok {
			return classB("flag --%s is not declared by operation %q (agent-protocol §5.1)", name, opName)
		}
	}
	return nil
}

func excludedFlag(name, value, opName string) bool {
	return name == "id" || name == "timeout" || (name == opName && value == "")
}

// populateFields fills declared context fields (other than text) from
// valued dispatch flags: --<field>=v becomes context.<field>: v.
func populateFields(op *registry.Operation, d *ann.Dispatch, extras map[string]*yaml.Node) {
	for name := range op.Context {
		if name == "text" {
			continue
		}
		if v, ok := d.Flags[name]; ok && v != "" {
			extras[name] = scalarNode(v)
		}
	}
}

// checkRequired rejects a required context field that no flag and no
// binding populated (§9.3); optional fields without a value are omitted.
func checkRequired(op *registry.Operation, opName string, extras map[string]*yaml.Node) error {
	for _, name := range sortedKeys(op.Context) {
		if name == "text" || !op.Context[name].Required {
			continue
		}
		if _, ok := extras[name]; !ok {
			return classB("required context field %q of operation %q has no value (§9.3)", name, opName)
		}
	}
	return nil
}

// effectiveFlags renders the §9.2 flags list: all dispatch flags minus the
// exclusions, plus defaults from op.Flags for absent valued flags.
func effectiveFlags(op *registry.Operation, opName string, d *ann.Dispatch) []string {
	entries := map[string]string{}
	for name, value := range d.Flags {
		if excludedFlag(name, value, opName) {
			continue
		}
		entries[name] = renderFlag(name, value)
	}
	for name, f := range op.Flags {
		if f.Default == "" {
			continue
		}
		if _, present := d.Flags[name]; present {
			continue
		}
		entries[name] = name + "=" + f.Default
	}
	out := make([]string, 0, len(entries))
	for _, name := range sortedKeys(entries) {
		out = append(out, entries[name])
	}
	return out
}

func renderFlag(name, value string) string {
	if value == "" {
		return name
	}
	return name + "=" + value
}

// marshalBlock emits the YAML document with the fixed §9 key order.
// context: {} and flags: [] are valid and emitted as such (§9.4);
// output_schema is a literal block carrying the schema verbatim.
func marshalBlock(opName, text string, hasText bool, extras map[string]*yaml.Node, flags []string, schema string) (string, error) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content,
		scalarNode("operation"), scalarNode(opName),
		scalarNode("context"), contextNode(text, hasText, extras),
		scalarNode("flags"), flagsNode(flags),
		scalarNode("output_schema"), literalNode(schema),
	)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("encoding context_block: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encoding context_block: %w", err)
	}
	return buf.String(), nil
}

// contextNode orders the context map deterministically: text first, then
// the remaining fields sorted by name.
func contextNode(text string, hasText bool, extras map[string]*yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if hasText {
		n.Content = append(n.Content, scalarNode("text"), scalarNode(text))
	}
	for _, name := range sortedKeys(extras) {
		n.Content = append(n.Content, scalarNode(name), extras[name])
	}
	return n
}

func flagsNode(flags []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, f := range flags {
		n.Content = append(n.Content, scalarNode(f))
	}
	return n
}

// valueNode serializes a RAM value per §9.3: strings as YAML strings,
// lists as sequences, maps as mappings with sorted keys.
func valueNode(v ram.Value) *yaml.Node {
	switch v.Kind {
	case ram.KList:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range v.List {
			n.Content = append(n.Content, valueNode(e))
		}
		return n
	case ram.KMap:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range sortedKeys(v.Map) {
			n.Content = append(n.Content, scalarNode(k), valueNode(v.Map[k]))
		}
		return n
	default:
		return scalarNode(v.Str)
	}
}

func scalarNode(s string) *yaml.Node {
	n := &yaml.Node{}
	_ = n.Encode(s) // encoding a plain string into a node cannot fail
	return n
}

func literalNode(s string) *yaml.Node {
	n := scalarNode(s)
	n.Style = yaml.LiteralStyle
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
