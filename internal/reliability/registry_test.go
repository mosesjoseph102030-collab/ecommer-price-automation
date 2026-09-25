package reliability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRegistryIsolatesFailuresByName(t *testing.T) {
	r := NewRegistry(2, time.Minute, nil)
	dead := r.Get("woocommerce:https://dead.example")
	healthy := r.Get("woocommerce:https://healthy.example")

	for i := 0; i < 2; i++ {
		dead.Record(errors.New("connection refused"))
	}
	if dead.Allow() {
		t.Fatal("the failing breaker should be open")
	}
	// The whole point of keying by host: one dead store must not pause a
	// healthy one, or a single outage would stop publishing for every tenant.
	if !healthy.Allow() {
		t.Error("an unrelated store's breaker must stay closed")
	}
	if err := healthy.Do(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Errorf("healthy store should succeed: %v", err)
	}
}

func TestRegistryReturnsTheSameBreakerForAName(t *testing.T) {
	r := NewRegistry(3, time.Minute, nil)
	first := r.Get("paystack")
	second := r.Get("paystack")
	if second != first {
		t.Fatal("the same name must return the same breaker instance")
	}
	// One failure against a threshold of three must NOT open it.
	first.Record(errors.New("boom"))
	if !second.Allow() {
		t.Error("a single failure below the threshold must not open the breaker")
	}
	// Drive it to the threshold to confirm the state is shared, not per-lookup.
	first.Record(errors.New("boom"))
	first.Record(errors.New("boom"))
	if second.Allow() {
		t.Error("state must be shared between lookups of the same name")
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	// Callers construct services without a registry in tests and single-tenant
	// installs; Get must not panic there.
	var r *Registry
	if r.Get("anything") != nil {
		t.Error("a nil registry should yield a nil breaker")
	}
	if r.Names() != nil {
		t.Error("a nil registry should have no names")
	}
	if len(r.States()) != 0 {
		t.Error("a nil registry should have no states")
	}
}

func TestRegistryIsConcurrencySafe(t *testing.T) {
	r := NewRegistry(100, time.Minute, nil)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b := r.Get("dep")
			b.Record(nil)
			_ = b.Allow()
			_ = r.States()
		}(i)
	}
	wg.Wait()
	if len(r.Names()) != 1 {
		t.Errorf("expected exactly one breaker, got %v", r.Names())
	}
}

func TestRegistryStatesReportTheOpenBreaker(t *testing.T) {
	r := NewRegistry(1, time.Minute, nil)
	b := r.Get("notify")
	b.Record(errors.New("smtp timeout"))
	states := r.States()
	state, ok := states["notify"]
	if !ok {
		t.Fatal("the breaker should appear in States")
	}
	if state["state"] != StateOpen {
		t.Errorf("state=%v, want open", state["state"])
	}
	if state["consecutive_failures"] != 1 {
		t.Errorf("failures=%v, want 1", state["consecutive_failures"])
	}
}

// A failure inside the cooldown must not be retry-stormed: the caller is
// expected to honour the short-circuit and skip the dependency.
func TestOpenBreakerSkipsTheDependencyEntirely(t *testing.T) {
	b := NewBreaker("flaky", 1, 30*time.Second, nil)
	b.Record(errors.New("first failure"))
	calls := 0
	for i := 0; i < 5; i++ {
		_ = b.Do(context.Background(), func(context.Context) error {
			calls++
			return nil
		})
	}
	if calls != 0 {
		t.Errorf("dependency was called %d times while the breaker was open, want 0", calls)
	}
}

// A single success after the cooldown closes the breaker, so a transient blip
// does not require operator action.
func TestTransientFailureRecoversWithoutIntervention(t *testing.T) {
	c := &clock{t: time.Now().UTC()}
	b := NewBreaker("flaky", 1, 5*time.Second, nil)
	b.now = c.now
	b.Record(errors.New("transient"))
	if b.Allow() {
		t.Fatal("should be open immediately after the failure")
	}
	c.advance(6 * time.Second)
	if err := b.Do(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("the trial call should succeed: %v", err)
	}
	if state, _, _ := b.State(); state != StateClosed {
		t.Errorf("state=%s, want closed", state)
	}
}
