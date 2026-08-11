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
	"time"
)

const (
	defaultMinAge          = 10 * time.Second
	defaultMaxBytes uint64 = 10 << 20 // 10 MiB — ~1s of trace data for a busy service
	snapshotDirMode        = 0o750    // owner rwx, group rx — no world access for trace dumps
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

	mu      sync.Mutex     // guards once, stopped, and all capture/retention state
	once    sync.Once      // ensures first Snapshot wins
	wg      sync.WaitGroup // tracks in-flight async captures (SnapshotIfAsync)
	stopped bool           // set once Stop/Close begins; blocks new async captures
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

	return &Recorder{ //nolint:exhaustruct // mu, once, wg are zero-value
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
	}, nil
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
	return r.snapshot(ctx, captureCtx{ //nolint:exhaustruct // no trigger context for manual captures
		Source: SnapshotSourceManual,
	})
}

// captureCtx carries capture-origin metadata that ends up on [SnapshotEvent].
// Kind and Type are empty for manual captures; they carry [TriggerContext]
// values for triggered and asynchronous captures so metrics hooks can label by
// operation.
type captureCtx struct {
	Source string
	Kind   string
	Type   string
}

// snapshot is the shared once-latched capture for writer sinks. The captureCtx
// labels the capture origin for the [MetricsHook].
func (r *Recorder) snapshot(ctx context.Context, cc captureCtx) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	var (
		snapErr  error
		event    SnapshotEvent
		captured bool
	)

	r.once.Do(func() {
		event, captured, snapErr = r.captureToWriter(cc)
	})

	if captured {
		r.metricsHook(event, snapErr)
	}

	return snapErr
}

// SnapshotToFile is a convenience that writes the trace to a file.
// It creates the file, writes the snapshot, and closes the file.
// Once-semantics apply as with [Recorder.Snapshot].
//
// The context is checked for cancellation before the snapshot begins.
// SnapshotToFile does NOT trigger retention cleanup; use [Recorder.SnapshotToDir]
// for the auto-named, retained directory pattern.
func (r *Recorder) SnapshotToFile(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // standard ctx propagation
	default:
	}

	var (
		err      error
		event    SnapshotEvent
		captured bool
	)

	r.once.Do(func() {
		event, captured, err = r.captureToFile(path, captureCtx{ //nolint:exhaustruct // no trigger context for manual
			Source: SnapshotSourceManual,
		})
	})

	if captured {
		r.metricsHook(event, err)
	}

	return err
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
func (r *Recorder) SnapshotToDir(ctx context.Context) (string, error) {
	return r.snapshotToDir(ctx, captureCtx{ //nolint:exhaustruct // no trigger context for manual
		Source: SnapshotSourceManual,
	})
}

