package flightrecorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/trace"
	"sync"
	"time"
)

const (
	defaultMinAge          = 10 * time.Second
	defaultMaxBytes uint64 = 10 << 20 // 10 MiB — ~1s of trace data for a busy service
)

// ErrAlreadyEnabled is returned by [Recorder.Start] when another flight
// recorder is already active in this process. Go's runtime/trace allows
// only one active [runtime/trace.FlightRecorder] at a time.
var ErrAlreadyEnabled = errors.New(
	"flightrecorder: another flight recorder is already active in this process",
)

// Recorder wraps [runtime/trace.FlightRecorder] with safe lifecycle
// management, configurable snapshot sinks, and once-semantics to prevent
// snapshot races.
//
// A Recorder is safe for concurrent use by multiple goroutines.
//
// Only one flight recorder may be active per process. Calling [Recorder.Start]
// when another recorder is already running returns [ErrAlreadyEnabled].
type Recorder struct {
	fr     *trace.FlightRecorder
	writer io.Writer // destination for trace snapshots

	mu   sync.Mutex // guards once, started, stopped
	once sync.Once  // ensures first Snapshot wins
}

// New creates a Recorder from the given options. Returns an error if
// the configuration is invalid.
//
// The Recorder is not started; call [Recorder.Start] to begin recording.
func New(opts ...Option) (*Recorder, error) {
	cfg := defaultConfig()

	for _, opt := range opts {
		opt(&cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Recorder{ //nolint:exhaustruct // mu and once are zero-value
		fr: trace.NewFlightRecorder(trace.FlightRecorderConfig{
			MinAge:   cfg.minAge,
			MaxBytes: cfg.maxBytes,
		}),
		writer: cfg.writer,
	}, nil
}

// Start begins buffering execution trace in memory.
// Returns [ErrAlreadyEnabled] if another flight recorder is already active
// in this process.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.fr.Start(); err != nil {
		if err.Error() == "flight recorder already enabled" {
			return fmt.Errorf("%w: %w", ErrAlreadyEnabled, err)
		}

		return fmt.Errorf("flightrecorder: starting recorder: %w", err)
	}

	return nil
}

// Stop stops recording and releases the in-memory trace buffer.
// After Stop, [Recorder.Enabled] returns false and [Recorder.Snapshot]
// is a no-op. It is safe to call Stop multiple times.
func (r *Recorder) Stop() {
	r.mu.Lock() //art-dupl:accept same-file mutex guard idiom
	defer r.mu.Unlock()

	r.fr.Stop()
}

// Close stops the recorder and closes any underlying resources (e.g.,
// a file opened via [WithFile]). It is safe to call Close multiple times.
//
// Recorder implements [io.Closer] so it can participate in shutdown
// ordering alongside other closable resources.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.fr.Stop()

	if lf, ok := r.writer.(*lazyFile); ok {
		_ = lf.Close()
	}

	return nil
}

// Enabled reports whether the recorder is actively buffering traces.
func (r *Recorder) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.fr.Enabled()
}

// Snapshot writes the buffered trace to the configured writer.
// By default, only the first successful call has effect (once-semantics)
// to prevent snapshot races when multiple goroutines detect a problem
// simultaneously. Call [Recorder.Reset] to allow subsequent captures.
//
// The context is checked for cancellation before the snapshot begins.
// If the context is already cancelled, Snapshot returns the context
// error immediately without writing. Note: [runtime/trace.FlightRecorder.WriteTo]
// does not accept a context, so cancellation cannot abort a write
// already in progress.
//
// If the recorder is not enabled or has already been snapshotted,
// Snapshot is a no-op and returns nil.
func (r *Recorder) Snapshot(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	var snapErr error

	r.once.Do(func() {
		snapErr = r.capture()
	})

	return snapErr
}

// SnapshotToFile is a convenience that writes the trace to a file.
// It creates the file, writes the snapshot, and closes the file.
// Once-semantics apply as with [Recorder.Snapshot].
//
// The context is checked for cancellation before the snapshot begins.
func (r *Recorder) SnapshotToFile(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	var err error

	r.once.Do(func() {
		err = r.captureToFile(path)
	})

	return err
}

// SnapshotIf evaluates the trigger against the given context and captures
// a snapshot if the trigger returns true. Returns true if a snapshot was
// initiated, false otherwise.
//
// This is the primary method for middleware integration: the middleware
// constructs a [TriggerContext] from the operation result and delegates
// the decision to the trigger function.
func (r *Recorder) SnapshotIf(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool {
	if trigger == nil || !trigger(tc) {
		return false
	}

	if err := r.Snapshot(ctx); err != nil {
		return false
	}

	return true
}

// Reset clears the once-latch so that [Recorder.Snapshot] can fire again.
// Use this when you want to capture multiple snapshots over the recorder's
// lifetime (e.g., periodic slow-operation captures).
//
// Reset does not restart a stopped recorder. Call [Recorder.Start] first
// if the recorder has been stopped.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.once = sync.Once{}
}

// capture writes the buffered trace to the configured writer.
// The once.Do ensures only the first caller reaches capture.
func (r *Recorder) capture() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() || r.writer == nil {
		return nil
	}

	if _, err := r.fr.WriteTo(r.writer); err != nil {
		return fmt.Errorf("flightrecorder: writing snapshot: %w", err)
	}

	return nil
}

// captureToFile writes the buffered trace to a file at path.
// Same once.Do + lock pattern as capture but with a fresh file.
func (r *Recorder) captureToFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("flightrecorder: creating snapshot file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := r.fr.WriteTo(f); err != nil {
		return fmt.Errorf("flightrecorder: writing snapshot to %s: %w", path, err)
	}

	return nil
}
