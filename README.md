# go-flightrecorder

[![Go Reference](https://pkg.go.dev/badge/github.com/larsartmann/go-flightrecorder.svg)](https://pkg.go.dev/github.com/larsartmann/go-flightrecorder)

Production-safe wrapper around Go 1.25's `runtime/trace.FlightRecorder` with composable trigger conditions.

A flight recorder buffers the last few seconds of execution trace in memory, continuously discarding old data. When something goes wrong — a slow request, an error, a panic — you snapshot exactly the problematic window for offline analysis with `go tool trace`.

## Why?

`runtime/trace.FlightRecorder` is powerful but bare. Every project that uses it needs the same scaffolding:

- Safe lifecycle management (start/stop/close with idempotency)
- Once-semantics to prevent snapshot races when multiple goroutines detect a problem simultaneously
- Configurable snapshot destinations (writer, file, lazy file)
- Composable trigger conditions ("capture on errors OR latency above 100ms")
- Process-global singleton enforcement (Go allows only one active recorder)

This library provides all of that with zero dependencies (stdlib only).

## Install

```bash
go get github.com/larsartmann/go-flightrecorder
```

Requires Go 1.26+ (`go.mod` pins `1.26.5`).

## Quick start

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    flightrecorder "github.com/larsartmann/go-flightrecorder"
)

func main() {
    recorder, err := flightrecorder.New(
        flightrecorder.WithMinAge(10*time.Second),
        flightrecorder.WithMaxBytes(10<<20), // 10 MiB
        flightrecorder.WithFile("trace.bin"),
    )
    if err != nil {
        log.Fatal(err)
    }
    if err := recorder.Start(); err != nil {
        log.Fatal(err)
    }
    defer recorder.Close()

    // Later, when something goes wrong:
    if err := recorder.Snapshot(context.Background()); err != nil {
        log.Printf("snapshot failed: %v", err)
    }

    // Analyze: go tool trace trace.bin
}
```

## Trigger-based capture

The trigger system lets you declaratively specify when to snapshot:

```go
// Capture on any error or operation slower than 100ms:
trigger := flightrecorder.OnErrorOrLatency(100 * time.Millisecond)

recorder.SnapshotIf(ctx, flightrecorder.TriggerContext{
    Kind:     "http.request",
    Type:     "GET /api/users",
    Duration: 150 * time.Millisecond,
    Err:      nil,
}, trigger)
```

### Composable triggers

```go
// Fire on errors, OR on commands that exceed 200ms:
trigger := flightrecorder.OnAny(
    flightrecorder.OnError(),
    flightrecorder.OnLatency(200*time.Millisecond),
)

// Fire ONLY on slow errors (not fast errors or slow successes):
trigger := flightrecorder.OnAll(
    flightrecorder.OnError(),
    flightrecorder.OnLatency(500*time.Millisecond),
)
```

### Built-in triggers

| Trigger | Fires when |
|---------|-----------|
| `OnLatency(threshold)` | Duration exceeds threshold |
| `OnError()` | Operation returned a non-nil error |
| `OnErrorOrLatency(threshold)` | Either of the above |
| `OnAlways()` | Every call (testing/baseline) |
| `OnAny(triggers...)` | Any trigger fires (OR) |
| `OnAll(triggers...)` | All triggers fire (AND) |

## Process-global constraint

Go's `runtime/trace` allows only **one** active `FlightRecorder` per process. Calling `Start()` when another recorder is already running returns `ErrAlreadyEnabled`.

Design your application around a single recorder, created at startup and shared across all middleware, handlers, and background workers.

## Configuration

| Option | Default | Description |
|--------|---------|-------------|
| `WithMinAge(d)` | 10s | Minimum age of reliably retained trace data. Set to ~2x your debugging window. |
| `WithMaxBytes(n)` | 10 MiB | Maximum in-memory trace buffer size. ~10 MB/s for a busy service. |
| `WithWriter(w)` | `io.Discard` | Destination for `Snapshot()` writes. |
| `WithFile(path)` | (none) | Lazy-opened file for snapshot output. File is created on first snapshot. |

## License

Proprietary. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
