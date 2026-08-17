# Feedback: Making go-flightrecorder SUPER

**Date:** 2026-08-11
**Source:** Real-world consumer analysis — `project-discovery-sdk/daemon/flightrecorder.go` (395 lines of hand-rolled wrapper code that should not exist)
**Verdict:** The library has an excellent core (lifecycle, triggers, typed errors, once-semantics) but is missing the operational features that every production consumer needs. Adding them would make this the only flight recorder code any Go project ever needs.

---

## Context: Why This Feedback Exists

The `project-discovery-sdk` daemon ships a **395-line hand-rolled flight recorder wrapper** (`daemon/flightrecorder.go`) that wraps raw `runtime/trace.FlightRecorder` directly. It does NOT import `go-flightrecorder`.

This is not an accident or oversight. The library was extracted AFTER the daemon code was written, and the library does not yet cover the operational features the daemon needs. So the daemon rolls its own.

This document catalogs every feature the daemon had to build by hand, explains why each matters, and proposes how the library should absorb them. The goal: **delete `daemon/flightrecorder.go` entirely and consume the library directly.**

---

## Gap Analysis: What the Daemon Has That the Library Lacks

| #  | Feature                    | Library | Daemon (in-tree)                            | Complexity                            |
| -- | -------------------------- | ------- | ------------------------------------------- | ------------------------------------- |
| 1  | Gzip compression           | ❌      | ✅ `CompressSnapshots`, `.trace.gz`         | Low — stdlib `compress/gzip`          |
| 2  | Snapshot retention         | ❌      | ✅ `MaxSnapshots` + `cleanupOldSnapshots()` | Low — stdlib `os.ReadDir`/`os.Remove` |
| 3  | Metrics hooks              | ❌      | ✅ Prometheus counters/histograms           | Medium — needs interface, not dep     |
| 4  | Non-blocking capture       | ❌      | ✅ `sync.WaitGroup` + goroutine             | Low — stdlib                          |
| 5  | Auto-timestamped filenames | ❌      | ✅ `snapshot-<nanos>.trace`                 | Trivial                               |
| 6  | Snapshot-to-directory      | ❌      | ✅ `SnapshotToFile() → dir`                 | Low                                   |
| 7  | Nil-safe receivers         | ❌      | ✅ `Stop()`/`Enabled()` on nil              | Trivial                               |
| 8  | Graceful drain on stop     | ❌      | ✅ `wg.Wait()` before `Stop()`              | Low                                   |
| 9  | Diagnostic logging         | ❌      | ✅ `slog` structured events                 | Medium — needs interface              |
| 10 | SnapshotIf with triggers   | ✅      | ❌ (hardcoded threshold)                    | Library wins                          |

The library already wins on triggers, typed errors, once-semantics, and zero dependencies. The daemon wins on everything operational. They are complementary halves of the same product.

---

## Recommendations

Grouped by theme. Each includes rationale, proposed API, and design constraints.

### Theme 1: Snapshot Compression (stdlib, zero-risk)

**Why:** A 1 MiB trace file compresses to ~100 KB (10x). On long-running services that auto-capture snapshots, compression prevents disk exhaustion. `compress/gzip` is stdlib and `go tool trace` has supported `.trace.gz` since Go 1.19.

**Proposed API:**

```go
// Option
func WithCompression(level int) Option  // gzip.NoCompression, gzip.BestSpeed, gzip.BestCompression

// Recorder gains a compress flag internally.
// SnapshotToFile writes .trace or .trace.gz based on the option.
// SnapshotToWriter wraps the writer in gzip.NewWriter if compression is enabled.
```

**Design constraint:** `level = 0` means no compression (backward compatible default). `level = -1` means `gzip.DefaultCompression`. This avoids a breaking change to the option system.

**Acceptance criteria:** A `.trace.gz` file produced by the library is loadable by `go tool trace`.

---

### Theme 2: Snapshot Retention (stdlib, zero-risk)

**Why:** Without retention, auto-triggered snapshots accumulate forever. The daemon's `cleanupOldSnapshots()` scans the directory, sorts by modification time, and removes the oldest files beyond a count limit. This is pure stdlib, universally needed, and tedious to re-implement.

**Proposed API:**

