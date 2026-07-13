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

// annHelp is a self-contained tutorial for the Ann v0.2 command language and
// the arkannie CLI. Code examples are shown indented (no fences) so the whole
// text fits in one Go raw string.
const annHelp = `arkannie — stateless AI agent harness · Ann language tutorial (v0.2)

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
    # ann v0.2
  Line comments use //. There are no block comments.

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
        [writer] : use $result.payload
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

CONTROL FLOW
  Sequential iteration over a list binding:
    foreach $items {
      [worker] : $item
    }
  Bounded repetition:
    loop limit=3 {
      [worker] : retry
    }
  Concurrent dispatch (every dispatch needs a unique --id):
    parallel {
      [a] --id=one : x
      [b] --id=two : y
    }
      each -> {
        [notify] : $result.payload
      }

MULTIPLE AGENTS IN ONE PROGRAM
  A single .ann may dispatch different agents — just name them. Each [command]
  resolves to its own registered agent under .agents/:
    # ann v0.2
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

OUTPUT FILE (.output/<id>.md)
  Frontmatter: id, agent(s), status, started, finished, input. Body: the
  concatenated [return] blocks (plus any Question / Notices). Credential-shaped
  content is redacted before anything is written.
`
