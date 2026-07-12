package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var commandPattern = regexp.MustCompile(`^\[[a-z][a-z0-9-]*\]$`)

var (
	validModels    = map[string]bool{"haiku": true, "sonnet": true, "opus": true}
	validScopes    = map[string]bool{"agnostic": true, "executor": true}
	validFlagTypes = map[string]bool{"boolean": true, "string": true, "integer": true}
	validGrants    = map[string]bool{"read": true, "write": true, "execute": true, "network": true}
	agnosticGrants = map[string]bool{"read": true, "network": true}
)

// Registry holds every valid agent contract, keyed by bracketed command.
type Registry struct {
	agents map[string]*Agent
}

// Load scans agentsDir for agent directories (.agents/<name>/), validates
// every contract, and returns the registry plus every validation error found.
// All agents are validated and all violations reported; a missing agentsDir
// yields an empty registry with no errors. Dot-directories (.personalities)
// are not agents and are skipped.
func Load(agentsDir string) (*Registry, []error) {
	r := &Registry{agents: map[string]*Agent{}}
	entries, err := os.ReadDir(agentsDir)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return r, []error{fmt.Errorf("reading agents dir %s: %w", agentsDir, err)}
	}
	root := filepath.Dir(agentsDir)
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		agent, agentErrs := loadAgent(agentsDir, root, e.Name())
		if len(agentErrs) > 0 {
			errs = append(errs, agentErrs...)
			continue
		}
		if prev, dup := r.agents[agent.Command]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate command %s already declared in %s",
				filepath.Join(agent.Dir, "agent.yaml"), agent.Command, prev.Dir))
			continue
		}
		r.agents[agent.Command] = agent
	}
	return r, errs
}

// Resolve returns the agent registered for command. It accepts both the bare
// name ("echo") and the bracketed command token ("[echo]").
func (r *Registry) Resolve(command string) (*Agent, bool) {
	if !strings.HasPrefix(command, "[") {
		command = "[" + command + "]"
	}
	a, ok := r.agents[command]
	return a, ok
}

// Names returns the registered command tokens in sorted order.
func (r *Registry) Names() []string {
	return sortedKeys(r.agents)
}

func loadAgent(agentsDir, root, name string) (*Agent, []error) {
	dir := filepath.Join(agentsDir, name)
	yamlPath := filepath.Join(dir, "agent.yaml")
	src, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: agent.yaml is required: %w", yamlPath, err)}
	}
	af, err := parseAgentFile(src)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", yamlPath, err)}
	}
	errs := validateAgentFile(yamlPath, af)
	errs = append(errs, validateLayer(yamlPath, root, af.Layer)...)
	harness, herr := readHarness(dir)
	if herr != nil {
		errs = append(errs, herr)
	}
	errs = append(errs, checkDirectiveSlots(yamlPath, af, harness)...)
	if len(errs) > 0 {
		return nil, errs
	}
	return buildAgent(dir, src, af, harness), nil
}

func readHarness(dir string) (string, error) {
	p := filepath.Join(dir, "harness.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("%s: harness.md is required: %w", p, err)
	}
	return string(b), nil
}

// validateLayer enforces VAL-17 on a declared layer block: origin must be an
// absolute existing directory containing a readable CLAUDE.md identity file,
// and must not overlap the arkannie root (anti-recursion). A nil layer is valid:
// the agent is simply not a layer agent. Checks run in order and stop at the
// first failure, since each later check presumes the earlier ones hold.
func validateLayer(yamlPath, root string, layer *Layer) []error {
	if layer == nil {
		return nil
	}
	if layer.Origin == "" {
		return []error{fmt.Errorf("%s: VAL-17: layer.origin is required", yamlPath)}
	}
	if !filepath.IsAbs(layer.Origin) {
		return []error{fmt.Errorf("%s: VAL-17: layer.origin must be an absolute path", yamlPath)}
	}
	info, err := os.Stat(layer.Origin)
	if err != nil || !info.IsDir() {
		return []error{fmt.Errorf("%s: VAL-17: layer.origin must be an existing directory", yamlPath)}
	}
	if _, err := os.ReadFile(filepath.Join(layer.Origin, "CLAUDE.md")); err != nil {
		return []error{fmt.Errorf("%s: VAL-17: layer.origin must contain a readable CLAUDE.md identity file", yamlPath)}
	}
	if pathsOverlap(layer.Origin, root) {
		return []error{fmt.Errorf("%s: VAL-17: layer.origin must not overlap the arkannie root", yamlPath)}
	}
	return nil
}

// pathsOverlap reports whether a and b name the same directory or one is
// nested inside the other. The prefix check is separator-aware so sibling
// dirs sharing a name prefix (/x/ab vs /x/abc) do not overlap.
func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(a, b+sep) || strings.HasPrefix(b, a+sep)
}

