# nova activities — usage manual

The user-facing how-to for driving nova's activity lifecycle on top of the
absorbed agents. Companion to `ACTIVITIES-ORCHESTRATION.md` (the internal
contract). Every command below is either **pure orchestrator logic** (state
bookkeeping, no agent) or **drives one dispatch** to a deployed nova agent.

> Playing the orchestrator: until the G1 orchestrator exists, *you* (or a Claude
> session) are the orchestrator — you keep `.activities/` and run the dispatches.

## Mental model

- **The board** (`.activities/`) is the single source of truth. It persists; the
  agents do not.
- **You drive**: read the board → dispatch an agent → write its `payload` back to
  the board.
- **Traceability**: every task carries one `[tag]` (its ID) that links its
  `.activities/<id>.md`, its `.devs/*`, and its commits.

## Setup — the board

```
.activities/
  active.md      # | ID | Title | Status | Plan file | Started |
  finished.md    # | ID | Title | Completed | Coverage | Commits |
  <id>.md        # per task: ## Spec / ## Plan / ## Tasks / ## Tests + metrics:
```

Create `active.md` / `finished.md` empty (just the header row) on day one.

## Command reference

| You type | What it does | Under the hood |
|---|---|---|
| `resume` | Restore context: pending tasks, worktrees, where you left off | **pure** — read `active.md` + `git worktree list` + last `[wave-N]` commit |
| `itinerary` | Map pending tasks to today's work blocks | **pure** — `active.md` × `work_blocks` |
| `new <id>: title [block:deep_work]` | Register a task | **pure** — append row to `active.md` (status `pending`) |
| `activity <id>: note` | Timestamped status note; `done`→ move to `finished.md` | **pure** — edit `<id>.md` / move row |
| `close` | End-of-day summary | **pure** — today's `git log` × board |
| `plan <id>: <feature>` | Produce the 4-section plan for a task | dispatch `[nova] --plan --full : "<feature>"` → write `payload.plan` into `<id>.md` |
| `hotfix <id>: <incident>` | Abbreviated incident plan | dispatch `[nova] --plan --hotfix : "<incident>"` |
| `segment <id>` | Split a plan into per-story task files | **pure** — carve `<id>.md`'s `## Tasks` into `<id>-N.md` |
| `implement <id>` | Build the task from its plan, verify | read `<id>.md` → brief → `[nova] --build : "<brief>"` → append `metrics:`, move to `finished.md` |
| `audit <id> [security\|perf\|health]` | Rated findings on the code | `[nova] --scan --<lens> --path=<abs>` → attach `payload.findings` to `<id>.md` |
| `review <id> --as <persona>` | Expert review of a diff/design | `[nova] --review --personality=<persona> : "<material>"` → attach `payload.review` |
| `investigate <id>: <topic>` | Formal code+git investigation | `[nova] --investigate --reverse : "<topic>"` → `.devs/investigation-*.md` |
| `report <from> <to>` | Velocity/coverage report for a range | `[nova] --report --from=<f> --to=<t> --path=.activities` → `payload.report` |
| `doc <project>` | Regenerate project docs | `[nova] --doc --project=<abs> --overview` → record `payload.written` |

Dispatch reminders: `[nova]` is `executor` → every dispatch needs
`--allow-workspace` and runs in program (`.ann`) mode; `--path` may be relative
(cwd = your invocation dir). The fragmented `nova-*` agents work identically;
their read-only ones (`nova-scan/review/plan/report`) skip `--allow-workspace`
but need **absolute** `--path`.

## Daily loop

```
morning:   resume            # what's pending, where you left off
           itinerary         # today's plan by work block
midday:    new PROJ-12: …    # register work as it lands
           plan PROJ-12: …   # → writes the plan into the board
           implement PROJ-12 # → builds, verifies, writes metrics
           audit PROJ-12 security
evening:   close             # end-of-day summary
weekly:    report 2026-07-01 2026-07-11
```

## Ticket lifecycle — the happy path

```
new  →  plan  →  (segment)  →  implement  →  audit / review  →  report
             ↑ writes <id>.md      ↑ appends met:, →finished    ↑ reads metrics back
```

The metric loop closes here: `implement` writes the `metrics:` block from the
`--build` payload, and `report` aggregates it — the connection nova's
`[implement]` step 9 and `[report]` had, restored at the orchestrator.

## Worked example — one ticket

```bash
# 1. register + plan
new HC-1: healthcheck should re-probe a 25h-stale cache      # active.md += HC-1 (pending)
#   → dispatch:  [nova] --plan --full : "healthcheck re-probes a 25h-stale cache"
#   → write payload.plan into .activities/HC-1.md  (Spec/Plan/Tasks/Tests)

# 2. implement
implement HC-1
#   → read HC-1.md ## Plan+## Tasks → brief
#   → dispatch:  [nova] --build : "<brief>"
#   → payload {outcome: DONE, modified_files:[…], tests_written, deviations}
#   → append metrics: to HC-1.md ; move HC-1 → finished.md

# 3. audit
audit HC-1 security
#   → dispatch:  [nova] --scan --security --path=/abs/internal/config
#   → attach payload.findings to HC-1.md

# 4. report
report 2026-07-01 2026-07-31
#   → dispatch:  [nova] --report --path=.activities
#   → payload.report aggregates HC-1's metrics
```

At no step does an agent read the board — you carry the state between stateless
dispatches. That is the whole discipline: **the board is yours; the agents are
pure functions you feed and read.**

## Gotchas

- **`--allow-workspace`** on every `[nova]` dispatch (single executor scope).
- **Agents never persist** — if you don't write the payload back to the board, it
  is gone (it lives only in `.output/<id>.md` for that run).
- **No `[resume]` magic for free** — cross-session intelligence is your `git`
  correlation, not an agent call.
- **`.mem/` is off-limits** — the board is your `.activities/`, never the runtime's
  `.mem/`.
