package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	redisstore "github.com/zeotap/ims/internal/store/redis"
)

// SSEHandler streams live incident events to connected browser clients.
func SSEHandler(rds *redisstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		sub := rds.Subscribe(r.Context())
		defer sub.Close()

		ch := sub.Channel()

		// Send a heartbeat comment to keep the connection alive.
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		// Ticker sends a ping every 25s so Nginx (60s proxy_read_timeout) never
		// closes an idle SSE connection between real events.
		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
				flusher.Flush()
				if err := r.Context().Err(); err != nil {
					log.Printf("[sse] client disconnected: %v", err)
					return
				}
			}
		}
	}
}

