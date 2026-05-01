package ingest

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// entry wraps a rate limiter with a last-seen timestamp for eviction.
type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// perUserLimiter holds a rate limiter per user ID (or IP fallback).
// Idle entries are evicted by a background janitor to prevent unbounded growth.
type perUserLimiter struct {
	mu      sync.Mutex
	entries map[string]*entry
	rps     rate.Limit
	burst   int
}

func newPerUserLimiter(rps float64, burst int) *perUserLimiter {
	p := &perUserLimiter{
		entries: make(map[string]*entry),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
	go p.janitor()
	return p
}

func (p *perUserLimiter) get(key string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[key]; ok {
		e.lastSeen = time.Now()
		return e.limiter
	}
	e := &entry{
		limiter:  rate.NewLimiter(p.rps, p.burst),
		lastSeen: time.Now(),
	}
	p.entries[key] = e
	return e.limiter
}

// janitor purges entries that have been idle for more than 30 minutes.
// Runs every 5 minutes so map size stays bounded even under many unique users.
func (p *perUserLimiter) janitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		p.mu.Lock()
		for k, e := range p.entries {
			if now.Sub(e.lastSeen) > 30*time.Minute {
				delete(p.entries, k)
			}
		}
		p.mu.Unlock()
	}
}

// RateLimitMiddleware limits requests to rps/burst per user (falls back to IP).
func RateLimitMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
	pl := newPerUserLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			if uid := r.Header.Get("X-User-ID"); uid != "" {
				key = uid
			}
			if !pl.get(key).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
