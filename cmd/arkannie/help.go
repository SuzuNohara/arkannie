package main

import (
	"fmt"
	"io"
)

// printHelp writes the full Ann language tutorial to w. It is the immediate
// response to `arkannie --help` and never touches the registry or filesystem.
func printHelp(w io.Writer) {
	fmt.Fprint(w, annHelp)
}

// annHelp is a self-contained tutorial for the Ann v0.3 command language and
// the arkannie CLI. Code examples are shown indented (no fences) so the whole
// text fits in one Go raw string.
const annHelp = `arkannie — stateless AI agent harness · Ann language tutorial (v0.3)

WHAT IS ANN
  Ann is a small dispatch language, not a general-purpose one. A program
  orchestrates wave agents (each a claude process), passes results through
  RAM bindings, and defines exactly what goes to the output.

  Three structural levels:
    Level 1  arkannie      the runtime; interprets Ann; the only caller of agents
    Level 2  wave agent an ephemeral agent dispatched per command; returns one
                        envelope {id, status, payload}
    Level 3  sub-agent  an anonymous worker a wave spawns internally

RUNNING
  Program mode (a .ann file; each dispatch names its own agent):
    arkannie --id <id> program.ann

  Prompt mode (free text against one agent):
    arkannie --agent <name> --id <id> "your prompt here"

  --id is REQUIRED for every execution. It names the output file:
  .output/<id>.md. The newest run keeps the clean name; on a name clash the
  previous file is archived to .output/<id>-N.md.

  Other flags / subcommands:
    --detach          print the output path and run in the background
    --interpret       on a parse error, ask claude to repair the program once
    --allow-workspace let executor-scoped agents write in the invoker's cwd
    --forge[=name]    open an interactive Agent Forge session; =name targets
                      an existing agent (value accepted in the = form only)
    --absorb=<path>   absorb a codebase into the Forge session (requires
                      --forge)
    --mode=<complete|fragment|layer>
                      how the codebase is absorbed (requires --absorb)
    --allow-layer[=name,name]
                      consent to dispatch layer agents (optionally only the
                      named ones; value accepted in the = form only)
    --catalog[=agent] print the agent capability catalog — every agent's
                      calling card, or just <agent> (value in the = form only)
    --man[=agent]     print the per-agent execution manual — full dispatch and
                      per-operation detail, or just <agent> (= form only)
    --check <prog.ann>
                      parse-check a program without running it (syntax only; no
                      agents, no output file); exit 0 OK, 1 on a parse error
    validate [--agent=<name>]   check agent contracts under .agents/
    --version         print the arkannie version and exit
    --help, -h        print this tutorial

  Exit codes:  0 success · 1 error · 2 info · 64 usage error

PROGRAM STRUCTURE
  A .ann file must begin with the version header on line 1 (column 0):
    # ann v0.3
  Line comments use //. There are no block comments.

STRINGS, ESCAPES and MULTI-LINE CONTEXT
  String literals use "double quotes" and recognize three escapes; everything
  else is literal:
    $q = "with \"quotes\" and \\backslash"   // -> with "quotes" and \backslash
    $p = "costs \$5, not interpolated"        // -> costs $5 (the $ is literal)
  \" and \\ give a literal quote and backslash; \$ yields a literal $ and turns
  OFF interpolation at that spot. Any other \X (e.g. \q) is a syntax error.
  {{ slots }} and real $refs inside a string are carried verbatim (real $refs
  still resolve).

  A context block (after ": ") may span lines. It starts at the first indented
  line and ends at the first dedent, a } line, or a line containing ->. Internal
  blank lines are KEPT (they separate paragraphs) and deeper indentation is
  preserved relative to the block's first line:
    [activity] act-004 :
      intro paragraph

      - item with detail
          nested note

DISPATCH — the command atom
  A dispatch invokes one agent operation:
    [seeker] "query" --depth=2 --id=find : some context text

  Parts: [command] is the agent; positional args and --flags follow; --id
  labels the dispatch; everything after ": " is verbatim context (may span
  lines). $refs inside args or context are substituted from RAM.

TRINARY HANDLERS
  Every agent returns success, error, or info. Attach up to three handlers:
    [seeker] : find the config
      success -> {
        [writer] : use $result.payload.result
      }
      error -> {
        [notify] could not find it
      }
      info -> { }
  Inside a handler, $result exposes {id, status, payload}. An error with no
  error handler escalates and fails the run.

BINDINGS (RAM) and $result
    $x = "a literal string"
    $items = list("a", "b", $x)
    $r = [seeker] : find it        // $r holds the success payload
  Names are [A-Za-z0-9_]+ and cannot be reserved keywords. Every block { }
  is a scope: bindings created inside it vanish when it closes.

BUILDING DATA (list, concat, map)
  Three data constructors nest freely; an element is a literal, a $ref (dotted
  or not), or a nested constructor:
    $items  = list("a", list("b"), $r.items)      // ordered list; nests lists
    $joined = concat($items, "x")                  // flattens ONE level, in order
    $cfg    = map(k: "v", n: $r.campo)             // ordered key->value map
  concat spreads a list argument's elements and drops a non-list argument in
  place, flattening exactly one level. map keys are identifiers; a duplicate or
  malformed key is a syntax error. An unresolvable $ref element is omitted with a
  Class A notice (it does not become an empty string). concat and map are only
  constructors right before "("; as bare words or inside text they are literal.

DOT ACCESS ($ref.seg.seg)
  A binding that holds a map is read field-by-field with dots. A dotted path
  resolves to the VALUE of that field — there is no "whole envelope"
  shorthand:
    [echo] : status is $r.status              // inlines the string value
    [writer] : the answer is $r.payload.out   // walks payload, then out
  Each segment indexes one level into a map by key. If an intermediate step
  is not a map, or the key is missing, the path does not resolve. In context
  text an unresolved path is a pre-dispatch error naming the base and the
  failing segment.

CONTROL FLOW
  Sequential iteration over a list binding (accepts a dotted path too):
    foreach $r.items {
      [worker] : $item
    }
  Bounded repetition:
    loop limit=3 {
      [worker] : retry
    }
  Retry-until-success — the canonical poll loop. The until guard runs AFTER
  the body of each iteration, so it observes the bindings the body just made;
  if it holds, the loop breaks early:
    loop limit=5 until $r.status == "success" {
      $r = [seeker] : poll
    }
  Without until, the loop runs exactly N times.
  Concurrent dispatch (every dispatch needs a unique --id):
    parallel {
      [a] --id=one : x
      [b] --id=two : y
    }
      each -> {
        [notify] : $result.payload.out
      }
  Dynamic fan-out — run ONE template once per list element. --id is the base;
  the runtime synthesizes W-1, W-2, … (1-based). $item and $index are bound per
  element and live only for the statement. Though it runs in parallel, the report
  is assembled in index order (deterministic):
    parallel foreach $r.items --id=W {
      [echo] : "$item @ $index"
    }
      each -> {
        [notify] : "$result"
      }
  A non-list binding is a Class A skip; an empty list runs nothing. A plain
  foreach stays sequential — only "parallel foreach" fans out.

CONDITIONALS (if / else)
  A deterministic guard picks one branch. The guard is a single comparison,
  == or != only, between two operands; an operand is a $ref (dotted or not),
  a string literal, or null. There are no compound operators (no &&, ||):
    if $r.status == "success" {
      [notify] $r.payload.result
    }
    else {
      [ask-user] retry
    }
  null == null is true; an unresolved $ref is null, so $missing == null holds.
  If an operand resolves to a map or list it is not comparable: the whole if
  is skipped (a local, non-escalating notice) and the program continues.

DECLARATIVE RETRY (--retry / --backoff)
  A single dispatch can retry itself without writing a loop:
    [seeker] fetch --id=q --retry=2 --backoff=1
      error -> { [notify] : "exhausted after 3 attempts" }
  --retry=N allows up to 1+N attempts, retrying only a RECOVERABLE error
  (payload.recoverable: true) or a timeout; a non-recoverable error is not
  retried. --backoff=S waits linearly (S, 2S, … seconds) before each retry. When
  retries run out the last error is still catchable by error -> {}. Only agnostic
  agents: --retry on an executor is a Class B stop (nothing is dispatched). Use
  --retry for a transient failure of the same dispatch; use loop ... until when
  you poll a condition you evaluate yourself.

MODULE COMPOSITION (call)
  call runs another .ann as a function: isolated RAM and an explicit return.
    $sub = call "sub.ann"   // binds the module's return value
    call "sub.ann"          // runs the module, binds nothing
  The child cannot see the parent's bindings and its own never leak back. A single
  child [return] becomes the value; several labeled [return]s become a map keyed
  by --id; the child's [return]s never appear in the parent report. Depth is fixed
  at 1 (no nested call, no recursion, no argument passing in v0.3). The path is
  relative to the program dir and may not escape it; a bad path, a missing module,
  or a wrong version header stops the run (Class B).

MULTIPLE AGENTS IN ONE PROGRAM
  A single .ann may dispatch different agents — just name them. Each [command]
  resolves to its own registered agent under .agents/:
    # ann v0.3
    $a = [researcher] : gather sources
    $b = [summarizer] : condense $a
    [return] --id=research $a
    [return] --id=summary  $b
  The output frontmatter lists every agent the program used.

[return] — the output indicator
  The program decides what appears in the output. Success payloads are NOT
  dumped automatically: bind them, then emit them explicitly with [return].
    [return] $summary              // single return: no heading, just content
    [return] --id=result $summary  // section titled "## result"
    [return] "a fixed note"        // string literal, verbatim
  A [return] takes one operand: a $binding or a string literal. Bindings that
  hold a map or list render as a fenced YAML block; strings render verbatim.
  A program with no [return] produces an empty body.

  --id here is the output SECTION label (not the CLI --id). Rules, checked at
  parse time:
    - a single return may omit --id (its section has no heading);
    - with two or more returns, EVERY return must have --id;
    - all --id values must be unique;
    - a return inside a foreach/loop/each requires --id; each run emits its
      own numbered section: --id-1, --id-2, …
  Violating any rule is a compile error.

NATIVE KEYWORDS
    [ask-user] <text>   surface a question; the run stops with info status
    [notify] <text>     add a note to the report's Notices section
    [clarify] <text>    same as notify, for clarifying remarks
    [return] <operand>  emit an output block (see above)

VALIDATING WITHOUT RUNNING (--check)
  Parse-check a program before running it, with zero side effects — no
  registry load, no claude healthcheck, no dispatch, no .output/ or .mem/:
    arkannie --check program.ann
  A clean parse prints OK with the disclaimer
  "syntax only — no agents were run" and exits 0; a parse error goes to
  stderr as "parse error at L:C [category]: message" and exits 1. --check is
  mutually exclusive with the execution flags (--agent, --forge, --detach,
  --interpret); an invalid combination is a usage error (exit 64).

OUTPUT FILE (.output/<id>.md)
  Frontmatter: id, agent(s), status, started, finished, input. Body: the
  concatenated [return] blocks (plus any Question / Notices). Credential-shaped
  content is redacted before anything is written.
`