```go
// Option
func WithMaxSnapshots(n int) Option  // 0 = unlimited (default)

// Behavior:
// - Cleanup runs after every SnapshotToFile call
// - Cleanup runs once during Start() (prune files from a previous process crash)
// - Files identified by configurable prefix/suffix (default: "snapshot-", ".trace"/".trace.gz")
```

**Design constraint:** Cleanup errors must not fail the snapshot. They should be reported via the logging hook (Theme 5), not returned to the caller.

**Acceptance criteria:** With `WithMaxSnapshots(5)`, the directory never contains more than 5 snapshot files after a capture.

---

### Theme 3: Snapshot-to-Directory with Auto-Timestamped Files

**Why:** The library's `SnapshotToFile(ctx, path)` requires the caller to generate the filename. Every consumer does the same thing: `fmt.Sprintf("snapshot-%d.trace", time.Now().UnixNano())`. This belongs in the library.

**Proposed API:**

```go
// Option — set a directory for auto-named snapshots
func WithSnapshotDir(dir string) Option

// Method — write to auto-generated filename in the configured directory
func (r *Recorder) SnapshotToDir(ctx context.Context) (path string, err error)

// Filename format: snapshot-<unix-nano>.trace (or .trace.gz if compressed)
// Directory is created with os.MkdirAll(0o750) if it does not exist.
```

**Design constraint:** `SnapshotToDir` requires `WithSnapshotDir` to be set. Calling it without a configured directory returns a `*ConfigError`. `os.TempDir()` should NOT be a silent default — implicit temp file creation is a footgun (files scattered across `/tmp` with no cleanup).

**Relationship to existing API:** `WithFile(path)` writes to a FIXED path (overwrite semantics — one snapshot replaces the previous). `WithSnapshotDir(dir)` writes to a DIRECTORY with auto-generated UNIQUE names (append semantics — each snapshot is preserved). These are distinct use cases. Both should coexist.

**Acceptance criteria:** Two consecutive `SnapshotToDir` calls produce two distinct files, not one overwritten file.

---

### Theme 4: Non-Blocking Capture + Graceful Drain

**Why:** The library's `Snapshot()` and `SnapshotToFile()` are synchronous. In the daemon, the slow-request handler fires a snapshot in a goroutine via `wg.Go()` so the HTTP response is never blocked by trace file I/O. On shutdown, `Stop()` calls `wg.Wait()` to ensure in-flight snapshot goroutines complete before the recorder stops (avoiding a data race between `WriteTo` and `Stop`).

This is a universal need: any middleware that captures on latency/error will want non-blocking capture, and any shutdown path needs to drain.

**Proposed API:**

```go
// SnapshotIfAsync evaluates the trigger, and if it fires, captures in a goroutine.
// Returns immediately. The snapshot write happens in the background.
// Call Stop/Close to wait for in-flight captures before shutting down.
func (r *Recorder) SnapshotIfAsync(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool

// The existing Stop/Close methods already wait for the WaitGroup internally.
// No API change needed there — just internal wiring.
```

**Design constraint:** The context passed to `SnapshotIfAsync` is captured by the goroutine. If the context is cancelled mid-flight, the pre-write check in `capture()` catches it. However, `WriteTo` itself is uninterruptible (runtime limitation). Document this clearly.

**Race safety:** The recorder's internal `sync.WaitGroup` tracks all goroutines spawned by `SnapshotIfAsync`. `Stop()` and `Close()` call `wg.Wait()` before `fr.Stop()`. This matches the daemon's pattern exactly and prevents the `WriteTo`/`Stop` data race.

**Acceptance criteria:** A `SnapshotIfAsync` call that triggers a large file write does not block the calling goroutine. `Stop()` blocks until the write completes.

---

### Theme 5: Observability Hooks (no dependencies)

**Why:** The daemon tracks snapshot count and duration via Prometheus. But importing Prometheus into a stdlib-only library violates the zero-dependency principle. The solution is a **callback interface** — let consumers wire their own metrics backend.

This aligns with ROADMAP.md item 3 ("Observability Hooks — Prometheus metrics, structured logging").

**Proposed API:**

