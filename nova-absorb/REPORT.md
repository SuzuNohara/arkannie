# Absorption Report — nova → arkannie

**Source AI:** `/home/suzu/Documents/agents/nova` (nova — a stateful AI-developer-OS, ~40 commands, 12 roles)
**Strategy:** fragmented · **Date:** 2026-07-11 · **Runtime:** arkannie (`arkannie/go-runtime`)
**The source was never modified.** Everything below lives in `arkannie/.agents/` and `arkannie/nova-absorb/`.

---

## What was absorbed

### Agents (7, base `nova-`) — registry: 8 valid (incl. `echo`)

| Agent | scope | model | Absorbs | Smoke |
|---|---|---|---|---|
| `[nova-scan]` | agnostic | sonnet | `[audit]` worker — lens group `{security,perf,health}` | ✅ SQLi Critical + hardcoded cred High |
| `[nova-review]` | agnostic | opus | `[role]` — 12 personas as inline `personality` | ✅ security persona → "REJECT / STRIDE" |
| `[nova-report]` | agnostic | sonnet | `[report]` — metrics aggregation | ✅ zero-metrics report |
| `[nova-plan]` | agnostic | opus | `[planning]`/`[engineering]`/`[hotfix]` — depth group `{full,hotfix}` | ✅ 4-section plan, no invention |
| `[nova-investigate]` | executor | opus | `[investigation]`/`[diagnose]` — modifiers `{reverse,predictive,knowledge}` | ✅ real git analysis; Class B guard |
| `[nova-build]` | executor | sonnet | `[implement]` — RED-GREEN-REFACTOR cluster worker | ✅ guard-path info, zero writes |
| `[nova-doc]` | executor | sonnet | `[doc]` — per-project docs, kind group `{overview,history,arch}` | ✅ guard-path info, zero writes |

### Personalities
nova's 12 `[role]` files (architect, techlead, reviewer, security, tester, programmer, programmer-e, product-owner, coach, rubber-duck, mediator, twilight-documenter) were distilled into the **inline** `personality: {default, values}` block of `nova-review/agent.yaml`. The file-based `.agents/.personalities/*.md` mechanism is **deprecated** in this runtime (`internal/registry/registry_test.go:145`) — so 12 roles became 12 inline `values`, not 12 files.

### Recomposition programs (`nova-absorb/`)
- **`audit.ann`** — `parallel{}` fan-out of `nova-scan` ×3 lenses over one target, each lens returned to the body. Reconstructs `[audit]`'s 3-parallel-Explore core. **Smoke ✅** (`--id=absorb-smoke`): security→2 Critical, perf→1 High, health→5 findings.
- **`implement.ann`** — `foreach` over a list of externalized briefs dispatching `nova-build`. Reconstructs the `[implement]` wave as a sequential loop. **Parse-verified** (real run needs code fixtures + `--allow-workspace`).

---

## What was dropped, and why

| Dropped | Reason |
|---|---|
| **`[db]`** (was in the roster) | Its safety model *is* infrastructure: all `[db]` commands delegate to `.daemon/db-proxy/run.py`, which reads `.env` internally and never exposes credentials. That proxy is out of perimeter; an executor running raw `mysql` would have to handle credentials itself — strictly *less* safe. Not portable as a stateless agent. Candidate for `--mode=layer` if ever needed. |
| **Stateful OS shell** — `[resume]`, `[itinerary]`, `[new]`, `[activity]`, `[close]`, `[context]`, `[wt-*]`, daemons | arkannie is stateless by design. These commands *are* the task board / worktree / session state; there is nothing to externalize — the state is the function. |
| **Conversational mediation** — `[requirement]`/`[product]`/`[engineering]` mediation, `[idea]` discovery | Multi-turn sessions with interactivity levels and persisted turn history. A dispatch is single-shot; these do not compress to one envelope. Candidate for `--mode=layer`. |
| **VCS/CI side-effects** — `[pr]`, `[pr-update]`, `[review-import]`, `[release]`, `[deploy]`, `[rollback]` | Host/VCS integration; some are Class C (irreversible). These belong to the runtime/host, not to a wave agent. |
| **`[rebirth]`/`[rebirth-apply]`, `[init]`, `[workspace]`, `[project]`** | Meta-snapshot and workspace-state setup — no agent surface. |

---

## What state was externalized

nova is deeply stateful; every absorbed agent trades a filesystem dependency for an explicit input/output contract:

- **`[report]`** read `metrics:` blocks from `.activities/` → `nova-report` reads a `--path=<dir>` and returns the report as `payload.report`.
- **`[planning]`** wrote the plan into `.activities/[task].md` and resumed by phase markers → `nova-plan` returns the 4-section plan as `payload.plan`; no resume, no write. Persisting is the caller's job.
- **`[implement]`** orchestrated waves with a reviewer loop over `.activities/dispatch/` → `nova-build` is a pure cluster worker; the wave state (the briefs) is externalized into the `implement.ann` list.
- **`[doc]`** kept run-state in `.doc-run/*-state.md` → `nova-doc` is a single-project dispatch; `payload.written` reports what it wrote.

The dropped **activities core** (the persistent `.activities/` board — the connective tissue `[implement]`/`[report]` relied on, plus `[resume]`/`[itinerary]`/`[close]`/traceability) is not lost, it is **relocated to the orchestration layer (gap G1)**. Its contract — how the superior AI owns the board and carries state between stateless dispatches — is specified in `ACTIVITIES-ORCHESTRATION.md`.

---

## Frictions recorded

1. **Refinement vs. the F3 plan:** `report` and `plan` were reclassified `executor → agnostic` (they analyze and *return* an artifact, they don't write). Only 3 agents ended up executor.
2. **executor is "post-v1"** in `spec/divergence-notes.md`, but the mechanism is live: executor agents run with `--allow-workspace`; without it a dispatch is a catchable Class B pre-dispatch error.
3. **`nova-investigate` is executor** only because git-history reconstruction needs Bash; it is semantically read-only (never mutates the repo).
4. **`[audit] diff` was dropped** from `nova-scan`: diff-scope needs git (execute), incompatible with agnostic. Path-scope (`--path`) is preserved.
5. **Envelope binding rule** (found during the `nova-scan` slice): in Ann, `$x = [agent]` binds only on `status: success`; `info`/`error` leave the binding unset. Every absorbed agent therefore returns `success` for a valid-but-empty result and reserves `info` for "not executable".

---

## How to use

```bash
# Catalog (what the orchestrator reads):
arkannie --catalog

# A single agnostic agent (free-prompt smoke — no agent flags):
arkannie --agent=nova-review --id=r "review this: <code>"

# Flags/groups/personality require program mode:
arkannie --id=audit nova-absorb/audit.ann                 # parallel multi-lens audit
arkannie --allow-workspace --id=impl nova-absorb/implement.ann   # executor wave

# Validate the whole roster:
arkannie validate
```

`nova-absorb/fixture/app.js` is a throwaway multi-lens fixture for the `audit.ann` smoke (it contains a deliberately fake `sk-...` string to trip the security lens). Delete it if secret-scanners in CI object.
