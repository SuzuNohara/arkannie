// Package template implements the Ann v0.2 template engine (spec §5).
//
// Rendering is a pure function of the template text and the slot map.
// Per §5.5 the caller resolves $bindings before calling Render; here
// conditional blocks are applied first, then simple and fallback slots,
// and any slot left unresolved renders as the empty string.
package template

import "strings"

// Render substitutes slots into tmpl following spec §5.
//
// Supported constructs:
//
//	{{ key }}              -> slots[key]; "" when absent or empty (§5.1, §5.4)
//	{{#if key}}...{{/if}}  -> content when key is present and non-empty,
//	                          otherwise the whole block plus its
//	                          surrounding whitespace is removed (§5.2)
//	{{ key | fallback }}   -> slots[key] when present and non-empty,
//	                          otherwise the literal fallback text (§5.3)
//
// A key mapped to "" is equivalent to an absent key (§5.4). Slot values
// and fallback text are emitted verbatim and never re-scanned, so
// fallback text cannot expand nested slots.
func Render(tmpl string, slots map[string]string) string {
	return renderSlots(renderConditionals(tmpl, slots), slots)
}

const (
	openIf  = "{{#if "
	closeIf = "{{/if}}"
)

// condBlock is one {{#if key}}...{{/if}} occurrence inside a template.
type condBlock struct {
	start   int    // index of "{{#if"
	end     int    // index just past "{{/if}}"
	key     string // trimmed condition key
	content string // raw text between the open and close tags
}

// renderConditionals applies every conditional block in tmpl (§5.2).
func renderConditionals(tmpl string, slots map[string]string) string {
	var b strings.Builder
	pos := 0
	for {
		blk, found := findConditional(tmpl, pos)
		if !found {
			b.WriteString(tmpl[pos:])
			return b.String()
		}
		if v, ok := slots[blk.key]; ok && v != "" {
			b.WriteString(tmpl[pos:blk.start])
			b.WriteString(blk.content)
			pos = blk.end
			continue
		}
		left, right := trimSurrounding(tmpl, pos, blk.start, blk.end)
		b.WriteString(tmpl[pos:left])
		pos = right
	}
}

// findConditional locates the first conditional block at or after from.
func findConditional(tmpl string, from int) (condBlock, bool) {
	rel := strings.Index(tmpl[from:], openIf)
	if rel < 0 {
		return condBlock{}, false
	}
	start := from + rel
	keyStart := start + len(openIf)
	keyLen := strings.Index(tmpl[keyStart:], "}}")
	if keyLen < 0 {
		return condBlock{}, false
	}
	contentStart := keyStart + keyLen + len("}}")
	contentLen := strings.Index(tmpl[contentStart:], closeIf)
	if contentLen < 0 {
		return condBlock{}, false
	}
	return condBlock{
		start:   start,
		end:     contentStart + contentLen + len(closeIf),
		key:     strings.TrimSpace(tmpl[keyStart : keyStart+keyLen]),
		content: tmpl[contentStart : contentStart+contentLen],
	}, true
}

// trimSurrounding widens a removed block's span over the whitespace that
// surrounds it, so a block standing on its own line leaves no phantom
// blank line behind (§5.2). left never crosses min, the start of the
// not-yet-consumed segment of tmpl.
func trimSurrounding(tmpl string, min, start, end int) (left, right int) {
	left = start
	for left > min && isBlank(tmpl[left-1]) {
		left--
	}
	right = end
	for right < len(tmpl) && isBlank(tmpl[right]) {
		right++
	}
	atLineStart := left == 0 || tmpl[left-1] == '\n'
	if atLineStart && right < len(tmpl) && tmpl[right] == '\n' {
		right++
	}
	return left, right
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// renderSlots applies simple and fallback slots left to right (§5.1,
// §5.3). Substituted text is written straight to the output and never
// re-scanned, which keeps slot values and fallback text literal.
func renderSlots(tmpl string, slots map[string]string) string {
	var b strings.Builder
	pos := 0
	for {
		rel := strings.Index(tmpl[pos:], "{{")
		if rel < 0 {
			b.WriteString(tmpl[pos:])
			return b.String()
		}
		start := pos + rel
		b.WriteString(tmpl[pos:start])
		inner, end, ok := matchSlot(tmpl, start)
		if !ok {
			b.WriteString("{{")
			pos = start + len("{{")
			continue
		}
		b.WriteString(resolveSlot(inner, slots))
		pos = end
	}
}

// matchSlot finds the "}}" matching the "{{" at start, tolerating nested
// brace pairs so that fallback text such as "{{ a | {{ b }} }}" is
// captured whole. inner excludes the outer braces.
func matchSlot(tmpl string, start int) (inner string, end int, ok bool) {
	depth := 0
	for i := start; i+1 < len(tmpl); {
		switch {
		case tmpl[i] == '{' && tmpl[i+1] == '{':
			depth++
			i += 2
		case tmpl[i] == '}' && tmpl[i+1] == '}':
			depth--
			i += 2
			if depth == 0 {
				return tmpl[start+len("{{") : i-len("}}")], i, true
			}
		default:
			i++
		}
	}
	return "", 0, false
}

// resolveSlot renders the inside of one slot per the §5.4 table: the
// value when present and non-empty, the literal fallback when one is
// declared, otherwise the empty string.
func resolveSlot(inner string, slots map[string]string) string {
	key, fallback, hasFallback := strings.Cut(inner, "|")
	if v, ok := slots[strings.TrimSpace(key)]; ok && v != "" {
		return v
	}
	if hasFallback {
		return strings.TrimSpace(fallback)
	}
	return ""
}