```go
// SnapshotEvent describes a captured snapshot for observability consumers.
type SnapshotEvent struct {
    Duration   time.Duration // capture wall-clock time
    Bytes      int64         // bytes written
    Path       string        // file path (empty for writer snapshots)
    Compressed bool          // gzip was applied
    Source     string        // "manual", "trigger", "async" (caller-set)
}

// MetricsHook is called after every successful or failed snapshot.
// Implementations can increment Prometheus counters, emit OpenTelemetry spans,
// write structured logs, or do nothing (the zero-value default).
type MetricsHook func(event SnapshotEvent, err error)

// LoggerHook receives diagnostic lifecycle events (start, stop, cleanup, errors).
// It receives human-readable messages with structured key-value pairs.
type LoggerHook func(msg string, args ...any)

// Options
func WithMetrics(hook MetricsHook) Option
func WithLogger(hook LoggerHook) Option
```

**Why two hooks:** Metrics and logging are distinct concerns. Metrics need structured typed data (duration, bytes, labels). Logging needs human-readable messages with context. Merging them into one interface creates an awkward API. Keeping them separate lets consumers wire one, both, or neither.

**Design constraint:** Both hooks default to no-op functions. The library remains stdlib-only. Consumers provide their own adapter:

```go
// Example: Prometheus adapter (in consumer code, not the library)
recorder, _ := flightrecorder.New(
    flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, err error) {
        if err != nil {
            snapshotErrors.Inc()
            return
        }
        snapshotTotal.WithLabelValues(e.Source).Inc()
        snapshotDuration.Observe(e.Duration.Seconds())
    }),
    flightrecorder.WithLogger(func(msg string, args ...any) {
        slog.Info(fmt.Sprintf(msg, args...))  // or slog.Default().LogAttrs(...)
    }),
)
```

**Acceptance criteria:** A consumer can wire full Prometheus metrics without the library importing Prometheus. A consumer can wire `slog` without the library importing `log/slog`.

---

### Theme 6: Nil-Safe Receivers (defensive, zero-risk)

**Why:** The daemon's `Enabled()` and `Stop()` both guard against nil receivers:

```go
func (fr *flightRecorder) Enabled() bool {
    if fr == nil || fr.recorder == nil {
        return false
    }
    return fr.recorder.Enabled()
}
```

This enables the pattern of an optional recorder field on a struct (`flightRecorder *flightRecorder` that may be nil when the feature is disabled) without sprinkling nil checks at every call site. The library should adopt this pattern.

**Proposed change:** Add nil guards to `Enabled()`, `Stop()`, and `Close()`. Document the nil-safe contract.

**Design constraint:** `Snapshot()` and `SnapshotToFile()` should NOT be nil-safe — calling Snapshot on a nil recorder is a programming error and should panic. Only the lifecycle query/teardown methods should be nil-safe.

**Acceptance criteria:** A struct with a `*Recorder` field that is nil can call `recorder.Stop()` and `recorder.Enabled()` without panicking.

---

### Theme 7: SnapshotPrefix Option (configurability)

**Why:** The daemon hardcodes `"snapshot-"` as the filename prefix. Different services writing to the same temp directory would collide. A configurable prefix lets multiple services (or multiple instances) coexist.

**Proposed API:**

```go
func WithSnapshotPrefix(prefix string) Option  // default: "snapshot-"
```

**Design constraint:** The prefix is only used by `SnapshotToDir`. `SnapshotToFile(ctx, path)` uses the explicit path.

---

## Proposed API Summary (All Additions)

```go
// New options
func WithCompression(level int) Option
func WithMaxSnapshots(n int) Option
func WithSnapshotDir(dir string) Option
func WithSnapshotPrefix(prefix string) Option
func WithMetrics(hook MetricsHook) Option
func WithLogger(hook LoggerHook) Option

// New types
type SnapshotEvent struct { ... }
type MetricsHook func(event SnapshotEvent, err error)
type LoggerHook func(msg string, args ...any)

// New method
func (r *Recorder) SnapshotToDir(ctx context.Context) (string, error)
func (r *Recorder) SnapshotIfAsync(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool

// Modified methods (nil-safe)
func (r *Recorder) Enabled() bool  // nil-safe
func (r *Recorder) Stop()          // nil-safe, drains in-flight async captures
func (r *Recorder) Close() error   // nil-safe, drains in-flight async captures
```

