package flightrecorder_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	flightrecorder "github.com/larsartmann/go-flightrecorder"
)

// The tests in this file pin how SnapshotIfAsync treats its context.
//
// The context is read exactly once per capture: by the non-blocking check at
// the top of the capture goroutine, before anything is written. A context that
// is already cancelled there makes the goroutine drop the capture — no bytes,
// no file, no MetricsHook or LoggerHook call — although SnapshotIfAsync has
// already returned true. Past that check the context is never read again: the
// write runs to completion and Stop/Close wait for it.

// awaitSignal waits for a channel the capture goroutine is expected to close.
// Without the timeout a regression that never reaches the signal would hang
// the whole test binary instead of failing the test.
func awaitSignal(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// hookSpy records every MetricsHook call of one test recorder.
type hookSpy struct {
	mu    sync.Mutex
	calls int
	last  flightrecorder.SnapshotEvent
	err   error
	at    time.Time
}

// hookReport is a consistent snapshot of a hookSpy.
type hookReport struct {
	calls int
	last  flightrecorder.SnapshotEvent
	err   error
	at    time.Time
}

func (s *hookSpy) hook(event flightrecorder.SnapshotEvent, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	s.last = event
	s.err = err
	s.at = time.Now()
}

func (s *hookSpy) report() hookReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	return hookReport{calls: s.calls, last: s.last, err: s.err, at: s.at}
}

// lifecycleLogLines are the only logger messages a recorder emits between
// Start and Close when nothing unusual happens.
var lifecycleLogLines = []string{"flightrecorder: started", "flightrecorder: closed"}

// logSpy records every LoggerHook call of one test recorder.
type logSpy struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSpy) logf(format string, _ ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lines = append(s.lines, format)
}

func (s *logSpy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.lines)
}

// gateContext parks the capture goroutine inside the pre-write context check.
// Done() closes started to report that the goroutine has reached the check,
// then blocks until release is closed, so the test can cancel the context at
// exactly that point: after SnapshotIfAsync returned, before anything is
// written. That is the window a request context cancelled at handler return
// lands in.
type gateContext struct {
	context.Context //nolint:containedctx // test double: embeds the cancellable context and gates only Done

	cancel  context.CancelFunc
	started chan struct{}
	release chan struct{}
	reached sync.Once
	opened  sync.Once
}

func newGateContext() *gateContext {
	inner, cancel := context.WithCancel(context.Background())

	return &gateContext{
		Context: inner,
		cancel:  cancel,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *gateContext) Done() <-chan struct{} {
	g.reached.Do(func() { close(g.started) })
	<-g.release

	return g.Context.Done()
}

// unblock lets a parked capture goroutine continue; safe to call twice.
func (g *gateContext) unblock() {
	g.opened.Do(func() { close(g.release) })
}

// newGate returns a gateContext that unblocks its goroutine during cleanup,
// so a test that fails midway cannot leave Recorder.Stop draining forever.
func newGate(t *testing.T) *gateContext {
	t.Helper()

	gate := newGateContext()
	t.Cleanup(gate.unblock)
	t.Cleanup(gate.cancel)

	return gate
}

// blockingWriter parks the first write of an in-flight capture so a test can
// cancel the context while the trace write is in progress, and keeps
// everything that reached the sink.
type blockingWriter struct {
	started chan struct{} // closed on the first Write: the trace write is in progress
	release chan struct{} // every Write returns only once this is closed
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  int
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// Write parks the first call until unblock, then stores p.
func (w *blockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	first := w.writes == 1
	w.mu.Unlock()

	if first {
		close(w.started)
	}

	<-w.release

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

// unblock lets parked writes continue; safe to call twice.
func (w *blockingWriter) unblock() {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.release:
	default:
		close(w.release)
	}
}

func (w *blockingWriter) len() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Len()
}

// sinkState describes what a capture left at a sink. empty stays false once
// anything was created there, so an empty file does not pass as untouched.
type sinkState struct {
	empty bool
	bytes int64
}

// pingPongLoad keeps two goroutines passing a token until stop is closed.
// Each handoff emits scheduler events, the cheapest way to grow the trace.
func pingPongLoad(stop <-chan struct{}) {
	var wg sync.WaitGroup

	ping, pong := make(chan struct{}), make(chan struct{})

	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			case ping <- struct{}{}:
			}

			select {
			case <-stop:
				return
			case <-pong:
			}
		}
	})

	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			case <-ping:
			}

			select {
			case <-stop:
				return
			case pong <- struct{}{}:
			}
		}
	})

	go wg.Wait()
}

