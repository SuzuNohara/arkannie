package ann

import (
	"fmt"
	"strings"
)

// tokKind enumerates the lexical token kinds of Ann v0.1 (§1, §2).
type tokKind int

const (
	tkCommand     tokKind = iota // [name]
	tkIdent                      // bare word (args, keywords, numbers)
	tkFlag                       // --name or --name=value (text: "name" or "name=value")
	tkString                     // "..." (text: content without quotes, verbatim)
	tkBinding                    // $name (text: name without $)
	tkArrow                      // ->
	tkLBrace                     // {
	tkRBrace                     // }
	tkAssign                     // =
	tkComma                      // ,
	tkRParen                     // )
	tkListOpen                   // list(
	tkContext                    // single-line context text after ": " (verbatim)
	tkContextOpen                // ":" at end of line — multi-line context follows
)

type token struct {
	kind tokKind
	text string
	line int // 1-based
	col  int // 1-based
}

// lineLexer tokenizes a single source line. Context blocks and blank lines
// are line-level constructs, so the parser drives the lexer per line.
type lineLexer struct {
	src  string
	pos  int
	line int
}

// lexLine tokenizes one line. Comments (// outside strings) end the line.
// A context token (single or multi-line opener) is always the last token.
func lexLine(src string, line int) ([]token, *ParseError) {
	lx := &lineLexer{src: src, line: line}
	var toks []token
	for {
		tok, done, err := lx.next()
		if err != nil {
			return nil, err
		}
		if done {
			return toks, nil
		}
		toks = append(toks, tok)
		if tok.kind == tkContext || tok.kind == tkContextOpen {
			return toks, nil
		}
	}
}

func (lx *lineLexer) next() (token, bool, *ParseError) {
	lx.skipSpaces()
	if lx.pos >= len(lx.src) || strings.HasPrefix(lx.src[lx.pos:], "//") {
		return token{}, true, nil
	}
	c := lx.src[lx.pos]
	col := lx.pos + 1
	switch {
	case c == '[':
		return lx.lexCommand(col)
	case c == '"':
		return lx.lexString(col)
	case c == '$':
		return lx.lexBinding(col)
	case strings.HasPrefix(lx.src[lx.pos:], "->"):
		lx.pos += 2
		return token{kind: tkArrow, line: lx.line, col: col}, false, nil
	case strings.HasPrefix(lx.src[lx.pos:], "--"):
		return lx.lexFlag(col)
	case c == ':':
		return lx.lexContext(col)
	case c == '{' || c == '}' || c == '=' || c == ',' || c == ')':
		lx.pos++
		return token{kind: symbolKind(c), line: lx.line, col: col}, false, nil
	default:
		return lx.lexWord(col)
	}
}

func symbolKind(c byte) tokKind {
	switch c {
	case '{':
		return tkLBrace
	case '}':
		return tkRBrace
	case '=':
		return tkAssign
	case ',':
		return tkComma
	default: // ')'
		return tkRParen
	}
}

// lexCommand reads a [name] token: alphanumeric plus '-', no spaces (§2.1).
func (lx *lineLexer) lexCommand(col int) (token, bool, *ParseError) {
	end := strings.IndexByte(lx.src[lx.pos:], ']')
	if end < 0 {
		return token{}, false, lx.errf(col, "unterminated [command] token")
	}
	name := lx.src[lx.pos+1 : lx.pos+end]
	if !isCommandName(name) {
		return token{}, false, lx.errf(col, "invalid command name [%s]", name)
	}
	lx.pos += end + 1
	return token{kind: tkCommand, text: name, line: lx.line, col: col}, false, nil
}

// lexString reads a quoted string. Content is verbatim: {{ }} slots, $refs
// and // sequences inside strings are preserved untouched (§2.5).
func (lx *lineLexer) lexString(col int) (token, bool, *ParseError) {
	rest := lx.src[lx.pos+1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return token{}, false, lx.errf(col, "unterminated string literal")
	}
	lx.pos += end + 2
	return token{kind: tkString, text: rest[:end], line: lx.line, col: col}, false, nil
}

// lexBinding reads $name: alphanumeric plus '_' (§2.3).
func (lx *lineLexer) lexBinding(col int) (token, bool, *ParseError) {
	i := lx.pos + 1
	for i < len(lx.src) && isBindingChar(lx.src[i]) {
		i++
	}
	if i == lx.pos+1 {
		return token{}, false, lx.errf(col, "empty binding name after $")
	}
	name := lx.src[lx.pos+1 : i]
	lx.pos = i
	return token{kind: tkBinding, text: name, line: lx.line, col: col}, false, nil
}

// lexFlag reads --name or --name=value; the value runs to whitespace.
func (lx *lineLexer) lexFlag(col int) (token, bool, *ParseError) {
	i := lx.pos + 2
	start := i
	for i < len(lx.src) && isFlagNameChar(lx.src[i]) {
		i++
	}
	if i == start {
		return token{}, false, lx.errf(col, "empty flag name after --")
	}
	if i >= len(lx.src) || lx.src[i] != '=' {
		text := lx.src[start:i]
		lx.pos = i
		return token{kind: tkFlag, text: text, line: lx.line, col: col}, false, nil
	}
	i++ // consume '='
	for i < len(lx.src) && !isSpace(lx.src[i]) {
		i++
	}
	text := lx.src[start:i]
	lx.pos = i
	return token{kind: tkFlag, text: text, line: lx.line, col: col}, false, nil
}

