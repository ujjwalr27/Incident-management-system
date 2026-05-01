package metrics

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Counters holds atomically updated throughput counters.
type Counters struct {
	SignalsIn        atomic.Int64
	SignalsProcessed atomic.Int64
	QueueDepth       atomic.Int64
}

var Global = &Counters{}

// StartLogger prints throughput metrics to stdout every 5 seconds.
func StartLogger(done <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var prevIn, prevProcessed int64
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			in := Global.SignalsIn.Load()
			processed := Global.SignalsProcessed.Load()
			depth := Global.QueueDepth.Load()

			rateIn := (in - prevIn) / 5
			rateProcessed := (processed - prevProcessed) / 5
			prevIn = in
			prevProcessed = processed

			fmt.Printf("[METRICS] signals_in/s=%d signals_processed/s=%d queue_depth=%d total_in=%d total_processed=%d\n",
				rateIn, rateProcessed, depth, in, processed)
		}
	}
}
