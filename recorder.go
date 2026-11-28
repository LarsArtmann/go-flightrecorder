package flightrecorder

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/trace"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	atomicwrite "github.com/larsartmann/go-atomic-write"
)

const (
	defaultMinAge           = 10 * time.Second
	defaultMaxBytes  uint64 = 10 << 20 // 10 MiB — ~1s of trace data for a busy service
	snapshotDirMode         = 0o750    // owner rwx, group rx — no world access for trace dumps
	snapshotFileMode        = 0o600    // owner rw — trace dumps carry stacks and argument values
)

// Recorder wraps [runtime/trace.FlightRecorder] with safe lifecycle
// management, configurable snapshot sinks, and once-semantics to prevent
// snapshot races.
//
// A Recorder is safe for concurrent use by multiple goroutines.
//
// Only one flight recorder may be active per process. Calling [Recorder.Start]
// when another recorder is already running returns [ErrAlreadyEnabled].
//
// The lifecycle methods [Recorder.Enabled], [Recorder.Stop], and [Recorder.Close]
// are nil-safe: calling them on a nil *Recorder is a no-op (returns false / does
// nothing / returns nil). This supports the optional-recorder pattern where a
// struct embeds a *Recorder that may be nil when the feature is disabled.
type Recorder struct {
	fr     *trace.FlightRecorder
	writer io.Writer // destination for trace snapshots

	compressLevel  int
	maxSnapshots   int
	snapshotDir    string
	snapshotPrefix string
	metricsHook    MetricsHook
	loggerHook     LoggerHook

	mu      sync.Mutex                // guards stopped and all capture/retention state
	once    atomic.Pointer[sync.Once] // once-latch, first Snapshot wins; Reset swaps it under mu while an in-flight ASYNC capture may still be loading it — hence the atomic pointer, not a bare sync.Once
	wg      sync.WaitGroup            // tracks in-flight async captures (SnapshotIfAsync)
	stopped bool                      // set once Stop/Close begins; blocks new async captures
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

	rec := &Recorder{ //nolint:exhaustruct_v5 // mu and wg are zero-value
		fr: trace.NewFlightRecorder(trace.FlightRecorderConfig{
			MinAge:   cfg.minAge,
			MaxBytes: cfg.maxBytes,
		}),
		writer:         cfg.writer,
		compressLevel:  cfg.compressLevel,
		maxSnapshots:   cfg.maxSnapshots,
		snapshotDir:    cfg.snapshotDir,
		snapshotPrefix: cfg.snapshotPrefix,
		metricsHook:    cfg.metricsHook,
		loggerHook:     cfg.loggerHook,
	}
	rec.once.Store(&sync.Once{})

	return rec, nil
}

// Start begins buffering execution trace in memory.
// Returns [*AlreadyEnabledError] (which satisfies [errors.Is] with
// [ErrAlreadyEnabled]) if another flight recorder or tracer is already
// active in this process.
//
// If [WithMaxSnapshots] and [WithSnapshotDir] are configured, Start also prunes
// stale snapshot files left over from a previous process.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.fr.Start(); err != nil {
		// FlightRecorder.Start only fails when a recorder or tracer is
		// already active. Both "flight recorder already enabled" and
		// "tracing is already enabled" indicate the same condition.
		return &AlreadyEnabledError{Cause: err}
	}

	// (Re)start clears any prior shutdown so async captures work again after a
	// Stop→Start cycle.
	r.stopped = false

	r.loggerHook("flightrecorder: started")

	// Prune crash-leftover snapshots so the configured limit holds from boot.
	if r.snapshotDir != "" && r.maxSnapshots > 0 {
		r.cleanupSnapshots()
	}

	return nil
}

// Stop stops recording and releases the in-memory trace buffer.
// After Stop, [Recorder.Enabled] returns false and [Recorder.Snapshot]
// is a no-op. It is safe to call Stop multiple times.
//
// Stop drains any in-flight asynchronous captures ([Recorder.SnapshotIfAsync])
// before stopping, preventing a data race between [runtime/trace.FlightRecorder.WriteTo]
// and the runtime stop.
//
// Stop is nil-safe: calling it on a nil *Recorder is a no-op.
func (r *Recorder) Stop() {
	if r == nil {
		return
	}

	if !r.beginShutdown() {
		return // already stopped
	}

	r.wg.Wait() // drain in-flight async captures (no lock held — avoids deadlock)

	r.mu.Lock() //art-dupl:accept same-file mutex guard idiom
	defer r.mu.Unlock()

	r.fr.Stop()
	r.loggerHook("flightrecorder: stopped")
}

