package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// interceptSleep replaces the package sleep with a recorder for the duration
// of a test.
func interceptSleep(t *testing.T, fn func(ctx context.Context, d time.Duration) error) *int {
	t.Helper()
	calls := 0
	original := sleep
	sleep = func(ctx context.Context, d time.Duration) error {
		calls++
		return fn(ctx, d)
	}
	t.Cleanup(func() { sleep = original })
	return &calls
}

func TestDoSucceedsWithoutSleeping(t *testing.T) {
	sleeps := interceptSleep(t, func(context.Context, time.Duration) error { return nil })

	attempts := 0
	err := Do(context.Background(), Policy{MaxAttempts: 3, BaseDelay: time.Second}, func(context.Context) error {
		attempts++
		return nil
	})

	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if *sleeps != 0 {
		t.Errorf("sleeps = %d, want 0", *sleeps)
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	sleeps := interceptSleep(t, func(context.Context, time.Duration) error { return nil })

	attempts := 0
	err := Do(context.Background(), Policy{MaxAttempts: 5, BaseDelay: time.Second}, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Do() = %v, want nil", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if *sleeps != 2 {
		t.Errorf("sleeps = %d, want 2", *sleeps)
	}
}

func TestDoNeverSleepsAfterFinalAttempt(t *testing.T) {
	sleeps := interceptSleep(t, func(context.Context, time.Duration) error { return nil })

	wantErr := errors.New("permanent")
	attempts := 0
	err := Do(context.Background(), Policy{MaxAttempts: 3, BaseDelay: time.Second}, func(context.Context) error {
		attempts++
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("Do() = %v, want %v", err, wantErr)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if *sleeps != 2 {
		t.Errorf("sleeps = %d, want 2 (must not sleep after the last attempt)", *sleeps)
	}
}

func TestDoStopsWhenContextCancelledDuringBackoff(t *testing.T) {
	interceptSleep(t, func(ctx context.Context, _ time.Duration) error { return context.Canceled })

	fnErr := errors.New("transient")
	attempts := 0
	err := Do(context.Background(), Policy{MaxAttempts: 5, BaseDelay: time.Second}, func(context.Context) error {
		attempts++
		return fnErr
	})

	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if !errors.Is(err, fnErr) || !errors.Is(err, context.Canceled) {
		t.Errorf("Do() = %v, want error wrapping both %v and %v", err, fnErr, context.Canceled)
	}
}

func TestDoTreatsZeroAttemptsAsOne(t *testing.T) {
	attempts := 0
	_ = Do(context.Background(), Policy{}, func(context.Context) error {
		attempts++
		return errors.New("boom")
	})
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestDelayGrowsAndRespectsCap(t *testing.T) {
	p := Policy{MaxAttempts: 10, BaseDelay: time.Second, MaxDelay: 4 * time.Second}
	for attempt := 1; attempt <= 6; attempt++ {
		uncapped := p.BaseDelay << (attempt - 1)
		limit := min(uncapped, p.MaxDelay)
		for range 50 {
			d := p.delay(attempt)
			if d <= 0 || d > limit {
				t.Fatalf("delay(%d) = %v, want in (0, %v]", attempt, d, limit)
			}
		}
	}
}
