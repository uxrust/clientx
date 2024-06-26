package clientx

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var ErrRateLimitExceeded = errors.New("rate limit is exceeded")

// Limiter is a general interface responsible for rate-limiting functional.
type Limiter interface {
	Wait(ctx context.Context) error
	SetBurstAt(at time.Time, burst int)
	SetLimitAt(at time.Time, limit rate.Limit)
}

// This bucket implementation is wrapper around rate.Limiter.
//
// Using adaptive rate-limiting may cause Thundering herd problem, when all clients
// (in our situation - goroutines) simultaneously wait till ResetAt time
// and then immediately hit rate limit (because they're bursting requests).
// See: https://en.wikipedia.org/wiki/Thundering_herd_problem
type adaptiveBucketLimiter struct {
	r               *rate.Limiter
	mu              *sync.Mutex
	nextResetAt     time.Time
	nextResetEvents []func()
}

var _ Limiter = (*adaptiveBucketLimiter)(nil)

func newAdaptiveBucketLimiter(limit rate.Limit, burst int) *adaptiveBucketLimiter {
	return &adaptiveBucketLimiter{
		mu: new(sync.Mutex),
		r:  rate.NewLimiter(limit, burst),
	}
}

func newUnlimitedAdaptiveBucketLimiter() *adaptiveBucketLimiter {
	return newAdaptiveBucketLimiter(rate.Inf, 1)
}

func (l *adaptiveBucketLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	if l.tryReset() {
		for i := range l.nextResetEvents {
			l.nextResetEvents[i]()
		}
		l.nextResetAt = time.Time{}               // reset time
		l.nextResetEvents = l.nextResetEvents[:0] // reset consumed events
	}
	l.mu.Unlock()

	return l.r.Wait(ctx)
}

func (l *adaptiveBucketLimiter) SetBurstAt(at time.Time, burst int) {
	l.insertEvent(validateResetAt(at), func() {
		l.r.SetBurst(burst)
	})
}

func (l *adaptiveBucketLimiter) SetLimitAt(at time.Time, limit rate.Limit) {
	l.insertEvent(validateResetAt(at), func() {
		l.r.SetLimit(limit)
	})
}

func (l *adaptiveBucketLimiter) insertEvent(at time.Time, f func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextResetAt = at
	l.nextResetEvents = append(l.nextResetEvents, f)
}

func (l *adaptiveBucketLimiter) tryReset() bool {
	now := time.Now()
	return l.nextResetAt.Equal(now) || l.nextResetAt.After(now)
}

func validateResetAt(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now()
	}
	return at
}
