package test

import (
	"testing"

	"github.com/zeotap/ims/internal/alerting"
	"github.com/zeotap/ims/internal/models"
)

func TestAlertingFactory(t *testing.T) {
	tests := []struct {
		componentType    models.ComponentType
		expectedPriority string
		expectedChannel  string
		expectedSeverity models.Severity
	}{
		{models.ComponentRDBMS, "P0", "pagerduty", models.SeverityP0},
		{models.ComponentMCP, "P1", "slack", models.SeverityP1},
		{models.ComponentAPI, "P1", "slack", models.SeverityP1},
		{models.ComponentCache, "P2", "email", models.SeverityP2},
		{models.ComponentQueue, "P2", "email", models.SeverityP2},
		{models.ComponentNoSQL, "P3", "log", models.SeverityP3},
	}

	for _, tt := range tests {
		t.Run(string(tt.componentType), func(t *testing.T) {
			a := alerting.Factory(tt.componentType)
			if a.Priority() != tt.expectedPriority {
				t.Errorf("Factory(%s).Priority() = %s, want %s", tt.componentType, a.Priority(), tt.expectedPriority)
			}
			if a.Channel() != tt.expectedChannel {
				t.Errorf("Factory(%s).Channel() = %s, want %s", tt.componentType, a.Channel(), tt.expectedChannel)
			}
			sev := alerting.SeverityFor(tt.componentType)
			if sev != tt.expectedSeverity {
				t.Errorf("SeverityFor(%s) = %s, want %s", tt.componentType, sev, tt.expectedSeverity)
			}
		})
	}
}

func TestAlertRecord(t *testing.T) {
	wi := &models.WorkItem{ComponentType: models.ComponentRDBMS}
	a := alerting.Factory(models.ComponentRDBMS)
	rec := alerting.AlertRecord(wi, a)

	if rec.Priority != "P0" {
		t.Errorf("expected priority P0, got %s", rec.Priority)
	}
	if rec.Channel != "pagerduty" {
		t.Errorf("expected channel pagerduty, got %s", rec.Channel)
	}
	if rec.SentAt.IsZero() {
		t.Error("expected non-zero SentAt")
	}
}