**Zero breaking changes.** All additions are new options, new types, or new methods. Existing code continues to work identically.

---

## Migration Path: Eliminating the Daemon Wrapper

After these additions, the daemon's 395-line `flightrecorder.go` shrinks to a ~30-line adapter:

```go
// daemon/flightrecorder_adapter.go — the ONLY flight recorder code in the daemon

package daemon

import (
    "github.com/larsartmann/go-flightrecorder"
    "github.com/prometheus/client_golang/prometheus"
)

func newFlightRecorder(opts FlightRecorderOptions, reg *prometheus.Registry) (*flightrecorder.Recorder, error) {
    frOpts := []flightrecorder.Option{
        flightrecorder.WithMinAge(opts.MinAge),
        flightrecorder.WithMaxBytes(opts.MaxBytes),
        flightrecorder.WithSnapshotDir(opts.SnapshotDir),
        flightrecorder.WithCompression(gzipLevel(opts.CompressSnapshots)),
        flightrecorder.WithMaxSnapshots(opts.MaxSnapshots),
        flightrecorder.WithMetrics(prometheusAdapter(reg)),
    }

    return flightrecorder.New(frOpts...)
}
```

The daemon keeps only:

- `FlightRecorderOptions` (its own config type, mapped to library options)
- `SlowRequestThreshold` handling (mapped to `OnLatency` trigger + `SnapshotIfAsync`)
- Prometheus metric wiring (via the `MetricsHook` adapter)

Everything else — lifecycle, compression, retention, timestamps, async capture, graceful drain — is the library's job.

**Lines eliminated:** ~365 of 395.

---

## Anti-Patterns to Avoid

1. **Do NOT add `os.TempDir()` as a default snapshot directory.** Implicit temp file creation is a footgun. Require explicit `WithSnapshotDir`.

2. **Do NOT import `log/slog` or `prometheus` in the library.** Use callback hooks. The zero-dependency principle is the library's core value proposition.

3. **Do NOT make `Snapshot()` nil-safe.** Only lifecycle methods (`Enabled`, `Stop`, `Close`) should tolerate nil receivers. Calling `Snapshot` on a nil recorder is a bug — let it panic.

4. **Do NOT add a `SnapshotToFile` variant that takes no path argument.** That is `SnapshotToDir`. Keep the existing `SnapshotToFile(ctx, path)` for explicit-path use cases and add `SnapshotToDir(ctx)` for auto-named use cases. Two methods, two intents, no overloading.

5. **Do NOT add retention cleanup to `Snapshot()` (writer-based snapshots).** Retention only makes sense for file-based snapshots. Adding it to writer snapshots is a category error.

6. **Do NOT make the `MetricsHook` synchronous-blocking.** The hook should return quickly. If a consumer needs expensive metric processing, they should do it asynchronously in their own code.

7. **Do NOT add `OnPanic` to the trigger system.** It was referenced in the README but never implemented, and panic recovery belongs in middleware, not in the recorder. The trigger system is for conditions, not for control-flow interception. The README reference should be removed (already flagged as stale in the v0.1.1 status report).

---

## Prioritization (Pareto)

| Priority | Theme                                   | Impact                       | Effort  |
| -------- | --------------------------------------- | ---------------------------- | ------- |
| P0       | Theme 3: SnapshotToDir + auto-timestamp | Every consumer needs this    | ~30 min |
| P0       | Theme 1: Compression                    | 10x storage savings          | ~20 min |
| P0       | Theme 4: Non-blocking capture + drain   | Middleware-critical          | ~40 min |
| P1       | Theme 2: Retention                      | Disk safety for auto-trigger | ~30 min |
| P1       | Theme 5: Observability hooks            | Metrics without deps         | ~45 min |
| P2       | Theme 6: Nil-safe receivers             | Defensive convenience        | ~10 min |
| P2       | Theme 7: SnapshotPrefix                 | Multi-instance coexistence   | ~5 min  |

Total estimated effort: ~3 hours for all seven themes.

The P0 items alone would let the daemon delete the majority of its wrapper code. P1 items complete the picture. P2 items are polish.