// waitForSinkEntry polls dir until it holds an entry and reports how long that
// took. For SnapshotIfAsync the entry appears only once the pre-write check
// passed, so it marks the moment the file write is in progress.
func waitForSinkEntry(dir string) (time.Duration, error) {
	const (
		timeout  = 5 * time.Second
		interval = 50 * time.Microsecond
	)

	start := time.Now()
	deadline := start.Add(timeout)

	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}

		if len(entries) > 0 {
			return time.Since(start), nil
		}

		if time.Now().After(deadline) {
			return 0, errors.New("no file appeared in the snapshot dir before the timeout")
		}

		time.Sleep(interval)
	}
}

// readGzipSnapshot decompresses a compressed snapshot and returns it with the
// on-disk size. A truncated stream fails here, in the deflate stream or at
// the CRC trailer.
func readGzipSnapshot(path string) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, 0, err
	}

	data, err := io.ReadAll(gr)

	return data, info.Size(), errors.Join(err, gr.Close())
}

// asyncSink is a capture sink SnapshotIfAsync can route to. build returns the
// option for one test recorder and a func reporting what reached the sink.
type asyncSink struct {
	name  string
	build func(dir string) (flightrecorder.Option, func() sinkState)
}

// dirSinkState reports the entries in dir and their total size.
func dirSinkState(dir string) sinkState {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sinkState{empty: true}
	}

	var total int64

	for _, e := range entries {
		info, statErr := e.Info()
		if statErr == nil {
			total += info.Size()
		}
	}

	return sinkState{empty: len(entries) == 0, bytes: total}
}

// fileSinkState reports the file at path; a missing file counts as untouched.
func fileSinkState(path string) sinkState {
	info, err := os.Stat(path)
	if err != nil {
		return sinkState{empty: true}
	}

	return sinkState{empty: false, bytes: info.Size()}
}

// asyncSinks covers every sink SnapshotIfAsync can route to. The snapshot dir
// is the directory path; writer and file share the once-latched writer path
// but only the file sink touches the disk.
var asyncSinks = []asyncSink{
	{
		name: "snapshot dir",
		build: func(dir string) (flightrecorder.Option, func() sinkState) {
			return flightrecorder.WithSnapshotDir(dir), func() sinkState { return dirSinkState(dir) }
		},
	},
	{
		name: "writer",
		build: func(string) (flightrecorder.Option, func() sinkState) {
			var buf bytes.Buffer

			return flightrecorder.WithWriter(&buf), func() sinkState {
				return sinkState{empty: buf.Len() == 0, bytes: int64(buf.Len())}
			}
		},
	},
	{
		name: "file",
		build: func(dir string) (flightrecorder.Option, func() sinkState) {
			path := filepath.Join(dir, "lazy.trace")

			return flightrecorder.WithFile(path), func() sinkState { return fileSinkState(path) }
		},
	},
}

// cancelTiming is one moment at which the context can die relative to an
// async capture. start returns the context to pass and the func that brings
// it to the tested state once SnapshotIfAsync has returned.
type cancelTiming struct {
	name  string
	drops bool
	start func(t *testing.T) (context.Context, func())
}

// asyncCancelTimings covers both moments that drop a capture plus the live
// control, which proves the same setup does write.
var asyncCancelTimings = []cancelTiming{
	{
		name:  "cancelled before the call",
		drops: true,
		start: func(*testing.T) (context.Context, func()) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			return ctx, func() {}
		},
	},
	{
		name:  "cancelled after return, before the pre-write check",
		drops: true,
		start: func(t *testing.T) (context.Context, func()) {
			t.Helper()

			gate := newGate(t)

			return gate, func() {
				awaitSignal(t, "the capture goroutine to reach the pre-write check", gate.started)
				gate.cancel()
				gate.unblock()
			}
		},
	},
	{
		name: "live",
		start: func(t *testing.T) (context.Context, func()) {
			t.Helper()

			gate := newGate(t)

			return gate, func() {
				awaitSignal(t, "the capture goroutine to reach the pre-write check", gate.started)
				gate.unblock()
			}
		},
	},
}

// TestRecorder_SnapshotIfAsync_CancelledBeforeWriteDropsCapture pins the two
// timings in which a cancelled context makes SnapshotIfAsync drop the capture
// before anything is written: the context is already dead when the call is
// made, or it dies after the call returned but before the capture goroutine
// ran its pre-write check. SnapshotIfAsync returns true in both cases — it
// reports an initiated capture, not a written one — and nothing else reports
// the drop. The live rows are the control proving the same setup does write
// with a live context.
func TestRecorder_SnapshotIfAsync_CancelledBeforeWriteDropsCapture(t *testing.T) {
	for _, sink := range asyncSinks {
		for _, timing := range asyncCancelTimings {
			t.Run(sink.name+"/"+timing.name, func(t *testing.T) {
				checkAsyncCapture(t, sink, timing)
			})
		}
	}
}

