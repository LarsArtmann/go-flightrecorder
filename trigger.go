package flightrecorder

import "time"

// TriggerContext describes an operation that just completed.
// It is passed to [TriggerFunc] so the trigger can decide whether
// the operation warrants a flight recorder snapshot.
type TriggerContext struct {
	// Kind is the operation category: "command", "event", "query",
	// "projection", or any custom string.
	Kind string

	// Type is the specific message or operation type
	// (e.g., "user.created", "order.processed").
	Type string

	// Duration is how long the operation took.
	Duration time.Duration

	// Err is the error returned by the operation, or nil on success.
	Err error
}

// TriggerFunc decides whether a flight recorder snapshot should be
// captured based on the operation context.
//
// Return true to capture a snapshot, false to skip.
type TriggerFunc func(TriggerContext) bool

// OnLatency returns a trigger that fires when an operation's duration
// exceeds the given threshold. Use this to capture traces of slow
// commands, queries, or event handlers.
//
// Example: capture when any operation exceeds 100ms.
//
//	trigger := flightrecorder.OnLatency(100 * time.Millisecond)
func OnLatency(threshold time.Duration) TriggerFunc {
	return func(tc TriggerContext) bool {
		return tc.Duration > threshold
	}
}

// OnError returns a trigger that fires when an operation returns a
// non-nil error. Use this to capture traces of failures for root-cause
// analysis.
func OnError() TriggerFunc {
	return func(tc TriggerContext) bool {
		return tc.Err != nil
	}
}

// OnErrorOrLatency returns a trigger that fires when an operation
// either errors OR exceeds the given duration threshold. This is the
// most common trigger for production debugging — you want traces for
// both failures and latency spikes.
func OnErrorOrLatency(threshold time.Duration) TriggerFunc {
	return func(tc TriggerContext) bool {
		return tc.Err != nil || tc.Duration > threshold
	}
}

// OnAlways returns a trigger that always fires. Useful for testing
// or for capturing a baseline trace on the first operation after startup.
func OnAlways() TriggerFunc {
	return func(_ TriggerContext) bool {
		return true
	}
}

// OnAny returns a trigger that fires if any of the given triggers fire.
// This allows combining independent conditions:
//
//	// Fire on errors, or on commands that exceed 200ms:
//	trigger := flightrecorder.OnAny(
//	    flightrecorder.OnError(),
//	    flightrecorder.OnLatency(200*time.Millisecond),
//	)
func OnAny(triggers ...TriggerFunc) TriggerFunc {
	return func(tc TriggerContext) bool {
		for _, t := range triggers {
			if t(tc) {
				return true
			}
		}

		return false
	}
}

// OnAll returns a trigger that fires only if all given triggers fire.
// Useful for narrowing the capture window:
//
//	// Fire only on slow errors (not fast errors or slow successes):
//	trigger := flightrecorder.OnAll(
//	    flightrecorder.OnError(),
//	    flightrecorder.OnLatency(500*time.Millisecond),
//	)
func OnAll(triggers ...TriggerFunc) TriggerFunc {
	return func(tc TriggerContext) bool {
		for _, t := range triggers {
			if !t(tc) {
				return false
			}
		}

		return true
	}
}
