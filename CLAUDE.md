# CLAUDE.md

Canonical instruction file for `cloudkey`. Shared conventions come from the
templates block below and the inherited `~/.claude/CLAUDE.md`. There are no
repo-specific rules yet; add them above the templates block as needed.

<!-- templates: lang-golang -->

<!-- BEGIN TEMPLATES v:1 hash:0237d3aaf765 -->
## go

### Formatting

- All Go code must be `gofmt`-clean before committing. Run `gofmt -l .` — any output is a failure.
- Imports must be grouped and ordered by `goimports`: stdlib, then external, then internal. Do not hand-sort.
- Do not leave unused imports or variables — the compiler rejects them, and they indicate incomplete work.

### Error handling

- Check every error. Do not assign errors to `_` unless the function is documented as always-nil.
- Wrap errors with context using `fmt.Errorf("operation: %w", err)` so callers can use `errors.Is`/`errors.As`.
- Do not use `panic` in library or application code for recoverable errors. Reserve it for truly unrecoverable
  programmer errors (e.g. invalid state at init time).
- Do not use `log.Fatal` or `os.Exit` deep inside packages unless the user requires it — only at `main()` or top-level entrypoints.

### Idioms

- Accept interfaces, return concrete types.
- Use `context.Context` as the first argument in any function that does I/O, makes network calls, or may need cancellation.
- Use `defer` for cleanup, but be aware that deferred calls in loops execute at function return, not loop iteration.
- Short variable names (`i`, `v`, `err`) are fine in short scopes. Use descriptive names across function boundaries.

### Documentation (GoDoc)

Every package has a package comment, but only some deserve a full doc.go. Two tiers:

- **Contract packages** get a full doc.go: the package is imported by two or more other
  packages AND defines a contract the source alone does not show — lifecycle ordering,
  concurrency guarantees, wire/data formats, ldflags injection points, or an extension
  point other packages implement against. Document the purpose, the contract, and any
  non-obvious design decisions. Nothing else.
- **Everything else** gets a brief package comment, ten lines or fewer: what the package
  provides and who consumes it. It may live atop the primary source file instead of a
  separate doc.go.

Rules that apply to both tiers:

- Never include file-layout listings, "why this is a separate package" rationale, or
  prose that restates code structure. All of it is derivable from the code and goes
  stale silently the moment the code moves.
- When several packages implement one shared pattern (e.g. pipeline stages), document
  the pattern once in the contract package they implement against; each implementation
  documents only its deltas — data source, outputs, quirks.
- Right-sizing cuts both ways: an oversized doc.go that violates its tier should be
  shrunk, not preserved.

### Concurrency

- Every goroutine must have a defined owner responsible for its lifetime.
- Do not start a goroutine without a way to observe its termination (via `sync.WaitGroup`, channel drain, or context cancellation).
- Prefer channels for communication, mutexes for protecting shared state — do not mix casually.
- Run the race detector on tests: `go test -race ./...`. A race condition is a bug, not a warning.

### Building

- To produce a binary, use `make build` or `go build -o bin/<binary>` — never let `go build` drop a binary
  in the project root.
- To verify a change compiles (no artifact wanted), use `go build ./...` — it discards output and covers
  every package, unlike `go build ./cmd/...`.

### Project Structure

