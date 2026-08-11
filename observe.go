package flightrecorder

import (
	"io"
	"time"
)

// SnapshotSource labels the origin of a capture for observability consumers.
// Values are passed as [SnapshotEvent.Source] so a metrics hook can distinguish
// manual snapshots from triggered and asynchronous ones.
const (
	// SnapshotSourceManual is set by [Recorder.Snapshot], [Recorder.SnapshotToFile],
	// and [Recorder.SnapshotToDir].
	SnapshotSourceManual = "manual"

	// SnapshotSourceTrigger is set by [Recorder.SnapshotIf].
	SnapshotSourceTrigger = "trigger"

	// SnapshotSourceAsync is set by [Recorder.SnapshotIfAsync].
	SnapshotSourceAsync = "async"
)

// SnapshotEvent describes a captured (or attempted) snapshot. It is passed to
// a [MetricsHook] after every capture attempt that reaches the write stage,
// whether the write succeeded or failed.
type SnapshotEvent struct {
	// Duration is the wall-clock time spent writing the trace to its sink.
	Duration time.Duration

	// Bytes is the number of bytes written to the final sink (after any
	// compression). For failed writes this may be zero.
	Bytes int64

	// Path is the file path the snapshot was written to. It is empty for
	// writer-based ([WithWriter]) snapshots.
	Path string

	// Compressed reports whether gzip compression was applied.
	Compressed bool

	// Source labels the capture origin: one of the SnapshotSource* constants
	// (e.g. "manual", "trigger", "async").
	Source string

	// Kind is the operation category from [TriggerContext.Kind]
	// (e.g. "command", "event", "query"). Empty for manual captures that
	// have no associated trigger context.
	Kind string

	// Type is the specific operation type from [TriggerContext.Type]
	// (e.g. "user.created", "order.processed"). Empty for manual captures.
	Type string
}

// MetricsHook is invoked after every snapshot capture attempt that reaches the
// write stage. err is nil on success. Implementations must return quickly; if
// expensive processing is needed, do it asynchronously in consumer code.
//
// The hook defaults to a no-op, keeping the library dependency-free. Consumers
// wire their own backend (Prometheus, OpenTelemetry, structured logs, etc.):
//
//	flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, err error) {
//	    if err != nil {
//	        snapshotErrors.Inc()
//	        return
//	    }
//	    snapshotTotal.WithLabelValues(e.Source).Inc()
//	    snapshotBytes.Observe(float64(e.Bytes))
//	})
type MetricsHook func(event SnapshotEvent, err error)

// LoggerHook receives diagnostic lifecycle messages (start, stop, cleanup,
// errors). The message is a printf-style format string and args are its
// arguments, mirroring [log.Printf]. It defaults to a no-op so the library
// never imports a logging package.
//
//	flightrecorder.WithLogger(func(format string, args ...any) {
//	    slog.Info(fmt.Sprintf(format, args...))
//	})
type LoggerHook func(format string, args ...any)

func noopMetrics(SnapshotEvent, error) {}

func noopLogger(string, ...any) {}

// countingWriter wraps an [io.Writer] and counts the bytes that reach it.
// It is used to report actual sink bytes (post-compression) via [SnapshotEvent].
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err //nolint:wrapcheck // direct delegation
}
