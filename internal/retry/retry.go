// Package retry provides a small, context-aware exponential backoff helper.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Policy describes an exponential backoff with full jitter.
type Policy struct {
	// MaxAttempts is the total number of attempts, including the first one.
	// Values below 1 are treated as 1.
	MaxAttempts int
	// BaseDelay is the backoff for the first retry; it doubles per attempt.
	BaseDelay time.Duration
	// MaxDelay caps the backoff. Zero means no cap.
	MaxDelay time.Duration
}

// sleep is a variable so tests can intercept waits.
var sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Do runs fn until it succeeds or MaxAttempts is exhausted. It never sleeps
// after the final attempt and aborts early when ctx is cancelled, returning
// the last fn error joined with the context error.
func Do(ctx context.Context, p Policy, fn func(ctx context.Context) error) error {
	attempts := p.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		if attempt == attempts {
			break
		}
		if sleepErr := sleep(ctx, p.delay(attempt)); sleepErr != nil {
			return errors.Join(err, sleepErr)
		}
	}
	return err
}

// delay returns the wait before the next attempt: exponential growth from
// BaseDelay, capped at MaxDelay, with full jitter applied.
func (p Policy) delay(attempt int) time.Duration {
	d := p.BaseDelay << (attempt - 1)
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)) + 1)
}
