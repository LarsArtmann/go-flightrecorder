package flightrecorder_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	flightrecorder "github.com/larsartmann/go-flightrecorder"
)

// recorderMu serializes tests that call Start/Stop because Go's
// runtime/trace allows only ONE active flight recorder per process.
var recorderMu sync.Mutex

func TestNew_DefaultConfig(t *testing.T) {
	t.Parallel()

	r, err := flightrecorder.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if r == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  flightrecorder.Option
	}{
		{"zero minAge", flightrecorder.WithMinAge(0)},
		{"negative minAge", flightrecorder.WithMinAge(-1 * time.Second)},
		{"zero maxBytes", flightrecorder.WithMaxBytes(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := flightrecorder.New(tt.opt)
			if err == nil {
				t.Fatal("expected error for invalid config")
			}
		})
	}
}

func TestRecorder_Lifecycle(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(100*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
	)

	if r.Enabled() {
		t.Fatal("recorder should not be enabled before Start")
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !r.Enabled() {
		t.Fatal("recorder should be enabled after Start")
	}

	r.Stop()

	if r.Enabled() {
		t.Fatal("recorder should not be enabled after Stop")
	}
}

func TestRecorder_StopIdempotent(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New()
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	r.Stop()
	r.Stop()
	r.Stop()
}

func TestRecorder_Snapshot(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty trace data in buffer")
	}
}

func TestRecorder_SnapshotOnceSemantics(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error: %v", err)
	}

	firstSize := buf.Len()
	if firstSize == 0 {
		t.Fatal("expected non-empty trace data after first snapshot")
	}

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot() error: %v", err)
	}

	if buf.Len() != firstSize {
		t.Fatalf("once-semantics violated: buffer grew from %d to %d bytes",
			firstSize, buf.Len())
	}
}

func TestRecorder_ResetAllowsSecondSnapshot(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error: %v", err)
	}

	firstSize := buf.Len()
	if firstSize == 0 {
		t.Fatal("expected data from first snapshot")
	}

	r.Reset()

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot() after Reset error: %v", err)
	}

	if buf.Len() <= firstSize {
		t.Fatalf("Reset should allow new snapshot: buffer %d -> %d",
			firstSize, buf.Len())
	}
}

func TestRecorder_SnapshotWhenNotEnabled(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() when not enabled should be no-op, got error: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatal("expected no data when recorder is not enabled")
	}
}

func TestRecorder_SnapshotToFile(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.trace")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.SnapshotToFile(context.Background(), path); err != nil {
		t.Fatalf("SnapshotToFile() error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("expected non-empty trace file")
	}
}

func TestRecorder_WithFile(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.bin")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithFile(path),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created via WithFile: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("expected non-empty trace file")
	}
}

func TestRecorder_SnapshotIf_TriggersFire(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	fired := r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{
			Kind:     "command",
			Type:     "slow.cmd",
			Duration: 200 * time.Millisecond,
		},
		flightrecorder.OnLatency(100*time.Millisecond),
	)

	if !fired {
		t.Fatal("expected SnapshotIf to fire on slow operation")
	}

	if buf.Len() == 0 {
		t.Fatal("expected trace data after triggered snapshot")
	}
}

func TestRecorder_SnapshotIf_TriggerDoesNotFire(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	fired := r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{
			Kind:     "command",
			Type:     "fast.cmd",
			Duration: 10 * time.Millisecond,
		},
		flightrecorder.OnLatency(100*time.Millisecond),
	)

	if fired {
		t.Fatal("expected SnapshotIf to NOT fire on fast operation")
	}

	if buf.Len() != 0 {
		t.Fatal("expected no trace data when trigger does not fire")
	}
}

func TestRecorder_SnapshotIf_NilTrigger(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New()
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	fired := r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{},
		nil,
	)

	if fired {
		t.Fatal("expected false with nil trigger")
	}
}

func TestRecorder_ConcurrentSnapshots(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{}, 10)

	for range 10 {
		go func() {
			_ = r.Snapshot(context.Background())
			done <- struct{}{}
		}()
	}

	for range 10 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent snapshots")
		}
	}

	if buf.Len() == 0 {
		t.Fatal("expected at least one successful snapshot")
	}
}

