// Package registry loads and validates the agent contracts stored under
// .agents/ (ROM). Rules VAL-01..VAL-12 follow spec/agent-schema.yaml as
// amended by spec/divergence-notes.md (VAL-02/command_type is retired;
// VAL-10..12 cover model, scope and agnostic grant containment).
package registry

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Field describes a named context input declared by an operation.
type Field struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
}

// Flag describes a dispatch flag declared by an operation.
type Flag struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// Operation is one callable operation of an agent contract.
type Operation struct {
	ID            string
	Description   string
	Context       map[string]Field
	Flags         map[string]Flag
	Grants        []string
	OutputSchema  string            // verbatim YAML block from agent.yaml, never re-serialized
	SuccessSchema *PayloadSchema    // parsed success contract; nil only if output_schema absent
	InfoSchema    *PayloadSchema    // parsed info contract; nil when info is not declared
	Groups        map[string]Group  // group name -> {option -> directive text}; tag "#"+group
	Modifiers     map[string]string // modifier name -> directive text; all under one "#modifiers" tag
	optionGroups  map[string]string // option name -> group name (built at load)
	modifierSet   map[string]bool   // modifier names (built at load)
}

// Group is a mutually-exclusive flag group: option name -> directive text.
// At most one option per group may be active in a single dispatch.
type Group map[string]string

// Personality is the agent-level personality flag: a Default section plus a
// value->section map selected by --personality=<value>.
type Personality struct {
	Default string            `yaml:"default"`
	Values  map[string]string `yaml:"values"`
}

// FlagRole classifies a dispatch flag within an operation.
type FlagRole int

// The classifications a flag name can resolve to (personality is agent-level
// and handled by the caller).
const (
	RoleData FlagRole = iota
	RoleGroupOption
	RoleModifier
	RoleUnknown
)

// buildFlagIndex fills the option->group and modifier lookup maps from the
// operation's declared groups and modifiers.
func buildFlagIndex(op *Operation) {
	op.optionGroups = map[string]string{}
	for group, opts := range op.Groups {
		for option := range opts {
			op.optionGroups[option] = group
		}
	}
	op.modifierSet = map[string]bool{}
	for m := range op.Modifiers {
		op.modifierSet[m] = true
	}
}

// ClassifyFlag resolves a flag name to its role within the operation. The
// personality flag is agent-level and resolved by the caller. Value receiver
// so it is callable on non-addressable map values.
func (op Operation) ClassifyFlag(name string) (FlagRole, string) {
	if g, ok := op.optionGroups[name]; ok {
		return RoleGroupOption, g
	}
	if op.modifierSet[name] {
		return RoleModifier, ""
	}
	if _, ok := op.Flags[name]; ok {
		return RoleData, ""
	}
	return RoleUnknown, ""
}

// SchemaKind is the declared kind of a success or info payload.
type SchemaKind int

// The three payload kinds an output_schema may declare.
const (
	KindObject SchemaKind = iota
	KindString
	KindList
)

// PayloadSchema is the parsed contract for a success or info payload. Fields
// is populated only when Kind is KindObject (field name -> declared type).
type PayloadSchema struct {
	Kind   SchemaKind
	Fields map[string]string
}