// checkDirectiveSlots enforces VAL-16: a contract that declares directive
// sections must expose the matching harness slots, or the directives would
// be silently dropped at assembly time.
func checkDirectiveSlots(path string, af *agentFile, harness string) []error {
	needPre := af.Personality != nil
	needPost := false
	for _, op := range af.Operations {
		if len(op.Groups) > 0 {
			needPre = true
		}
		if len(op.Modifiers) > 0 {
			needPost = true
		}
	}
	var errs []error
	if needPre && !strings.Contains(harness, "directives_pre") {
		errs = append(errs, fmt.Errorf("%s: VAL-16: agent declares groups/personality but harness.md has no {{ directives_pre }} slot", path))
	}
	if needPost && !strings.Contains(harness, "directives_post") {
		errs = append(errs, fmt.Errorf("%s: VAL-16: agent declares modifiers but harness.md has no {{ directives_post }} slot", path))
	}
	return errs
}

func validateAgentFile(path string, af *agentFile) []error {
	var errs []error
	if !commandPattern.MatchString(af.Command) {
		errs = append(errs, fmt.Errorf("%s: VAL-01: command %q must match %s", path, af.Command, commandPattern))
	}
	if !validModels[af.Model] {
		errs = append(errs, fmt.Errorf("%s: VAL-10: model %q must be one of haiku, sonnet, opus", path, af.Model))
	}
	if !validScopes[af.Scope] {
		errs = append(errs, fmt.Errorf("%s: VAL-11: scope %q must be one of agnostic, executor", path, af.Scope))
	}
	if af.Timeout != nil && *af.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("%s: timeout must be greater than 0, got %d", path, *af.Timeout))
	}
	if len(af.Operations) == 0 {
		errs = append(errs, fmt.Errorf("%s: VAL-03: at least one operation is required", path))
	}
	if af.DefaultOperation != "" {
		if _, ok := af.Operations[af.DefaultOperation]; !ok {
			errs = append(errs, fmt.Errorf("%s: default_operation %q is not defined in operations", path, af.DefaultOperation))
		}
	}
	errs = append(errs, validateCapabilities(path, af.Capabilities)...)
	errs = append(errs, validateDirectives(path, af)...)
	return append(errs, validateOperations(path, af)...)
}

// validateCapabilities enforces VAL-18: every agent must declare a capabilities
// calling card with a non-empty purpose and use_when, so the catalog can present
// it and the orchestrator can select the agent.
func validateCapabilities(path string, cap *Capabilities) []error {
	if cap == nil {
		return []error{fmt.Errorf("%s: VAL-18: capabilities block is required", path)}
	}
	var errs []error
	if cap.Purpose == "" {
		errs = append(errs, fmt.Errorf("%s: VAL-18: capabilities.purpose must be non-empty", path))
	}
	if cap.UseWhen == "" {
		errs = append(errs, fmt.Errorf("%s: VAL-18: capabilities.use_when must be non-empty", path))
	}
	return errs
}

// validateDirectives enforces VAL-13 (flag-namespace uniqueness), VAL-14
// (personality shape) and VAL-15 (non-empty groups/options/modifiers).
func validateDirectives(path string, af *agentFile) []error {
	var errs []error
	if af.Personality != nil {
		if af.Personality.Default == "" {
			errs = append(errs, fmt.Errorf("%s: VAL-14: personality.default must be non-empty", path))
		}
		if len(af.Personality.Values) == 0 {
			errs = append(errs, fmt.Errorf("%s: VAL-14: personality.values must declare at least one value", path))
		}
		for _, v := range sortedKeys(af.Personality.Values) {
			if af.Personality.Values[v] == "" {
				errs = append(errs, fmt.Errorf("%s: VAL-14: personality value %q has empty text", path, v))
			}
		}
	}
	opNames := map[string]bool{}
	for name := range af.Operations {
		opNames[name] = true
	}
	for _, opName := range sortedKeys(af.Operations) {
		errs = append(errs, validateOpNamespace(path, opName, af.Operations[opName], opNames, af.Personality != nil)...)
	}
	return errs
}