func TestRecorder_ErrAlreadyEnabled(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r1, _ := flightrecorder.New()
	r2, _ := flightrecorder.New()

	if err := r1.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	t.Cleanup(r1.Stop)

	err := r2.Start()
	if !errors.Is(err, flightrecorder.ErrAlreadyEnabled) {
		t.Fatalf("expected ErrAlreadyEnabled from second Start(), got: %v", err)
	}
}

func TestRecorder_Close(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "close_test.trace")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithFile(path),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !r.Enabled() {
		t.Fatal("recorder should be enabled after Start")
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if r.Enabled() {
		t.Fatal("recorder should not be enabled after Close")
	}

	// Close should be idempotent.
	if err := r.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestRecorder_SnapshotCancelledContext(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Snapshot(ctx); err == nil {
		t.Fatal("expected error from cancelled context")
	}

	if buf.Len() != 0 {
		t.Fatal("expected no data when context is cancelled before snapshot")
	}
}

func TestRecorder_SnapshotToFileCancelledContext(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "cancelled.trace")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.SnapshotToFile(ctx, path); err == nil {
		t.Fatal("expected error from cancelled context")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file NOT to be created with cancelled context, got err: %v", err)
	}
}

func TestRecorder_LazyFileCloseWithoutSnapshot(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "never_written.trace")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithFile(path),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Close without ever calling Snapshot — lazyFile should not have created the file.
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file NOT to be created when no snapshot was taken, got err: %v", err)
	}
}

func TestRecorder_CloseIdempotentAfterSnapshot(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()
	path := filepath.Join(dir, "written_then_closed.trace")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithFile(path),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Snapshot opens the lazyFile. After this, lf.f is a real *os.File.
	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	// First Close closes the underlying file.
	if err := r.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}

	// Second Close must be safe (documented idempotent) — not "file already closed".
	if err := r.Close(); err != nil {
		t.Fatalf("second Close() after snapshot should be a no-op, got: %v", err)
	}
}

func TestConfigError_TypedMatching(t *testing.T) {
	t.Parallel()

	_, err := flightrecorder.New(flightrecorder.WithMinAge(-1 * time.Second))
	if err == nil {
		t.Fatal("expected error for negative MinAge")
	}

	cfgErr, ok := errors.AsType[*flightrecorder.ConfigError](err)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}

	if cfgErr.Field != "MinAge" {
		t.Errorf("Field = %q, want %q", cfgErr.Field, "MinAge")
	}

	if cfgErr.Constraint != "must be positive" {
		t.Errorf("Constraint = %q, want %q", cfgErr.Constraint, "must be positive")
	}
}

func TestConfigError_MaxBytesZero(t *testing.T) {
	t.Parallel()

	_, err := flightrecorder.New(flightrecorder.WithMaxBytes(0))
	if err == nil {
		t.Fatal("expected error for zero MaxBytes")
	}

	cfgErr, ok := errors.AsType[*flightrecorder.ConfigError](err)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}

	if cfgErr.Field != "MaxBytes" {
		t.Errorf("Field = %q, want %q", cfgErr.Field, "MaxBytes")
	}
}

func TestAlreadyEnabledError_TypedMatching(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r1, _ := flightrecorder.New()
	r2, _ := flightrecorder.New()

	if err := r1.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	t.Cleanup(r1.Stop)

	err := r2.Start()

	// errors.Is with sentinel — backward compatible.
	if !errors.Is(err, flightrecorder.ErrAlreadyEnabled) {
		t.Fatalf("errors.Is(err, ErrAlreadyEnabled) = false, err: %v", err)
	}

	// errors.As with typed error — richer context.
	ae, ok := errors.AsType[*flightrecorder.AlreadyEnabledError](err)
	if !ok {
		t.Fatalf("expected *AlreadyEnabledError, got %T: %v", err, err)
	}

	if ae.Cause == nil {
		t.Error("AlreadyEnabledError.Cause should not be nil")
	}
}

func TestSnapshotError_WriteFailure(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&failingWriter{}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	err := r.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected error from failing writer")
	}

	snapErr, ok := errors.AsType[*flightrecorder.SnapshotError](err)
	if !ok {
		t.Fatalf("expected *SnapshotError, got %T: %v", err, err)
	}

	if snapErr.Op != "write" {
		t.Errorf("Op = %q, want %q", snapErr.Op, "write")
	}
}

