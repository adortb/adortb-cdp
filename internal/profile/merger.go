package profile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Merger 负责跨设备/跨 ID 的 profile 合并。
// 合并策略：保留 primary 的 canonical_id，将 secondary 的所有 external_ids / attributes / traits / tags 合入。
type Merger struct {
	store Store
	db    *sql.DB
}

// NewMerger 构造 Merger。
func NewMerger(store Store, db *sql.DB) *Merger {
	return &Merger{store: store, db: db}
}

// MergeResult 描述合并结果。
type MergeResult struct {
	Primary   *Profile `json:"primary"`
	Merged    bool     `json:"merged"`
	OldID     string   `json:"old_canonical_id,omitempty"`
}

// Merge 将 secondaryID 对应的 profile 合入 primaryID。
// 若两者本已相同则幂等返回。
func (m *Merger) Merge(ctx context.Context, primaryID, secondaryID string) (*MergeResult, error) {
	if primaryID == secondaryID {
		p, err := m.store.GetByCanonicalID(ctx, primaryID)
		if err != nil {
			return nil, err
		}
		return &MergeResult{Primary: p, Merged: false}, nil
	}

	secondary, err := m.store.GetByCanonicalID(ctx, secondaryID)
	if err != nil {
		return nil, fmt.Errorf("get secondary: %w", err)
	}

	// 将 secondary 数据合入 primary
	req := UpsertRequest{
		CanonicalID: primaryID,
		ExternalIDs: make(map[string]string),
		Attributes:  secondary.Attributes,
		Traits:      secondary.Traits,
		Tags:        secondary.Tags,
	}
	for k, v := range secondary.ExternalIDs {
		req.ExternalIDs[k] = v
	}

	primary, err := m.store.Upsert(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("merge upsert: %w", err)
	}

	// 重定向 secondary 的事件到 primary（更新 canonical_id）
	if err := m.redirectEvents(ctx, secondaryID, primaryID); err != nil {
		return nil, fmt.Errorf("redirect events: %w", err)
	}

	// 删除 secondary profile
	if _, err := m.db.ExecContext(ctx,
		`DELETE FROM cdp_profiles WHERE canonical_id = $1`, secondaryID); err != nil {
		return nil, fmt.Errorf("delete secondary: %w", err)
	}

	return &MergeResult{Primary: primary, Merged: true, OldID: secondaryID}, nil
}

func (m *Merger) redirectEvents(ctx context.Context, from, to string) error {
	_, err := m.db.ExecContext(ctx,
		`UPDATE cdp_events SET canonical_id = $2 WHERE canonical_id = $1`, from, to)
	return err
}

// GenerateCanonicalID 根据最稳定的外部 ID 生成确定性 canonical_id。
func GenerateCanonicalID(externalIDs map[string]string) string {
	// 按 key 排序以保证幂等
	keys := make([]string, 0, len(externalIDs))
	for k := range externalIDs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+externalIDs[k])
	}
	raw := strings.Join(parts, "&")
	h := sha256.Sum256([]byte("cdp:" + raw))
	return hex.EncodeToString(h[:])[:32]
}
