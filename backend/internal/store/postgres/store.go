package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/zeotap/ims/internal/models"
	"github.com/zeotap/ims/internal/resilience"
)

type Store struct {
	db      *sqlx.DB
	breaker interface{ Execute(func() (interface{}, error)) (interface{}, error) }
}

func New(dsn string) (*Store, error) {
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &Store{
		db:      db,
		breaker: resilience.NewBreaker("postgres"),
	}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) Close() error { return s.db.Close() }

// RunMigrations runs SQL from migration string (called at startup with embedded SQL).
func (s *Store) Exec(ctx context.Context, query string) error {
	_, err := s.db.ExecContext(ctx, query)
	return err
}

// --- User ---

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := s.db.GetContext(ctx, &u, `SELECT id, email, password_hash, role, created_at FROM users WHERE email=$1`, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	err := s.db.GetContext(ctx, &u, `SELECT id, email, password_hash, role, created_at FROM users WHERE id=$1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// --- WorkItem ---

func (s *Store) CreateWorkItem(ctx context.Context, wi *models.WorkItem) error {
	return resilience.Retry(ctx, 5, func() error {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO work_items (id, component_id, component_type, severity, status, title, signal_count, first_signal_at, last_signal_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			wi.ID, wi.ComponentID, wi.ComponentType, wi.Severity, wi.Status,
			wi.Title, wi.SignalCount, wi.FirstSignalAt, wi.LastSignalAt)
		return err
	})
}

func (s *Store) GetWorkItem(ctx context.Context, id uuid.UUID) (*models.WorkItem, error) {
	var wi models.WorkItem
	err := s.db.GetContext(ctx, &wi, `SELECT * FROM work_items WHERE id=$1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &wi, nil
}

func (s *Store) ListWorkItems(ctx context.Context, limit, offset int) ([]*models.WorkItem, error) {
	var items []*models.WorkItem
	err := s.db.SelectContext(ctx, &items, `
		SELECT * FROM work_items
		ORDER BY
			CASE severity WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END,
			last_signal_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	return items, err
}

func (s *Store) ListActiveWorkItems(ctx context.Context) ([]*models.WorkItem, error) {
	var items []*models.WorkItem
	err := s.db.SelectContext(ctx, &items, `
		SELECT * FROM work_items
		WHERE status NOT IN ('CLOSED')
		ORDER BY
			CASE severity WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END,
			last_signal_at DESC`)
	return items, err
}

// IncrementSignalCount atomically increments the signal counter and updates last_signal_at.
// Uses 8 retries to handle the race where a concurrent worker creates the work item
// just after this worker checks for it.
func (s *Store) IncrementSignalCount(ctx context.Context, id uuid.UUID, ts time.Time) error {
	return resilience.Retry(ctx, 8, func() error {
		result, err := s.db.ExecContext(ctx, `
			UPDATE work_items
			SET signal_count = signal_count + 1, last_signal_at = $2, updated_at = NOW()
			WHERE id = $1`, id, ts)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("work item %s not yet visible, retrying", id)
		}
		return nil
	})
}

// TransitionStatus updates status inside a serializable transaction and records the transition.
func (s *Store) TransitionStatus(ctx context.Context, id uuid.UUID, from, to models.Status, byUser *uuid.UUID, notes *string) error {
	return resilience.Retry(ctx, 3, func() error {
		tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var current string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM work_items WHERE id=$1 FOR UPDATE`, id).Scan(&current); err != nil {
			return err
		}
		if current != string(from) {
			return fmt.Errorf("concurrent update: expected %s, got %s", from, current)
		}

		if _, err := tx.ExecContext(ctx, `UPDATE work_items SET status=$1, updated_at=NOW() WHERE id=$2`, to, id); err != nil {
			return err
		}

		fromStr := string(from)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO state_transitions (work_item_id, from_state, to_state, transitioned_by, notes)
			VALUES ($1,$2,$3,$4,$5)`,
			id, fromStr, string(to), byUser, notes); err != nil {
			return err
		}

		return tx.Commit()
	})
}

// GetTransitions returns the state history for a work item.
func (s *Store) GetTransitions(ctx context.Context, workItemID uuid.UUID) ([]*models.StateTransition, error) {
	t := make([]*models.StateTransition, 0)
	err := s.db.SelectContext(ctx, &t, `SELECT * FROM state_transitions WHERE work_item_id=$1 ORDER BY transitioned_at ASC`, workItemID)
	return t, err
}

// --- RCA ---

func (s *Store) CreateRCA(ctx context.Context, rca *models.RCA) error {
	return resilience.Retry(ctx, 5, func() error {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO rca (id, work_item_id, category, fix_applied, prevention_steps, incident_start, incident_end, submitted_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (work_item_id) DO UPDATE SET
				category         = EXCLUDED.category,
				fix_applied      = EXCLUDED.fix_applied,
				prevention_steps = EXCLUDED.prevention_steps,
				incident_start   = EXCLUDED.incident_start,
				incident_end     = EXCLUDED.incident_end,
				submitted_by     = EXCLUDED.submitted_by,
				submitted_at     = NOW()`,
			rca.ID, rca.WorkItemID, rca.Category, rca.FixApplied, rca.PreventionSteps,
			rca.IncidentStart, rca.IncidentEnd, rca.SubmittedBy)
		return err
	})
}

func (s *Store) GetRCA(ctx context.Context, workItemID uuid.UUID) (*models.RCA, error) {
	var r models.RCA
	err := s.db.GetContext(ctx, &r, `SELECT * FROM rca WHERE work_item_id=$1`, workItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// --- Alerts ---

func (s *Store) CreateAlert(ctx context.Context, a *models.Alert) error {
	return resilience.Retry(ctx, 3, func() error {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO alerts (id, work_item_id, priority, channel, sent_at)
			VALUES ($1,$2,$3,$4,$5)`,
			a.ID, a.WorkItemID, a.Priority, a.Channel, a.SentAt)
		return err
	})
}

// --- Timeseries ---

func (s *Store) UpsertSignalCount(ctx context.Context, bucket time.Time, componentID string, componentType models.ComponentType, severity models.Severity, delta int) error {
	return resilience.Retry(ctx, 3, func() error {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO signal_counts (bucket, component_id, component_type, severity, count)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (bucket, component_id, severity) DO UPDATE SET count = signal_counts.count + EXCLUDED.count`,
			bucket, componentID, componentType, severity, delta)
		return err
	})
}

// SeedUsers upserts the three demo users with a freshly-hashed password.
func (s *Store) SeedUsers(ctx context.Context, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	seeds := []struct {
		id    string
		email string
		role  string
	}{
		{"00000000-0000-0000-0000-000000000001", "admin@ims.local", "admin"},
		{"00000000-0000-0000-0000-000000000002", "producer@ims.local", "producer"},
		{"00000000-0000-0000-0000-000000000003", "responder@ims.local", "responder"},
	}
	for _, u := range seeds {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO users (id, email, password_hash, role)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash`,
			u.id, u.email, string(hash), u.role); err != nil {
			return fmt.Errorf("seed user %s: %w", u.email, err)
		}
	}
	return nil
}
