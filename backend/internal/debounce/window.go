package debounce

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const windowDuration = 10 * time.Second

// Window holds the state for one component's debounce window.
type Window struct {
	WorkItemID string
	FirstSeen  time.Time
	Count      int
	mu         sync.Mutex
}

// Debouncer manages per-component debounce windows.
type Debouncer struct {
	windows sync.Map // map[componentID]*Window
	done    chan struct{}
}

// New creates and starts a Debouncer with a background janitor.
func New() *Debouncer {
	d := &Debouncer{done: make(chan struct{})}
	go d.janitor()
	return d
}

// Result tells the caller what to do with the incoming signal.
type Result struct {
	WorkItemID string
	IsNew      bool // true → a new work item must be created
}

// Process checks whether a signal for componentID should open a new work item.
// Returns a Result containing the relevant work item ID.
func (d *Debouncer) Process(componentID string, now time.Time) Result {
	for {
		val, loaded := d.windows.LoadOrStore(componentID, &Window{
			WorkItemID: uuid.New().String(),
			FirstSeen:  now,
			Count:      1,
		})
		w := val.(*Window)
		w.mu.Lock()

		age := now.Sub(w.FirstSeen)
		if !loaded {
			// We just created a new window — signal a new work item.
			w.mu.Unlock()
			return Result{WorkItemID: w.WorkItemID, IsNew: true}
		}

		if age <= windowDuration {
			// Still within the 10s debounce window — attach all signals to the
			// same work item regardless of count (per assignment spec:
			// "100 signals within 10s → only ONE Work Item created").
			w.Count++
			wid := w.WorkItemID
			w.mu.Unlock()
			return Result{WorkItemID: wid, IsNew: false}
		}

		// Window has expired (>10s) — evict and create a new work item.
		d.windows.Delete(componentID)
		w.mu.Unlock()
		// Loop will retry LoadOrStore, creating a new window.
	}
}

// Stop shuts down the janitor goroutine.
func (d *Debouncer) Stop() { close(d.done) }

// janitor sweeps expired windows every second to reclaim memory.
func (d *Debouncer) janitor() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case now := <-ticker.C:
			d.windows.Range(func(key, val interface{}) bool {
				w := val.(*Window)
				w.mu.Lock()
				expired := now.Sub(w.FirstSeen) > windowDuration
				w.mu.Unlock()
				if expired {
					d.windows.Delete(key)
				}
				return true
			})
		}
	}
}
