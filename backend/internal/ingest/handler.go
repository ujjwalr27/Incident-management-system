package ingest

import (
	"encoding/json"
	"net/http"

	"github.com/zeotap/ims/internal/models"
)

// Handler returns the HTTP handler for signal ingestion.
func Handler(p *Pipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req models.IngestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if len(req.Signals) == 0 {
			http.Error(w, "signals array is empty", http.StatusBadRequest)
			return
		}
		if len(req.Signals) > 1000 {
			http.Error(w, "batch too large (max 1000)", http.StatusRequestEntityTooLarge)
			return
		}

		dropped := 0
		for i := range req.Signals {
			if !p.Submit(&req.Signals[i]) {
				dropped++
			}
		}

		if dropped == len(req.Signals) {
			// All signals were dropped — system is overwhelmed.
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "ingestion queue full",
				"dropped": dropped,
			})
			return
		}

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accepted": len(req.Signals) - dropped,
			"dropped":  dropped,
		})
	}
}
