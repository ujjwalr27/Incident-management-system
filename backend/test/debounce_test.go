package test

import (
	"testing"
	"time"

	"github.com/zeotap/ims/internal/debounce"
)

func TestDebounce_SameComponentSameWindow(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	now := time.Now()
	r1 := d.Process("CACHE_01", now)
	if !r1.IsNew {
		t.Fatal("first signal should create a new work item")
	}

	r2 := d.Process("CACHE_01", now.Add(2*time.Second))
	if r2.IsNew {
		t.Fatal("second signal within window should NOT create a new work item")
	}
	if r2.WorkItemID != r1.WorkItemID {
		t.Fatalf("expected same work item: got %s vs %s", r1.WorkItemID, r2.WorkItemID)
	}
}

func TestDebounce_DifferentComponents(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	now := time.Now()
	r1 := d.Process("CACHE_01", now)
	r2 := d.Process("RDBMS_01", now)

	if !r1.IsNew || !r2.IsNew {
		t.Fatal("different components should each create a new work item")
	}
	if r1.WorkItemID == r2.WorkItemID {
		t.Fatal("different components should get different work item IDs")
	}
}

func TestDebounce_ExpiredWindowCreatesNew(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	now := time.Now()
	r1 := d.Process("API_01", now)
	if !r1.IsNew {
		t.Fatal("first signal should be new")
	}

	// Simulate a signal after the 10s window has expired.
	r2 := d.Process("API_01", now.Add(11*time.Second))
	if !r2.IsNew {
		t.Fatal("signal after expired window should create a new work item")
	}
	if r1.WorkItemID == r2.WorkItemID {
		t.Fatal("new window should produce a different work item ID")
	}
}

func TestDebounce_Concurrent(t *testing.T) {
	d := debounce.New()
	defer d.Stop()

	now := time.Now()
	const goroutines = 100
	results := make([]debounce.Result, goroutines)
	done := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			results[idx] = d.Process("QUEUE_01", now.Add(time.Duration(idx)*time.Millisecond))
			done <- idx
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Count new-work-item results — only the first goroutine should get IsNew=true.
	newCount := 0
	firstID := ""
	for _, r := range results {
		if r.IsNew {
			newCount++
			firstID = r.WorkItemID
		}
	}
	if newCount != 1 {
		t.Fatalf("expected exactly 1 new work item across %d concurrent signals, got %d", goroutines, newCount)
	}
	// All non-new signals should share the same work item ID.
	for _, r := range results {
		if !r.IsNew && r.WorkItemID != firstID {
			t.Fatalf("non-new signal has wrong work item ID: %s vs %s", r.WorkItemID, firstID)
		}
	}
}
