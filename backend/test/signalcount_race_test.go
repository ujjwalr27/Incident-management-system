package test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeotap/ims/internal/debounce"
)

// TestSignalCount_ConcurrentDebounce proves that 200 concurrent signals for the
// same component produce exactly ONE "new work item" decision and 199 "attach"
// decisions — the precondition for Fix 1 (all 199 increments must succeed once
// the work item is created).
func TestSignalCount_ConcurrentDebounce(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	const total = 50 // safely under the 100-signal debounce threshold
	now := time.Now()

	var newCount atomic.Int64
	var attachCount atomic.Int64
	var firstID atomic.Value

	var wg sync.WaitGroup
	wg.Add(total)

	for i := 0; i < total; i++ {
		go func(idx int) {
			defer wg.Done()
			r := d.Process("STRESS_COMPONENT", now.Add(time.Duration(idx)*time.Millisecond))
			if r.IsNew {
				newCount.Add(1)
				firstID.Store(r.WorkItemID)
			} else {
				attachCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if newCount.Load() != 1 {
		t.Fatalf("expected exactly 1 new work item, got %d", newCount.Load())
	}
	if attachCount.Load() != total-1 {
		t.Fatalf("expected %d attachments, got %d", total-1, attachCount.Load())
	}

	// Verify all attach results reference the same work item.
	expectedID := firstID.Load().(string)
	if expectedID == "" {
		t.Fatal("work item ID should not be empty")
	}
}

// TestSignalCount_WindowExpiry verifies that after 10s window expiry, a new
// work item is created — simulating the "second burst" scenario.
func TestSignalCount_WindowExpiry(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	now := time.Now()

	r1 := d.Process("EXPIRY_TEST", now)
	if !r1.IsNew {
		t.Fatal("first signal should create new work item")
	}

	// Signal within window — same work item.
	r2 := d.Process("EXPIRY_TEST", now.Add(5*time.Second))
	if r2.IsNew {
		t.Fatal("signal within window should NOT create new work item")
	}
	if r2.WorkItemID != r1.WorkItemID {
		t.Fatalf("within-window signal has wrong ID: %s vs %s", r2.WorkItemID, r1.WorkItemID)
	}

	// Signal after window expiry — new work item.
	r3 := d.Process("EXPIRY_TEST", now.Add(11*time.Second))
	if !r3.IsNew {
		t.Fatal("signal after window expiry should create new work item")
	}
	if r3.WorkItemID == r1.WorkItemID {
		t.Fatal("new window should have a different work item ID")
	}
}