// validateOpNamespace checks that every flag name an operation accepts —
// data flags, group options, modifiers and the reserved "personality" — is
// unique and does not collide with an operation name (VAL-13), and that
// groups/options/modifiers carry non-empty text (VAL-15).
func validateOpNamespace(path, opName string, op opFile, opNames map[string]bool, hasPersonality bool) []error {
	var errs []error
	seen := map[string]bool{}
	claim := func(name, where string) {
		if seen[name] {
			errs = append(errs, fmt.Errorf("%s: VAL-13: flag name %q in operation %q is declared more than once (%s)", path, name, opName, where))
			return
		}
		if opNames[name] {
			errs = append(errs, fmt.Errorf("%s: VAL-13: %s %q in operation %q collides with an operation name", path, where, name, opName))
		}
		seen[name] = true
	}
	for _, f := range sortedKeys(op.Flags) {
		claim(f, "data flag")
	}
	for _, g := range sortedKeys(op.Groups) {
		opts := op.Groups[g]
		if len(opts) == 0 {
			errs = append(errs, fmt.Errorf("%s: VAL-15: group %q in operation %q has no options", path, g, opName))
		}
		for _, o := range sortedKeys(opts) {
			if opts[o] == "" {
				errs = append(errs, fmt.Errorf("%s: VAL-15: option %q of group %q in operation %q has empty text", path, o, g, opName))
			}
			claim(o, "group option")
		}
	}
	for _, m := range sortedKeys(op.Modifiers) {
		if op.Modifiers[m] == "" {
			errs = append(errs, fmt.Errorf("%s: VAL-15: modifier %q in operation %q has empty text", path, m, opName))
		}
		claim(m, "modifier")
	}
	if hasPersonality {
		claim("personality", "personality flag")
	}
	return errs
}

func validateOperations(path string, af *agentFile) []error {
	var errs []error
	seenIDs := map[string]string{}
	for _, name := range sortedKeys(af.Operations) {
		op := af.Operations[name]
		errs = append(errs, validateOperation(path, name, op, af.Scope)...)
		if op.ID == "" {
			continue
		}
		if other, dup := seenIDs[op.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: VAL-07: operation id %q duplicated in %q and %q", path, op.ID, other, name))
			continue
		}
		seenIDs[op.ID] = name
	}
	return errs
}

func validateOperation(path, name string, op opFile, scope string) []error {
	var errs []error
	if op.Description == "" || op.ID == "" || op.OutputSchema.IsZero() {
		errs = append(errs, fmt.Errorf("%s: VAL-04: operation %q must have description, id and output_schema", path, name))
	}
	errs = append(errs, validateFlags(path, name, op.Flags)...)
	errs = append(errs, validateGrants(path, name, op.Grants, scope)...)
	if !op.OutputSchema.IsZero() {
		errs = append(errs, validateOutputSchema(path, name, op.OutputSchema)...)
	}
	return errs
}

func validateFlags(path, opName string, flags map[string]Flag) []error {
	var errs []error
	for _, fname := range sortedKeys(flags) {
		if !validFlagTypes[flags[fname].Type] {
			errs = append(errs, fmt.Errorf("%s: VAL-08: flag %q in operation %q has type %q, want boolean, string or integer",
				path, fname, opName, flags[fname].Type))
		}
	}
	return errs
}

func validateGrants(path, opName string, grants []string, scope string) []error {
	var errs []error
	for _, g := range grants {
		if !validGrants[g] {
			errs = append(errs, fmt.Errorf("%s: VAL-09: grant %q in operation %q must be one of read, write, execute, network",
				path, g, opName))
			continue
		}
		if scope == "agnostic" && !agnosticGrants[g] {
			errs = append(errs, fmt.Errorf("%s: VAL-12: agnostic scope forbids grant %q in operation %q (only read, network)",
				path, g, opName))
		}
	}
	return errs
}

// toGroups converts the raw YAML shape into the typed Group map.
func toGroups(raw map[string]map[string]string) map[string]Group {
	if raw == nil {
		return nil
	}
	out := make(map[string]Group, len(raw))
	for name, opts := range raw {
		out[name] = Group(opts)
	}
	return out
}

func buildAgent(dir string, src []byte, af *agentFile, harness string) *Agent {
	ops := make(map[string]Operation, len(af.Operations))
	for name, op := range af.Operations {
		succNode, infoNode := outputSchemaNodes(op.OutputSchema)
		// Errors were already reported by validateOutputSchema at Load; a
		// non-nil schema here is well-formed.
		succSchema, _ := parsePayloadSchema(succNode)
		infoSchema, _ := parsePayloadSchema(infoNode)
		built := Operation{
			ID:            op.ID,
			Description:   op.Description,
			Context:       op.Context,
			Flags:         op.Flags,
			Grants:        op.Grants,
			OutputSchema:  verbatimBlock(src, &op.OutputSchema),
			SuccessSchema: succSchema,
			InfoSchema:    infoSchema,
			Groups:        toGroups(op.Groups),
			Modifiers:     op.Modifiers,
		}
		buildFlagIndex(&built)
		ops[name] = built
	}
	var timeout int
	if af.Timeout != nil {
		timeout = *af.Timeout
	}
	return &Agent{
		Command:          af.Command,
		Model:            af.Model,
		Scope:            af.Scope,
		Personality:      af.Personality,
		DefaultOperation: af.DefaultOperation,
		Timeout:          timeout,
		Layer:            af.Layer,
		Capabilities:     af.Capabilities,
		Operations:       ops,
		Dir:              dir,
		Harness:          harness,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
