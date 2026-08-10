# AGENTS.md

Concise context for AI sessions working in `go-flightrecorder`.

## What This Is

Zero-dependency Go library (stdlib only) wrapping Go 1.25's `runtime/trace.FlightRecorder` with safe lifecycle management, configurable snapshot sinks, and composable trigger conditions. Single package: `flightrecorder`. Module: `github.com/larsartmann/go-flightrecorder`.

## Commands

```bash
go test ./... -race         # tests (always with -race)
golangci-lint run ./...     # lint (v2 config in .golangci.yml)
go vet ./...                # vet
```

No `flake.nix`, `Makefile`, or `justfile` — `go` and `golangci-lint` are the only tools required. This matches the sibling micro-library pattern (go-retry, go-idempotency).

Requires Go 1.26+ (`go.mod` pins `go 1.26.5`).

## Architecture

Four source files, one package:

| File | Responsibility |
|------|----------------|
| `doc.go` | Package documentation only (no code) |
| `options.go` | Functional options (`With*`), `recorderConfig` validation, `lazyFile` type |
| `recorder.go` | Core `Recorder` type: lifecycle (`Start`/`Stop`/`Close`), snapshot (`Snapshot`/`SnapshotToFile`/`SnapshotIf`), `Reset` |
| `trigger.go` | `TriggerFunc` type, `TriggerContext` struct, composable trigger constructors (`OnLatency`, `OnError`, `OnErrorOrLatency`, `OnAlways`, `OnAny`, `OnAll`) |

**Data flow**: `New(opts)` builds a config-validated `Recorder` wrapping `trace.NewFlightRecorder`. `Start()` begins in-memory buffering. When a problem occurs, `Snapshot`/`SnapshotIf` writes the buffered window to the configured sink (`io.Writer` or file). The trace is then analyzed offline with `go tool trace`.

## Critical Gotchas

### Process-global singleton (most important constraint)

Go's `runtime/trace` allows **only one** active `FlightRecorder` per process. Calling `Start()` when another recorder is running returns `ErrAlreadyEnabled`.

This is detected via **string matching** in `recorder.go`:

```go
if err.Error() == "flight recorder already enabled" {
    return fmt.Errorf("%w: %w", ErrAlreadyEnabled, err)
}
```

This is fragile: if Go changes the runtime error message, `ErrAlreadyEnabled` detection breaks silently.

### Test serialization via `recorderMu`

Because of the singleton constraint, every test that calls `Start`/`Stop` **must** acquire the package-level `recorderMu sync.Mutex` (`recorder_test.go:18`). These tests are intentionally **not** `t.Parallel()`. The `paralleltest` linter is excluded for test files in `.golangci.yml` for this reason.

**When adding tests**: if the test calls `Start()`, wrap it with `recorderMu.Lock(); defer recorderMu.Unlock()` and do NOT call `t.Parallel()`.

### Snapshot once-semantics

`Snapshot` and `SnapshotToFile` use `sync.Once` internally: only the **first** successful call writes trace data. All subsequent calls are silent no-ops (return `nil`). `Reset()` re-arms the latch by replacing the `sync.Once` value.

### `lazyFile` deferred file creation

`WithFile(path)` stores a `*lazyFile` that opens the file on first `Write` call, so the file is not created until a snapshot is actually captured. `Close()` type-asserts the writer to `*lazyFile` to close it. `WithWriter` and `WithFile` are mutually exclusive in intent — last option wins via struct assignment.

### Context cancellation is pre-write only

`Snapshot` checks `ctx.Done()` before starting the write, but `trace.FlightRecorder.WriteTo` does not accept a context, so an in-progress write **cannot** be cancelled.

## Conventions

### Lint configuration

`.golangci.yml` enables ~90 linters (golangci-lint v2). Key config decisions:

- `gosec` excludes G304 (file path from variable) and G115 (integer overflow) — both are intentional patterns in this library.
- Test files (`_test.go`) exclude: `paralleltest`, `gochecknoglobals`, `goconst`, `varnamelen`, `wsl_v5`, `mnd`, `exhaustruct`, `err113` — all due to the singleton test serialization pattern and standard Go test idioms.
- `varnamelen` ignore-names includes `r`, `f`, `p`, `tc` — standard Go abbreviations for Recorder, File, byte parameter, and TriggerContext.

### Error wrapping

- Package-level sentinel errors: exported ones (`ErrAlreadyEnabled`) use `var Err... = errors.New(...)`; internal validation errors use lowercase `err...` sentinels.
- Wrapping: `fmt.Errorf("%w: ...", sentinel, ...)`.
- All error messages are prefixed with `flightrecorder:`.

### `//nolint:` directives

The codebase uses nolint directives with justifying comments:

| Directive | Used for |
|-----------|----------|
| `//nolint:exhaustruct` | Intentional zero-value struct fields (mutex, once, lazy file handle) |
| `//nolint:wrapcheck` | Direct delegation (`lf.f.Write`) and standard context error propagation (`ctx.Err()`) |
| `//art-dupl:accept` | Accepted duplication (same-file mutex guard idiom) |

### Functional options

`Option` is `func(*recorderConfig)`. Options mutate the config struct directly. Validation happens in `recorderConfig.validate()` after all options apply, not inside individual option functions.

### Testing patterns

- Table-driven tests with named subtests (`t.Run`).
- `t.Cleanup(r.Stop)` for recorder teardown.
- Tests that need trace data sleep (`time.Sleep(100*time.Millisecond)`) to let the buffer fill, with `MinAge` set low (e.g., `50*time.Millisecond`).
- `t.TempDir()` for file-based snapshot tests.
