# Ann Language Specification v0.1

## Document Authority

This file is the normative Ann interpreter specification. It overrides any description of Ann syntax, semantics, or runtime behavior in any other file, including PLAN.md and arkannie.md. When in doubt, this file wins.

---

## Purpose

Ann is the command language for Arkannie. It provides a minimal, structured syntax for orchestrating wave agents, managing RAM bindings, and expressing control flow. Ann is not a general-purpose language — it is a dispatch language. Its grammar is intentionally small.

---

## Level Architecture

Three levels. Each is a contract, not a description.

| Level | Identity | Lifecycle | Protocol surface |
|---|---|---|---|
| **Level 1 — Arkannie** | Runtime. Interprets Ann. Sole caller of `Agent()`. | Permanent — exists for the full session | None — Arkannie is never the recipient of a protocol envelope |
| **Level 2 — Wave agents** | Ephemeral agents dispatched via `Agent()`. Defined by `[agent].md` + `[agent].annspec.md`. | Spawned per dispatch, destroyed on return | Returns exactly one envelope `{ id, status, payload }` |
| **Level 3 — Sub-agents** | Anonymous workers constructed inline by Level 2. No files. | Spawned inside a wave, invisible to Arkannie | Returns payload to parent Level 2 only |

**Invariant:** Level 1 is Arkannie. Level 2 is a wave. Level 3 is a sub-worker. These are not roles — they are structural positions. An agent cannot change levels during a session.

---

## §1 Lexical Structure

### §1.0 Version Header

Ann programs (`.ann` files) must begin with:

```
# ann v0.1
```

The `#` must be the first character of the file (column 0). Any mismatch is a hard parse error. In interactive mode, the version header is not required and is silently ignored if present.

### §1.1 Comments

```
// this is a comment
```

`//` comments are line-only. Block comments are not supported. Comments may appear anywhere a newline is valid.

### §1.2 Reserved Symbols

| Symbol | Role |
|---|---|
| `[name]` | Command token |
| `{{ key }}` | Template slot — replaced at render time |
| `$name` | RAM binding reference |
| `->` | Handler arrow |
| `{}` | Block delimiter |
| `//` | Comment marker |
| `#` | Version header marker (line 1 only) |
| `--` | Flag prefix |

### §1.3 Language Keywords

The following tokens are reserved and have fixed semantics in the Ann grammar. They cannot be used as binding names.

```
parallel  foreach  loop  success  error  info  each  limit
ask-user  notify  clarify  null
```

---

## §2 Token Grammar

### §2.1 Command Atom

```
[command] arg1 arg2 --flag1 --flag2=value
```

- Command name is `[word]` — alphanumeric plus `-`, no spaces inside brackets
- Arguments are positional strings (no quoting required for single-word values)
- Flags are prefixed with `--`; boolean flags have no value; valued flags use `=`
- A command atom on its own line is a complete statement
- A command atom followed by `->` handlers is a dispatch with result routing

### §2.2 Trinary Handlers

Every wave dispatch may optionally be followed by trinary handlers:

```
[command] args
  success -> { ... }
  error   -> { ... }
  info    -> { ... }
```

- All three handlers are optional
- Handlers execute when the wave returns with the matching `status`
- If a handler is absent and the wave returns that status: `success` with no handler → output discarded silently; `error` with no handler → Class B escalation; `info` with no handler → output discarded silently **unless** `payload.missing_field` is present (Ask Protocol — see §2.7.1), in which case Arkannie always surfaces the message regardless of handler presence
- Handler bodies are Ann blocks — they may contain bindings, commands, and control flow
- Handlers execute in the scope of the enclosing block (see §4)

### §2.3 Binding Assignment

```
$name = [command] args
$name = "literal string"
$name = list("a", "b", "c")
```

- Bindings are RAM-local (see §4.3)
- Left side is `$identifier` — alphanumeric plus `_`, no `-`
- Right side is a command atom, a string literal, or a `list()` constructor
- A binding assignment does not produce output; the result is stored in RAM only
- If the command returns `error`, the binding is not set and error escalation applies

### §2.4 Control Flow Constructs

**parallel block:**

