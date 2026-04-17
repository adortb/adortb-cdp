package journey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Store 定义 Journey 持久化接口。
type Store interface {
	CreateJourney(ctx context.Context, j *Journey) (*Journey, error)
	GetJourney(ctx context.Context, id int64) (*Journey, error)
	UpdateJourneyStatus(ctx context.Context, id int64, status string) error

	UpsertInstance(ctx context.Context, inst *Instance) (*Instance, error)
	GetInstance(ctx context.Context, journeyID int64, canonicalID string) (*Instance, error)
	ListActiveInstances(ctx context.Context, journeyID int64, limit int) ([]*Instance, error)
	CompleteInstance(ctx context.Context, id int64, completedAt time.Time) error
}

type pgStore struct {
	db *sql.DB
}

// NewPGStore 构造 Journey pgStore。
func NewPGStore(db *sql.DB) Store {
	return &pgStore{db: db}
}

func (s *pgStore) CreateJourney(ctx context.Context, j *Journey) (*Journey, error) {
	entryJSON, _ := json.Marshal(j.EntryCondition)
	stepsJSON, err := json.Marshal(j.Steps)
	if err != nil {
		return nil, fmt.Errorf("marshal steps: %w", err)
	}
	const q = `INSERT INTO cdp_journeys (name, entry_condition, steps, status)
	           VALUES ($1, $2, $3, $4)
	           RETURNING id, name, entry_condition, steps, status, created_at, updated_at`
	row := s.db.QueryRowContext(ctx, q, j.Name, entryJSON, stepsJSON, j.Status)
	return scanJourney(row)
}

func (s *pgStore) GetJourney(ctx context.Context, id int64) (*Journey, error) {
	const q = `SELECT id, name, entry_condition, steps, status, created_at, updated_at
	            FROM cdp_journeys WHERE id = $1`
	row := s.db.QueryRowContext(ctx, q, id)
	j, err := scanJourney(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

func (s *pgStore) UpdateJourneyStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cdp_journeys SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

func (s *pgStore) UpsertInstance(ctx context.Context, inst *Instance) (*Instance, error) {
	stateJSON, _ := json.Marshal(inst.State)
	const q = `INSERT INTO cdp_journey_instances (journey_id, canonical_id, current_step, state)
	           VALUES ($1, $2, $3, $4)
	           ON CONFLICT (journey_id, canonical_id) DO UPDATE SET
	               current_step = EXCLUDED.current_step,
	               state = EXCLUDED.state
	           RETURNING id, journey_id, canonical_id, current_step, state, started_at, completed_at`
	row := s.db.QueryRowContext(ctx, q, inst.JourneyID, inst.CanonicalID, inst.CurrentStep, stateJSON)
	return scanInstance(row)
}

func (s *pgStore) GetInstance(ctx context.Context, journeyID int64, canonicalID string) (*Instance, error) {
	const q = `SELECT id, journey_id, canonical_id, current_step, state, started_at, completed_at
	            FROM cdp_journey_instances WHERE journey_id = $1 AND canonical_id = $2`
	row := s.db.QueryRowContext(ctx, q, journeyID, canonicalID)
	inst, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInstanceNotFound
	}
	return inst, err
}

func (s *pgStore) ListActiveInstances(ctx context.Context, journeyID int64, limit int) ([]*Instance, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `SELECT id, journey_id, canonical_id, current_step, state, started_at, completed_at
	            FROM cdp_journey_instances
	           WHERE journey_id = $1 AND completed_at IS NULL
	           LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, journeyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Instance
	for rows.Next() {
		inst, err := scanInstanceRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inst)
	}
	return result, rows.Err()
}

func (s *pgStore) CompleteInstance(ctx context.Context, id int64, completedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cdp_journey_instances SET completed_at = $2 WHERE id = $1`, id, completedAt)
	return err
}

func scanJourney(row *sql.Row) (*Journey, error) {
	j := &Journey{}
	var entryRaw, stepsRaw []byte
	if err := row.Scan(&j.ID, &j.Name, &entryRaw, &stepsRaw, &j.Status, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	if len(entryRaw) > 0 && string(entryRaw) != "null" {
		j.EntryCondition = &EntryTrigger{}
		_ = json.Unmarshal(entryRaw, j.EntryCondition)
	}
	_ = json.Unmarshal(stepsRaw, &j.Steps)
	return j, nil
}

func scanInstance(row *sql.Row) (*Instance, error) {
	inst := &Instance{}
	var stateRaw []byte
	var completedAt sql.NullTime
	if err := row.Scan(&inst.ID, &inst.JourneyID, &inst.CanonicalID,
		&inst.CurrentStep, &stateRaw, &inst.StartedAt, &completedAt); err != nil {
		return nil, err
	}
	if completedAt.Valid {
		inst.CompletedAt = &completedAt.Time
	}
	_ = json.Unmarshal(stateRaw, &inst.State)
	return inst, nil
}

func scanInstanceRows(rows *sql.Rows) (*Instance, error) {
	inst := &Instance{}
	var stateRaw []byte
	var completedAt sql.NullTime
	if err := rows.Scan(&inst.ID, &inst.JourneyID, &inst.CanonicalID,
		&inst.CurrentStep, &stateRaw, &inst.StartedAt, &completedAt); err != nil {
		return nil, err
	}
	if completedAt.Valid {
		inst.CompletedAt = &completedAt.Time
	}
	_ = json.Unmarshal(stateRaw, &inst.State)
	return inst, nil
}

var (
	ErrNotFound         = errors.New("journey not found")
	ErrInstanceNotFound = errors.New("journey instance not found")
)
