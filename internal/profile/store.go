package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Store 定义 profile 存储接口，支持多后端实现。
type Store interface {
	// Upsert 创建或更新 profile，返回最新状态。
	Upsert(ctx context.Context, req UpsertRequest) (*Profile, error)
	// GetByCanonicalID 按主键查询。
	GetByCanonicalID(ctx context.Context, canonicalID string) (*Profile, error)
	// GetByExternalID 按外部 ID 查询，e.g. uid/idfa/email_hash。
	GetByExternalID(ctx context.Context, idType, idValue string) (*Profile, error)
	// ListEvents 列出某 profile 的事件流（按时间倒序分页）。
	ListEvents(ctx context.Context, canonicalID string, limit int, cursor time.Time) ([]*Event, error)
	// WriteEvent 写入事件流水。
	WriteEvent(ctx context.Context, ev *Event) error
	// UpdateTags 覆盖写标签（原子）。
	UpdateTags(ctx context.Context, canonicalID string, tags []string) error
}

// UpsertRequest 提供 Upsert 所需的全部字段。
type UpsertRequest struct {
	CanonicalID string            // 若空则由调用层生成
	ExternalIDs map[string]string // 新增或覆盖
	Attributes  map[string]any    // 浅合并
	Traits      map[string]any    // 浅合并
	Tags        []string          // 浅合并（去重追加）
}

// pgStore 使用 PostgreSQL 实现 Store。
type pgStore struct {
	db    *sql.DB
	cache *redisCache
}

// NewPGStore 构造 pgStore，cache 可为 nil（降级为纯 PG 模式）。
func NewPGStore(db *sql.DB, rdb *redis.Client) Store {
	var c *redisCache
	if rdb != nil {
		c = &redisCache{rdb: rdb, ttl: 10 * time.Minute}
	}
	return &pgStore{db: db, cache: c}
}

const upsertSQL = `
INSERT INTO cdp_profiles (canonical_id, external_ids, attributes, traits, tags, last_seen_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (canonical_id) DO UPDATE SET
    external_ids = cdp_profiles.external_ids || EXCLUDED.external_ids,
    attributes   = cdp_profiles.attributes   || EXCLUDED.attributes,
    traits       = cdp_profiles.traits       || EXCLUDED.traits,
    tags         = (
        SELECT ARRAY(SELECT DISTINCT unnest(cdp_profiles.tags || EXCLUDED.tags))
        WHERE EXCLUDED.tags IS NOT NULL
        UNION ALL
        SELECT cdp_profiles.tags WHERE EXCLUDED.tags IS NULL
        LIMIT 1
    ),
    last_seen_at = NOW(),
    updated_at   = NOW()
RETURNING id, canonical_id, external_ids, attributes, traits, tags, first_seen_at, last_seen_at, updated_at
`

func (s *pgStore) Upsert(ctx context.Context, req UpsertRequest) (*Profile, error) {
	eidJSON, err := json.Marshal(req.ExternalIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal external_ids: %w", err)
	}
	attrJSON, err := json.Marshal(req.Attributes)
	if err != nil {
		return nil, fmt.Errorf("marshal attributes: %w", err)
	}
	traitsJSON, err := json.Marshal(req.Traits)
	if err != nil {
		return nil, fmt.Errorf("marshal traits: %w", err)
	}

	row := s.db.QueryRowContext(ctx, upsertSQL,
		req.CanonicalID, eidJSON, attrJSON, traitsJSON, pq.Array(req.Tags))

	p, err := scanProfile(row)
	if err != nil {
		return nil, fmt.Errorf("upsert profile: %w", err)
	}

	if s.cache != nil {
		_ = s.cache.set(ctx, p)
	}
	return p, nil
}

func (s *pgStore) GetByCanonicalID(ctx context.Context, canonicalID string) (*Profile, error) {
	if s.cache != nil {
		if p, _ := s.cache.get(ctx, canonicalID); p != nil {
			return p, nil
		}
	}

	const q = `SELECT id, canonical_id, external_ids, attributes, traits, tags,
	                   first_seen_at, last_seen_at, updated_at
	             FROM cdp_profiles WHERE canonical_id = $1`
	row := s.db.QueryRowContext(ctx, q, canonicalID)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		_ = s.cache.set(ctx, p)
	}
	return p, nil
}

func (s *pgStore) GetByExternalID(ctx context.Context, idType, idValue string) (*Profile, error) {
	const q = `SELECT id, canonical_id, external_ids, attributes, traits, tags,
	                   first_seen_at, last_seen_at, updated_at
	             FROM cdp_profiles WHERE external_ids->$1 = to_jsonb($2::text) LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, idType, idValue)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *pgStore) ListEvents(ctx context.Context, canonicalID string, limit int, cursor time.Time) ([]*Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `SELECT id, canonical_id, event_type, properties, source, occurred_at, received_at
	             FROM cdp_events
	            WHERE canonical_id = $1 AND occurred_at < $2
	            ORDER BY occurred_at DESC LIMIT $3`
	rows, err := s.db.QueryContext(ctx, q, canonicalID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		ev := &Event{}
		var propRaw []byte
		if err := rows.Scan(&ev.ID, &ev.CanonicalID, &ev.EventType, &propRaw,
			&ev.Source, &ev.OccurredAt, &ev.ReceivedAt); err != nil {
			return nil, err
		}
		if len(propRaw) > 0 {
			_ = json.Unmarshal(propRaw, &ev.Properties)
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (s *pgStore) WriteEvent(ctx context.Context, ev *Event) error {
	propJSON, err := json.Marshal(ev.Properties)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	const q = `INSERT INTO cdp_events (canonical_id, event_type, properties, source, occurred_at)
	           VALUES ($1, $2, $3, $4, $5)`
	_, err = s.db.ExecContext(ctx, q, ev.CanonicalID, ev.EventType, propJSON, ev.Source, ev.OccurredAt)
	return err
}

func (s *pgStore) UpdateTags(ctx context.Context, canonicalID string, tags []string) error {
	const q = `UPDATE cdp_profiles SET tags = $2, updated_at = NOW() WHERE canonical_id = $1`
	_, err := s.db.ExecContext(ctx, q, canonicalID, pq.Array(tags))
	if s.cache != nil {
		_ = s.cache.del(ctx, canonicalID)
	}
	return err
}

// scanProfile 从 *sql.Row 扫描为 Profile，统一处理 JSON 反序列化。
func scanProfile(row *sql.Row) (*Profile, error) {
	p := &Profile{}
	var (
		eidRaw    []byte
		attrRaw   []byte
		traitsRaw []byte
		tags      pq.StringArray
	)
	err := row.Scan(&p.ID, &p.CanonicalID, &eidRaw, &attrRaw, &traitsRaw, &tags,
		&p.FirstSeenAt, &p.LastSeenAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Tags = []string(tags)
	if len(eidRaw) > 0 {
		_ = json.Unmarshal(eidRaw, &p.ExternalIDs)
	}
	if len(attrRaw) > 0 {
		_ = json.Unmarshal(attrRaw, &p.Attributes)
	}
	if len(traitsRaw) > 0 {
		_ = json.Unmarshal(traitsRaw, &p.Traits)
	}
	return p, nil
}

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("profile not found")