// checkAsyncCapture fires one SnapshotIfAsync into sink with the context of
// timing, drains it, and asserts what the contract promises for that timing.
func checkAsyncCapture(t *testing.T, sink asyncSink, timing cancelTiming) {
	t.Helper()

	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	var (
		spy    hookSpy
		logger logSpy
	)

	option, state := sink.build(dir)

	r, err := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		option,
		flightrecorder.WithMetrics(spy.hook),
		flightrecorder.WithLogger(logger.logf),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	ctx, after := timing.start(t)

	fired := r.SnapshotIfAsync(ctx, flightrecorder.TriggerContext{Kind: "http.request"}, flightrecorder.OnAlways())
	if !fired {
		t.Fatal("expected SnapshotIfAsync to report an initiated capture")
	}

	after()
	r.Close() // drains the capture goroutine and closes a WithFile sink

	got := state()
	hook := spy.report()

	if timing.drops {
		if !got.empty {
			t.Errorf("expected the sink to stay untouched, got %+v", got)
		}

		if hook.calls != 0 {
			t.Errorf("expected no MetricsHook call for a dropped capture, got %d (err=%v)", hook.calls, hook.err)
		}

		if lines := logger.recorded(); !slices.Equal(lines, lifecycleLogLines) {
			t.Errorf("expected no logger line for a dropped capture, got %q", lines)
		}

		return
	}

	if got.empty || got.bytes == 0 {
		t.Fatalf("control: expected the live-context capture to reach the sink, got %+v", got)
	}

	if hook.calls != 1 || hook.err != nil {
		t.Fatalf("control: expected one successful MetricsHook call, got %d calls, err=%v", hook.calls, hook.err)
	}

	if hook.last.Source != flightrecorder.SnapshotSourceAsync {
		t.Errorf("control: Source = %q, want %q", hook.last.Source, flightrecorder.SnapshotSourceAsync)
	}
}

// TestRecorder_SnapshotIfAsync_DroppedCaptureLeavesOnceLatchArmed pins that a
// capture the pre-write check drops does not burn the once-latch: the check
// runs before once.Do, so the next capture with a live context still writes.
func TestRecorder_SnapshotIfAsync_DroppedCaptureLeavesOnceLatchArmed(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	var spy hookSpy

	r, err := flightrecorder.New(flightrecorder.WithWriter(&buf), flightrecorder.WithMetrics(spy.hook))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	gate := newGate(t)

	if !r.SnapshotIfAsync(gate, flightrecorder.TriggerContext{Kind: "http.request"}, flightrecorder.OnAlways()) {
		t.Fatal("expected SnapshotIfAsync to report an initiated capture")
	}

	awaitSignal(t, "the capture goroutine to reach the pre-write check", gate.started)
	gate.cancel()
	gate.unblock()
	r.Stop()

	if buf.Len() != 0 || spy.report().calls != 0 {
		t.Fatalf("expected the cancelled capture to be dropped, got %d bytes and %+v", buf.Len(), spy.report())
	}

	// The writer sink is once-latched: a burnt latch would turn the next
	// capture into a silent no-op.
	if err := r.Start(); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() after a dropped async capture: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected the once-latch to stay armed, but Snapshot() wrote nothing")
	}

	if hook := spy.report(); hook.calls != 1 || hook.last.Source != flightrecorder.SnapshotSourceManual {
		t.Errorf("expected one manual MetricsHook call, got %+v", hook)
	}
}