// Close stops the recorder and closes any underlying resources (e.g.,
// a file opened via [WithFile]). It is safe to call Close multiple times.
//
// Recorder implements [io.Closer] so it can participate in shutdown
// ordering alongside other closable resources. Like [Recorder.Stop], Close
// drains in-flight asynchronous captures first.
//
// Close is nil-safe: calling it on a nil *Recorder returns nil.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}

	if !r.beginShutdown() {
		return nil // already stopped
	}

	r.wg.Wait() // drain in-flight async captures (no lock held — avoids deadlock)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.fr.Stop()
	r.loggerHook("flightrecorder: closed")

	if lf, ok := r.writer.(*lazyFile); ok {
		if err := lf.Close(); err != nil {
			return &SnapshotError{Op: "close", Path: lf.path, Err: err}
		}
	}

	return nil
}

// beginShutdown atomically marks the recorder as stopping under r.mu. It returns
// false if Stop/Close already began (idempotent teardown). Setting stopped
// under the lock happens-before [sync.WaitGroup.Wait] in Stop/Close, so no
// [SnapshotIfAsync] goroutine can call [sync.WaitGroup.Add] concurrently with Wait.
func (r *Recorder) beginShutdown() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		return false
	}

	r.stopped = true

	return true
}

// Enabled reports whether the recorder is actively buffering traces.
//
// Enabled is nil-safe: calling it on a nil *Recorder returns false.
func (r *Recorder) Enabled() bool {
	if r == nil {
		return false
	}

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
	return r.snapshot(ctx, captureMeta{ //nolint:exhaustruct_v5 // no trigger context for manual captures
		Source: SnapshotSourceManual,
	})
}

// captureMeta carries capture-origin metadata that ends up on [SnapshotEvent].
// Kind and Type are empty for manual captures; they carry [TriggerContext]
// values for triggered and asynchronous captures so metrics hooks can label by
// operation.
type captureMeta struct {
	Source string
	Kind   string
	Type   string
}

// captureOnce is the shared once-latched capture pipeline for the writer and
// file sinks ([Recorder.Snapshot], [Recorder.SnapshotToFile]): context guard,
// then the latch, then metrics. The given capture runs at most once per latch
// arm ([Recorder.Reset] re-arms). Its bool result is false when nothing was
// attempted — no hook fires and nil returns.
//
// The guard is the only place a capture reads its context, and it runs before
// the latch: a cancelled context drops the capture without consuming the
// once-latch. [Recorder.SnapshotIfAsync] documents the contract this upholds.
func (r *Recorder) captureOnce(ctx context.Context, capture func() (SnapshotEvent, bool, error)) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	var (
		event    SnapshotEvent
		captured bool
		err      error
	)

	r.once.Load().Do(func() {
		event, captured, err = capture()
	})

	if captured {
		r.metricsHook(event, err)
	}

	return err
}

// snapshot is the once-latched capture to the configured writer sink. The
// captureMeta labels the capture origin for the [MetricsHook].
func (r *Recorder) snapshot(ctx context.Context, origin captureMeta) error {
	return r.captureOnce(ctx, func() (SnapshotEvent, bool, error) {
		return r.captureToWriter(origin)
	})
}

// SnapshotToFile is a convenience that writes the trace to a file.
// The file appears atomically: a failed or interrupted write never leaves a
// truncated file at path. Once-semantics apply as with [Recorder.Snapshot].
//
// The context is checked for cancellation before the snapshot begins.
// SnapshotToFile does NOT trigger retention cleanup; use [Recorder.SnapshotToDir]
// for the auto-named, retained directory pattern.
func (r *Recorder) SnapshotToFile(ctx context.Context, path string) error {
	return r.captureOnce(ctx, func() (SnapshotEvent, bool, error) {
		return r.captureToFile(path, captureMeta{ //nolint:exhaustruct_v5 // no trigger context for manual
			Source: SnapshotSourceManual,
		})
	})
}