func TestSnapshotError_FileCreationFailure(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithFile("/nonexistent/dir/trace.bin"),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	err := r.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected error from invalid file path")
	}

	snapErr, ok := errors.AsType[*flightrecorder.SnapshotError](err)
	if !ok {
		t.Fatalf("expected *SnapshotError, got %T: %v", err, err)
	}

	if snapErr.Op != "create" {
		t.Errorf("Op = %q, want %q", snapErr.Op, "create")
	}

	if snapErr.Path != "/nonexistent/dir/trace.bin" {
		t.Errorf("Path = %q, want %q", snapErr.Path, "/nonexistent/dir/trace.bin")
	}
}

// failingWriter is an [io.Writer] that always returns an error.
type failingWriter struct{}

func (*failingWriter) Write(_ []byte) (int, error) {
	return 0, errWriteFailed
}

var errWriteFailed = errors.New("write failed")

// --- Theme 1: Compression ---

func TestRecorder_Compression_WriterGzip(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
		flightrecorder.WithCompression(gzip.BestSpeed),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty compressed output")
	}

	// Verify it is valid gzip that decompresses to trace data.
	gr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("compressed output is not valid gzip: %v", err)
	}

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip decompress failed: %v", err)
	}

	if len(decompressed) == 0 {
		t.Fatal("expected non-empty decompressed trace data")
	}
}

func TestRecorder_Compression_InvalidLevel(t *testing.T) {
	t.Parallel()

	_, err := flightrecorder.New(flightrecorder.WithCompression(99))
	if err == nil {
		t.Fatal("expected error for invalid compression level 99")
	}

	_, ok := errors.AsType[*flightrecorder.ConfigError](err)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}
}

// --- Theme 2: Retention ---