```
parallel {
  [command-a] --id=a args
  [command-b] --id=b args
}
  each -> { ... }
```

- `--id` is required on every dispatch inside `parallel {}` (see §6.1)
- Dispatches run concurrently; Arkannie waits for all before processing `each`
- `each` handler receives one result at a time, in completion order

**foreach:**

```
foreach $list {
  [command] $item
}
```

- Iterates over a `list()` binding
- `$item` is bound to the current element inside the block
- Dispatches are sequential, not concurrent

**loop:**

```
loop limit=N {
  [command] args
  // break condition expressed via success -> {} with explicit stop
}
```

- Executes the block up to N times
- N must be a positive integer; N < 1 is a Class A error
- No implicit break — use `success -> {}` handler with no recursive call to stop

### §2.5 Interpolated String

```
"text with {{ slot }} and $binding references"
```

- `{{ slot }}` — resolved from context_block at render time (see §5)
- `$binding` — resolved from RAM at execution time
- Both may coexist in a single string

### §2.6 list() Constructor

```
$items = list("alpha", "beta", "gamma")
$items = list($a, $b, $c)
```

- Creates a typed list binding
- Elements are strings or RAM references
- Lists are immutable after creation

### §2.7 Context Block

A command atom may be followed by a context block — free-form text that the
wave agent or native command interprets to extract the information it needs.

```
[command] arg1 --flag1 : context text goes here
```

The `:` (colon + space) separates the structured header from the context block.

**Syntax rules:**

- Single-line: context ends at end of line
  ```
  [activity] act-001 --new --priority=high : refactorizar el middleware de auth, tipo simple
  ```
- Multi-line: context continues on subsequent indented lines until a blank line
  or a `->` handler token is encountered
  ```
  [activity] act-001 --new --priority=high :
    Refactorizar el middleware de auth.
    Separar token validation de session handling.
    Tipo simple. Ticket FEAT-89.
  ```
- No context: operations that don't need it omit the `:` entirely
  ```
  [activity] --active
  ```

**Mapping to context_block:**

Arkannie passes the full context text as `context_block.context.text` (plain string).
The positional argument and flags are resolved before dispatch as usual.
Arkannie does not parse or validate the context text — that is the agent's responsibility.

**Agent extraction responsibility:**

The wave agent receives `context.text` and must extract the structured fields it
needs to execute the operation. If a required field cannot be determined from the
text, the agent must return `status: info` with a question (see §2.7.1) rather
than proceeding with missing data or failing silently.

### §2.7.1 Agent Ask Protocol

When a wave agent cannot determine a required field from `context.text`, it returns
`status: info` with a question instead of `status: error`:

```yaml
id: "..."
status: info
payload:
  message: "¿Cuál es el tipo de actividad? (simple | project)"
  missing_field: "type"
  resumable: true
```

**Arkannie behavior on `info` with `missing_field`:**
Arkannie always surfaces the `message` to the developer — it does NOT discard it
silently (exception to the default `info` discard rule in §2.2).
Arkannie waits. The developer re-issues the command with the missing information
added to the context block. Arkannie re-dispatches. No automatic re-dispatch in v0.1.

**`resumable: true`** signals that the agent expects to be re-dispatched with
more context. `resumable: false` (or absent) means the agent gave up — treat
as a terminal info, no re-dispatch expected.

---

## §3 Instruction Classifier

Classification is deterministic. Arkannie checks in order and takes the first match.

```
1. Extract [command] token from the input
2. If .arkannie/[command].annspec.md exists  → wave command  → dispatch via Agent()
3. Else if .arkannie/[command].md exists     → native command → execute with Arkannie's tools
4. Else if token is ask-user, notify, clarify → native keyword → handle directly
5. Else if token is parallel, foreach, loop   → control flow  → handle locally
6. Else → parse error: unknown command [command]
```

**v0.1 command registry** — commands guaranteed present in every arkannie installation:

| Command | Type | Source |
|---|---|---|
| `[mem]` | native | `.arkannie/mem.md` |
| `[personality]` | native | `.arkannie/personality.md` |
| `[ask-user]` | native keyword | built-in |
| `[notify]` | native keyword | built-in |
| `[clarify]` | native keyword | built-in |
| `parallel` | control flow | built-in |
| `foreach` | control flow | built-in |
| `loop` | control flow | built-in |

