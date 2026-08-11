# Roadmap

> Long-term direction and raw ideas. Items here are NOT actionable tasks.
> When an idea is refined into bounded work, it moves to TODO_LIST.md.

## Themes

### 1. Ecosystem Integration

Pre-built adapters so users get flight recording with minimal wiring.

Raw ideas:

- HTTP middleware adapter for `net/http` that constructs `TriggerContext` from
  request method, path, status code, and duration automatically
- Framework-specific wrappers (chi, echo, gin) that follow each framework's
  middleware conventions
- gRPC interceptor that populates `TriggerContext.Kind` and `Type` from RPC
  method names
- Integration examples for common Go service patterns (worker pools, cron jobs,
  event-driven handlers)

### 2. Richer Trigger Conditions

Expand the vocabulary of when to capture beyond latency and errors.

Raw ideas:

- Memory pressure trigger (capture when `runtime.MemStats` exceeds threshold)
- Goroutine count trigger (capture on suspected goroutine leaks)
- Custom predicate triggers (user-supplied function with access to arbitrary
  runtime state)
- Trigger presets: named bundles for common production scenarios ("debug
  timeouts", "debug errors", "debug everything")

### 3. Observability Hooks

Make snapshot events visible to monitoring systems.

**Status:** The dependency-free callback layer is shipped — `WithMetrics`
(`MetricsHook` / `SnapshotEvent`) and `WithLogger` (`LoggerHook`) let consumers
wire Prometheus, OpenTelemetry, or `log/slog` without the library importing
them. Remaining raw ideas:

- Prometheus metrics integration (snapshot count, last capture timestamp,
  buffer fill ratio) — as an adapter built on top of `WithMetrics`, in a
  separate package
- Structured logging of trigger evaluations (which trigger fired, why)
- OpenTelemetry span events on snapshot capture — as an adapter built on top of
  `WithMetrics`

### 4. Multi-Sink Snapshots

Capture to multiple destinations simultaneously.

Raw ideas:

- Fan-out writer that distributes snapshot data to multiple `io.Writer` targets
- Conditional routing: different triggers write to different sinks (errors to
  one file, latency spikes to another)
- Streaming sink for real-time trace streaming over network

## Non-goals

Things we are deliberately NOT pursuing and why:

- **Trace analysis:** That is `go tool trace`'s job. This library captures
  traces; it does not interpret them.
- **External dependencies:** This library stays stdlib-only. Any feature that
  requires a third-party import belongs in a separate adapter package or a
  different repository.
- **Multiple active recorders:** Go's runtime limits this to one per process.
  Working around it would be fighting the runtime, not wrapping it.
- **Backwards compatibility with pre-Go 1.25:** `runtime/trace.FlightRecorder`
  does not exist before Go 1.25. Polyfilling a flight recorder would be a
  fundamentally different project.
