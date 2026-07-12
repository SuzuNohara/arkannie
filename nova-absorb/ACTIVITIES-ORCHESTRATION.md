# Activities Core — orchestration-layer contract

Companion to `REPORT.md`. The absorption dropped nova's stateful `.activities/`
board (Gate 1: the state *is* the function, and arkannie agents are pure
`input → envelope`). This document specifies where that core is recovered: **not
in an agent, but in the orchestrator (roadmap gap G1)** — the superior AI that
reads `--catalog` and dispatches. The board becomes ordinary workspace state the
orchestrator owns; the agents stay stateless and unchanged (both the fragmented
`nova-*` set and the encapsulated `[nova]` work as-is).

## The boundary

```
        ┌─────────────────────────── ORCHESTRATOR (G1) ───────────────────────────┐
        │  owns .activities/  ·  resume/itinerary/close  ·  traceability [tag]      │
        │  reads board → builds context/flags → dispatches → writes payload back    │
        └───────────────▲───────────────────────────────────────────▲──────────────┘
                        │ context in (plan text, brief, path)        │ payload out
              ┌─────────┴───────────────────────────────────────────┴───────────┐
              │  STATELESS AGENTS  [nova] / nova-*   (pure: input → envelope)     │
              └──────────────────────────────────────────────────────────────────┘
```

Hard rules the boundary preserves:
- Agents never read or write the board directly — they receive material via
  `context.text`/flags and return it via `payload`. No agent contract changes.
- `.mem/` stays runtime-only and untouched. The board is a normal workspace
  directory (`.activities/`) the orchestrator reads and writes itself.
- Persistence, cross-session memory, worktree correlation and `[tag]`
  traceability are orchestrator responsibilities — there is no agent for them.

## Board layout (orchestrator-owned)

```
.activities/
  active.md          # table: | ID | Title | Status | Plan file | Started |
  finished.md        # table: | ID | Title | Completed | Coverage | Commits |
  <id>.md            # per task: ## Spec / ## Plan / ## Tasks / ## Tests + metrics:
```

The per-task file is exactly what `[nova] --plan` produces; the orchestrator
writes the payload there rather than the agent doing it.

## The loop (read → select → dispatch → write-back)

For every stage the orchestrator: (1) reads the relevant board state, (2) picks
the agent/operation from `--catalog`, (3) assembles `context.text` + flags from
the board, (4) dispatches, (5) parses the envelope `payload`, (6) writes it back
to the board. Mapping each nova lifecycle stage to a deployed dispatch and its
payload:

| nova stage | Dispatch | Orchestrator write-back |
|---|---|---|
| `[planning]` / `[engineering]` | `[nova] --plan --full : "<feature>"` → `payload.plan` | create `.activities/<id>.md` from `payload.plan`; add row to `active.md` (status `pending`) |
| `[segmentation]` | (split `payload.plan`'s `## Tasks` into N stories) | one `<id>.md` per story, each registered |
| `[hotfix]` | `[nova] --plan --hotfix : "<incident>"` | same, status `pending`, tag `hotfix/…` |
| `[implement]` | read `<id>.md` → build brief (Plan+Tasks+files+verify) → `[nova] --build : "<brief>"` → `payload.{outcome,modified_files,tests_written,deviations}` | append a `metrics:` block; on `outcome: DONE` move row `active → finished.md` |
| `[audit]` | `[nova] --scan --security --path=<abs>` → `payload.findings` | attach findings section to the activity or a `.devs/audit-*.md` |
| `[review-import]`-ish | `[nova] --review --personality=<p> : "<diff>"` → `payload.review` | attach to the activity |
| `[investigation]` | `[nova] --investigate --reverse : "<topic>"` → `payload.report` | write `.devs/investigation-*.md` |
| `[report]` | `[nova] --report --path=.activities` → `payload.report` | the metric loop closes — the blocks the orchestrator wrote at implement are now read back |
| `[doc]` | `[nova] --doc --project=<abs> --overview` → `payload.written` | record which files were regenerated |

The metric loop is the key repair: nova's `[implement]` step 9 wrote `metrics:`
and `[report]` read them. Here the **orchestrator** writes the `metrics:` block
(synthesizing from `--build`'s payload + its own timing + `git`), so `--report`
has something to aggregate again.

## Cross-session ops have no agent — they are pure orchestrator logic

- `[resume]` — read `active.md` + `git worktree list` + last `[wave-N]` commit per
  branch; reconstruct in-progress next steps. No dispatch.
- `[itinerary]` — map `active.md` rows to `work_blocks`. No dispatch.
- `[close]` — diff today's `git log` against the board; summarize. No dispatch.
- Traceability — the orchestrator assigns and carries the `[tag]` across
  `<id>.md`, `.devs/*`, and commit messages.

These are exactly the commands dropped at Gate 1: they are stateful glue, and the
glue lives in the layer that holds the state.

## Worked example — one ticket end to end

```
1. orchestrator: [nova] --plan --full : "add --json to healthcheck"
     → writes .activities/HC-1.md (Spec/Plan/Tasks/Tests), active.md += HC-1 (pending)
2. orchestrator: reads HC-1.md ## Plan+## Tasks → brief;  [nova] --build : "<brief>"
     → payload {outcome: DONE, modified_files: [...], tests_written, deviations}
     → appends metrics: to HC-1.md; moves HC-1 to finished.md
3. orchestrator: [nova] --scan --security --path=/abs/pkg
     → payload.findings → appended to HC-1.md
4. orchestrator: [nova] --report --path=.activities
     → payload.report aggregates HC-1's metrics block
```

At no point does an agent see the board; at every point the orchestrator carries
the state between stateless dispatches. That is the activities core, relocated to
where arkannie's design says it belongs.
