package test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/zeotap/ims/internal/resilience"
)

func TestRetry_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := resilience.Retry(context.Background(), 3, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetry_RetriesAndSucceeds(t *testing.T) {
	calls := 0
	sentinel := errors.New("transient")
	err := resilience.Retry(context.Background(), 5, func() error {
		calls++
		if calls < 3 {
			return sentinel
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_GivesUpAfterMax(t *testing.T) {
	calls := 0
	persistent := errors.New("permanent failure")
	err := resilience.Retry(context.Background(), 3, func() error {
		calls++
		return persistent
	})
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestRetry_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int64
	go func() {
		// Cancel after first failure.
		for calls.Load() == 0 {
		}
		cancel()
	}()

	err := resilience.Retry(ctx, 10, func() error {
		calls.Add(1)
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
}