// TestRecorder_SnapshotIfAsync_MidWriteCancellationDoesNotAbortWrite pins that
// a context cancelled while the trace write is in progress changes nothing:
// the write runs to completion, the hook reports success, and neither the
// cancellation nor Stop cuts it short.
func TestRecorder_SnapshotIfAsync_MidWriteCancellationDoesNotAbortWrite(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	sink := newBlockingWriter()

	var spy hookSpy

	r, err := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(sink),
		flightrecorder.WithMetrics(spy.hook),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)
	t.Cleanup(sink.unblock) // runs before the Stop cleanup: never leave it draining a parked write

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if !r.SnapshotIfAsync(ctx, flightrecorder.TriggerContext{Kind: "http.request"}, flightrecorder.OnAlways()) {
		t.Fatal("expected SnapshotIfAsync to report an initiated capture")
	}

	// The pre-write check passed; WriteTo is parked in its first Write.
	awaitSignal(t, "the trace write to start", sink.started)

	cancel()

	stopped := make(chan struct{})

	go func() {
		r.Stop()
		close(stopped)
	}()

	// Cancelling the context is not an abort: Stop keeps draining.
	select {
	case <-stopped:
		t.Fatal("Stop returned while the async write was still parked")
	case <-time.After(50 * time.Millisecond):
	}

	sink.unblock()

	awaitSignal(t, "Stop to return once the write was released", stopped)

	hook := spy.report()

	if hook.calls != 1 || hook.err != nil {
		t.Fatalf("expected one successful MetricsHook call, got %d calls, err=%v", hook.calls, hook.err)
	}

	if hook.last.Source != flightrecorder.SnapshotSourceAsync || hook.last.Bytes != int64(sink.len()) {
		t.Errorf("hook reported %+v, sink holds %d bytes", hook.last, sink.len())
	}
}

// TestRecorder_SnapshotIfAsync_MidWriteCancellationLeavesFileComplete pins the
// file-sink half of the same contract with a real file write: the context is
// cancelled after the snapshot file appeared on disk (the write is in
// progress) and the file that Stop drains is complete — the gzip stream
// decodes to a full trace and its size matches the hook's byte count.
func TestRecorder_SnapshotIfAsync_MidWriteCancellationLeavesFileComplete(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	var spy hookSpy

	r, err := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithCompression(gzip.BestCompression),
		flightrecorder.WithMetrics(spy.hook),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	// Grow the trace so the compressed write lasts long enough to be
	// cancelled in the middle.
	stopLoad := make(chan struct{})
	pingPongLoad(stopLoad)
	time.Sleep(20 * time.Millisecond)
	close(stopLoad)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if !r.SnapshotIfAsync(ctx, flightrecorder.TriggerContext{Kind: "http.request"}, flightrecorder.OnAlways()) {
		t.Fatal("expected SnapshotIfAsync to report an initiated capture")
	}

	if _, err := waitForSinkEntry(dir); err != nil {
		t.Fatalf("snapshot file never appeared: %v", err)
	}

	cancelledAt := time.Now()
	cancel()
	r.Stop()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Fatalf("expected exactly one snapshot file, got %v", names)
	}

	path := filepath.Join(dir, entries[0].Name())

	data, size, err := readGzipSnapshot(path)
	if err != nil {
		t.Fatalf("snapshot %s is not a complete gzip stream: %v", path, err)
	}

	if !bytes.HasPrefix(data, []byte("go 1.")) {
		t.Fatalf("snapshot %s does not start with a Go trace header", path)
	}

	hook := spy.report()

	if hook.calls != 1 || hook.err != nil {
		t.Fatalf("expected one successful MetricsHook call, got %d calls, err=%v", hook.calls, hook.err)
	}

	if hook.last.Source != flightrecorder.SnapshotSourceAsync || hook.last.Path != path || hook.last.Bytes != size {
		t.Errorf("hook reported %+v, file is %s with %d bytes", hook.last, path, size)
	}

	if !hook.at.After(cancelledAt) {
		t.Fatal(
			"the write had already finished when the context was cancelled; trace too small to pin mid-write cancellation",
		)
	}
}

// TestRecorder_SnapshotIf_CancelledContextReturnsFalse pins the synchronous
// counterpart: SnapshotIf answers false for a cancelled context instead of
// reporting an initiated capture, and it leaves the once-latch armed as well.
func TestRecorder_SnapshotIf_CancelledContextReturnsFalse(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	var spy hookSpy

	r, err := flightrecorder.New(flightrecorder.WithWriter(&buf), flightrecorder.WithMetrics(spy.hook))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if r.SnapshotIf(ctx, flightrecorder.TriggerContext{Kind: "http.request"}, flightrecorder.OnAlways()) {
		t.Fatal("expected SnapshotIf to return false for a cancelled context")
	}

	if buf.Len() != 0 || spy.report().calls != 0 {
		t.Fatalf("expected nothing to be written or reported, got %d bytes and %+v", buf.Len(), spy.report())
	}

	if !r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{Kind: "http.request"},
		flightrecorder.OnAlways(),
	) {
		t.Fatal("expected the once-latch to stay armed, but SnapshotIf declined to capture")
	}

	if buf.Len() == 0 {
		t.Fatal("expected the second SnapshotIf to write, got nothing")
	}
}
