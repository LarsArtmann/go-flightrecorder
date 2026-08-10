# AGENTS.md

Concise context for AI sessions working in `go-flightrecorder`.

## What This Is

Zero-dependency Go library (stdlib only) wrapping Go 1.25's `runtime/trace.FlightRecorder` with safe lifecycle management, configurable snapshot sinks, and composable trigger conditions. Single package: `flightrecorder`. Module: `github.com/larsartmann/go-flightrecorder`.

## Commands

```bash
go test ./...     # run tests (~1s; tests sleep for trace buffer fill)
go build ./...    # build
go vet ./...      # vet
```

No `flake.nix`, Makefile, or CI config exists. No linter config file, but golangci-lint runs via LSP and the code uses `//nolint:` directives (see Conventions below).

Requires Go 1.25+ for `runtime/trace.FlightRecorder`. `go.mod` pins `go 1.26.5`.

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

This is fragile: if Go changes the runtime error message, `ErrAlreadyEnabled` detection breaks silently. If you touch this code, verify the string still matches the current Go runtime.

### Test serialization via `recorderMu`

Because of the singleton constraint, every test that calls `Start`/`Stop` **must** acquire the package-level `recorderMu sync.Mutex` (`recorder_test.go:18`). These tests are intentionally **not** `t.Parallel()`. Trigger-only tests (`trigger_test.go`) are all `t.Parallel()`-safe because they never touch a live recorder.

**When adding tests**: if the test calls `Start()`, wrap it with `recorderMu.Lock(); defer recorderMu.Unlock()` and do NOT call `t.Parallel()`.

### Snapshot once-semantics

`Snapshot` and `SnapshotToFile` use `sync.Once` internally: only the **first** successful call writes trace data. All subsequent calls are silent no-ops (return `nil`). This prevents snapshot races when multiple goroutines detect a problem simultaneously.

`Reset()` re-arms the latch by replacing the `sync.Once` value (`r.once = sync.Once{}`). `Reset` does **not** restart a stopped recorder.

### `lazyFile` deferred file creation

`WithFile(path)` stores a `*lazyFile` that opens the file on first `Write` call, so the file is not created until a snapshot is actually captured. `Close()` type-asserts the writer to `*lazyFile` to close it. `WithWriter` and `WithFile` are mutually exclusive in intent (last option wins via simple struct assignment).

### Context cancellation is pre-write only

`Snapshot` checks `ctx.Done()` before starting the write, but `trace.FlightRecorder.WriteTo` does not accept a context, so an in-progress write **cannot** be cancelled.

## Conventions

### Error wrapping

- Package-level sentinel errors: exported ones (`ErrAlreadyEnabled`) use `var Err... = errors.New(...)`; internal validation errors use lowercase `err...` sentinels.
- Wrapping: `fmt.Errorf("%w: ...", sentinel, ...)` or `fmt.Errorf("%w: %w", wrappingErr, wrappedErr)` for dual-wrap.
- All error messages are prefixed with `flightrecorder:`.

### `//nolint:` directives

golangci-lint is active. The codebase uses these directives with justifying comments (always include the reason):

| Directive | Used for |
|-----------|----------|
| `//nolint:exhaustruct` | Intentional zero-value struct fields (mutex, once, lazy file handle) |
| `//nolint:wrapcheck` | Direct delegation (`lf.f.Write`) and standard context error propagation (`ctx.Err()`) |
| `//nolint:gosec` | File creation from user-supplied config path |
| `//art-dupl:accept` | Accepted duplication (same-file mutex guard idiom) |

Follow this pattern when adding code that triggers lint warnings.

### Functional options

`Option` is `func(*recorderConfig)`. Options mutate the config struct directly. Validation happens in `recorderConfig.validate()` after all options apply, not inside individual option functions.

### Testing patterns

- Table-driven tests with named subtests (`t.Run`).
- `t.Cleanup(r.Stop)` for recorder teardown.
- Tests that need trace data sleep (`time.Sleep(100*time.Millisecond)`) to let the buffer fill, with `MinAge` set low (e.g., `50*time.Millisecond`).
- `t.TempDir()` for file-based snapshot tests.
- Existing `errcheck` warnings on `r.Start()` calls in some tests are known/intentional (the Start error is asserted elsewhere or the test focuses on other behavior).
