package main

import (
	"regexp"
	"strings"
)

// forgeNameRe validates the agent name accepted by --forge=<name>.
var forgeNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parsedArgs is the decoded command line. usageErr non-empty means the argv
// was malformed and Run must exit 64 without touching the registry.
type parsedArgs struct {
	subcommand     string // "" or "validate"
	agent          string
	id             string
	input          string
	runID          string // from the internal --_runid flag
	allowWorkspace bool
	detach         bool
	interpret      bool
	forge          bool
	forgeName      string   // optional value of --forge=<name>
	absorb         string   // --absorb=<path>, requires --forge
	mode           string   // --mode=<complete|fragment|layer>, requires --absorb
	allowLayer     bool     // --allow-layer consent flag
	allowLayerList []string // optional names from --allow-layer=<name,name>
	catalog        bool     // --catalog query flag
	catalogAgent   string   // optional agent name from --catalog=<agent>
	man            bool     // --man query flag
	manAgent       string   // optional agent name from --man=<agent>
	version        bool     // --version query flag
	help           bool
	usageErr       string
}

// parseArgs decodes argv supporting both --flag=val and --flag val forms and
// boolean flags. The first token may be the "validate" subcommand; the last
// positional argument is the input (prompt text or .ann path).
func parseArgs(argv []string) parsedArgs {
	var p parsedArgs
	i := 0
	if len(argv) > 0 && argv[0] == "validate" {
		p.subcommand = "validate"
		i = 1
	}
	var positionals []string
	for i < len(argv) {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			i++
			continue
		}
		name, val, hasEq := splitFlag(arg)
		next, hasNext := "", false
		if i+1 < len(argv) {
			next, hasNext = argv[i+1], true
		}
		usedNext, err := p.applyFlag(name, val, hasEq, next, hasNext)
		if err != "" {
			p.usageErr = err
			return p
		}
		i++
		if usedNext {
			i++
		}
	}
	if len(positionals) > 0 {
		p.input = positionals[len(positionals)-1]
	}
	if p.usageErr == "" {
		p.usageErr = checkComposition(&p)
	}
	return p
}

// checkComposition enforces the cross-flag rules of the forge/absorb family.
// It returns a usage-error message, or "" when the combination is valid.
func checkComposition(p *parsedArgs) string {
	if p.absorb != "" && !p.forge {
		return "--absorb requires --forge"
	}
	if p.mode != "" && p.absorb == "" {
		return "--mode requires --absorb"
	}
	switch p.mode {
	case "", "complete", "fragment", "layer":
	default:
		return "--mode must be one of complete|fragment|layer"
	}
	if p.forgeName != "" && !forgeNameRe.MatchString(p.forgeName) {
		return "--forge name must match ^[a-z][a-z0-9-]*$"
	}
	return ""
}

// splitFlag strips the leading "--" and splits an optional "=value".
func splitFlag(arg string) (name, val string, hasEq bool) {
	name = arg[2:]
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		return name[:eq], name[eq+1:], true
	}
	return name, "", false
}

// applyFlag records one flag. usedNext reports that the following argv token
// was consumed as the flag value (--flag val form).
func (p *parsedArgs) applyFlag(name, val string, hasEq bool, next string, hasNext bool) (usedNext bool, usageErr string) {
	switch name {
	case "allow-workspace":
		p.allowWorkspace = true
	case "detach":
		p.detach = true
	case "interpret":
		p.interpret = true
	case "forge":
		return p.applyForge(val, hasEq)
	case "allow-layer":
		return p.applyAllowLayer(val, hasEq)
	case "catalog":
		return p.applyCatalog(val, hasEq)
	case "man":
		return p.applyMan(val, hasEq)
	case "version":
		p.version = true
	case "help", "h":
		p.help = true
	default:
		return p.applyStringFlag(name, val, hasEq, next, hasNext)
	}
	return false, ""
}

// applyForge handles --forge, whose value is optional and accepted only in
// the = form, so the flag never consumes the next positional token.
func (p *parsedArgs) applyForge(val string, hasEq bool) (bool, string) {
	if hasEq && val == "" {
		return false, "missing value for flag --forge (use --forge=<name> or bare --forge)"
	}
	p.forge = true
	p.forgeName = val
	return false, ""
}

// applyCatalog handles --catalog[=agent], whose value is optional and accepted
// only in the = form, so the flag never consumes the next positional token.
func (p *parsedArgs) applyCatalog(val string, hasEq bool) (bool, string) {
	if hasEq && val == "" {
		return false, "missing value for flag --catalog (use --catalog=<agent> or bare --catalog)"
	}
	p.catalog = true
	p.catalogAgent = val
	return false, ""
}

// applyMan handles --man[=agent], whose value is optional and accepted only in
// the = form, so the flag never consumes the next positional token.
func (p *parsedArgs) applyMan(val string, hasEq bool) (bool, string) {
	if hasEq && val == "" {
		return false, "missing value for flag --man (use --man=<agent> or bare --man)"
	}
	p.man = true
	p.manAgent = val
	return false, ""
}

// applyAllowLayer handles --allow-layer[=name,name]. The value is optional
// and accepted only in the = form; the flag never consumes the next token.
func (p *parsedArgs) applyAllowLayer(val string, hasEq bool) (bool, string) {
	p.allowLayer = true
	if !hasEq {
		return false, ""
	}
	items := strings.Split(val, ",")
	for _, it := range items {
		if it == "" {
			return false, "empty name in --allow-layer list"
		}
	}
	p.allowLayerList = items
	return false, ""
}

// applyStringFlag handles the value-carrying flags.
func (p *parsedArgs) applyStringFlag(name, val string, hasEq bool, next string, hasNext bool) (bool, string) {
	v, usedNext, err := resolveVal(name, val, hasEq, next, hasNext)
	if err != "" {
		return false, err
	}
	switch name {
	case "agent":
		p.agent = v
	case "id":
		p.id = v
	case "absorb":
		p.absorb = v
	case "mode":
		p.mode = v
	case "_runid":
		p.runID = v
	default:
		return false, "unknown flag --" + name
	}
	return usedNext, ""
}

// resolveVal returns the value of a string flag: the inline =value, otherwise
// the next argv token. A missing value is a usage error.
func resolveVal(name, val string, hasEq bool, next string, hasNext bool) (string, bool, string) {
	if hasEq {
		return val, false, ""
	}
	if hasNext {
		return next, true, ""
	}
	return "", false, "missing value for flag --" + name
}