func TestRecorder_Retention_LimitsFiles(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithMaxSnapshots(3),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	for i := range 5 {
		time.Sleep(2 * time.Millisecond) // ensure distinct mod times

		path, err := r.SnapshotToDir(context.Background())
		if err != nil {
			t.Fatalf("SnapshotToDir #%d error: %v", i, err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Fatalf("snapshot file %s not created: %v", path, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count != 3 {
		t.Fatalf("expected 3 retained snapshots, got %d", count)
	}
}

func TestRecorder_Retention_ZeroIsUnlimited(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithMaxSnapshots(0), // unlimited
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	for range 4 {
		time.Sleep(2 * time.Millisecond)
		if _, err := r.SnapshotToDir(context.Background()); err != nil {
			t.Fatalf("SnapshotToDir error: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count != 4 {
		t.Fatalf("expected 4 snapshots with unlimited retention, got %d", count)
	}
}

func TestRecorder_Retention_InvalidNegative(t *testing.T) {
	t.Parallel()

	_, err := flightrecorder.New(flightrecorder.WithMaxSnapshots(-1))
	if err == nil {
		t.Fatal("expected error for negative MaxSnapshots")
	}

	_, ok := errors.AsType[*flightrecorder.ConfigError](err)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}
}

// --- Theme 3: SnapshotToDir ---

func TestRecorder_SnapshotToDir(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	path, err := r.SnapshotToDir(context.Background())
	if err != nil {
		t.Fatalf("SnapshotToDir() error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("expected non-empty trace file")
	}

	if !strings.HasPrefix(filepath.Base(path), "snapshot-") {
		t.Errorf("expected default prefix, got %s", filepath.Base(path))
	}

	if !strings.HasSuffix(path, ".trace") {
		t.Errorf("expected .trace suffix, got %s", path)
	}
}

func TestRecorder_SnapshotToDir_MultipleDistinctFiles(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	path1, err := r.SnapshotToDir(context.Background())
	if err != nil {
		t.Fatalf("first SnapshotToDir error: %v", err)
	}

	time.Sleep(2 * time.Millisecond) // distinct timestamps

	path2, err := r.SnapshotToDir(context.Background())
	if err != nil {
		t.Fatalf("second SnapshotToDir error: %v", err)
	}

	if path1 == path2 {
		t.Fatal("expected two distinct files, got the same path")
	}
}

func TestRecorder_SnapshotToDir_WithoutDirReturnsConfigError(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New()

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	_, err := r.SnapshotToDir(context.Background())
	if err == nil {
		t.Fatal("expected error when SnapshotToDir called without WithSnapshotDir")
	}

	_, ok := errors.AsType[*flightrecorder.ConfigError](err)
	if !ok {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}
}

func TestRecorder_SnapshotToDir_CreatesMissingDirectory(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	base := t.TempDir()
	dir := filepath.Join(base, "nested", "snapshots")

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if _, err := r.SnapshotToDir(context.Background()); err != nil {
		t.Fatalf("SnapshotToDir error: %v", err)
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected directory created at %s, err: %v", dir, err)
	}
}

func TestRecorder_SnapshotToDir_CustomPrefix(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithSnapshotPrefix("svc-a-"),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	path, err := r.SnapshotToDir(context.Background())
	if err != nil {
		t.Fatalf("SnapshotToDir error: %v", err)
	}

	if !strings.HasPrefix(filepath.Base(path), "svc-a-") {
		t.Errorf("expected custom prefix 'svc-a-', got %s", filepath.Base(path))
	}
}

func TestRecorder_SnapshotToDir_CompressionProducesGzExtension(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithCompression(gzip.BestSpeed),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	path, err := r.SnapshotToDir(context.Background())
	if err != nil {
		t.Fatalf("SnapshotToDir error: %v", err)
	}

	if !strings.HasSuffix(path, ".trace.gz") {
		t.Fatalf("expected .trace.gz suffix, got %s", path)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("file is not valid gzip: %v", err)
	}

	data, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip decompress failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty decompressed trace data")
	}
}

// --- Theme 4: Non-blocking capture + graceful drain ---

func TestRecorder_SnapshotIfAsync_FiresAndDrains(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fired := r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{Kind: "command", Duration: 200 * time.Millisecond},
		flightrecorder.OnLatency(100*time.Millisecond),
	)

	if !fired {
		t.Fatal("expected SnapshotIfAsync to fire")
	}

	// Stop must drain the in-flight async capture before returning.
	r.Stop()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count != 1 {
		t.Fatalf("expected 1 snapshot drained before Stop, got %d", count)
	}
}

func TestRecorder_SnapshotIfAsync_DoesNotFire(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	fired := r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{Kind: "command", Duration: 10 * time.Millisecond},
		flightrecorder.OnLatency(100*time.Millisecond),
	)

	if fired {
		t.Fatal("expected SnapshotIfAsync to NOT fire on fast operation")
	}
}

func TestRecorder_SnapshotIfAsync_NilTrigger(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	r, _ := flightrecorder.New()
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	fired := r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{},
		nil,
	)

	if fired {
		t.Fatal("expected false with nil trigger")
	}
}

func TestRecorder_SnapshotIfAsync_RoutesToWriterSink(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(&buf),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	fired := r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{},
		flightrecorder.OnAlways(),
	)

	if !fired {
		t.Fatal("expected SnapshotIfAsync to fire with OnAlways")
	}

	r.Stop()

	if buf.Len() == 0 {
		t.Fatal("expected trace data in writer after async capture drained")
	}
}

// --- Theme 5: Observability hooks ---

func TestRecorder_MetricsHook_FiresWithEvent(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var (
		mu        sync.Mutex
		event     flightrecorder.SnapshotEvent
		hookErr   error
		callCount int
	)

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(io.Discard),
		flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, err error) {
			mu.Lock()
			defer mu.Unlock()

			callCount++
			event = e
			hookErr = err
		}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if callCount != 1 {
		t.Fatalf("expected metrics hook called once, got %d", callCount)
	}

	if event.Bytes == 0 {
		t.Error("expected non-zero Bytes in event")
	}

	if event.Source != flightrecorder.SnapshotSourceManual {
		t.Errorf("Source = %q, want %q", event.Source, flightrecorder.SnapshotSourceManual)
	}

	if hookErr != nil {
		t.Errorf("expected nil error in hook, got %v", hookErr)
	}
}

func TestRecorder_MetricsHook_NotCalledOnNoOp(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var callCount atomic.Int64

	r, _ := flightrecorder.New(
		flightrecorder.WithWriter(io.Discard),
		flightrecorder.WithMetrics(func(flightrecorder.SnapshotEvent, error) {
			callCount.Add(1)
		}),
	)

	// Not started — Snapshot is a no-op, hook must not fire.
	if err := r.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	if callCount.Load() != 0 {
		t.Fatalf("expected metrics hook NOT called on disabled no-op, got %d", callCount.Load())
	}
}

