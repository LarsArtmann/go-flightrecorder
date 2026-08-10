// Package flightrecorder wraps Go 1.25's runtime/trace.FlightRecorder
// with a clean lifecycle API and composable trigger conditions.
//
// A flight recorder buffers the last few seconds of execution trace in memory.
// When a problem is detected (slow operation, error, panic), the program can
// snapshot exactly the problematic window of time for offline analysis with
// `go tool trace`.
//
// This is a zero-dependency library (stdlib only).
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
//	    flightrecorder.WithMinAge(10*time.Second),
//	    flightrecorder.WithMaxBytes(1<<20), // 1 MiB
//	    flightrecorder.WithWriter(os.Stdout),
//	)
//	if err := recorder.Start(); err != nil {
//	    log.Fatal(err)
//	}
//	defer recorder.Close() // Close stops recording AND closes any file writers
//
//	// Later, when something goes wrong:
//	recorder.Snapshot(context.Background())
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
package flightrecorder
