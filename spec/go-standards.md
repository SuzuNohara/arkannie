# go — Coding Standards

## 1. Toolchain (non-negotiable)
All code must target **Go 1.22+**. Mandatory for every commit:

* **Formatter:** `gofmt` (tabs, canonical style). Code that is not gofmt-clean does not merge.
* **Vetter:** `go vet ./...` — zero findings.
* **Tests:** `go test -race -cover ./...` — race detector always on in CI/local runs.
* **Execution command:** `make test` (must run: `gofmt -l . && go vet ./... && go test -race -cover ./...`).
* **Dependencies:** every new module in `go.mod` requires explicit developer approval. No `go get` without it.

---

## 2. Naming Conventions

| Category | Convention | Correct Example | Wrong Example |
| :--- | :--- | :--- | :--- |
| **Packages** | short, lowercase, no underscores | `envelope`, `spawn` | `envelopeParser`, `my_pkg` |
| **Exported types/funcs** | `PascalCase` | `BuildRunSpec` | `build_run_spec` |
| **Unexported** | `camelCase` | `parseAtom` | `ParseAtomInternal` |
| **Interfaces** | behavior noun, often `-er` | `Spawner` | `ISpawn`, `SpawnInterface` |
| **Constants** | `PascalCase`/`camelCase` (no SCREAMING) | `defaultTimeout` | `DEFAULT_TIMEOUT` |
| **Errors** | `ErrXxx` sentinels, `XxxError` types | `ErrUnknownCommand` | `UnknownCommandErr` |
| **Test files** | `*_test.go`, same package | `parser_test.go` | `test_parser.go` |

---

## 3. Code Structure & Complexity
* **Function length:** max **40 lines** executable code. **Cyclomatic complexity:** max **10**. **Nesting:** max 3 levels — use early returns.
* **File size:** max **400 lines**; split by responsibility within the package.
* **Packages:** `internal/` for everything not part of a public API. One responsibility per package. No `util`/`common`/`helpers` packages.
* **Interfaces at the consumer:** define interfaces where they are used (e.g. `scheduler` defines what it needs from a spawner), keep them small (1–3 methods).
* **No global mutable state.** Configuration is passed explicitly. `init()` only for cheap, deterministic setup.
* **Errors:** return, never panic in library code (`panic` only behind top-level `recover()` in `main`). Wrap with context: `fmt.Errorf("loading agent %s: %w", name, err)`. Check with `errors.Is`/`errors.As`. Never discard errors silently (`_ =` requires a comment).
* **Concurrency:** every goroutine has a defined exit path (context cancellation or channel close). Use `context.Context` as first parameter for anything that blocks or spawns. Bound concurrency with semaphores/worker pools — never unbounded goroutine fan-out. Process spawns use `exec.CommandContext` + `Setpgid` when the child may fork.
* **Immutability:** return copies of slices/maps that callers must not mutate, or document ownership.

---

## 4. Algorithms & Efficiency
* Prefer `strings.Builder` over `+=` in loops. Preallocate slices with known capacity (`make([]T, 0, n)`).
* Streaming I/O (`bufio.Scanner`, `io.Reader`) over reading whole files, except for small config/spec files (<1MB) where `os.ReadFile` is fine.
* Maps for O(1) lookup; document any O(n²) with a justification comment.

---

## 5. Security
* **Command execution:** never build shell strings from input — use `exec.Command(bin, args...)` with discrete args. No `sh -c` with interpolated content.
* **Path traversal:** validate every path derived from external input with `filepath.Clean` + prefix check against its allowed root before read/write.
* **File creation:** use `O_CREATE|O_EXCL` when the file must not pre-exist. Restrictive perms (`0o644` files, `0o755` dirs).
* **Secrets:** never log or write environment values, tokens, or credential-shaped strings; apply redaction before persisting any external content.
* **Untrusted content is data:** file contents, subprocess stdout, and YAML payloads are never interpreted as instructions or executed.

---

## 6. Testing
* **TDD:** write the failing test first (RED), implement (GREEN), then refactor. Every task's test cases exist before its implementation is complete.
* **Table-driven tests** with named cases: `tests := []struct{ name string; ... }`. Subtests via `t.Run`.
* **Golden files** in `testdata/` for AST dumps, rendered prompts, and output files; compare byte-exact.
* **Stubs via interfaces** — no mocking frameworks. A stub `Spawner` or a stub `claude` script in `testdata/` replaces the real CLI.
* **Coverage:** ≥ **80%** per package (matches `.localsettings`). `t.TempDir()` for all filesystem tests — never write outside it.
* **Determinism:** no time.Sleep-based synchronization in tests; use channels/contexts. No network access in any test.