func TestRecorder_MetricsHook_SourceLabels(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var mu sync.Mutex
	sources := []string{}

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(io.Discard),
		flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, _ error) {
			mu.Lock()
			sources = append(sources, e.Source)
			mu.Unlock()
		}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	// manual via Snapshot
	_ = r.Snapshot(context.Background())
	// trigger via SnapshotIf (once already fired, so this is a no-op for the write,
	// but the hook records the attempt only when a capture runs)
	r.Reset()
	time.Sleep(50 * time.Millisecond)
	_ = r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{Duration: 200 * time.Millisecond},
		flightrecorder.OnLatency(100*time.Millisecond),
	)

	mu.Lock()
	defer mu.Unlock()

	if len(sources) == 0 {
		t.Fatal("expected at least one source recorded")
	}

	for _, s := range sources {
		if s != flightrecorder.SnapshotSourceManual && s != flightrecorder.SnapshotSourceTrigger {
			t.Errorf("unexpected source %q", s)
		}
	}
}

func TestRecorder_LoggerHook_LifecycleEvents(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var mu sync.Mutex
	messages := []string{}

	r, _ := flightrecorder.New(
		flightrecorder.WithLogger(func(format string, _ ...any) {
			mu.Lock()
			messages = append(messages, format)
			mu.Unlock()
		}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	r.Stop()

	mu.Lock()
	defer mu.Unlock()

	joined := strings.Join(messages, "|")
	if !strings.Contains(joined, "started") {
		t.Errorf("expected 'started' log message, got: %s", joined)
	}

	if !strings.Contains(joined, "stopped") {
		t.Errorf("expected 'stopped' log message, got: %s", joined)
	}
}

// --- Theme 6: Nil-safe receivers ---

func TestRecorder_NilSafe_Enabled(t *testing.T) {
	t.Parallel()

	var r *flightrecorder.Recorder

	if r.Enabled() {
		t.Fatal("nil receiver Enabled() should return false")
	}
}

func TestRecorder_NilSafe_Stop(t *testing.T) {
	t.Parallel()

	var r *flightrecorder.Recorder

	r.Stop() // must not panic
}

func TestRecorder_NilSafe_Close(t *testing.T) {
	t.Parallel()

	var r *flightrecorder.Recorder

	if err := r.Close(); err != nil {
		t.Fatalf("nil receiver Close() should return nil, got: %v", err)
	}
}

// --- SnapshotToWriter (low-level escape hatch) ---

func TestRecorder_SnapshotToWriter(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var buf bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	n, err := r.SnapshotToWriter(context.Background(), &buf)
	if err != nil {
		t.Fatalf("SnapshotToWriter error: %v", err)
	}

	if n == 0 || buf.Len() == 0 {
		t.Fatal("expected non-zero bytes written to the writer")
	}

	if int(n) != buf.Len() {
		t.Errorf("returned bytes %d != buffer len %d", n, buf.Len())
	}
}

func TestRecorder_SnapshotToWriter_NotOnceLatched(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var first, second bytes.Buffer

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	if _, err := r.SnapshotToWriter(context.Background(), &first); err != nil {
		t.Fatalf("first SnapshotToWriter error: %v", err)
	}

	if _, err := r.SnapshotToWriter(context.Background(), &second); err != nil {
		t.Fatalf("second SnapshotToWriter error: %v", err)
	}

	if first.Len() == 0 || second.Len() == 0 {
		t.Fatal("expected both writes to produce data (not once-latched)")
	}
}

func TestRecorder_AsyncWorksAfterRestart(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_ = r.SnapshotIfAsync(context.Background(), flightrecorder.TriggerContext{}, flightrecorder.OnAlways())
	r.Stop()

	if err := r.Start(); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	// After Stop→Start, async capture must work again (stopped flag reset).
	_ = r.SnapshotIfAsync(context.Background(), flightrecorder.TriggerContext{}, flightrecorder.OnAlways())
	r.Stop()

	if got := countFiles(t, dir); got != 2 {
		t.Fatalf("expected 2 snapshots after restart cycle, got %d", got)
	}
}

// --- Integration: compression + dir + retention + metrics together ---

// countFiles returns the number of non-directory entries in dir.
func countFiles(t *testing.T, dir string) int {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error: %v", dir, err)
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}

	return count
}

func TestRecorder_Integration_CompressDirRetentionMetrics(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	var (
		mu         sync.Mutex
		eventCount int
		totalBytes int64
		allGzipped = true
	)

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithCompression(gzip.BestSpeed),
		flightrecorder.WithMaxSnapshots(2),
		flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, _ error) {
			mu.Lock()
			defer mu.Unlock()

			eventCount++
			totalBytes += e.Bytes
			if !e.Compressed {
				allGzipped = false
			}
		}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	for range 4 {
		time.Sleep(2 * time.Millisecond)

		if _, err := r.SnapshotToDir(context.Background()); err != nil {
			t.Fatalf("SnapshotToDir error: %v", err)
		}
	}

	if got := countFiles(t, dir); got != 2 {
		t.Fatalf("expected 2 retained compressed snapshots, got %d", got)
	}

	verifyAllGzExtension(t, dir)

	mu.Lock()
	defer mu.Unlock()

	if eventCount != 4 {
		t.Errorf("expected 4 metrics events, got %d", eventCount)
	}

	if totalBytes == 0 {
		t.Error("expected non-zero total bytes across events")
	}

	if !allGzipped {
		t.Error("expected all events to report Compressed=true")
	}
}

