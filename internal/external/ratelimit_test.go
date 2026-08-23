package external

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterEnforcesMinimumGap(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	lim := NewRateLimiterWithClock(10*time.Second, 60, 1440, func() time.Time { return now }, func(_ context.Context, d time.Duration) error { waits = append(waits, d); now = now.Add(d); return nil })
	if err := lim.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lim.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 10*time.Second {
		t.Fatalf("waits=%v", waits)
	}
}
