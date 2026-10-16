package attemptguard

import (
	"context"
	"testing"
	"time"
)

func TestLocalCounterWindow(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	counter := New(nil, "test", time.Minute)
	counter.now = func() time.Time { return now }
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		if got := counter.Increment(ctx, "a"); got != int64(i) {
			t.Fatalf("increment %d = %d", i, got)
		}
	}
	if counter.Count(ctx, "a") != 3 || counter.Count(ctx, "b") != 0 {
		t.Fatalf("unexpected counts")
	}
	now = now.Add(2 * time.Minute)
	if counter.Count(ctx, "a") != 0 {
		t.Fatalf("count must expire with the window")
	}
	if counter.Increment(ctx, "a") != 1 {
		t.Fatalf("new window must restart at 1")
	}
	counter.Reset(ctx, "a")
	if counter.Count(ctx, "a") != 0 {
		t.Fatalf("reset must clear the count")
	}
	counter.Set(ctx, "flag")
	if counter.Count(ctx, "flag") != 1 {
		t.Fatalf("flag must be set")
	}
}
