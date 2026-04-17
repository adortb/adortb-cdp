// Package profile 管理客户统一画像的存储与聚合。
package profile

import (
	"encoding/json"
	"time"
)

// Profile 是 CDP 的核心数据结构，代表一个统一客户。
type Profile struct {
	ID          int64           `json:"id"`
	CanonicalID string          `json:"canonical_id"`
	ExternalIDs ExternalIDs     `json:"external_ids"`
	Attributes  map[string]any  `json:"attributes"`
	Traits      map[string]any  `json:"traits"`
	Tags        []string        `json:"tags"`
	FirstSeenAt time.Time       `json:"first_seen_at"`
	LastSeenAt  time.Time       `json:"last_seen_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ExternalIDs 是跨系统的 ID 映射，支持任意 key。
type ExternalIDs map[string]string

// Event 是单个行为事件流水。
type Event struct {
	ID          int64          `json:"id"`
	CanonicalID string         `json:"canonical_id"`
	EventType   string         `json:"event_type"`
	Properties  map[string]any `json:"properties"`
	Source      string         `json:"source"`
	OccurredAt  time.Time      `json:"occurred_at"`
	ReceivedAt  time.Time      `json:"received_at"`
}

// UpdateRequest 描述对 profile 的局部更新。
type UpdateRequest struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Attributes  map[string]any    `json:"attributes,omitempty"`
	Traits      map[string]any    `json:"traits,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	LastSeenAt  *time.Time        `json:"last_seen_at,omitempty"`
}

// MarshalAttributes 序列化 Attributes 为 JSON 字节。
func (p *Profile) MarshalAttributes() ([]byte, error) {
	return json.Marshal(p.Attributes)
}

// MarshalTraits 序列化 Traits 为 JSON 字节。
func (p *Profile) MarshalTraits() ([]byte, error) {
	return json.Marshal(p.Traits)
}

// MarshalExternalIDs 序列化 ExternalIDs 为 JSON 字节。
func (p *Profile) MarshalExternalIDs() ([]byte, error) {
	return json.Marshal(p.ExternalIDs)
}