Wave commands (`[name].annspec.md`) and additional native commands (`[name].md`) are discovered at runtime per §3.1.

### §3.1 Agent Registry Discovery Rules

At startup, Arkannie scans `registry_path` (default: `.arkannie/`) and builds the command registry:

1. For each file matching `[name].annspec.md` → register `[name]` as a wave command
2. For each file matching `[name].md` where `[name]` is not in `CORE_FILES` → register `[name]` as a native command
3. If the same `[name]` appears as both `.annspec.md` and `.md` → `.annspec.md` wins (wave takes precedence); emit a startup warning
4. Built-in commands are always registered regardless of scan results
5. `CORE_FILES = {ann-lang.md, agent-protocol.md, agent-schema.yaml, mem.md, personality.md}` — never registered as commands

---

## §4 Scope Rules

### §4.1 What is a Block

A block is any `{}` delimited body: handler bodies, parallel bodies, foreach bodies, loop bodies. Blocks are the unit of scope.

### §4.2 Binding Visibility

- Bindings created in an outer block are visible to inner blocks
- Bindings created in an inner block are NOT visible to outer blocks
- Bindings created in parallel sub-blocks are NOT visible to sibling sub-blocks
- Bindings created in `each ->` are visible for that one handler execution only

### §4.3 RAM Lifetime

**Interactive mode:** RAM persists across all commands within a single turn. When Arkannie returns to the prompt (turn boundary), RAM is cleared.

**Program mode:** RAM persists for the entire program execution. RAM is cleared on program completion (success or error). Checkpoint protocol in §10 applies.

---

## §5 Template Engine

The template engine resolves slots and bindings in string values before they are passed to wave agents via `context_block` (see §9).

### §5.1 Simple Slot

```
{{ key }}
```

Replaced with the value of `key` from the context_block. If `key` is absent from the context_block, apply §5.4 null handling.

### §5.2 Conditional Block

```
{{#if key}} content {{/if}}
```

Renders `content` only if `key` is present and non-null in context_block. If absent: entire block (including surrounding whitespace) is removed.

### §5.3 Fallback Block

```
{{ key | fallback text }}
```

If `key` is absent or null: renders `fallback text`. If `key` is present: renders its value. `fallback text` is a literal string — it cannot itself contain slots.

### §5.4 Null Handling Summary

| Construct | Key absent | Key null |
|---|---|---|
| `{{ key }}` | render as empty string | render as empty string |
| `{{#if key}}` | skip block | skip block |
| `{{ key \| fallback }}` | render fallback | render fallback |

### §5.5 Render Order

1. Resolve all `$binding` references from RAM
2. Apply conditional blocks (`{{#if}}`)
3. Apply simple slots and fallback slots
4. Remaining unresolved `{{ key }}` slots render as empty string (not an error)

---

## §6 Concurrency

### §6.1 Dispatch Mechanism — id required

Every dispatch inside a `parallel {}` block must carry `--id=<identifier>`. The id is used for correlation (see §3 of agent-protocol.md). Missing id is a parse error.

```
parallel {
  [seeker] query: "auth module" --id=seek-auth
  [reviewer] target: "auth" --id=review-auth
}
  each -> {
    // $result.id identifies which wave returned
  }
```

### §6.2 each Handler Execution

The `each ->` handler is called once per completed dispatch. Arkannie passes:
- `$result.id` — the `--id` value from the originating dispatch
- `$result.status` — `success`, `error`, or `info`
- `$result.payload` — the wave's payload object

The handler body executes serially, in completion order. Arkannie does not re-enter the handler for a new result until the current execution completes.

### §6.3 Completion Rule

`parallel {}` is complete when all dispatched waves have returned (any status). Arkannie then proceeds to the next statement after the block.

### §6.4 info Status in parallel

A wave returning `info` inside `parallel {}` is treated as a non-terminal notification. The wave is considered complete; its result is passed to `each ->` with `status: info`. The block continues to wait for remaining dispatches.

### §6.5 parallel Output

