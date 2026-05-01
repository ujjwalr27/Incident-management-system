package test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/models"
	"github.com/zeotap/ims/internal/workflow"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		from    models.Status
		to      models.Status
		wantErr bool
	}{
		{models.StatusOpen, models.StatusInvestigating, false},
		{models.StatusInvestigating, models.StatusResolved, false},
		{models.StatusResolved, models.StatusClosed, false},
		{models.StatusInvestigating, models.StatusOpen, false},   // re-open allowed
		{models.StatusResolved, models.StatusInvestigating, false}, // re-open investigating allowed
		// Invalid
		{models.StatusOpen, models.StatusClosed, true},
		{models.StatusOpen, models.StatusResolved, true},
		{models.StatusClosed, models.StatusOpen, true},
		{models.StatusClosed, models.StatusInvestigating, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			err := workflow.CanTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanTransition(%s, %s) error=%v wantErr=%v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

func TestValidateClose_RequiresRCA(t *testing.T) {
	if err := workflow.ValidateClose(nil); err == nil {
		t.Fatal("expected error for nil RCA")
	}
}

func TestValidateClose_RejectsEmptyFields(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	tests := []struct {
		name string
		rca  *models.RCA
	}{
		{"empty category", &models.RCA{FixApplied: "fixed", PreventionSteps: "steps", IncidentStart: start, IncidentEnd: end}},
		{"empty fix_applied", &models.RCA{Category: "cat", PreventionSteps: "steps", IncidentStart: start, IncidentEnd: end}},
		{"empty prevention_steps", &models.RCA{Category: "cat", FixApplied: "fixed", IncidentStart: start, IncidentEnd: end}},
		{"end before start", &models.RCA{Category: "cat", FixApplied: "fixed", PreventionSteps: "steps", IncidentStart: end, IncidentEnd: start}},
		{"zero times", &models.RCA{Category: "cat", FixApplied: "fixed", PreventionSteps: "steps"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := workflow.ValidateClose(tt.rca); err == nil {
				t.Fatalf("expected validation error for case %q", tt.name)
			}
		})
	}
}

func TestValidateClose_AcceptsCompleteRCA(t *testing.T) {
	rca := &models.RCA{
		Category:        "Configuration",
		FixApplied:      "Rolled back deployment v2.3",
		PreventionSteps: "Add canary deployment gates",
		IncidentStart:   time.Now().Add(-2 * time.Hour),
		IncidentEnd:     time.Now().Add(-30 * time.Minute),
	}
	if err := workflow.ValidateClose(rca); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMTTR(t *testing.T) {
	firstSignal := time.Now().Add(-90 * time.Minute)
	wi := &models.WorkItem{
		ID:            uuid.New(),
		FirstSignalAt: firstSignal,
	}
	// MTTR = SubmittedAt - FirstSignalAt (per assignment: "end_time = RCA submission")
	rca := &models.RCA{
		IncidentStart: firstSignal,
		IncidentEnd:   firstSignal.Add(90 * time.Minute),
		SubmittedAt:   firstSignal.Add(90 * time.Minute), // filed exactly 90m later
	}
	mttr := workflow.MTTR(wi, rca)
	if mttr != 5400 { // 90 * 60
		t.Fatalf("expected MTTR 5400s, got %.0f", mttr)
	}
}

func TestMTTR_NilRCA(t *testing.T) {
	wi := &models.WorkItem{ID: uuid.New()}
	if workflow.MTTR(wi, nil) != 0 {
		t.Fatal("expected 0 MTTR for nil RCA")
	}
}
