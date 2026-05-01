package alerting

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/models"
)

// Alerter is the Strategy interface.
type Alerter interface {
	Priority() string
	Channel() string
	Send(ctx context.Context, wi *models.WorkItem) error
}

// P0Alerter simulates a PagerDuty page for critical failures (e.g. RDBMS).
type P0Alerter struct{}

func (a *P0Alerter) Priority() string { return "P0" }
func (a *P0Alerter) Channel() string  { return "pagerduty" }
func (a *P0Alerter) Send(ctx context.Context, wi *models.WorkItem) error {
	log.Printf("[P0 ALERT][PagerDuty] CRITICAL incident %s — component=%s title=%q", wi.ID, wi.ComponentID, wi.Title)
	return nil
}

// P1Alerter simulates a Slack page for high-severity failures (e.g. MCP_HOST).
type P1Alerter struct{}

func (a *P1Alerter) Priority() string { return "P1" }
func (a *P1Alerter) Channel() string  { return "slack" }
func (a *P1Alerter) Send(ctx context.Context, wi *models.WorkItem) error {
	log.Printf("[P1 ALERT][Slack] HIGH incident %s — component=%s title=%q", wi.ID, wi.ComponentID, wi.Title)
	return nil
}

// P2Alerter simulates an email for moderate failures (e.g. CACHE).
type P2Alerter struct{}

func (a *P2Alerter) Priority() string { return "P2" }
func (a *P2Alerter) Channel() string  { return "email" }
func (a *P2Alerter) Send(ctx context.Context, wi *models.WorkItem) error {
	log.Printf("[P2 ALERT][Email] MODERATE incident %s — component=%s title=%q", wi.ID, wi.ComponentID, wi.Title)
	return nil
}

// P3Alerter logs a low-priority notice.
type P3Alerter struct{}

func (a *P3Alerter) Priority() string { return "P3" }
func (a *P3Alerter) Channel() string  { return "log" }
func (a *P3Alerter) Send(ctx context.Context, wi *models.WorkItem) error {
	log.Printf("[P3 ALERT][Log] LOW incident %s — component=%s title=%q", wi.ID, wi.ComponentID, wi.Title)
	return nil
}

// Factory selects the correct Alerter based on component type.
func Factory(ct models.ComponentType) Alerter {
	switch ct {
	case models.ComponentRDBMS:
		return &P0Alerter{}
	case models.ComponentMCP, models.ComponentAPI:
		return &P1Alerter{}
	case models.ComponentCache, models.ComponentQueue:
		return &P2Alerter{}
	default:
		return &P3Alerter{}
	}
}

// SeverityFor maps component type to the expected alert severity.
func SeverityFor(ct models.ComponentType) models.Severity {
	switch ct {
	case models.ComponentRDBMS:
		return models.SeverityP0
	case models.ComponentMCP, models.ComponentAPI:
		return models.SeverityP1
	case models.ComponentCache, models.ComponentQueue:
		return models.SeverityP2
	default:
		return models.SeverityP3
	}
}

// AlertRecord converts a send action into a storable models.Alert.
func AlertRecord(wi *models.WorkItem, a Alerter) *models.Alert {
	return &models.Alert{
		ID:         uuid.New(),
		WorkItemID: wi.ID,
		Priority:   a.Priority(),
		Channel:    a.Channel(),
		SentAt:     time.Now(),
	}
}
