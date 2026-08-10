package flightrecorder_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
	r.Start()

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

	r.Start()
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

	r.Start()
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

	r.Start()
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

	r.Start()
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

	r.Start()
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

	r.Start()
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
	r.Start()
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

	r.Start()
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

	r.Start()
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

	r.Start()
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