The `parallel {}` block does not produce a binding. Results are accessed only through the `each ->` handler. To accumulate results, create a binding inside `each ->` using `[mem]`.

### §6.6 foreach

```
foreach $list {
  [command] $item
}
```

Sequential. `$item` is auto-bound to the current element. The block body executes once per element. Errors inside the body follow standard handler rules. `foreach` over an empty list is a no-op.

### §6.7 loop limit=N

```
loop limit=5 {
  [command] args
    success -> {
      // to stop: simply do not call loop again
    }
}
```

- Maximum N iterations
- N must be a positive integer; N ≤ 0 is a Class A error (parse time)
- No implicit break condition — the loop runs until limit is reached unless the body contains a path that does not recurse
- To implement conditional stop: use `success -> {}` to capture output and decide whether to continue

### §6.8 Error Escalation Inside parallel

If a dispatch inside `parallel {}` returns `error` and no `each ->` handler is defined → Class B escalation after all dispatches complete. Arkannie reports all errors, proposes recovery, waits.

If `each ->` is defined, the handler is responsible for error handling.

---

## §7 Parse Error Behavior

### §7.1 Error Categories

| Category | Description |
|---|---|
| Syntax error | Malformed token, unclosed block, missing `--id` |
| Unknown command | `[name]` not in registry and not a keyword |
| Type error | Wrong argument type, binding used before set, list operation on non-list |
| Version mismatch | `.ann` file first non-comment line ≠ `# ann v0.1` |

### §7.2 Stop-on-First-Error

Ann is stop-on-first-error for parse errors. When a parse error is detected, Arkannie stops before executing any statement in the block. Already-executing parallel dispatches are allowed to complete before error is reported.

### §7.3 Escalation Class Mapping

| Error category | Class |
|---|---|
| Syntax error in `.ann` file | B |
| Unknown command | B |
| Type error at parse time | A |
| Type error at runtime | A |
| Version mismatch in `.ann` file | B |
| Missing `--id` in `parallel {}` | B |
| `loop limit` ≤ 0 | A |
| Unresolvable `$binding` in `context_block` render | B |

### §7.4 Error Classes Full Protocol

**Class A — Local failure, handle autonomously**

Arkannie resolves, reports, and continues. No developer gate.

Trigger examples:
- Test failure with deterministic fix
- Lint error with auto-fix available
- Coverage miss where threshold is known
- Ann type error (wrong argument type)
- `loop limit` ≤ 0 — reject at parse with message

Arkannie action: fix or skip, emit a brief notice, continue.

**Class B — Shared-state risk, stop and propose**

Arkannie stops execution. If an activity file is open, writes `error_state: [description]` to it. Reports the failure in full. Proposes a recovery path. Waits. Does not execute any recovery without explicit developer instruction.

Trigger examples:
- Wave returns `error` with no handler defined
- `parallel {}` with unhandled error returns
- Push fails
- CI fails after merge
- Binding unresolvable during `context_block` render
- `.ann` version mismatch
- Missing required file at startup

Arkannie action: STOP. Write error_state. Report. Propose. Wait.

**Class C — Irreversible, zero recovery without explicit instruction**

Arkannie stops immediately. No proposal. No error_state write. No further action of any kind. Developer must provide explicit instruction with clear authorization before Arkannie takes any next step.

Trigger examples:
- Production system touch detected
- Force push to protected branch requested
- Rollback to previous release
- Destructive database operation (DROP, TRUNCATE, DELETE without WHERE)

Arkannie action: STOP. State what was detected. Wait for explicit authorization.

---

## §8 Ann v0.1 Construct Status

