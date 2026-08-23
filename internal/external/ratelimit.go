package external

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter centralizes upstream pacing and rolling request ceilings.
// Production Polisen settings are intentionally conservative and can be
// reduced in tests through NewRateLimiterWithClock.
type RateLimiter struct {
	mu         sync.Mutex
	minimumGap time.Duration
	perHour    int
	perDay     int
	requests   []time.Time
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

func NewRateLimiter() *RateLimiter {
	return NewRateLimiterWithClock(10*time.Second, 60, 1440, time.Now, sleepContext)
}

func NewRateLimiterWithClock(gap time.Duration, perHour, perDay int, now func() time.Time, sleep func(context.Context, time.Duration) error) *RateLimiter {
	return &RateLimiter{minimumGap: gap, perHour: perHour, perDay: perDay, now: now, sleep: sleep}
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := r.now()
		r.prune(now)
		wait := time.Duration(0)
		if len(r.requests) > 0 {
			wait = r.minimumGap - now.Sub(r.requests[len(r.requests)-1])
			if wait < 0 {
				wait = 0
			}
		}
		hourCount := 0
		for _, request := range r.requests {
			if request.After(now.Add(-time.Hour)) {
				hourCount++
			}
		}
		if hourCount >= r.perHour {
			d := requestDelay(r.requests, now, time.Hour)
			if d > wait {
				wait = d
			}
		}
		if len(r.requests) >= r.perDay {
			d := requestDelay(r.requests, now, 24*time.Hour)
			if d > wait {
				wait = d
			}
		}
		if wait == 0 {
			r.requests = append(r.requests, now)
			r.mu.Unlock()
			return nil
		}
		r.mu.Unlock()
		if err := r.sleep(ctx, wait); err != nil {
			return fmt.Errorf("väntan på API-hastighetsgräns avbröts: %w", err)
		}
	}
}

func (r *RateLimiter) prune(now time.Time) {
	cutoff := now.Add(-24 * time.Hour)
	i := 0
	for i < len(r.requests) && r.requests[i].Before(cutoff) {
		i++
	}
	r.requests = r.requests[i:]
}
func requestDelay(requests []time.Time, now time.Time, window time.Duration) time.Duration {
	cutoff := now.Add(-window)
	for _, request := range requests {
		if request.After(cutoff) {
			d := request.Add(window).Sub(now)
			if d > 0 {
				return d
			}
		}
	}
	return 0
}
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
