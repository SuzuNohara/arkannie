# Agent Protocol v0.1

## Document Authority

This file is the normative specification for all inter-agent communication in arkannie. It overrides any description of envelope format, timeout behavior, or grant inheritance in any other file.

---

## §1 Output Envelope

Every wave agent (Level 2) must return exactly one envelope. The envelope is a structured object with three top-level fields.

```yaml
id: string          # mirrors the --id from the dispatch atom
status: string      # one of: success | error | info
payload: object     # content varies by status — see §1.2
```

### §1.1 Field Rules

| Field | Required | Constraint |
|---|---|---|
| `id` | yes | Must match the `--id` value from the originating dispatch exactly |
| `status` | yes | Must be one of the three literal strings: `success`, `error`, `info` |
| `payload` | yes | Must be an object (map); may be empty `{}` but never null or absent |

An envelope that violates any field rule is a structural violation → Class B escalation (see §2).

### §1.2 Payload Contracts

**`status: success`** — The operation completed as intended.

```yaml
payload:
  # Fields defined in the operation's output_schema.success block
  # Arkannie does not inspect field VALUES, but success matching is STRICT on
  # SHAPE: every declared field must be present and correctly typed, and any
  # field NOT in the schema is a contract violation (guards against silent
  # field drift between heterogeneous agents). A `success: {}` schema is the
  # permissive escape hatch and accepts any object.
```

**`status: error`** — The operation failed.

```yaml
payload:
  reason: string        # required — human-readable failure description
  recoverable: boolean  # required — true if retry is meaningful
  detail: string        # optional — additional context, stack trace, etc.
```

**`status: info`** — A non-terminal notification. The agent is still running (for streaming) or providing a status update. Not a completion signal.

```yaml
payload:
  message: string        # required — the notification content
  step: string           # optional — current step name or identifier
  missing_field: string  # optional — Ask Protocol: name of the field the agent needs
  resumable: boolean     # optional — Ask Protocol: true = agent expects re-dispatch with more context
```

> **Matching rigor:** `success` is matched strictly (exact shape, unknown fields
> rejected). `info` is matched **laxly** — declared fields are still required and
> typed, but extra fields are allowed so the Ask Protocol may add
> `missing_field`/`resumable` beyond the declared `message`. `error` payloads are
> not schema-matched (only the structural `reason`+`recoverable` check applies).

If `missing_field` is present, Arkannie treats this as an Ask Protocol response (see ann-lang.md §2.7.1):
Arkannie surfaces `message` to the developer and waits — it does NOT discard the notification silently.

---

## §2 Arkannie Validation on Receipt

Arkannie performs structural validation on every returned envelope before routing to handlers.

Validation steps (in order):

1. Confirm envelope is a map (not a string, list, or null)
2. Confirm `id` field is present and is a string
3. Confirm `status` field is present and is one of `{success, error, info}`
4. Confirm `payload` field is present and is a map
5. If `status: error`: confirm `payload.reason` (string) and `payload.recoverable` (boolean) are present

If any check fails → Class B escalation. Arkannie does not attempt to route to handlers. Arkannie reports:

```
Wave [name] (--id=[id]) returned a malformed envelope.
Violation: [specific check that failed]
Expected envelope schema:
  id: string
  status: success | error | info
  payload: object
Raw return: [verbatim envelope]
```

Arkannie then waits for developer instruction.

---

## §3 Correlation in parallel {}

When dispatches run inside a `parallel {}` block, Arkannie uses the `id` field to correlate each returned envelope with its originating dispatch.

Correlation rules:

1. Arkannie maintains a registry of `{ --id: dispatch_spec }` for the duration of the `parallel {}` block
2. When an envelope arrives, Arkannie looks up `envelope.id` in the registry
3. If found: route to `each ->` handler with `$result.id`, `$result.status`, `$result.payload`
4. If not found: Class B escalation — orphan envelope (id does not match any active dispatch)
5. Duplicate id in a single `parallel {}` block is a parse error (detected before dispatch)

---

## §4 Timeout Protocol

### §4.1 Timeout Hierarchy

Arkannie applies timeouts at three levels, evaluated in priority order (highest wins):

| Level | Source | Default |
|---|---|---|
| Per-operation | `--timeout=N` flag on the dispatch atom | none |
| Per-command | `timeout:` field in `[agent].annspec.md` | none |
| Global default | `timeout_default` in `arkannie.config.yaml` | 120s |

