package resilience

import (
	"time"

	"github.com/sony/gobreaker"
)

// NewBreaker creates a circuit breaker that opens after 5 consecutive failures
// and half-opens after 30 seconds.
func NewBreaker(name string) *gobreaker.CircuitBreaker {
	return gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})
}
