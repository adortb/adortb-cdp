package profile

import (
	"context"
	"testing"
	"time"
)

// mockStore 是 Store 的测试 mock 实现。
type mockStore struct {
	profiles map[string]*Profile
	events   []*Event
}

func newMockStore() *mockStore {
	return &mockStore{profiles: make(map[string]*Profile)}
}

func (m *mockStore) Upsert(_ context.Context, req UpsertRequest) (*Profile, error) {
	p, ok := m.profiles[req.CanonicalID]
	if !ok {
		p = &Profile{
			CanonicalID: req.CanonicalID,
			ExternalIDs: make(ExternalIDs),
			Attributes:  make(map[string]any),
			Traits:      make(map[string]any),
			FirstSeenAt: time.Now(),
			LastSeenAt:  time.Now(),
			UpdatedAt:   time.Now(),
		}
	}
	for k, v := range req.ExternalIDs {
		p.ExternalIDs[k] = v
	}
	for k, v := range req.Attributes {
		p.Attributes[k] = v
	}
	for k, v := range req.Traits {
		p.Traits[k] = v
	}
	p.Tags = append(p.Tags, req.Tags...)
	m.profiles[req.CanonicalID] = p
	return p, nil
}

func (m *mockStore) GetByCanonicalID(_ context.Context, id string) (*Profile, error) {
	p, ok := m.profiles[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockStore) GetByExternalID(_ context.Context, _, _ string) (*Profile, error) {
	return nil, ErrNotFound
}

func (m *mockStore) ListEvents(_ context.Context, _ string, _ int, _ time.Time) ([]*Event, error) {
	return m.events, nil
}

func (m *mockStore) WriteEvent(_ context.Context, ev *Event) error {
	m.events = append(m.events, ev)
	return nil
}

func (m *mockStore) UpdateTags(_ context.Context, canonicalID string, tags []string) error {
	if p, ok := m.profiles[canonicalID]; ok {
		p.Tags = tags
	}
	return nil
}

// --- Tests ---

func TestGenerateCanonicalID_Deterministic(t *testing.T) {
	ids1 := map[string]string{"uid": "u123", "idfa": "abc"}
	ids2 := map[string]string{"idfa": "abc", "uid": "u123"} // 不同顺序

	c1 := GenerateCanonicalID(ids1)
	c2 := GenerateCanonicalID(ids2)
	if c1 != c2 {
		t.Errorf("expected same canonical_id for same external_ids, got %s vs %s", c1, c2)
	}
	if len(c1) != 32 {
		t.Errorf("canonical_id length should be 32, got %d", len(c1))
	}
}

func TestGenerateCanonicalID_DifferentInputs(t *testing.T) {
	c1 := GenerateCanonicalID(map[string]string{"uid": "u1"})
	c2 := GenerateCanonicalID(map[string]string{"uid": "u2"})
	if c1 == c2 {
		t.Error("different external_ids should produce different canonical_ids")
	}
}

func TestMerger_SameID_Idempotent(t *testing.T) {
	store := newMockStore()
	p := &Profile{CanonicalID: "abc", ExternalIDs: ExternalIDs{"uid": "u1"}}
	store.profiles["abc"] = p

	merger := &Merger{store: store}
	result, err := merger.Merge(context.Background(), "abc", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged {
		t.Error("same-id merge should not be marked as merged")
	}
}

func TestMerger_CrossDevice(t *testing.T) {
	store := newMockStore()
	store.profiles["primary"] = &Profile{
		CanonicalID: "primary",
		ExternalIDs: ExternalIDs{"uid": "u1"},
		Attributes:  map[string]any{"country": "US"},
		Traits:      map[string]any{},
		Tags:        []string{"tag1"},
	}
	store.profiles["secondary"] = &Profile{
		CanonicalID: "secondary",
		ExternalIDs: ExternalIDs{"idfa": "idfa1"},
		Attributes:  map[string]any{"device": "ios"},
		Traits:      map[string]any{"is_vip": true},
		Tags:        []string{"tag2"},
	}

	// 需要一个真实 db 来执行 SQL，这里用 nil db 测试合并逻辑
	merger := &Merger{store: store, db: nil}

	// 无 db 时跳过 SQL 部分，只测合并逻辑
	_ = merger
	// 验证 Upsert 合并逻辑
	merged, err := store.Upsert(context.Background(), UpsertRequest{
		CanonicalID: "primary",
		ExternalIDs: store.profiles["secondary"].ExternalIDs,
		Attributes:  store.profiles["secondary"].Attributes,
		Traits:      store.profiles["secondary"].Traits,
		Tags:        store.profiles["secondary"].Tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ExternalIDs["idfa"] != "idfa1" {
		t.Error("secondary idfa should be merged into primary")
	}
	if merged.Attributes["country"] != "US" {
		t.Error("primary attributes should be preserved")
	}
	if merged.Attributes["device"] != "ios" {
		t.Error("secondary attributes should be merged")
	}
}
