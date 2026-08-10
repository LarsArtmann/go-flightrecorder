package flightrecorder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Sentinel validation errors.
var (
	errMinAgeMustBePositive   = errors.New("flightrecorder: MinAge must be positive")
	errMaxBytesMustBePositive = errors.New("flightrecorder: MaxBytes must be positive")
)

type recorderConfig struct {
	minAge   time.Duration
	maxBytes uint64
	writer   io.Writer
}

func defaultConfig() recorderConfig {
	return recorderConfig{
		minAge:   defaultMinAge,
		maxBytes: defaultMaxBytes,
		writer:   io.Discard,
	}
}

func (c recorderConfig) validate() error {
	if c.minAge <= 0 {
		return fmt.Errorf("%w: got %s", errMinAgeMustBePositive, c.minAge)
	}

	if c.maxBytes == 0 {
		return errMaxBytesMustBePositive
	}

	return nil
}

// Option configures a [Recorder].
type Option func(*recorderConfig)

// WithMinAge sets the minimum age of trace data that is reliably retained.
// The Go blog recommends setting this to ~2x the time window of the event
// you are debugging. For example, for a 5-second timeout, set 10 seconds.
// Default: 10s.
func WithMinAge(d time.Duration) Option {
	return func(c *recorderConfig) {
		c.minAge = d
	}
}

// WithMaxBytes sets the maximum size of the in-memory trace buffer.
// On average, expect a few MB of trace data per second of execution,
// or 10 MB/s for a busy service. Default: 10 MiB.
func WithMaxBytes(n uint64) Option {
	return func(c *recorderConfig) {
		c.maxBytes = n
	}
}

// WithWriter sets the destination for [Recorder.Snapshot] writes.
// If not set, snapshots are discarded (use [Recorder.SnapshotToFile]
// for file-based capture). For file output, use [WithFile].
func WithWriter(w io.Writer) Option {
	return func(c *recorderConfig) {
		c.writer = w
	}
}

// WithFile sets the snapshot destination to a file at the given path.
// The file is opened (created or truncated) at [Recorder.Snapshot] time.
// For streaming to an existing io.Writer, use [WithWriter] instead.
func WithFile(path string) Option {
	return func(c *recorderConfig) {
		c.writer = &lazyFile{path: path} //nolint:exhaustruct // f is lazily opened
	}
}

// lazyFile opens the file on first write so the file is not created
// until a snapshot is actually captured.
type lazyFile struct {
	path string
	f    *os.File
}

func (lf *lazyFile) Write(p []byte) (int, error) {
	if lf.f == nil {
		f, err := os.Create(lf.path)
		if err != nil {
			return 0, fmt.Errorf("flightrecorder: creating snapshot file %s: %w", lf.path, err)
		}

		lf.f = f
	}

	return lf.f.Write(p) //nolint:wrapcheck // direct delegation
}

func (lf *lazyFile) Close() error {
	if lf.f == nil {
		return nil
	}

	return lf.f.Close() //nolint:wrapcheck // direct delegation
}

// openFile creates or truncates the file at path.
func openFile(path string) (*os.File, error) {
	f, err := os.Create(path) //nolint:gosec // path from user config
	if err != nil {
		return nil, fmt.Errorf("flightrecorder: creating file %s: %w", path, err)
	}

	return f, nil
}
