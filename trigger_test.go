package flightrecorder_test

import (
	"errors"
	"testing"
	"time"

	flightrecorder "github.com/larsartmann/go-flightrecorder"
)

func TestOnLatency(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnLatency(100 * time.Millisecond)

	tests := []struct {
		name     string
		duration time.Duration
		want     bool
	}{
		{"below threshold", 50 * time.Millisecond, false},
		{"at threshold", 100 * time.Millisecond, false},
		{"above threshold", 150 * time.Millisecond, true},
		{"well above", 5 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tc := flightrecorder.TriggerContext{Duration: tt.duration}
			if got := trigger(tc); got != tt.want {
				t.Errorf("OnLatency(%s)(duration=%s) = %v, want %v",
					100*time.Millisecond, tt.duration, got, tt.want)
			}
		})
	}
}

func TestOnError(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnError()

	if trigger(flightrecorder.TriggerContext{Err: nil}) {
		t.Error("OnError should not fire on nil error")
	}

	if !trigger(flightrecorder.TriggerContext{Err: errors.New("boom")}) {
		t.Error("OnError should fire on non-nil error")
	}
}

func TestOnErrorOrLatency(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnErrorOrLatency(100 * time.Millisecond)
	testErr := errors.New("fail")

	tests := []struct {
		name     string
		duration time.Duration
		err      error
		want     bool
	}{
		{"fast success", 10 * time.Millisecond, nil, false},
		{"slow success", 200 * time.Millisecond, nil, true},
		{"fast error", 10 * time.Millisecond, testErr, true},
		{"slow error", 200 * time.Millisecond, testErr, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tc := flightrecorder.TriggerContext{Duration: tt.duration, Err: tt.err}
			if got := trigger(tc); got != tt.want {
				t.Errorf("OnErrorOrLatency = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnAlways(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnAlways()

	if !trigger(flightrecorder.TriggerContext{}) {
		t.Error("OnAlways should always fire")
	}
}

func TestOnAny(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnAny(
		flightrecorder.OnLatency(100*time.Millisecond),
		flightrecorder.OnError(),
	)

	// Neither condition met.
	if trigger(flightrecorder.TriggerContext{Duration: 10 * time.Millisecond}) {
		t.Error("OnAny should not fire when no trigger fires")
	}

	// Latency condition met.
	if !trigger(flightrecorder.TriggerContext{Duration: 200 * time.Millisecond}) {
		t.Error("OnAny should fire when latency trigger fires")
	}

	// Error condition met.
	if !trigger(flightrecorder.TriggerContext{Err: errors.New("boom")}) {
		t.Error("OnAny should fire when error trigger fires")
	}
}

func TestOnAll(t *testing.T) {
	t.Parallel()

	testErr := errors.New("fail")
	trigger := flightrecorder.OnAll(
		flightrecorder.OnLatency(100*time.Millisecond),
		flightrecorder.OnError(),
	)

	// Only latency met.
	if trigger(flightrecorder.TriggerContext{Duration: 200 * time.Millisecond}) {
		t.Error("OnAll should not fire when only one trigger fires")
	}

	// Only error met.
	if trigger(flightrecorder.TriggerContext{Err: testErr}) {
		t.Error("OnAll should not fire when only one trigger fires")
	}

	// Both met.
	if !trigger(flightrecorder.TriggerContext{Duration: 200 * time.Millisecond, Err: testErr}) {
		t.Error("OnAll should fire when all triggers fire")
	}
}

func TestOnAny_Empty(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnAny()

	if trigger(flightrecorder.TriggerContext{}) {
		t.Error("OnAny with no triggers should never fire")
	}
}

func TestOnAll_Empty(t *testing.T) {
	t.Parallel()

	trigger := flightrecorder.OnAll()

	// Vacuously true: all zero triggers fire.
	if !trigger(flightrecorder.TriggerContext{}) {
		t.Error("OnAll with no triggers should fire (vacuous truth)")
	}
}

func TestTriggerContext_Fields(t *testing.T) {
	t.Parallel()

	tc := flightrecorder.TriggerContext{
		Kind:     "command",
		Type:     "user.create",
		Duration: 42 * time.Millisecond,
		Err:      errors.New("timeout"),
	}

	if tc.Kind != "command" {
		t.Errorf("Kind = %q, want %q", tc.Kind, "command")
	}

	if tc.Type != "user.create" {
		t.Errorf("Type = %q, want %q", tc.Type, "user.create")
	}

	if tc.Duration != 42*time.Millisecond {
		t.Errorf("Duration = %v, want %v", tc.Duration, 42*time.Millisecond)
	}

	if tc.Err == nil {
		t.Error("Err should not be nil")
	}
}