// snapshotToDir is the internal variant that carries capture-origin metadata
// so asynchronous captures ([Recorder.SnapshotIfAsync]) can thread their
// [TriggerContext] through to the metrics hook.
func (r *Recorder) snapshotToDir(ctx context.Context, cc captureCtx) (string, error) {
	if r.snapshotDir == "" {
		return "", &ConfigError{
			Field:      "SnapshotDir",
			Value:      "",
			Constraint: "SnapshotToDir requires WithSnapshotDir",
		}
	}

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

	event, captured, err := r.captureToFile(path, cc)
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
// initiated, false otherwise.
//
// This is the primary method for middleware integration: the middleware
// constructs a [TriggerContext] from the operation result and delegates
// the decision to the trigger function.
func (r *Recorder) SnapshotIf(ctx context.Context, tc TriggerContext, trigger TriggerFunc) bool {
	if trigger == nil || !trigger(tc) {
		return false
	}

	return r.snapshot(ctx, captureCtx{
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
// Returns true only when a capture was actually initiated. Returns false if the
// trigger did not fire OR if the recorder is shutting down ([Recorder.Stop] /
// [Recorder.Close] already began). In the shutdown case the capture is silently
// dropped — no goroutine is spawned.
//
// The capture routes to the configured sink: [WithSnapshotDir] writes a new
// timestamped file; otherwise the writer sink ([WithWriter]/[WithFile]) is used
// with once-semantics.
//
// The context is captured by the goroutine. If the context is cancelled before
// the write begins, the pre-write check skips the capture. Pass a context whose
// lifetime exceeds the write (e.g., detached from the request) if the snapshot
// must survive the caller returning.
//
// [Recorder.Stop] and [Recorder.Close] drain all in-flight async captures
// before stopping, so the goroutine never outlives a clean shutdown.
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

	cc := captureCtx{
		Source: SnapshotSourceAsync,
		Kind:   tc.Kind,
		Type:   tc.Type,
	}

	go func() {
		defer r.wg.Done()

		if snapshotDir != "" {
			_, _ = r.snapshotToDir(ctx, cc)
		} else {
			_ = r.snapshot(ctx, cc)
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

	counter := &countingWriter{w: dest} //nolint:exhaustruct // n is intentionally zero
	start := time.Now()
	err := r.writeCompressed(counter)

	event := SnapshotEvent{ //nolint:exhaustruct // Path is empty for writer snapshots
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
// Reset does not restart a stopped recorder. Call [Recorder.Start] first
// if the recorder has been stopped.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.once = sync.Once{}
}

// captureToWriter writes the buffered trace to the configured writer sink.
// The once.Do in the callers ensures only the first caller reaches it.
// The bool result is false (with a zero event and nil error) when the recorder
// is disabled or has no sink — i.e., nothing was attempted.
func (r *Recorder) captureToWriter(cc captureCtx) (SnapshotEvent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() || r.writer == nil {
		return SnapshotEvent{}, false, nil //nolint:exhaustruct // zero-value: nothing attempted
	}

	event, err := r.writeTrace(r.writer, "", cc)

	return event, true, err
}

// captureToFile writes the buffered trace to a freshly created file at path.
// The bool result is false when the recorder is disabled (no-op).
func (r *Recorder) captureToFile(path string, cc captureCtx) (SnapshotEvent, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.fr.Enabled() {
		return SnapshotEvent{}, false, nil //nolint:exhaustruct // zero-value: nothing attempted
	}

	f, err := os.Create(path)
	if err != nil {
		wrapped := &SnapshotError{Op: "create", Path: path, Err: err}
		r.metricsHook(SnapshotEvent{ //nolint:exhaustruct // no bytes/duration: write never started
			Path:       path,
			Source:     cc.Source,
			Kind:       cc.Kind,
			Type:       cc.Type,
			Compressed: r.compressLevel != 0,
		}, wrapped)

		return SnapshotEvent{}, true, wrapped
	}

	event, writeErr := r.writeTraceFile(f, path, cc)

	return event, true, writeErr
}

// writeTrace performs the trace write to the given sink, applying compression
// when configured, and timing/byte-counting for the [MetricsHook]. The caller
// must hold r.mu.
func (r *Recorder) writeTrace(w io.Writer, path string, cc captureCtx) (SnapshotEvent, error) {
	counter := &countingWriter{w: w} //nolint:exhaustruct // n is intentionally zero

	start := time.Now()
	err := r.writeCompressed(counter)

	event := SnapshotEvent{
		Duration:   time.Since(start),
		Bytes:      counter.n,
		Path:       path,
		Compressed: r.compressLevel != 0,
		Source:     cc.Source,
		Kind:       cc.Kind,
		Type:       cc.Type,
	}

	if err != nil {
		return event, wrapWriteErr(err, path)
	}

	return event, nil
}

// writeTraceFile is writeTrace for an *os.File that must be closed afterwards.
// A close error after a successful write is reported as a SnapshotError.
func (r *Recorder) writeTraceFile(f *os.File, path string, cc captureCtx) (SnapshotEvent, error) {
	event, err := r.writeTrace(f, path, cc)

	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = &SnapshotError{Op: "close", Path: path, Err: closeErr}
	}

	return event, err
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
