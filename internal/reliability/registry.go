package reliability

import (
	"database/sql"
	"sync"
	"time"
)

// Registry holds named breakers for outbound dependencies.
//
// One breaker per dependency key is the point. WooCommerce stores are different
// hosts, so a registry keyed by the store's host means a single unreachable store
// trips only its own breaker rather than disabling publishing for every tenant.
type Registry struct {
	threshold int
	cooldown  time.Duration
	// persistence is optional; nil keeps state in process only.
	persistence *sql.DB

	mu       sync.Mutex
	breakers map[string]*Breaker
}

// NewRegistry builds a registry with a shared threshold and cooldown.
// persistence may be nil, in which case state does not survive a restart.
func NewRegistry(threshold int, cooldown time.Duration, persistence *sql.DB) *Registry {
	if threshold < 1 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	return &Registry{breakers: map[string]*Breaker{}, threshold: threshold, cooldown: cooldown, persistence: persistence}
}

// Get returns the breaker for a name, creating it on first use.
//
// The map is locked only for lookup and creation, never for the call itself, so
// one slow dependency cannot serialise unrelated breakers.
func (r *Registry) Get(name string) *Breaker {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.breakers == nil {
		r.breakers = map[string]*Breaker{}
	}
	if b, ok := r.breakers[name]; ok {
		return b
	}
	b := NewBreaker(name, r.threshold, r.cooldown, r.persistence)
	r.breakers[name] = b
	return b
}

// Names lists the known breakers, for diagnostics.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.breakers))
	for name := range r.breakers {
		out = append(out, name)
	}
	return out
}

// States snapshots every breaker for a status endpoint.
func (r *Registry) States() map[string]map[string]any {
	if r == nil {
		return map[string]map[string]any{}
	}
	names := r.Names()
	out := make(map[string]map[string]any, len(names))
	for _, name := range names {
		state, failures, lastErr := r.Get(name).State()
		out[name] = map[string]any{"state": state, "consecutive_failures": failures, "last_error": lastErr}
	}
	return out
}
