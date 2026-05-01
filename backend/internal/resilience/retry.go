package resilience

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Retry executes op up to maxAttempts times with exponential backoff + jitter.
// It stops early if ctx is cancelled or op returns nil.
func Retry(ctx context.Context, maxAttempts int, op func() error) error {
	var err error
	for i := 0; i < maxAttempts; i++ {
		if err = op(); err == nil {
			return nil
		}
		if i == maxAttempts-1 {
			break
		}
		wait := backoff(i)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// backoff returns exponential backoff with ±20% jitter.
func backoff(attempt int) time.Duration {
	base := 100 * time.Millisecond
	exp := math.Pow(2, float64(attempt))
	d := time.Duration(float64(base) * exp)
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	jitter := time.Duration(rand.Float64() * 0.2 * float64(d))
	return d + jitter
}
