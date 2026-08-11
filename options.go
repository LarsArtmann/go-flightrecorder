package flightrecorder

import (
	"compress/gzip"
	"io"
	"os"
	"time"
)

// validate returns a [*ConfigError] if the configuration is invalid.
type recorderConfig struct {
	minAge   time.Duration
	maxBytes uint64
	writer   io.Writer

	compressLevel  int    // 0 = off; otherwise a gzip level (see WithCompression)
	maxSnapshots   int    // 0 = unlimited retention
	snapshotDir    string // empty = no directory sink
	snapshotPrefix string // filename prefix for SnapshotToDir
	metricsHook    MetricsHook
	loggerHook     LoggerHook
}

const (
	defaultSnapshotPrefix = "snapshot-"
	traceSuffix           = ".trace"
	traceGzSuffix         = ".trace.gz"
)

func defaultConfig() recorderConfig {
	return recorderConfig{ //nolint:exhaustruct // retention/dir/compression/hooks use zero-value defaults
		minAge:         defaultMinAge,
		maxBytes:       defaultMaxBytes,
		writer:         io.Discard,
		snapshotPrefix: defaultSnapshotPrefix,
		metricsHook:    noopMetrics,
		loggerHook:     noopLogger,
	}
}

func (c recorderConfig) validate() error {
	if c.minAge <= 0 {
		return &ConfigError{Field: "MinAge", Value: c.minAge, Constraint: "must be positive"}
	}

	if c.maxBytes == 0 {
		return &ConfigError{Field: "MaxBytes", Value: c.maxBytes, Constraint: "must be non-zero"}
	}

	// gzip.NoCompression (0) is reserved as "off" in this API. The remaining
	// valid levels are DefaultCompression (-1), HuffmanOnly (-2), and the
	// explicit 1..9 speed/quality range.
	if c.compressLevel != 0 {
		inRange := c.compressLevel >= gzip.BestSpeed && c.compressLevel <= gzip.BestCompression
		if !inRange && c.compressLevel != gzip.DefaultCompression && c.compressLevel != gzip.HuffmanOnly {
			return &ConfigError{
				Field:      "Compression",
				Value:      c.compressLevel,
				Constraint: "must be 0 (off), -1 (default), -2 (huffman only), or 1..9",
			}
		}
	}

	if c.maxSnapshots < 0 {
		return &ConfigError{
			Field:      "MaxSnapshots",
			Value:      c.maxSnapshots,
			Constraint: "must be non-negative (0 means unlimited)",
		}
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

// WithCompression enables gzip compression of snapshot output. The level maps
// directly to [compress/gzip] constants. A level of 0 (the default) disables
// compression entirely; pass [gzip.DefaultCompression] (-1), [gzip.BestSpeed]
// (1), [gzip.BestCompression] (9), or [gzip.HuffmanOnly] (-2) to enable it.
//
// Compressed snapshot files use the ".trace.gz" extension and are loadable by
// `go tool trace` (supported since Go 1.19).
func WithCompression(level int) Option {
	return func(c *recorderConfig) {
		c.compressLevel = level
	}
}

// WithMaxSnapshots enables retention cleanup for directory-based snapshots. After
// each [Recorder.SnapshotToDir] call (and once during [Recorder.Start]), the
// oldest snapshot files in [WithSnapshotDir] beyond the count n are removed. A
// value of 0 (the default) disables retention.
//
// Cleanup failures are reported via [WithLogger] and never fail a snapshot.
func WithMaxSnapshots(n int) Option {
	return func(c *recorderConfig) {
		c.maxSnapshots = n
	}
}

// WithSnapshotDir configures a directory for [Recorder.SnapshotToDir], which
// writes each snapshot to an auto-generated, timestamped filename inside it.
// The directory is created (with [os.MkdirAll], mode 0o750) on first use. There
// is no implicit default directory — snapshot-to-directory requires this option.
func WithSnapshotDir(dir string) Option {
	return func(c *recorderConfig) {
		c.snapshotDir = dir
	}
}

// WithSnapshotPrefix sets the filename prefix for [Recorder.SnapshotToDir]
// files (e.g. "snapshot-1700000000.trace"). It lets multiple services or
// instances share a directory without colliding. Default: "snapshot-".
func WithSnapshotPrefix(prefix string) Option {
	return func(c *recorderConfig) {
		c.snapshotPrefix = prefix
	}
}

// WithMetrics registers a [MetricsHook] invoked after every snapshot capture
// attempt that reaches the write stage. This is the dependency-free integration
// point for Prometheus, OpenTelemetry, or any metrics backend. The hook must
// return quickly. See [MetricsHook] for details.
func WithMetrics(hook MetricsHook) Option {
	return func(c *recorderConfig) {
		if hook != nil {
			c.metricsHook = hook
		}
	}
}

// WithLogger registers a [LoggerHook] for diagnostic lifecycle events (start,
// stop, cleanup, errors). The hook receives a printf-style format string and
// args. This is the dependency-free integration point for log/slog or any
// logging backend. See [LoggerHook] for details.
func WithLogger(hook LoggerHook) Option {
	return func(c *recorderConfig) {
		if hook != nil {
			c.loggerHook = hook
		}
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
			return 0, &SnapshotError{Op: "create", Path: lf.path, Err: err}
		}

		lf.f = f
	}

	return lf.f.Write(p) //nolint:wrapcheck // direct delegation
}

func (lf *lazyFile) Close() error {
	if lf.f == nil {
		return nil
	}

	// Nil out the handle so repeated Close calls are safe. Without this,
	// a second Close would call Close on an already-closed *os.File and
	// return "file already closed", violating the idempotent Close contract.
	f := lf.f
	lf.f = nil

	return f.Close() //nolint:wrapcheck // direct delegation
}
