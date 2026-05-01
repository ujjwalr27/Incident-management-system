package workflow

import (
	"errors"
	"fmt"

	"github.com/zeotap/ims/internal/models"
)

// ErrRCAIncomplete is returned when a CLOSED transition is attempted without a valid RCA.
var ErrRCAIncomplete = errors.New("work item cannot be CLOSED: RCA is missing or incomplete")

// ErrInvalidTransition is returned when the requested transition is not allowed.
type ErrInvalidTransition struct {
	From models.Status
	To   models.Status
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("transition %s → %s is not allowed", e.From, e.To)
}

// WorkItemState defines the behaviour of each lifecycle state.
type WorkItemState interface {
	Name() models.Status
	// AllowedTransitions returns the set of states this state can move to.
	AllowedTransitions() []models.Status
}

// allowedMap is the transition matrix.
var allowedMap = map[models.Status][]models.Status{
	models.StatusOpen:          {models.StatusInvestigating},
	models.StatusInvestigating: {models.StatusResolved, models.StatusOpen},
	models.StatusResolved:      {models.StatusClosed, models.StatusInvestigating},
	models.StatusClosed:        {},
}

// CanTransition checks whether moving from → to is permitted.
func CanTransition(from, to models.Status) error {
	allowed, ok := allowedMap[from]
	if !ok {
		return &ErrInvalidTransition{From: from, To: to}
	}
	for _, a := range allowed {
		if a == to {
			return nil
		}
	}
	return &ErrInvalidTransition{From: from, To: to}
}

// ValidateClose checks RCA completeness before allowing a CLOSED transition.
func ValidateClose(rca *models.RCA) error {
	if rca == nil {
		return ErrRCAIncomplete
	}
	if rca.Category == "" {
		return fmt.Errorf("%w: category is empty", ErrRCAIncomplete)
	}
	if rca.FixApplied == "" {
		return fmt.Errorf("%w: fix_applied is empty", ErrRCAIncomplete)
	}
	if rca.PreventionSteps == "" {
		return fmt.Errorf("%w: prevention_steps is empty", ErrRCAIncomplete)
	}
	if rca.IncidentEnd.IsZero() || rca.IncidentStart.IsZero() {
		return fmt.Errorf("%w: incident start/end times are missing", ErrRCAIncomplete)
	}
	if !rca.IncidentEnd.After(rca.IncidentStart) {
		return fmt.Errorf("%w: incident_end must be after incident_start", ErrRCAIncomplete)
	}
	return nil
}

// MTTR returns the mean-time-to-repair in seconds.
// Uses SubmittedAt (when the RCA was actually filed) as the end time,
// per the assignment spec: "end_time (RCA submission)".
func MTTR(wi *models.WorkItem, rca *models.RCA) float64 {
	if rca == nil {
		return 0
	}
	return rca.SubmittedAt.Sub(wi.FirstSignalAt).Seconds()
}
