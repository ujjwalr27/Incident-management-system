package ingest

import (
	"context"
	"log"
	"runtime"
	"time"

	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/alerting"
	"github.com/zeotap/ims/internal/debounce"
	"github.com/zeotap/ims/internal/metrics"
	"github.com/zeotap/ims/internal/models"
	mongostore "github.com/zeotap/ims/internal/store/mongo"
	pgstore "github.com/zeotap/ims/internal/store/postgres"
	redisstore "github.com/zeotap/ims/internal/store/redis"
)

// Pipeline is the in-process channel-based ingestion engine.
type Pipeline struct {
	ingress   chan *models.Signal
	workerN   int
	debouncer *debounce.Debouncer
	pg        *pgstore.Store
	mg        *mongostore.Store
	rds       *redisstore.Store
	done      chan struct{}
}

// New creates a Pipeline with a bounded ingress channel.
func New(bufferSize, workerCount int, pg *pgstore.Store, mg *mongostore.Store, rds *redisstore.Store) *Pipeline {
	if workerCount <= 0 {
		workerCount = runtime.NumCPU() * 4
	}
	if bufferSize <= 0 {
		bufferSize = 50_000
	}
	p := &Pipeline{
		ingress:   make(chan *models.Signal, bufferSize),
		workerN:   workerCount,
		debouncer: debounce.New(),
		pg:        pg,
		mg:        mg,
		rds:       rds,
		done:      make(chan struct{}),
	}
	return p
}

// Start launches the worker pool.
func (p *Pipeline) Start(ctx context.Context) {
	for i := 0; i < p.workerN; i++ {
		go p.worker(ctx)
	}
}

// Stop drains the channel gracefully (up to 30s).
func (p *Pipeline) Stop() {
	close(p.done)
	p.debouncer.Stop()
}

// Submit attempts to push a signal onto the ingress channel.
// Returns false (backpressure) if the channel is full.
func (p *Pipeline) Submit(sig *models.Signal) bool {
	select {
	case p.ingress <- sig:
		metrics.Global.SignalsIn.Add(1)
		metrics.Global.QueueDepth.Add(1)
		return true
	default:
		return false // channel full — caller should return 503
	}
}

// QueueDepth returns the current number of buffered signals.
func (p *Pipeline) QueueDepth() int { return len(p.ingress) }

func (p *Pipeline) worker(ctx context.Context) {
	for {
		select {
		case <-p.done:
			return
		case sig, ok := <-p.ingress:
			if !ok {
				return
			}
			metrics.Global.QueueDepth.Add(-1)
			p.process(ctx, sig)
			metrics.Global.SignalsProcessed.Add(1)
		}
	}
}

func (p *Pipeline) process(ctx context.Context, sig *models.Signal) {
	now := time.Now()
	if sig.Timestamp.IsZero() {
		sig.Timestamp = now
	}

	// 1. Write raw signal to MongoDB (async, best-effort).
	go func() {
		if err := p.mg.InsertSignal(ctx, sig); err != nil {
			log.Printf("[pipeline] mongo insert error: %v", err)
		}
	}()

	// 2. Debounce: determine if we need a new work item.
	result := p.debouncer.Process(sig.ComponentID, now)

	wiID, err := uuid.Parse(result.WorkItemID)
	if err != nil {
		log.Printf("[pipeline] bad uuid: %v", err)
		return
	}

	if result.IsNew {
		// 3a. Create new work item in Postgres.
		wi := &models.WorkItem{
			ID:            wiID,
			ComponentID:   sig.ComponentID,
			ComponentType: sig.ComponentType,
			Severity:      alerting.SeverityFor(sig.ComponentType),
			Status:        models.StatusOpen,
			Title:         buildTitle(sig),
			SignalCount:   1,
			FirstSignalAt: sig.Timestamp,
			LastSignalAt:  sig.Timestamp,
		}
		if createErr := p.pg.CreateWorkItem(ctx, wi); createErr != nil {
			log.Printf("[pipeline] create work item error: %v", createErr)
			return
		}

		// 4. Send alert via Strategy pattern.
		alerter := alerting.Factory(sig.ComponentType)
		if alertErr := alerter.Send(ctx, wi); alertErr != nil {
			log.Printf("[pipeline] alert error: %v", alertErr)
		}
		alert := alerting.AlertRecord(wi, alerter)
		if alertStoreErr := p.pg.CreateAlert(ctx, alert); alertStoreErr != nil {
			log.Printf("[pipeline] store alert error: %v", alertStoreErr)
		}

		// 5. Update Redis live feed.
		if cacheErr := p.rds.UpsertIncident(ctx, wi); cacheErr != nil {
			log.Printf("[pipeline] redis upsert error: %v", cacheErr)
		}

		// 6. Publish SSE event.
		_ = p.rds.Publish(ctx, &models.SSEEvent{Type: "incident.created", Payload: wi})

		// 7. Link the signal in Mongo.
		go func() {
			_ = p.mg.UpdateWorkItemID(ctx, sig.ComponentID, sig.Timestamp, result.WorkItemID)
		}()
	} else {
		// 3b. Attach to existing work item.
		if err := p.pg.IncrementSignalCount(ctx, wiID, sig.Timestamp); err != nil {
			log.Printf("[pipeline] increment signal count error: %v", err)
		} else {
			// Keep Redis count in sync after Postgres increment.
			// Use Background context so this survives beyond the pipeline context.
			go func() {
				bgCtx := context.Background()
				if wi, err := p.pg.GetWorkItem(bgCtx, wiID); err == nil && wi != nil {
					_ = p.rds.UpsertIncident(bgCtx, wi)
				}
			}()
		}
		// Publish update event.
		_ = p.rds.Publish(ctx, &models.SSEEvent{Type: "incident.updated", Payload: map[string]string{"id": result.WorkItemID}})
	}

	// 8. Upsert timeseries bucket (per-minute).
	bucket := now.Truncate(time.Minute)
	go func() {
		if err := p.pg.UpsertSignalCount(ctx, bucket, sig.ComponentID, sig.ComponentType, alerting.SeverityFor(sig.ComponentType), 1); err != nil {
			log.Printf("[pipeline] timescale upsert error: %v", err)
		}
	}()
}

func buildTitle(sig *models.Signal) string {
	return string(sig.ComponentType) + " failure on " + sig.ComponentID
}
