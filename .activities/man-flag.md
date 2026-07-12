# man-flag — `--man`: per-agent execution-grade user manual

**Status:** DONE (2026-07-12) · **Tag:** `[man-flag]` · **Started:** 2026-07-12 · **Block:** deep_work · **Branch:** `arkannie-man-flag/user-manual`

## Resolution (2026-07-12)

**Open questions resolved:**
1. **Generation strategy** → pure runtime derivation from the loaded registry (like `--catalog`). No forge/absorb emission, no LLM.
2. **Source of prose** → structured `agent.yaml` fields (`Operation` struct); `harness.md` not read (prompt template with slots).
3. **Format** → **Markdown** (headings + per-operation sections).
4. **Relationship to `--feature`** → pure read-side flag; no forge/absorb wiring.

**Delivered:**
- `internal/registry/manual.go` — `Manual(onlyAgent) (string, bool)` + `renderAgentManual` / `renderOperation` / `formatSchema` / `manualExamples` / `synthExample`. Deterministic (all maps walked sorted).
- `cmd/arkannie/{args.go (man/manAgent + applyMan), man.go (runMan), main.go (dispatch), help.go (--man)}`.
- `spec/divergence-notes.md` — CLI-consulta section documents `--man`.

**Manual covers per agent:** identity (command/model/scope), scope-specific dispatch rule (executor→`--allow-workspace`; agnostic→read-only/absolute `--path`; layer→origin+`--allow-layer`), default-op / multi-op selection, overview (capabilities), per-operation contract (context, grants, flags, groups, modifiers, `output_schema` success/info/error), personalities list, Ask Protocol & trust boundary, examples (`capabilities.examples` + one synthesized per op).

**Verification:** suite verde (13 pkgs, `-race`); coverage registry 84.9% · cmd 89.5%. Smoke real: `--man=nova-scan` (flags/groups/modifiers/schema), `--man=nova-review` (12 personalities), `--man=nova` (complete multi-op executor, no default op), `--man=nope`→64, bare `--man`→9 agentes.

---


> Registered via the activities workflow (`new`). Awaiting `plan` — the sections
> below are the Problem Brief, not yet the formal Spec/Plan/Tasks/Tests.

## Problem Brief

`--catalog[=<agent>]` prints an agent's **calling card** (capabilities:
purpose / use_when / inputs / produces / examples) — enough for the orchestrator
to *select* an agent, but not to fully *drive* it. There is no single command
that yields an agent's complete execution detail; today you must read its
`agent.yaml` + `harness.md` by hand.

**Ask:** add `arkannie --man[=<agent>]` — a per-agent **user manual** with enough
detail to execute an agent's complete tasks and actions, produced by the feature
and consultable on demand.

## Intended manual content (per agent)

- **Identity & dispatch rule:** command, model, scope — and what the scope
  demands (executor → `--allow-workspace`, program mode; agnostic → read-only,
  absolute `--path`).
- **Invocation modes:** `--agent` free-prompt vs `.ann` program; when
  flags/groups/personality apply; op-flag operation selection for multi-op agents
  (no `default_operation` → an op must be selected).
- **Per operation:** id, description, `context` fields (type/required), `grants`,
  `flags`, `groups` (mutually-exclusive options), `modifiers` (combinable), and
  the full `output_schema` (success/error/info).
- **Personalities:** the `--personality` values and each one's one-line intent.
- **Ask Protocol & trust boundary** summary.
- **Runnable examples:** from `capabilities.examples` plus one synthesized per
  operation.

## Acceptance criteria (high level — formalize at `plan`)

- `--man=<agent>` prints the manual for one agent; bare `--man` covers all valid
  agents. Value accepted only in `=` form (never consumes the next token), like
  `--catalog`. Exit 0; unknown agent → 64; load errors to stderr without
  suppressing valid agents.
- **Deterministic:** derived from the loaded registry (agent.yaml + capabilities),
  no LLM spawn — consistent with `--catalog`. No program/prompt execution.
- The output covers every field an executor needs to run the agent end to end
  (verified against both a fragmented `nova-*` agent and the multi-op `[nova]`).

## Open questions (resolve at `plan`)

1. **Generation strategy:** derive on the fly from the contract (recommended —
   matches `--catalog`), or emit a stored manual at forge/absorb time (like
   `capabilities`)? The prompt mentions the feature "creates" the manual — clarify
   whether that means a forge/absorb emission step or pure runtime derivation.
2. **Source of operation prose:** `agent.yaml` only, or also read `harness.md`
   for per-operation instructions?
3. **Format:** plain text (like `--catalog`) or markdown?
4. **Relationship to a `--feature` step:** is manual generation also wired into
   the forge/absorb pipeline, or purely a read-side flag?

## Notes

- Sibling of `--catalog` (`spec/divergence-notes.md` → CLI consulta de catálogo);
  likely lives in `cmd/arkannie` next to it.
- Dogfoods the nova activities workflow (`nova-absorb/ACTIVITIES-MANUAL.md`): this
  file is the `new`-stage artifact; run `plan man-flag` next to fill in
  Spec/Plan/Tasks/Tests.