Follow [https://github.com/golang-standards/project-layout/](https://github.com/golang-standards/project-layout/) as
closely as possible.

```text
{repo}/
├── build/
│   └── package/
│       └── Dockerfile         # multi-stage: golang:<current-stable>-alpine → scratch
├── cmd/
│   └── {application}/
│       └── main.go           # entry point only — flag parsing, wiring, shutdown
├── deployments/              # Kubernetes manifests, Helm charts, Grafana dashboards, etc.
├── docs/                     # user-facing documentation
├── internal/                 # all non-exported application logic
│   └── {package}/
│       ├── {file}.go
│       └── {file}_test.go    # test files live next to the code they test
├── scripts/
│   ├── variables.mk          # computed build variables (VERSION, REVISION, etc.)
│   └── go.mk                 # Go-specific Makefile targets (build, test, vet, clean)
├── test/                     # integration, function, smoke, or e2e testing;
│   └── fixtures/             # fixture files used by tests;
├── Makefile                  # includes scripts/go.mk; top-level targets only
├── go.mod                    # module github.com/jnovack/{repo}
└── go.sum
```

- `main.go` is **wiring only** — parse flags, instantiate types, start goroutines, block on signal, shut down cleanly.
- All logic lives under `internal/`. No `pkg/` unless we have specific exportable packages, which is very rare.
- Never put a `main.go` at the repository root.

### Project initialization

For new projects, run the scaffolding script to generate standard boilerplate
(Makefile, Dockerfile, GitHub workflows, build-version wiring in `main.go`):

```bash
code/golang/src/init-go-project.sh <application> [--ghcr|--dockerhub] [--no-windows]
```

The script lives in the dotfiles repo at `code/golang/src/`. Do not hand-write
these files — use the script to ensure exact repeatability.

**Build version rules** (enforced in every binary regardless of scaffolding):

- Expose `version`, `buildRFC3339`, `revision` as ldflags (`-X main.version=...`).
- Call `populateBuildMetadataFromBuildInfo()` as the first line of `main()` to
  populate from `debug.ReadBuildInfo()` when ldflags are absent.
- Always include a `--version` flag that logs all three values and exits 0.
- Log all three values at startup via `slog.Info`.

### Cross-platform

#### Windows

When Windows binaries are needed, please apply the following rules:

- Use `filepath.Join()` everywhere — never hardcode `/` as a path separator.
- Use `os.UserHomeDir()` for home directory detection — never expand `~` manually.
- Use `runtime.GOOS` switch for OS-specific defaults when necessary:
  - Windows: hardcoded system path (e.g. `C:\actions-runner`)
  - darwin: `filepath.Join(os.UserHomeDir(), "...")`
  - default (linux + others): absolute path (e.g. `/actions-runner`)
- Strip `\r` before processing any line read from a file.
- Open files with `os.O_RDONLY` — assume other processes may hold write handles.

### Additional Libraries, Modules and Dependencies

- Do not add a module without calling it out explicitly. Run `go mod tidy` after any dependency change.
- Prefer the standard library. The stdlib covers most needs.  Notable exceptions below:
- Pin indirect dependencies by running `go mod tidy` and committing both `go.mod` and `go.sum`.
- Pin build tools (`mockery`, `golangci-lint`, etc.) with the `tool` directive
  in `go.mod` (`go get -tool <module>`, Go 1.24+) and invoke via `go tool <name>` —
  no untracked global installs.

#### Flags

Use [`github.com/jnovack/flag`](https://github.com/jnovack/flag) — drop-in replacement for `flag` with env-var and
config-file support.

```go
fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], "", flag.ExitOnError)
myFlag := fs.String("my-flag", "default", "description")
_ = fs.Parse(os.Args[1:])
```

- Flag `--my-flag` maps to env var `MY_FLAG` automatically (uppercase, dashes → underscores).
- No prefix needed when env vars are unambiguous; use a prefix if the binary shares a namespace with other tools.
- Always include a `--version` flag (see Build Version above).

#### Logging

Use `log/slog` exclusively for services where log files will be parsed by a computer.  For applications designed to run
by a user, please use `rs/zerolog`.

```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
```

- Set level from `--log-level` flag at startup.
- Use structured key-value pairs, not format strings.
- Log level order: `debug`, `info`, `warn`, `error`. slog has no `fatal` level —
  log at `error` and exit explicitly at the entrypoint; zerolog's `Fatal()` is
  acceptable in user-facing applications.

#### Prometheus Exporters

When the application calls for metrics, apply the following rules:

- Register counters and histograms once (in the type that owns the state machine) — do not recreate them per scrape.
- Implement `prometheus.Collector` (`Describe` + `Collect`) for gauges whose label values change at runtime.
- Use `prometheus.NewRegistry()` (not the default registry) for testability.
- Expose `{namespace}_info` as a gauge = 1 with constant labels for static identity (instance identifier, version,
  revision, OS).
- Use `orUnknown(s string) string` helper — never emit empty label values.
- Histogram buckets for duration metrics: `1, 5, 60, 300, 600, 1800, 3600` seconds.
- Metric label cardinality: ensure label sets are bounded (no unbounded dynamic values like timestamps or UUIDs as labels).
- Use `github.com/prometheus/client_golang/prometheus/testutil` (`CollectAndCompare`, `ToFloat64`) for Prometheus metric
  assertions.

### Testing

The language-agnostic rules (negative cases, smallest sufficient level,
regression tests, independence, no brittle timing) live in the Testing
Philosophy section. Go-specific mechanics follow.

#### Test Pyramid — Layer Definitions

| Layer       | Scope                              | Dependencies     | Speed    |
| ----------- | ---------------------------------- | ---------------- | -------- |
| Unit        | Single function/method             | Mocks/stubs only | < 1s     |
| Integration | Multiple components, single binary | Mocks acceptable | < 30s    |
| Functional  | Full feature slice                 | Mocks acceptable | < 2min   |
| Smoke       | Critical path, post-deploy         | Real             | < 5min   |
| E2E         | Full system, user journey          | Real             | Uncapped |

#### Directory Structure + Build Tags

```text
repo/
└── internal/        # unit tests co-located (*_test.go, no build tag)

repo/test/
├── integration/     # //go:build integration
├── functional/      # //go:build functional
├── smoke/           # //go:build smoke
└── e2e/             # //go:build e2e
```

Every non-unit test file must carry its corresponding build tag as the first
line, e.g.:

```go
//go:build integration
```

Unit tests carry no build tag — they run with plain `go test ./...`.

#### Running Tests

```bash
go test ./...                         # unit only
go test -tags=integration ./...       # integration
go test -tags=functional ./...        # functional
go test -tags=smoke ./...             # smoke
go test -tags=e2e ./...               # e2e
go test -race ./...                   # always run for shared-state code
```

#### What to Write Unprompted

- **Unit:** always — for every function with logic
- **Integration:** always — for any code crossing a component boundary

Do not write functional, smoke, or e2e tests unless explicitly asked.

#### Unit Test Rules

- Table-driven throughout: `[]struct{ name, input, want }`
- Group related cases under a single `t.Run` loop
- Use `t.TempDir()` for filesystem tests — never hardcode `/tmp/`
- Use `t.Context()` (Go 1.24+) when a test needs a context — it is
  canceled automatically when the test ends; do not use `context.Background()`
  in tests on modern toolchains
- Fixture files under `test/fixtures/` — read with
  `os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "..."))`
  relative to the test package (every path segment its own argument —
  no embedded `/`, per the Windows rules above)
- Always test Windows line endings (CRLF) for any log/file parsing code
- Coverage targets: 85%+ on core logic; error paths and OS-conditional
  branches are acceptable gaps

#### Integration Test Rules

- Live in `integration/` with `//go:build integration`
- Interface mocks are acceptable — prefer them over spinning real infra
- One `TestMain` per package for setup/teardown
- No shared mutable state between test cases
- Must clean up all resources regardless of pass/fail — use `t.Cleanup()`

#### Functional Test Rules (when asked)

- Live in `functional/` with `//go:build functional`
- Test complete feature slices, not implementation details
- Mocks acceptable for external services; real for internal components
- Named after the user-facing behaviour: `TestUserCanResetPassword`
- Ask to add or update function tests when behavior is implemented in a pure
  function, parser, formatter, validator, mapper, or other isolated module
  with meaningful branching or edge cases.

#### Smoke Test Rules (when asked)

- Live in `smoke/` with `//go:build smoke`
- Real dependencies only — no mocks
- Designed to run post-deploy against a live environment
- Must be read-only — no mutations to production state
- Fail fast: first failure aborts the suite
- Ask to add or update smoke tests when a change affects a user-visible flow,
  app startup, wiring between modules, routing, IPC, configuration loading, or
  other integration points where basic execution must be verified.

#### E2E Test Rules (when asked)

- Live in `e2e/` with `//go:build e2e`
- Real dependencies only — full system under test
- Environment config via environment variables only — no hardcoded URLs
- Idempotent: safe to run multiple times without manual cleanup
- Document required environment setup in `e2e/README.md`
- Ask to add or update end-to-end tests only when the change affects a critical
  user journey, cross-process behavior, regression-prone workflow, or a bug that
  can only be reliably proven through full-system execution.

#### Mocks

- Generate with `mockery` or hand-roll against interfaces — no monkey patching
- Mock files live alongside the interface they mock: `mock_<name>.go`
- Never mock types you don't own — wrap them behind an interface first
- Never fetch live external services to generate mocks without explicit approval.
- When approved, record the full response — headers, metadata, and body — to
  `test/fixtures/<service>/`. Recorded responses are the source of truth
  for all future mocks; do not hit the live service again if a fixture exists.

#### Shared Test Helpers

- Live in `internal/testutil/` — importable across all layers
- No production logic in testutil — helpers only
- Reuse `testutil` helpers by default instead of duplicating setup logic in
  individual tests.
- Prefer `testcontainers` for integration boundaries that depend on real
  database or service behavior.
- Do not mock external systems when a small, reliable container-backed test
  would better prove correctness.
<!-- END TEMPLATES -->