// verifyAllGzExtension fails the test if any file in dir lacks the .trace.gz suffix.
func verifyAllGzExtension(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".trace.gz") {
			t.Errorf("expected .trace.gz suffix, got %s", entry.Name())
		}
	}
}

// --- Reset + async interaction sanity ---

func TestRecorder_AsyncDrainsOnClose(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_ = r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{},
		flightrecorder.OnAlways(),
	)

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count != 1 {
		t.Fatalf("expected 1 snapshot drained before Close, got %d", count)
	}
}

func TestRecorder_SnapshotIfAsync_ReturnsFalseDuringShutdown(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop sets r.stopped = true; subsequent async captures must return false.
	r.Stop()

	fired := r.SnapshotIfAsync(
		context.Background(),
		flightrecorder.TriggerContext{Kind: "command", Type: "test.run"},
		flightrecorder.OnAlways(),
	)

	if fired {
		t.Fatal("expected SnapshotIfAsync to return false when recorder is stopped")
	}

	// No capture should have been produced.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			t.Fatalf("expected no snapshot files after stopped async, found %s", e.Name())
		}
	}
}

func TestRecorder_SnapshotIfAsync_ConcurrentStress(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	dir := t.TempDir()

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithSnapshotDir(dir),
		flightrecorder.WithMaxSnapshots(100),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			_ = r.SnapshotIfAsync(
				context.Background(),
				flightrecorder.TriggerContext{Kind: "http.request", Type: "GET /api"},
				flightrecorder.OnAlways(),
			)
		}()
	}

	wg.Wait()

	// Stop must drain all in-flight async captures without deadlock or panic.
	r.Stop()

	// Verify the directory contains files (at least some captures completed).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	count := 0

	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count == 0 {
		t.Fatal("expected at least one snapshot file after concurrent stress + drain")
	}

	// With MaxSnapshots(100) and 50 goroutines, retention should cap at 100.
	if count > 100 {
		t.Fatalf("expected at most 100 snapshots (retention limit), got %d", count)
	}
}

func TestRecorder_SnapshotIf_ThreadsKindAndTypeToMetricsHook(t *testing.T) {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var (
		mu    sync.Mutex
		event flightrecorder.SnapshotEvent
		got   bool
	)

	r, _ := flightrecorder.New(
		flightrecorder.WithMinAge(50*time.Millisecond),
		flightrecorder.WithMaxBytes(1<<20),
		flightrecorder.WithWriter(io.Discard),
		flightrecorder.WithMetrics(func(e flightrecorder.SnapshotEvent, _ error) {
			mu.Lock()
			defer mu.Unlock()

			event = e
			got = true
		}),
	)

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(r.Stop)

	time.Sleep(100 * time.Millisecond)

	fired := r.SnapshotIf(
		context.Background(),
		flightrecorder.TriggerContext{
			Kind: "http.request",
			Type: "GET /api/users",
		},
		flightrecorder.OnAlways(),
	)

	if !fired {
		t.Fatal("expected SnapshotIf to fire")
	}

	mu.Lock()
	defer mu.Unlock()

	if !got {
		t.Fatal("expected metrics hook to fire")
	}

	if event.Kind != "http.request" {
		t.Fatalf("expected Kind='http.request', got %q", event.Kind)
	}

	if event.Type != "GET /api/users" {
		t.Fatalf("expected Type='GET /api/users', got %q", event.Type)
	}

	if event.Source != flightrecorder.SnapshotSourceTrigger {
		t.Fatalf("expected Source=%q, got %q", flightrecorder.SnapshotSourceTrigger, event.Source)
	}
}
