package ingestion

import (
	"context"
	"database/sql"
	"time"
)

// PGEventCounter 实现 audience.EventCounter，通过 PG 统计事件数。
type PGEventCounter struct {
	db *sql.DB
}

// NewPGEventCounter 构造 PGEventCounter。
func NewPGEventCounter(db *sql.DB) *PGEventCounter {
	return &PGEventCounter{db: db}
}

// CountEvents 统计某 profile 在指定时间窗口内特定事件的发生次数。
func (c *PGEventCounter) CountEvents(ctx context.Context, canonicalID, eventType string, since time.Time) (int, error) {
	const q = `SELECT COUNT(*) FROM cdp_events
	            WHERE canonical_id = $1 AND event_type = $2 AND occurred_at >= $3`
	var cnt int
	err := c.db.QueryRowContext(ctx, q, canonicalID, eventType, since).Scan(&cnt)
	return cnt, err
}
