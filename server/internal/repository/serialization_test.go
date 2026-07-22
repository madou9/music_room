package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func serializationErr() error {
	return &pgconn.PgError{Code: "40001"}
}

// noSleep is injected so the retry tests run without real backoff delays.
func noSleep(time.Duration) {}

func TestRetryOnSerialization_SucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := retryOnSerialization(5, noSleep, func() error {
		calls++
		if calls < 3 {
			return serializationErr()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestRetryOnSerialization_GivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	err := retryOnSerialization(5, noSleep, func() error {
		calls++
		return serializationErr()
	})
	if !errors.Is(err, ErrSerializationFailed) {
		t.Fatalf("expected ErrSerializationFailed, got %v", err)
	}
	if calls != 5 {
		t.Errorf("expected 5 attempts, got %d", calls)
	}
}

func TestRetryOnSerialization_DoesNotRetryOtherErrors(t *testing.T) {
	other := errors.New("some other failure")
	calls := 0
	err := retryOnSerialization(5, noSleep, func() error {
		calls++
		return other
	})
	if !errors.Is(err, other) {
		t.Fatalf("expected the original error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected a single attempt for a non-serialization error, got %d", calls)
	}
}

func TestRetryOnSerialization_ReturnsOnFirstSuccess(t *testing.T) {
	calls := 0
	err := retryOnSerialization(5, noSleep, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected a single attempt on immediate success, got %d", calls)
	}
}

func TestRetryOnSerialization_BacksOffBetweenAttemptsOnly(t *testing.T) {
	var sleeps []time.Duration
	capture := func(d time.Duration) { sleeps = append(sleeps, d) }
	_ = retryOnSerialization(5, capture, func() error { return serializationErr() })
	// 5 attempts means a backoff after the first four, none after the last.
	if len(sleeps) != 4 {
		t.Fatalf("expected 4 backoffs across 5 attempts, got %d", len(sleeps))
	}
	for i, d := range sleeps {
		if d <= 0 {
			t.Errorf("backoff %d should be positive, got %v", i, d)
		}
	}
}

func TestSerializationBackoff_GrowsWithAttempt(t *testing.T) {
	// Jitter aside, each step's minimum (the base) at least doubles, so an
	// attempt's lower bound exceeds the previous attempt's upper bound is not
	// guaranteed, but the base floor must grow. Check the floor.
	for attempt := 0; attempt < 4; attempt++ {
		floor := moveRetryBaseBackoff << attempt
		got := serializationBackoff(attempt)
		if got < floor {
			t.Errorf("attempt %d: backoff %v below base floor %v", attempt, got, floor)
		}
		if got >= 2*floor {
			t.Errorf("attempt %d: backoff %v exceeds base+jitter bound %v", attempt, got, 2*floor)
		}
	}
}

func TestIsSerializationFailure(t *testing.T) {
	if !isSerializationFailure(serializationErr()) {
		t.Error("expected 40001 to be a serialization failure")
	}
	if isSerializationFailure(&pgconn.PgError{Code: "23505"}) {
		t.Error("unique violation (23505) should not be a serialization failure")
	}
	if isSerializationFailure(errors.New("plain error")) {
		t.Error("plain error should not be a serialization failure")
	}
}