// lexContext handles the ": " separator (§2.7). A colon at end of line (or
// followed only by spaces) opens a multi-line context block; a colon
// followed by a space yields the rest of the line verbatim.
func (lx *lineLexer) lexContext(col int) (token, bool, *ParseError) {
	rest := lx.src[lx.pos+1:]
	if strings.TrimSpace(rest) == "" {
		lx.pos = len(lx.src)
		return token{kind: tkContextOpen, line: lx.line, col: col}, false, nil
	}
	if rest[0] != ' ' {
		return token{}, false, lx.errf(col, "context separator ':' must be followed by a space")
	}
	lx.pos = len(lx.src)
	return token{kind: tkContext, text: rest[1:], line: lx.line, col: col}, false, nil
}

// lexWord reads a bare word: letters, digits, '_', '-', '.', '/'.
// "list(" is recognized as the list constructor opener (§2.6).
func (lx *lineLexer) lexWord(col int) (token, bool, *ParseError) {
	i := lx.pos
	for i < len(lx.src) && isWordChar(lx.src[i]) {
		if lx.src[i] == '-' && i+1 < len(lx.src) && lx.src[i+1] == '>' {
			break // don't swallow the -> arrow
		}
		i++
	}
	if i == lx.pos {
		return token{}, false, lx.errf(col, "unexpected character %q", lx.src[lx.pos])
	}
	word := lx.src[lx.pos:i]
	lx.pos = i
	if word == "list" && lx.pos < len(lx.src) && lx.src[lx.pos] == '(' {
		lx.pos++
		return token{kind: tkListOpen, text: word, line: lx.line, col: col}, false, nil
	}
	return token{kind: tkIdent, text: word, line: lx.line, col: col}, false, nil
}

func (lx *lineLexer) skipSpaces() {
	for lx.pos < len(lx.src) && isSpace(lx.src[lx.pos]) {
		lx.pos++
	}
}

func (lx *lineLexer) errf(col int, format string, args ...any) *ParseError {
	return &ParseError{Line: lx.line, Col: col, Category: Syntax, Msg: fmt.Sprintf(format, args...)}
}

const versionHeader = "# ann v0.1"

// checkHeader enforces §1.0: in ProgramMode the first non-comment line must
// be exactly "# ann v0.1" at column 0. In PromptMode a leading header line
// is optional and silently ignored. It returns the index of the first line
// to parse.
func checkHeader(lines []string, mode Mode) (int, *ParseError) {
	i := nextContent(lines, 0)
	if i >= len(lines) {
		if mode == ProgramMode {
			return 0, perrf(len(lines), 1, VersionMismatch, "missing version header %q", versionHeader)
		}
		return i, nil
	}
	if lines[i] == versionHeader {
		return i + 1, nil
	}
	if mode == PromptMode {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			return i + 1, nil // header-ish line: ignored in interactive mode
		}
		return i, nil
	}
	return 0, perrf(i+1, 1, VersionMismatch,
		"first non-comment line must be %q at column 0", versionHeader)
}

// nextContent returns the index of the next line that is neither blank nor
// a comment, starting at i.
func nextContent(lines []string, i int) int {
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t != "" && !strings.HasPrefix(t, "//") {
			return i
		}
		i++
	}
	return i
}

// collectContext gathers the indented lines of a multi-line context block
// (§2.7) starting at pos. The block ends at a blank line (consumed) or at a
// line that is not indented, closes a block, or contains a '->' handler
// token. It returns the verbatim text and the next line index.
func collectContext(lines []string, pos int) (string, int) {
	var parts []string
	for pos < len(lines) {
		raw := lines[pos]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			pos++
			break
		}
		if !isIndented(raw) || trimmed == "}" || strings.Contains(raw, "->") {
			break
		}
		parts = append(parts, trimmed)
		pos++
	}
	return strings.Join(parts, "\n"), pos
}

// isHandlerLine reports whether the line starts one of the given handlers
// ("name -> ..." after indentation).
func isHandlerLine(raw string, names ...string) bool {
	trimmed := strings.TrimSpace(raw)
	for _, n := range names {
		rest, ok := strings.CutPrefix(trimmed, n)
		if ok && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "->") {
			return true
		}
	}
	return false
}

func isIndented(raw string) bool {
	return raw != "" && (raw[0] == ' ' || raw[0] == '\t')
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isCommandName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isAlnum(s[i]) && s[i] != '-' {
			return false
		}
	}
	return true
}

func isBindingChar(c byte) bool  { return isAlnum(c) || c == '_' }
func isFlagNameChar(c byte) bool { return isAlnum(c) || c == '-' }

func isWordChar(c byte) bool {
	return isAlnum(c) || c == '_' || c == '-' || c == '.' || c == '/'
}
