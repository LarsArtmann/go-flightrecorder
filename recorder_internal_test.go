package flightrecorder

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// stageTestRecorder is a Recorder with only what stagedFileWrite reads: the
// metrics hook and the compression flag. No flight recorder is started.
func stageTestRecorder(hook func(SnapshotEvent, error)) *Recorder {
	return &Recorder{
		metricsHook:   hook,
		compressLevel: 5,
	}
}

// assertNoLeftovers fails when dir holds anything besides the names in keep.
func assertNoLeftovers(t *testing.T, dir string, keep ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	for _, e := range entries {
		for _, k := range keep {
			if e.Name() == k {
				goto next
			}
		}

		t.Errorf("unexpected leftover %q in %s (staged temp file not cleaned up)", e.Name(), dir)

	next:
	}
}

func TestStagedFileWrite_FailingWriteKeepsPreviousFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.trace")

	const previous = "previous complete snapshot"

	if err := os.WriteFile(path, []byte(previous), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hookCalls := 0

	r := stageTestRecorder(func(SnapshotEvent, error) { hookCalls++ })

	writeErr := errors.New("trace sink exploded")

	event, err := r.stagedFileWrite(
		path,
		captureMeta{Source: SnapshotSourceManual},
		func(w io.Writer) (SnapshotEvent, error) {
			if _, err := io.WriteString(w, strings.Repeat("x", 200_000)); err != nil { // exceeds the 64KB stage buffer
				t.Fatalf("write into stage: %v", err)
			}

			return SnapshotEvent{Bytes: 200_000}, &SnapshotError{Op: "write", Path: path, Err: writeErr}
		},
	)

	var snapErr *SnapshotError

	if !errors.As(err, &snapErr) || snapErr.Op != "write" || !errors.Is(err, writeErr) {
		t.Fatalf("expected the write's own *SnapshotError, got %v", err)
	}

	if event.Bytes != 200_000 {
		t.Errorf("event.Bytes = %d, want 200000", event.Bytes)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if string(got) != previous {
		t.Errorf("file at path was modified by a failed write: %q", got)
	}

	assertNoLeftovers(t, dir, "snapshot.trace")

	if hookCalls != 0 {
		t.Errorf("metricsHook called %d times for a write failure, want 0 (captureOnce reports that one)", hookCalls)
	}
}

// TestStagedFileWrite_AbortedWriteKeepsPreviousFile stands in for a process
// killed mid-write: the write func stops without returning (panic here instead
// of SIGKILL), so no cleanup code runs at all.
func TestStagedFileWrite_AbortedWriteKeepsPreviousFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.trace")

	const previous = "previous complete snapshot"

	if err := os.WriteFile(path, []byte(previous), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := stageTestRecorder(nil)

	defer func() {
		_ = recover() // the aborted write

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		if string(got) != previous {
			t.Errorf("file at path was modified by an aborted write: %q", got)
		}
	}()

	_, _ = r.stagedFileWrite(path, captureMeta{Source: SnapshotSourceManual}, func(w io.Writer) (SnapshotEvent, error) {
		_, _ = io.WriteString(w, "partial")

		panic("aborted mid-write")
	})
}

func TestStagedFileWrite_StageFailureReportsCreateError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// path is an existing directory: the temp file is written fine, the rename onto a
	// directory fails, and the temp file must be removed.
	path := filepath.Join(dir, "snapshot.trace")

	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	var (
		hookEvent SnapshotEvent
		hookErr   error
	)

	r := stageTestRecorder(func(e SnapshotEvent, err error) { hookEvent, hookErr = e, err })

	event, err := r.stagedFileWrite(path, captureMeta{
		Source: SnapshotSourceManual,
		Kind:   "command",
		Type:   "test",
	}, func(w io.Writer) (SnapshotEvent, error) {
		_, err := io.WriteString(w, "complete content")

		return SnapshotEvent{Bytes: int64(len("complete content"))}, err
	})

	var snapErr *SnapshotError

	if !errors.As(err, &snapErr) || snapErr.Op != "create" || snapErr.Path != path {
		t.Fatalf("expected *SnapshotError{Op: create} for the failed rename, got %v", err)
	}

	if event != (SnapshotEvent{}) {
		t.Errorf("event = %+v, want zero event", event)
	}

	if hookErr == nil || hookEvent.Path != path || hookEvent.Source != SnapshotSourceManual ||
		!hookEvent.Compressed || hookEvent.Bytes != 0 {
		t.Errorf("metricsHook failure event = %+v, err = %v", hookEvent, hookErr)
	}

	assertNoLeftovers(t, dir, "snapshot.trace")
}

func TestStagedFileWrite_SuccessUsesOwnerOnlyMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "snapshot.trace")

	// umask would otherwise narrow the mode and hide a wrong requested perm.
	old := syscall.Umask(0)
	defer syscall.Umask(old)

	r := stageTestRecorder(nil)

	event, err := r.stagedFileWrite(
		path,
		captureMeta{Source: SnapshotSourceManual},
		func(w io.Writer) (SnapshotEvent, error) {
			n, err := io.WriteString(w, "complete content")

			return SnapshotEvent{Bytes: int64(n)}, err
		},
	)
	if err != nil {
		t.Fatalf("stagedFileWrite: %v", err)
	}

	if event.Bytes != int64(len("complete content")) {
		t.Errorf("event.Bytes = %d", event.Bytes)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm() != fs.FileMode(0o600) {
		t.Errorf("file mode = %o, want 600", info.Mode().Perm())
	}

	assertNoLeftovers(t, filepath.Dir(path), "snapshot.trace")
}