If none of the above is set → no timeout applied (dispatch runs indefinitely).

### §4.2 --timeout Flag

`--timeout=N` on a dispatch atom sets a per-dispatch timeout in seconds. N must be a positive integer. N ≤ 0 is a Class A error.

```
[seeker] query: "auth" --id=seek --timeout=30
```

### §4.3 Timeout Escalation

When a dispatch times out:

1. Arkannie cancels the dispatch (best-effort — no guarantee the Agent() call terminates)
2. Synthesizes a timeout envelope:
   ```yaml
   id: [the --id value]
   status: error
   payload:
     reason: "Dispatch timed out after Ns"
     recoverable: true
   ```
3. Routes the synthetic envelope to the `error ->` handler if defined
4. If no `error ->` handler: Class B escalation

---

## §5 Pre-Dispatch Failure Protocol

A pre-dispatch failure occurs when Arkannie cannot construct or send the dispatch. The wave Agent() call is never made.

### §5.1 Type 3 Flag Failures

Type 3 failures are detected at parse time when the dispatch atom is classified.

| Failure | Example | Class |
|---|---|---|
| Missing `--id` in `parallel {}` | `[seeker] query: "x"` with no `--id` | B |
| `--timeout` ≤ 0 | `--timeout=0` | A |
| Unknown `--flag` not in spec | `--verbose` on a spec that defines no flags | B |

For Class B: Arkannie stops, reports the exact dispatch line and flag, waits.
For Class A: Arkannie emits an error message and skips the dispatch.

### §5.2 Other Pre-Dispatch Failures

| Failure | Cause | Class |
|---|---|---|
| Agent spec file missing | `.annspec.md` existed at startup but not at dispatch time | B |
| context_block render failure | Required binding is unresolvable (see §9.3 of ann-lang.md) | B |
| `output_schema` absent | Not defined in the operation spec | B |

---

## §6 Grant Inheritance — Level 3 Workers

Level 3 workers are anonymous agents constructed inline by Level 2 waves. They do not have spec files.

### §6.1 Subset Inheritance Rules

A Level 3 worker may only receive a subset of its parent Level 2's grants. It cannot receive grants the parent does not have.

Default inheritance model: **read-only subset**.

| Parent grant | Level 3 inherits |
|---|---|
| Read (files, registry) | Yes — read only |
| Write (files) | No — not inherited unless explicitly granted |
| Execute (Bash) | No — not inherited unless explicitly granted |
| Network | No — not inherited |

### §6.2 Explicit Override

A Level 2 wave may explicitly grant write or execute to a Level 3 worker by including a `grants:` block in the sub-agent construction prompt:

```
grants:
  - write: [specific path pattern]
  - execute: [specific command pattern]
```

Explicit grants are scoped — they apply only to the specific Level 3 worker in that invocation.

### §6.3 Why Read-Only for Level 3

Level 3 workers are invisible to Arkannie. Their actions cannot be audited at the Arkannie level. Defaulting to read-only prevents a compromised or misbehaving Level 3 worker from modifying shared state without the Level 2 parent's explicit decision.

---

## §7 {{ output_schema }} Copy-Paste Model and Drift Rule

Each operation in an agent spec defines an `output_schema` block. When Arkannie constructs a `context_block` for dispatch, it copies the `output_schema` verbatim into the `context_block.output_schema` field.

The wave agent receives this schema and must conform its return envelope to it.

**Drift rule:** The `output_schema` in the agent spec is the authoritative definition of what a wave must return. If the wave's actual return does not conform to the schema, Arkannie does not detect the mismatch at the structural level (§2 validation covers envelope shape only, not payload schema). Payload schema conformance is the agent's contract with its callers.

If a caller (Arkannie or another wave) depends on a specific field in `payload` and that field is absent, the caller is responsible for handling the missing field via the `error ->` handler.

---

## §8 Error Format Standard

All error messages produced by Arkannie — whether from validation failure, timeout, pre-dispatch failure, or Class escalation — follow this format:

```
[arkannie] ERROR — [short title]

Context:
  command: [name]
  operation: [op name if applicable]
  id: [dispatch id if applicable]
  class: A | B | C

Detail:
  [specific description of what failed and why]

[For Class B only] Proposed recovery:
  [one or more actionable options]

[For Class C only] Authorization required:
  [description of what explicit instruction is needed before Arkannie proceeds]
```

This format is non-negotiable. Do not abbreviate, paraphrase, or omit sections.
