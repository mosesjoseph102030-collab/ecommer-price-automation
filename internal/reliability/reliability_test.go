package reliability

import (
	"context"
	"testing"
	"time"
)

// clock is a mutable time source so cooldown behaviour can be tested without
// sleeping.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// A breaker must open after the threshold and short-circuit before the
// dependency is called at all, which is what stops a retry storm.
func TestBreakerOpensAfterThreshold(t *testing.T) {
	b := NewBreaker("woocommerce", 3, 30*time.Second, nil)
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("breaker opened too early at attempt %d", i)
		}
		b.Record(context.DeadlineExceeded)
	}
	if b.Allow() {
		t.Error("breaker should be open after the threshold")
	}
	state, failures, _ := b.State()
	if state != StateOpen || failures != 3 {
		t.Errorf("state=%s failures=%d", state, failures)
	}
}

func TestBreakerShortCircuitsTheCall(t *testing.T) {
	b := NewBreaker("ai", 1, time.Minute, nil)
	b.Record(context.DeadlineExceeded)
	called := false
	err := b.Do(context.Background(), func(context.Context) error { called = true; return nil })
	if err == nil {
		t.Error("expected ErrCircuitOpen")
	}
	if called {
		t.Error("the dependency must not be called while the breaker is open")
	}
}

func TestBreakerRecoversToHalfOpenThenClosed(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	b := NewBreaker("notify", 2, 10*time.Second, nil)
	b.now = c.now
	b.Record(context.DeadlineExceeded)
	b.Record(context.DeadlineExceeded)
	if b.Allow() {
		t.Fatal("breaker should be open")
	}
	// Cooldown elapses: the breaker must allow exactly one trial call.
	c.advance(11 * time.Second)
	if !b.Allow() {
		t.Fatal("breaker should allow a trial call after the cooldown")
	}
	if state, _, _ := b.State(); state != StateHalfOpen {
		t.Errorf("state should be half_open, got %s", state)
	}
	// A successful trial closes it.
	if err := b.Do(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := b.State(); state != StateClosed {
		t.Errorf("state should be closed after a successful trial, got %s", state)
	}
}

// A failure during the half-open trial must re-open immediately, not wait for
// the full threshold again.
func TestHalfOpenFailureReopensImmediately(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	b := NewBreaker("db", 5, 10*time.Second, nil)
	b.now = c.now
	for i := 0; i < 5; i++ {
		b.Record(context.DeadlineExceeded)
	}
	c.advance(11 * time.Second)
	if !b.Allow() {
		t.Fatal("expected trial call")
	}
	b.Record(context.DeadlineExceeded)
	if b.Allow() {
		t.Error("a failed trial must re-open the breaker immediately")
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	b := NewBreaker("paystack", 3, time.Minute, nil)
	b.Record(context.DeadlineExceeded)
	b.Record(context.DeadlineExceeded)
	b.Record(nil)
	b.Record(context.DeadlineExceeded)
	b.Record(context.DeadlineExceeded)
	if b.Allow() != true {
		// Two failures after a reset must not trip a threshold of three.
		t.Log("breaker still open, which would be wrong")
	}
	if state, failures, _ := b.State(); state != StateClosed || failures != 2 {
		t.Errorf("state=%s failures=%d, want closed/2", state, failures)
	}
}

func TestHealthSeparatesCriticalFromDegraded(t *testing.T) {
	h := &Health{Timeout: time.Second, Checks: []Check{
		{Name: "database", Critical: true, Run: func(context.Context) error { return nil }},
		{Name: "ai_provider", Critical: false, Run: func(context.Context) error { return context.DeadlineExceeded }},
	}}
	report := h.Run(context.Background())
	if report.Status != "degraded" {
		t.Errorf("a non-critical failure should degrade, got %s", report.Status)
	}
	if report.Critical {
		t.Error("a non-critical failure must not be marked critical")
	}

	h2 := &Health{Checks: []Check{{Name: "database", Critical: true, Run: func(context.Context) error { return context.DeadlineExceeded }}}}
	if r := h2.Run(context.Background()); r.Status != "unhealthy" || !r.Critical {
		t.Errorf("a critical failure must be unhealthy, got %+v", r)
	}
}

func TestHealthAllPassIsOK(t *testing.T) {
	h := &Health{Checks: []Check{{Name: "db", Critical: true, Run: func(context.Context) error { return nil }}}}
	if r := h.Run(context.Background()); r.Status != "ok" || r.Critical {
		t.Errorf("all-pass should be ok, got %+v", r)
	}
}

func TestTruncateBoundsStoredErrors(t *testing.T) {
	long := ""
	for i := 0; i < 5000; i++ {
		long += "x"
	}
	if got := truncate(long, 100); len(got) != 100 {
		t.Errorf("expected 100 chars, got %d", len(got))
	}
}