| Construct | Status | Notes |
|---|---|---|
| `[command] args` | ✅ Supported | Wave or native dispatch |
| `[command] arg : text` | ✅ Supported | Context block — free-form text passed as `context.text` |
| `$name = [command]` | ✅ Supported | Binding from wave return |
| `$name = "literal"` | ✅ Supported | String literal binding |
| `$name = list(...)` | ✅ Supported | List constructor |
| `success -> {}` | ✅ Supported | Handler on wave return |
| `error -> {}` | ✅ Supported | Handler on wave return |
| `info -> {}` | ✅ Supported | Handler on wave return |
| `parallel {}` + `each ->` | ✅ Supported | Concurrent dispatch |
| `foreach $list {}` | ✅ Supported | Sequential iteration |
| `loop limit=N {}` | ✅ Supported | Bounded loop |
| `{{ slot }}` in strings | ✅ Supported | Template slot |
| `{{#if key}} {{/if}}` | ✅ Supported | Conditional render |
| `{{ key \| fallback }}` | ✅ Supported | Fallback slot |
| `[if]` / bare `if` | ❌ Not supported | Use trinary handlers |
| `[while]` | ❌ Not supported | Use `loop limit=N` |
| Nested `parallel {}` | ❌ Not supported | Flat only in v0.1 |
| Anonymous blocks | ❌ Not supported | All blocks must be named |

---

## §9 context_block Canonical Schema

The `context_block` is the structured payload Arkannie sends to a wave agent. It contains all information the wave needs to execute. Arkannie is responsible for constructing it before dispatch.

### §9.1 Serialization Format

`context_block` is serialized as a YAML block. The wave receives it as part of its compiled prompt.

### §9.2 Field Definitions

| Field | Type | Required | Description |
|---|---|---|---|
| `operation` | string | yes | The operation name (matches `operations` key in the agent spec) |
| `context` | map | no | Key-value pairs resolved from RAM or literals. When the dispatch uses `: text` syntax (§2.7), Arkannie adds `context.text` with the full context string. |
| `flags` | list | no | Active `--flag` values from the dispatch atom. Boolean flags: `["flag-name"]`. Valued flags: `["flag-name=value"]`. Example: `--priority=high` → `["priority=high"]`. |
| `output_schema` | string | yes | Verbatim copy of the operation's `output_schema` block |

### §9.3 Binding Serialization

RAM bindings referenced in `context` are serialized at dispatch time:
- String bindings: serialized as YAML strings
- List bindings: serialized as YAML sequences
- Nested maps: serialized as YAML maps

If a binding is referenced in `context` but is null or unset in RAM:
- If `context_block` field is marked optional in the spec → omit the field
- If marked required → Class B escalation before dispatch

### §9.4 Empty and Missing Fields

- `context: {}` is valid (no context provided)
- `flags: []` is valid (no flags active)
- `output_schema` must always be present — absent is a pre-dispatch Class B failure

### §9.5 Example Full Block

```yaml
operation: analyze
context:
  target: "src/auth/"
  depth: "full"
flags:
  - verbose
output_schema: |
  success:
    findings: list of finding objects
    summary: string
  error:
    reason: string
    recoverable: boolean
  info:
    message: string
```

### §9.6 Passing Bindings Explicitly

To pass a RAM binding into a wave:

```
$report = [seeker] query: "auth"
[reviewer] target: $report --id=review
```

Arkannie resolves `$report` at dispatch time and serializes it into the `context_block.context.target` field.

---

## §10 RAM Checkpoint Protocol

### §10.1 The Problem

In `.ann` program mode, if Arkannie is interrupted between a wave dispatch and the use of its return value, RAM state is lost. This protocol prevents data loss.

### §10.2 Checkpoint Trigger

A checkpoint is written before any wave dispatch that meets all three conditions:
1. Executing in `.ann` program mode
2. A subsequent statement in the same block references the dispatch's binding (`$name`)
3. The dispatch has not yet been sent

### §10.3 Checkpoint Schema

Written to `.mem/checkpoints/[program-name]-[timestamp].yaml`:

```yaml
program: string           # .ann file path
timestamp: ISO8601
last_completed_step: int  # 0-indexed step number in program
bindings:                 # snapshot of all RAM bindings at checkpoint time
  $name: value
  ...
```

### §10.4 Recovery

On program restart after interruption:
1. Arkannie checks for a checkpoint file matching the program name
2. If found: loads bindings from checkpoint, resumes from `last_completed_step + 1`
3. If not found: starts from the beginning

### §10.5 Cleanup

Checkpoint files are deleted on:
- Successful program completion
- Developer-initiated `[mem] delete` targeting the checkpoint
- Explicit `--clean-checkpoints` flag on `arkanniestartup.py`

Checkpoint files are NOT deleted on error — they exist to enable recovery.