// Match reports the first way payload fails the schema, or "" when it
// satisfies it. Declared fields must always be present and correctly typed.
// When strict, a field NOT in the schema is also rejected — unless the schema
// declares no fields (`success: {}`), which stays permissive and accepts any
// object. Strict is used for success payloads (the interop contract); info
// stays lax so the Ask Protocol may add missing_field/resumable. The reason
// names fields and types only, never payload values (SEC1).
func (s *PayloadSchema) Match(payload any, strict bool) string {
	switch s.Kind {
	case KindString:
		if _, ok := payload.(string); !ok {
			return fmt.Sprintf("payload expected string, got %s", goType(payload))
		}
	case KindList:
		if _, ok := payload.([]any); !ok {
			return fmt.Sprintf("payload expected list, got %s", goType(payload))
		}
	case KindObject:
		m, ok := payload.(map[string]any)
		if !ok {
			return fmt.Sprintf("payload expected object, got %s", goType(payload))
		}
		for _, name := range sortedKeys(s.Fields) {
			fv, present := m[name]
			if !present {
				return fmt.Sprintf("field %q is missing", name)
			}
			if !typeMatches(s.Fields[name], fv) {
				return fmt.Sprintf("field %q expected %s, got %s", name, s.Fields[name], goType(fv))
			}
		}
		// An empty field set (`success: {}`) is the permissive escape hatch:
		// any object is accepted. Otherwise a payload key not in the schema is
		// a contract violation — this is what catches silent field drift
		// between heterogeneous agents (e.g. status-vs-outcome).
		if strict && len(s.Fields) > 0 {
			for _, name := range sortedKeys(m) {
				if _, declared := s.Fields[name]; !declared {
					return fmt.Sprintf("unknown field %q not in schema", name)
				}
			}
		}
	}
	return ""
}

// typeMatches reports whether v satisfies a declared field type. An unknown
// declared type does not block (lax).
func typeMatches(declared string, v any) bool {
	switch declared {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		switch v.(type) {
		case int, int64:
			return true
		}
		return false
	case "list":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	default:
		return true
	}
}

// goType names the schema type of a decoded YAML value without exposing its
// value (SEC1).
func goType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64:
		return "integer"
	case float64:
		return "number"
	case []any:
		return "list"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// parsePayloadSchema decodes a success/info node into a PayloadSchema. A
// scalar "string"/"list" yields KindString/KindList; a mapping yields
// KindObject with its field->type map ({} means an object with no required
// fields). Returns (nil, nil) for a nil node.
func parsePayloadSchema(node *yaml.Node) (*PayloadSchema, error) {
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Value {
		case "string":
			return &PayloadSchema{Kind: KindString}, nil
		case "list":
			return &PayloadSchema{Kind: KindList}, nil
		default:
			return nil, fmt.Errorf("scalar schema must be \"string\" or \"list\", got %q", node.Value)
		}
	case yaml.MappingNode:
		var fields map[string]string
		if err := node.Decode(&fields); err != nil {
			return nil, fmt.Errorf("object schema is malformed: %w", err)
		}
		return &PayloadSchema{Kind: KindObject, Fields: fields}, nil
	default:
		return nil, fmt.Errorf("schema must be a scalar (string|list) or a mapping")
	}
}

// childNode returns the value node mapped to key in a mapping node, or nil.
// Walking Content directly is deterministic; decoding into a *yaml.Node field
// mangles the node Kind for empty flow mappings ({}).
func childNode(node yaml.Node, key string) *yaml.Node {
	content := node.Content
	if node.Kind == yaml.DocumentNode && len(content) == 1 {
		content = content[0].Content
	}
	for i := 0; i+1 < len(content); i += 2 {
		if content[i].Value == key {
			return content[i+1]
		}
	}
	return nil
}

// outputSchemaNodes returns the success and info child nodes of an
// output_schema block; either may be nil when absent.
func outputSchemaNodes(node yaml.Node) (success, info *yaml.Node) {
	return childNode(node, "success"), childNode(node, "info")
}

// errorSchemaValid reports whether the error node declares reason (string)
// and recoverable (boolean) per VAL-06.
func errorSchemaValid(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	var m map[string]string
	if node.Decode(&m) != nil {
		return false
	}
	return m["reason"] == "string" && m["recoverable"] == "boolean"
}

// Layer marks an agent whose spawn runs in the origin directory of an
// absorbed AI instead of the sandbox run dir.
type Layer struct {
	Origin string `yaml:"origin"`
}

// Capabilities is the agent's "calling card": what it does and when the
// orchestrator should pick it. Rendered by the agent catalog. purpose and
// use_when are mandatory (VAL-18); inputs/produces/examples enrich the card.
type Capabilities struct {
	Purpose  string   `yaml:"purpose"`  // one line: what problem it solves
	UseWhen  string   `yaml:"use_when"` // when to choose this agent
	Inputs   string   `yaml:"inputs"`   // high-level shape of expected input
	Produces string   `yaml:"produces"` // high-level shape of output
	Examples []string `yaml:"examples"` // example dispatch lines
}

