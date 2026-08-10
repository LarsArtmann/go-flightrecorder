package flightrecorder

import (
	"errors"
	"fmt"
)

// ErrAlreadyEnabled is returned by [Recorder.Start] when another flight
// recorder is already active in this process. Go's runtime/trace allows
// only one active [runtime/trace.FlightRecorder] at a time.
//
// Callers can check for this error using [errors.Is]:
//
//	if errors.Is(err, flightrecorder.ErrAlreadyEnabled) {
//	    // Another recorder is active — reuse it or stop it first.
//	}
//
// For richer error context, use [errors.As] with [*AlreadyEnabledError].
var ErrAlreadyEnabled = errors.New(
	"flightrecorder: another flight recorder is already active in this process",
)

// ConfigError describes invalid recorder configuration passed to [New].
//
// Callers can inspect the specific field and constraint:
//
//	var cfgErr *flightrecorder.ConfigError
//	if errors.As(err, &cfgErr) {
//	    log.Printf("invalid %s: %s (got %v)", cfgErr.Field, cfgErr.Constraint, cfgErr.Value)
//	}
type ConfigError struct {
	// Field is the configuration option name: "MinAge" or "MaxBytes".
	Field string

	// Value is the invalid value that was provided.
	Value any

	// Constraint describes what the field must satisfy, e.g. "must be positive".
	Constraint string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("flightrecorder: %s %s, got %v", e.Field, e.Constraint, e.Value)
}

// AlreadyEnabledError indicates that [Recorder.Start] was called while
// another flight recorder or tracer is already active in this process.
//
// The [Cause] field holds the underlying runtime error.
//
// Both [errors.Is] with [ErrAlreadyEnabled] and [errors.As] with
// [*AlreadyEnabledError] match this error:
//
//	if errors.Is(err, flightrecorder.ErrAlreadyEnabled) {
//	    // Sentinel check — backward compatible.
//	}
//
//	var ae *flightrecorder.AlreadyEnabledError
//	if errors.As(err, &ae) {
//	    log.Printf("conflict: %v", ae.Cause)
//	}
type AlreadyEnabledError struct {
	// Cause is the underlying runtime/trace error.
	Cause error
}

func (e *AlreadyEnabledError) Error() string {
	return fmt.Sprintf("%s: %s", ErrAlreadyEnabled.Error(), e.Cause)
}

// Is reports whether this error matches the target. Returns true for
// [ErrAlreadyEnabled] so that [errors.Is] backward compatibility is preserved.
func (e *AlreadyEnabledError) Is(target error) bool {
	return target == ErrAlreadyEnabled
}

// Unwrap returns the underlying runtime error, enabling
// [errors.Is] and [errors.As] traversal of the cause chain.
func (e *AlreadyEnabledError) Unwrap() error {
	return e.Cause
}

// SnapshotError describes a failure during trace snapshot capture or
// snapshot file lifecycle.
//
// Callers can inspect the operation and path for diagnostics:
//
//	var snapErr *flightrecorder.SnapshotError
//	if errors.As(err, &snapErr) {
//	    log.Printf("%s %s failed: %v", snapErr.Op, snapErr.Path, snapErr.Err)
//	}
type SnapshotError struct {
	// Op is the operation that failed: "write", "create", or "close".
	Op string

	// Path is the file path involved, empty for writer-based snapshots.
	Path string

	// Err is the underlying error.
	Err error
}

func (e *SnapshotError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("flightrecorder: %s snapshot file %s: %s", e.Op, e.Path, e.Err)
	}

	return fmt.Sprintf("flightrecorder: %s snapshot: %s", e.Op, e.Err)
}

// Unwrap returns the underlying error, enabling [errors.Is] and
// [errors.As] traversal of the cause chain.
func (e *SnapshotError) Unwrap() error {
	return e.Err
}
