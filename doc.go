// Package flightrecorder wraps Go 1.25's runtime/trace.FlightRecorder
// with a clean lifecycle API and composable trigger conditions.
//
// A flight recorder buffers the last few seconds of execution trace in memory.
// When a problem is detected (slow operation, error, panic), the program can
// snapshot exactly the problematic window of time for offline analysis with
// `go tool trace`.
//
// # Process-global constraint
//
// Go's runtime/trace allows only ONE active FlightRecorder per process.
// Calling [Recorder.Start] when another recorder is already running returns
// [ErrAlreadyEnabled]. Do not create multiple Recorder instances and call
// Start on all of them — design your application around a single recorder
// (typically created at startup, shared across middleware and host hooks).
//
// # Quick start
//
//	recorder, _ := flightrecorder.New(
//		flightrecorder.WithSnapshotDir("/var/lib/app/traces"),
//		flightrecorder.WithCompression(gzip.BestSpeed), // 10x smaller files
//		flightrecorder.WithMaxSnapshots(50),           // retain newest 50
//		flightrecorder.WithMinAge(10 * time.Second),
//		flightrecorder.WithMaxBytes(1 << 20),          // 1 MiB
//	)
//	if err := recorder.Start(); err != nil {
//		log.Fatal(err)
//	}
//	defer recorder.Close()
//
//	// Later, when something goes wrong:
//	path, _ := recorder.SnapshotToDir(context.Background())
//
// Decompress before analysing (go tool trace does not read .gz directly):
//
//	gunzip snapshot-*.trace.gz && go tool trace snapshot-*.trace
//
// # Trigger integration
//
//	// Fire only when an operation exceeds 100ms:
//	trigger := flightrecorder.OnLatency(100*time.Millisecond)
//	recorder.SnapshotIf(ctx, flightrecorder.TriggerContext{
//	    Kind:     "command",
//	    Type:     "user.create",
//	    Duration: 150 * time.Millisecond,
//	}, trigger)
//
// Analyze the captured trace with: go tool trace snapshot.trace
//
// # Snapshot-to-directory, compression, and retention
//
// For auto-triggered captures, write timestamped snapshots to a directory,
// compress them, and retain only the newest:
//
//	recorder, _ := flightrecorder.New(
//	    flightrecorder.WithSnapshotDir("/var/lib/app/traces"),
//	    flightrecorder.WithCompression(gzip.BestSpeed),
//	    flightrecorder.WithMaxSnapshots(50),
//	)
//	path, _ := recorder.SnapshotToDir(context.Background())
//
// # Non-blocking capture and graceful drain
//
// SnapshotIfAsync captures in a background goroutine so trace I/O does not
// block hot paths. Stop and Close drain all in-flight captures before
// shutting down.
//
// # Observability hooks (no dependencies)
//
// WithMetrics and WithLogger register callbacks so consumers wire their own
// Prometheus, OpenTelemetry, or log/slog backend without the library importing
// any of them. The [SnapshotEvent] passed to the metrics hook carries the
// [TriggerContext.Kind] and [TriggerContext.Type] so dashboards can label by
// operation (e.g. "http.request" vs "event.handler").
//
// # Error handling
//
// The package returns typed errors so callers can handle failure modes
// programmatically. All error types implement the standard [error]
// interface and support [errors.Is] and [errors.As].
//
// - [ErrAlreadyEnabled] / [*AlreadyEnabledError] — another recorder is active.
// - [*ConfigError] — invalid option passed to [New].
// - [*SnapshotError] — IO failure during snapshot or close.
//
// Example: distinguish error categories after Start.
//
//	recorder, err := flightrecorder.New(opts...)
//	if err != nil {
//	    var cfgErr *flightrecorder.ConfigError
//	    if errors.As(err, &cfgErr) {
//	        log.Printf("bad config: %s %s", cfgErr.Field, cfgErr.Constraint)
//	    }
//	    return err
//	}
//
//	if err := recorder.Start(); err != nil {
//	    if errors.Is(err, flightrecorder.ErrAlreadyEnabled) {
//	        // Another recorder is active — reuse it or stop it first.
//	    }
//	}
package flightrecorder