// SnapshotToDir writes the trace to an auto-generated, timestamped file inside
// the directory configured with [WithSnapshotDir]. The directory is created on
// first use. The filename is "<prefix><unix-nano>.trace" (or ".trace.gz" when
// compression is enabled).
//
// Unlike [Recorder.Snapshot] and [Recorder.SnapshotToFile], SnapshotToDir is NOT
// once-latched: every call produces a new file. This supports the
// append-and-retain pattern for auto-triggered captures.
//
// Calling SnapshotToDir without [WithSnapshotDir] returns a [*ConfigError].
// When [WithMaxSnapshots] is set, retention cleanup runs after each write.
//
// The context is checked for cancellation before anything is written: a
// cancelled context returns its error, and neither the directory nor a file
// is created.
func (r *Recorder) SnapshotToDir(ctx context.Context) (string, error) {
	return r.snapshotToDir(ctx, captureMeta{ //nolint:exhaustruct_v5 // no trigger context for manual
		Source: SnapshotSourceManual,
	})
}

// snapshotToDir is the internal variant that carries capture-origin metadata
// so asynchronous captures ([Recorder.SnapshotIfAsync]) can thread their
// [TriggerContext] through to the metrics hook.
func (r *Recorder) snapshotToDir(ctx context.Context, origin captureMeta) (string, error) {
	if r.snapshotDir == "" {
		return "", &ConfigError{
			Field:      "SnapshotDir",
			Value:      "",
			Constraint: "SnapshotToDir requires WithSnapshotDir",
		}
	}

	// The only read of ctx: a cancelled context drops the capture before the
	// directory is touched.
	select {
	case <-ctx.Done():
		return "", ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	if err := os.MkdirAll(r.snapshotDir, snapshotDirMode); err != nil {
		return "", &SnapshotError{Op: "mkdir", Path: r.snapshotDir, Err: err}
	}

	suffix := traceSuffix
	if r.compressLevel != 0 {
		suffix = traceGzSuffix
	}

	path := filepath.Join(
		r.snapshotDir,
		r.snapshotPrefix+strconv.FormatInt(time.Now().UnixNano(), 10)+suffix,
	)

	event, captured, err := r.captureToFile(path, origin)
	if captured {
		r.metricsHook(event, err)
	}

	if err != nil {
		return "", err
	}

	if r.maxSnapshots > 0 {
		r.mu.Lock()
		r.cleanupSnapshots()
		r.mu.Unlock()
	}

	return path, nil
}

// SnapshotIf evaluates the trigger against the given context and captures
// a snapshot if the trigger returns true. Returns true if a snapshot was
// initiated, false otherwise. A cancelled context counts as not initiated:
// nothing is written, the once-latch stays armed, and false is returned.
//
// This is the primary method for middleware integration: the middleware
// constructs a [TriggerContext] from the operation result and delegates
// the decision to the trigger function.
func (r *Recorder) SnapshotIf(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool {
	if trigger == nil || !trigger(tc) {
		return false
	}

	return r.snapshot(ctx, captureMeta{
		Source: SnapshotSourceTrigger,
		Kind:   tc.Kind,
		Type:   tc.Type,
	}) == nil
}

// SnapshotIfAsync is the non-blocking variant of [Recorder.SnapshotIf]: it
// evaluates the trigger and, if it fires, captures in a background goroutine,
// returning immediately. This is intended for hot paths (e.g., HTTP middleware)
// where trace file I/O must not block the response.
//
// Returns true only when a capture was actually initiated, i.e. a capture
// goroutine was started. That is not a promise that a snapshot gets written;
// see the context rules below. Returns false if the trigger did not fire OR if
// the recorder is shutting down ([Recorder.Stop] / [Recorder.Close] already
// began). In the shutdown case the capture is silently dropped — no goroutine
// is spawned.
//
// The capture routes to the configured sink: [WithSnapshotDir] writes a new
// timestamped file; otherwise the writer sink ([WithWriter]/[WithFile]) is used
// with once-semantics.
//
// # Context
//
// The context is read exactly once per capture: by the capture goroutine, in a
// non-blocking check before anything is written.
//
//   - Cancelled at that check — it was already cancelled when SnapshotIfAsync
//     was called, or it dies afterwards before the goroutine gets scheduled —
//     the capture is dropped: no bytes, no file, no [MetricsHook] call, and
//     the once-latch of the writer sink stays armed. Nothing reports the drop,
//     because SnapshotIfAsync has already returned true. ([Recorder.SnapshotIf]
//     returns false in the same situation.)
//   - Live at that check — the capture completes no matter what happens to
//     the context afterwards. [runtime/trace.FlightRecorder.WriteTo] takes no
//     context, so a write in progress cannot be cancelled or cut short: the
//     snapshot is written in full and the [MetricsHook] reports a success.
//
// A request context is cancelled when the handler returns, so it races that
// check — and a lost race costs the snapshot. Detach the context when the
// capture must outlive the caller:
//
//	recorder.SnapshotIfAsync(context.WithoutCancel(r.Context()), tc, trigger)
//
// Cancelling the context neither aborts a capture nor shortens the drain:
// [Recorder.Stop] and [Recorder.Close] wait for all in-flight async captures,
// so the goroutine never outlives a clean shutdown.
func (r *Recorder) SnapshotIfAsync(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool {
	if trigger == nil || !trigger(tc) {
		return false
	}

	r.mu.Lock()
	if r.stopped {
		// Recorder is shutting down — do not spawn a capture that Stop/Close
		// cannot drain. No capture was initiated.
		r.mu.Unlock()

		return false
	}

	r.wg.Add(1)
	snapshotDir := r.snapshotDir
	r.mu.Unlock()

	origin := captureMeta{
		Source: SnapshotSourceAsync,
		Kind:   tc.Kind,
		Type:   tc.Type,
	}

	go func() {
		defer r.wg.Done()

		if snapshotDir != "" {
			_, _ = r.snapshotToDir(ctx, origin)
		} else {
			_ = r.snapshot(ctx, origin)
		}
	}()

	return true
}

// SnapshotToWriter writes the trace buffer directly to dest, bypassing the
// configured sink and the once-latch. This is the low-level escape hatch for
// callers that need a one-shot capture to an arbitrary destination (e.g., an
// HTTP response buffer for a /debug/trace endpoint).
//
// Unlike [Recorder.Snapshot], SnapshotToWriter:
//   - Does NOT use once-semantics (every call writes)
//   - Does NOT use the configured writer/file sink
//   - Does respect compression (if [WithCompression] is set)
//
// Returns the number of bytes written (post-compression).
func (r *Recorder) SnapshotToWriter(ctx context.Context, dest io.Writer) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() {
		return 0, nil
	}

	counter := &countingWriter{w: dest} //nolint:exhaustruct_v5 // n is intentionally zero
	start := time.Now()
	err := r.writeCompressed(counter)

	event := SnapshotEvent{ //nolint:exhaustruct_v5 // Path is empty for writer snapshots
		Duration:   time.Since(start),
		Bytes:      counter.n,
		Compressed: r.compressLevel != 0,
		Source:     SnapshotSourceManual,
	}
	r.metricsHook(event, err)

	if err != nil {
		return counter.n, wrapWriteErr(err, "")
	}

	return counter.n, nil
}

// Reset clears the once-latch so that [Recorder.Snapshot] can fire again.
// Use this when you want to capture multiple snapshots over the recorder's
// lifetime (e.g., periodic slow-operation captures).
//
// The latch is swapped atomically, so Reset is safe to call while an ASYNC
// capture ([Recorder.SnapshotIfAsync]) is still in flight: the in-flight
// capture consumes whichever latch instance it loaded; the fresh latch arms
// the next capture. Reset never races with capture goroutines.
//
// Reset does not restart a stopped recorder. Call [Recorder.Start] first
// if the recorder has been stopped.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.once.Store(&sync.Once{})
}

// captureToWriter writes the buffered trace to the configured writer sink.
// The once.Do in the callers ensures only the first caller reaches it.
// The bool result is false (with a zero event and nil error) when the recorder
// is disabled or has no sink — i.e., nothing was attempted.
func (r *Recorder) captureToWriter(origin captureMeta) (SnapshotEvent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() || r.writer == nil {
		return SnapshotEvent{}, false, nil //nolint:exhaustruct_v5 // zero-value: nothing attempted
	}

	event, err := r.writeTrace(r.writer, "", origin)

	return event, true, err
}

// captureToFile writes the buffered trace to the file at path.
// The bool result is false when the recorder is disabled (no-op).
//
// The write is staged: the trace goes to a temp file next to path, is fsync'd,
// and is renamed into place. A crash or a failed write therefore never leaves a
// truncated or empty file at path — a previous snapshot stays intact and a
// first snapshot either exists complete or not at all. The file is created
// owner-only (0600); trace dumps carry goroutine stacks and argument values.
func (r *Recorder) captureToFile(path string, origin captureMeta) (SnapshotEvent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() {
		return SnapshotEvent{}, false, nil //nolint:exhaustruct_v5 // zero-value: nothing attempted
	}

	event, err := r.stagedFileWrite(path, origin, func(w io.Writer) (SnapshotEvent, error) {
		return r.writeTrace(w, path, origin)
	})

	return event, true, err
}

// stagedFileWrite runs write against a temp file, fsyncs it, and renames it
// over path (go-atomic-write: stage + fsync + rename, file mode
// [snapshotFileMode]). write runs with r.mu held, like every other trace
// write.
//
// A *SnapshotError returned by write passes through unchanged. Any other
// failure — creating the temp file, flushing, fsync, closing it, or the
// rename — is reported as a *SnapshotError with Op "create", and the
// [MetricsHook] is invoked with a bytes-less failure event for it (the same
// report a failed os.Create produced before the write became staged).
func (r *Recorder) stagedFileWrite(
	path string,
	origin captureMeta,
	write func(io.Writer) (SnapshotEvent, error),
) (SnapshotEvent, error) {
	var (
		event    SnapshotEvent
		writeErr error
	)

	err := atomicwrite.WriteFuncWithPerm(path, func(w io.Writer) error {
		event, writeErr = write(w)

		return writeErr
	}, snapshotFileMode)
	if err == nil {
		return event, nil
	}

	if snapErr, ok := errors.AsType[*SnapshotError](err); ok {
		return event, snapErr
	}

	wrapped := &SnapshotError{Op: "create", Path: path, Err: err}
	r.metricsHook(SnapshotEvent{ //nolint:exhaustruct_v5 // no bytes/duration: the file at path was never written
		Path:       path,
		Source:     origin.Source,
		Kind:       origin.Kind,
		Type:       origin.Type,
		Compressed: r.compressLevel != 0,
	}, wrapped)

	return SnapshotEvent{}, wrapped
}

// writeTrace performs the trace write to the given sink, applying compression
// when configured, and timing/byte-counting for the [MetricsHook]. The caller
// must hold r.mu.
func (r *Recorder) writeTrace(w io.Writer, path string, origin captureMeta) (SnapshotEvent, error) {
	counter := &countingWriter{w: w} //nolint:exhaustruct_v5 // n is intentionally zero

	start := time.Now()
	err := r.writeCompressed(counter)

	event := SnapshotEvent{
		Duration:   time.Since(start),
		Bytes:      counter.n,
		Path:       path,
		Compressed: r.compressLevel != 0,
		Source:     origin.Source,
		Kind:       origin.Kind,
		Type:       origin.Type,
	}

	if err != nil {
		return event, wrapWriteErr(err, path)
	}

	return event, nil
}

// writeCompressed writes the trace to dest, wrapping it in a gzip.Writer when
// compression is configured. Raw errors are returned for the caller
// ([Recorder.writeTrace]) to wrap with the relevant path.
//
//nolint:wrapcheck // errors are path-aware-wrapped by the sole caller via wrapWriteErr
func (r *Recorder) writeCompressed(dest io.Writer) error {
	if r.compressLevel == 0 {
		if _, err := r.fr.WriteTo(dest); err != nil {
			return err
		}

		return nil
	}

	gzipWriter, err := gzip.NewWriterLevel(dest, r.compressLevel)
	if err != nil {
		return err
	}

	if _, err := r.fr.WriteTo(gzipWriter); err != nil {
		return err
	}

	return gzipWriter.Close()
}

// wrapWriteErr converts a raw WriteTo error into a [*SnapshotError], passing
// existing SnapshotErrors through unchanged to avoid double-wrapping (e.g.,
// errors surfaced from [lazyFile]).
func wrapWriteErr(err error, path string) error {
	if snapErr, ok := errors.AsType[*SnapshotError](err); ok {
		return snapErr
	}

	return &SnapshotError{Op: "write", Path: path, Err: err}
}
