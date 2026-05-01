package test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeotap/ims/internal/ingest"
)

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	mw := ingest.RateLimitMiddleware(100, 10)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 10 requests should be allowed (burst=10).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}
}

func TestRateLimit_BlocksOverBurst(t *testing.T) {
	// burst=2 so 3rd request should be 429.
	mw := ingest.RateLimitMiddleware(0.001, 2)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhausted, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

func TestRateLimit_SeparateLimitsPerUser(t *testing.T) {
	// burst=1 per user
	mw := ingest.RateLimitMiddleware(0.001, 1)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(userID string) int {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "10.0.0.1:0"
		req.Header.Set("X-User-ID", userID)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	// user-A: first request OK, second blocked
	if code := makeReq("user-A"); code != http.StatusOK {
		t.Fatalf("user-A first: expected 200 got %d", code)
	}
	if code := makeReq("user-A"); code != http.StatusTooManyRequests {
		t.Fatalf("user-A second: expected 429 got %d", code)
	}

	// user-B: first request still OK (separate limiter)
	if code := makeReq("user-B"); code != http.StatusOK {
		t.Fatalf("user-B first: expected 200 got %d (separate limiter should allow)", code)
	}
}