// Agent is a fully loaded agent contract plus its harness template.
type Agent struct {
	Command          string
	Model            string
	Scope            string
	Personality      *Personality // agent-level personality flag; nil when not declared
	DefaultOperation string
	Timeout          int           // seconds; 0 means "use config default"
	Layer            *Layer        // layer-agent marker; nil for regular agents
	Capabilities     *Capabilities // calling card for the catalog; required (VAL-18)
	Operations       map[string]Operation
	Dir              string // .agents/<name>/
	Harness          string // full contents of harness.md
}

// agentFile mirrors agent.yaml on disk. Unknown fields (e.g. the retired
// command_type) are ignored by design.
type agentFile struct {
	Command          string            `yaml:"command"`
	Model            string            `yaml:"model"`
	Scope            string            `yaml:"scope"`
	Personality      *Personality      `yaml:"personality"`
	DefaultOperation string            `yaml:"default_operation"`
	Timeout          *int              `yaml:"timeout"`
	Layer            *Layer            `yaml:"layer"`
	Capabilities     *Capabilities     `yaml:"capabilities"`
	Operations       map[string]opFile `yaml:"operations"`
}

type opFile struct {
	ID           string                       `yaml:"id"`
	Description  string                       `yaml:"description"`
	Context      map[string]Field             `yaml:"context"`
	Flags        map[string]Flag              `yaml:"flags"`
	Grants       []string                     `yaml:"grants"`
	OutputSchema yaml.Node                    `yaml:"output_schema"`
	Groups       map[string]map[string]string `yaml:"groups"`
	Modifiers    map[string]string            `yaml:"modifiers"`
}

func parseAgentFile(src []byte) (*agentFile, error) {
	var af agentFile
	if err := yaml.Unmarshal(src, &af); err != nil {
		return nil, fmt.Errorf("parsing agent.yaml: %w", err)
	}
	return &af, nil
}

// validateOutputSchema checks VAL-05 and VAL-06 on a present output_schema
// node. The node itself is stored verbatim; validation decodes a throwaway
// view and never rewrites the block.
func validateOutputSchema(path, opName string, node yaml.Node) []error {
	success, info := outputSchemaNodes(node)
	var errs []error
	if success == nil {
		errs = append(errs, fmt.Errorf("%s: VAL-05: output_schema.success is required in operation %q (may be {})", path, opName))
	} else if _, err := parsePayloadSchema(success); err != nil {
		errs = append(errs, fmt.Errorf("%s: VAL-05: output_schema.success in operation %q: %v", path, opName, err))
	}
	if _, err := parsePayloadSchema(info); err != nil {
		errs = append(errs, fmt.Errorf("%s: VAL-06: output_schema.info in operation %q: %v", path, opName, err))
	}
	if !errorSchemaValid(childNode(node, "error")) {
		errs = append(errs, fmt.Errorf("%s: VAL-06: output_schema.error in operation %q must define reason (string) and recoverable (boolean)", path, opName))
	}
	return errs
}

// verbatimBlock returns the raw source text of node n exactly as written in
// src, dedented to the node's own indentation. No re-serialization happens:
// the bytes come straight from the file. Multiline literal scalars are not
// used in agent contracts, so the last descendant line bounds the block.
func verbatimBlock(src []byte, n *yaml.Node) string {
	lines := strings.Split(string(src), "\n")
	start, end := n.Line, maxLine(n)
	if start < 1 || end > len(lines) {
		return ""
	}
	indent := n.Column - 1
	var b strings.Builder
	for i := start; i <= end; i++ {
		line := lines[i-1]
		if len(line) >= indent {
			line = line[indent:]
		} else {
			line = ""
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func maxLine(n *yaml.Node) int {
	m := n.Line
	for _, c := range n.Content {
		if l := maxLine(c); l > m {
			m = l
		}
	}
	return m
}
