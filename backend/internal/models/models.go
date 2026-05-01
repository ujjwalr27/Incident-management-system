package models

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleProducer  Role = "producer"
	RoleResponder Role = "responder"
)

type Status string

const (
	StatusOpen          Status = "OPEN"
	StatusInvestigating Status = "INVESTIGATING"
	StatusResolved      Status = "RESOLVED"
	StatusClosed        Status = "CLOSED"
)

type Severity string

const (
	SeverityP0 Severity = "P0"
	SeverityP1 Severity = "P1"
	SeverityP2 Severity = "P2"
	SeverityP3 Severity = "P3"
)

type ComponentType string

const (
	ComponentRDBMS ComponentType = "RDBMS"
	ComponentCache ComponentType = "CACHE"
	ComponentMCP   ComponentType = "MCP_HOST"
	ComponentAPI   ComponentType = "API"
	ComponentQueue ComponentType = "ASYNC_QUEUE"
	ComponentNoSQL ComponentType = "NOSQL"
)

// User is stored in Postgres.
type User struct {
	ID           uuid.UUID `db:"id"            json:"id"`
	Email        string    `db:"email"         json:"email"`
	PasswordHash string    `db:"password_hash" json:"-"`
	Role         Role      `db:"role"          json:"role"`
	CreatedAt    time.Time `db:"created_at"    json:"created_at"`
}

// WorkItem is the canonical "incident" record stored in Postgres.
type WorkItem struct {
	ID            uuid.UUID     `db:"id"              json:"id"`
	ComponentID   string        `db:"component_id"    json:"component_id"`
	ComponentType ComponentType `db:"component_type"  json:"component_type"`
	Severity      Severity      `db:"severity"        json:"severity"`
	Status        Status        `db:"status"          json:"status"`
	Title         string        `db:"title"           json:"title"`
	SignalCount   int           `db:"signal_count"    json:"signal_count"`
	FirstSignalAt time.Time     `db:"first_signal_at" json:"first_signal_at"`
	LastSignalAt  time.Time     `db:"last_signal_at"  json:"last_signal_at"`
	CreatedAt     time.Time     `db:"created_at"      json:"created_at"`
	UpdatedAt     time.Time     `db:"updated_at"      json:"updated_at"`
	MTTR          *float64      `db:"-"               json:"mttr_seconds,omitempty"`
}

// StateTransition records each status change.
type StateTransition struct {
	ID             uuid.UUID  `db:"id"               json:"id"`
	WorkItemID     uuid.UUID  `db:"work_item_id"     json:"work_item_id"`
	FromState      *string    `db:"from_state"       json:"from_state"`
	ToState        string     `db:"to_state"         json:"to_state"`
	TransitionedBy *uuid.UUID `db:"transitioned_by"  json:"transitioned_by,omitempty"`
	TransitionedAt time.Time  `db:"transitioned_at"  json:"transitioned_at"`
	Notes          *string    `db:"notes"            json:"notes,omitempty"`
}

// RCA is the root cause analysis record.
type RCA struct {
	ID               uuid.UUID  `db:"id"                json:"id"`
	WorkItemID       uuid.UUID  `db:"work_item_id"      json:"work_item_id"`
	Category         string     `db:"category"          json:"category"`
	FixApplied       string     `db:"fix_applied"       json:"fix_applied"`
	PreventionSteps  string     `db:"prevention_steps"  json:"prevention_steps"`
	IncidentStart    time.Time  `db:"incident_start"    json:"incident_start"`
	IncidentEnd      time.Time  `db:"incident_end"      json:"incident_end"`
	SubmittedBy      *uuid.UUID `db:"submitted_by"      json:"submitted_by,omitempty"`
	SubmittedAt      time.Time  `db:"submitted_at"      json:"submitted_at"`
}

// Signal is the raw inbound event — stored in MongoDB.
type Signal struct {
	ComponentID   string        `json:"component_id"   bson:"component_id"`
	ComponentType ComponentType `json:"component_type" bson:"component_type"`
	Severity      Severity      `json:"severity"       bson:"severity"`
	Message       string        `json:"message"        bson:"message"`
	Tags          map[string]string `json:"tags,omitempty" bson:"tags,omitempty"`
	Timestamp     time.Time     `json:"timestamp"      bson:"timestamp"`
	WorkItemID    *string       `json:"work_item_id,omitempty" bson:"work_item_id,omitempty"`
	ReceivedAt    time.Time     `json:"received_at"    bson:"received_at"`
}

// Alert is the record of a notification sent.
type Alert struct {
	ID         uuid.UUID `db:"id"           json:"id"`
	WorkItemID uuid.UUID `db:"work_item_id" json:"work_item_id"`
	Priority   string    `db:"priority"     json:"priority"`
	Channel    string    `db:"channel"      json:"channel"`
	SentAt     time.Time `db:"sent_at"      json:"sent_at"`
}

// IngestRequest is the HTTP body for bulk signal ingest.
type IngestRequest struct {
	Signals []Signal `json:"signals"`
}

// TransitionRequest is the HTTP body for status change.
type TransitionRequest struct {
	ToStatus Status  `json:"to_status"`
	Notes    *string `json:"notes,omitempty"`
}

// RCARequest is the HTTP body for submitting an RCA.
type RCARequest struct {
	Category        string    `json:"category"`
	FixApplied      string    `json:"fix_applied"`
	PreventionSteps string    `json:"prevention_steps"`
	IncidentStart   time.Time `json:"incident_start"`
	IncidentEnd     time.Time `json:"incident_end"`
}

// LoginRequest is the HTTP body for authentication.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenPair holds access + refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// SSEEvent is sent to dashboard clients.
type SSEEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
