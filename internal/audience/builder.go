package audience

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// Store 定义受众持久化接口。
type Store interface {
	Create(ctx context.Context, a *Audience) (*Audience, error)
	GetByID(ctx context.Context, id int64) (*Audience, error)
	List(ctx context.Context, status string) ([]*Audience, error)
	UpdateEstimate(ctx context.Context, id int64, estimate int, computedAt time.Time) error

	AddMember(ctx context.Context, audienceID int64, canonicalID string) error
	RemoveMember(ctx context.Context, audienceID int64, canonicalID string) error
	HasMember(ctx context.Context, audienceID int64, canonicalID string) (bool, error)
	ListMembers(ctx context.Context, audienceID int64, limit int, cursor string) ([]*Member, string, error)
}

// Builder 负责受众成员的实时增量更新和批量计算。
type Builder struct {
	store     Store
	evaluator *Evaluator
	profiles  profile.Store
}

// NewBuilder 构造 Builder。
func NewBuilder(store Store, evaluator *Evaluator, profiles profile.Store) *Builder {
	return &Builder{store: store, evaluator: evaluator, profiles: profiles}
}

// EvaluateProfile 对单个 profile 实时检查所有动态受众，增量更新成员资格。
func (b *Builder) EvaluateProfile(ctx context.Context, p *profile.Profile) error {
	audiences, err := b.store.List(ctx, "active")
	if err != nil {
		return fmt.Errorf("list audiences: %w", err)
	}

	for _, aud := range audiences {
		if aud.Type != "dynamic" {
			continue
		}
		matched, err := b.evaluator.Matches(ctx, p, aud.Conditions)
		if err != nil {
			continue // 单个受众失败不影响其他
		}
		isMember, _ := b.store.HasMember(ctx, aud.ID, p.CanonicalID)

		switch {
		case matched && !isMember:
			_ = b.store.AddMember(ctx, aud.ID, p.CanonicalID)
		case !matched && isMember:
			_ = b.store.RemoveMember(ctx, aud.ID, p.CanonicalID)
		}
	}
	return nil
}

// pgAudienceStore 使用 PostgreSQL 实现 Store。
type pgAudienceStore struct {
	db *sql.DB
}

// NewPGStore 构造 pgAudienceStore。
func NewPGStore(db *sql.DB) Store {
	return &pgAudienceStore{db: db}
}

func (s *pgAudienceStore) Create(ctx context.Context, a *Audience) (*Audience, error) {
	condJSON, err := json.Marshal(a.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	const q = `INSERT INTO cdp_audiences (name, description, conditions, type, status)
	           VALUES ($1, $2, $3, $4, $5)
	           RETURNING id, name, description, conditions, type, size_estimate,
	                     last_computed_at, status, created_at, updated_at`
	row := s.db.QueryRowContext(ctx, q, a.Name, a.Description, condJSON, a.Type, a.Status)
	return scanAudience(row)
}

func (s *pgAudienceStore) GetByID(ctx context.Context, id int64) (*Audience, error) {
	const q = `SELECT id, name, description, conditions, type, size_estimate,
	                  last_computed_at, status, created_at, updated_at
	            FROM cdp_audiences WHERE id = $1`
	row := s.db.QueryRowContext(ctx, q, id)
	a, err := scanAudience(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *pgAudienceStore) List(ctx context.Context, status string) ([]*Audience, error) {
	q := `SELECT id, name, description, conditions, type, size_estimate,
	             last_computed_at, status, created_at, updated_at
	       FROM cdp_audiences`
	args := []any{}
	if status != "" {
		q += " WHERE status = $1"
		args = append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Audience
	for rows.Next() {
		a, err := scanAudienceRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *pgAudienceStore) UpdateEstimate(ctx context.Context, id int64, estimate int, computedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cdp_audiences SET size_estimate = $2, last_computed_at = $3, updated_at = NOW() WHERE id = $1`,
		id, estimate, computedAt)
	return err
}

func (s *pgAudienceStore) AddMember(ctx context.Context, audienceID int64, canonicalID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cdp_audience_members (audience_id, canonical_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		audienceID, canonicalID)
	return err
}

func (s *pgAudienceStore) RemoveMember(ctx context.Context, audienceID int64, canonicalID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cdp_audience_members WHERE audience_id = $1 AND canonical_id = $2`,
		audienceID, canonicalID)
	return err
}

func (s *pgAudienceStore) HasMember(ctx context.Context, audienceID int64, canonicalID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM cdp_audience_members WHERE audience_id = $1 AND canonical_id = $2)`,
		audienceID, canonicalID).Scan(&exists)
	return exists, err
}

func (s *pgAudienceStore) ListMembers(ctx context.Context, audienceID int64, limit int, cursor string) ([]*Member, string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT audience_id, canonical_id, added_at FROM cdp_audience_members
	       WHERE audience_id = $1 AND canonical_id > $2
	       ORDER BY canonical_id LIMIT $3`
	rows, err := s.db.QueryContext(ctx, q, audienceID, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var members []*Member
	for rows.Next() {
		m := &Member{}
		if err := rows.Scan(&m.AudienceID, &m.CanonicalID, &m.AddedAt); err != nil {
			return nil, "", err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(members) == limit {
		nextCursor = members[len(members)-1].CanonicalID
	}
	return members, nextCursor, nil
}

func scanAudience(row *sql.Row) (*Audience, error) {
	a := &Audience{}
	var condRaw []byte
	var lc sql.NullTime
	if err := row.Scan(&a.ID, &a.Name, &a.Description, &condRaw, &a.Type,
		&a.SizeEstimate, &lc, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if lc.Valid {
		a.LastComputedAt = lc.Time
	}
	_ = json.Unmarshal(condRaw, &a.Conditions)
	return a, nil
}

func scanAudienceRows(rows *sql.Rows) (*Audience, error) {
	a := &Audience{}
	var condRaw []byte
	var lc sql.NullTime
	if err := rows.Scan(&a.ID, &a.Name, &a.Description, &condRaw, &a.Type,
		&a.SizeEstimate, &lc, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if lc.Valid {
		a.LastComputedAt = lc.Time
	}
	_ = json.Unmarshal(condRaw, &a.Conditions)
	return a, nil
}

// ErrNotFound 受众不存在。
var ErrNotFound = errors.New("audience not found")
