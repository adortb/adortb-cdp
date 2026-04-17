package journey

import (
	"context"
	"testing"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// mockJourneyStore 是 Store 的测试 mock。
type mockJourneyStore struct {
	journeys  map[int64]*Journey
	instances map[string]*Instance // key: journeyID_canonicalID
	nextID    int64
}

func newMockStore() *mockJourneyStore {
	return &mockJourneyStore{
		journeys:  make(map[int64]*Journey),
		instances: make(map[string]*Instance),
	}
}

func storeKey(journeyID int64, canonicalID string) string {
	return string(rune(journeyID)) + "_" + canonicalID
}

func (m *mockJourneyStore) CreateJourney(_ context.Context, j *Journey) (*Journey, error) {
	m.nextID++
	j.ID = m.nextID
	j.CreatedAt = time.Now()
	j.UpdatedAt = time.Now()
	m.journeys[j.ID] = j
	return j, nil
}

func (m *mockJourneyStore) GetJourney(_ context.Context, id int64) (*Journey, error) {
	j, ok := m.journeys[id]
	if !ok {
		return nil, ErrNotFound
	}
	return j, nil
}

func (m *mockJourneyStore) UpdateJourneyStatus(_ context.Context, id int64, status string) error {
	if j, ok := m.journeys[id]; ok {
		j.Status = status
	}
	return nil
}

func (m *mockJourneyStore) UpsertInstance(_ context.Context, inst *Instance) (*Instance, error) {
	if inst.ID == 0 {
		m.nextID++
		inst.ID = m.nextID
		inst.StartedAt = time.Now()
	}
	key := storeKey(inst.JourneyID, inst.CanonicalID)
	m.instances[key] = inst
	return inst, nil
}

func (m *mockJourneyStore) GetInstance(_ context.Context, journeyID int64, canonicalID string) (*Instance, error) {
	key := storeKey(journeyID, canonicalID)
	inst, ok := m.instances[key]
	if !ok {
		return nil, ErrInstanceNotFound
	}
	return inst, nil
}

func (m *mockJourneyStore) ListActiveInstances(_ context.Context, journeyID int64, _ int) ([]*Instance, error) {
	var result []*Instance
	for _, inst := range m.instances {
		if inst.JourneyID == journeyID && inst.CompletedAt == nil {
			result = append(result, inst)
		}
	}
	return result, nil
}

func (m *mockJourneyStore) CompleteInstance(_ context.Context, id int64, completedAt time.Time) error {
	for _, inst := range m.instances {
		if inst.ID == id {
			inst.CompletedAt = &completedAt
			return nil
		}
	}
	return nil
}

// mockActionExecutor 记录执行过的 action。
type mockActionExecutor struct {
	executed []string
}

func (m *mockActionExecutor) Execute(_ context.Context, canonicalID string, action *Action) error {
	m.executed = append(m.executed, canonicalID+":"+action.Kind)
	return nil
}

// --- Tests ---

func TestOrchestrator_EnterNonActiveJourney(t *testing.T) {
	store := newMockStore()
	executor := &mockActionExecutor{}
	orch := NewOrchestrator(store, executor, nil)

	j, _ := store.CreateJourney(context.Background(), &Journey{
		Name:   "test",
		Steps:  []Step{{ID: "s1", Type: "action", Action: &Action{Kind: "tag"}}},
		Status: "draft",
	})

	p := &profile.Profile{CanonicalID: "user1"}
	_, err := orch.Enter(context.Background(), j.ID, p)
	if err == nil {
		t.Error("should fail to enter draft journey")
	}
}

func TestOrchestrator_EnterAndComplete(t *testing.T) {
	store := newMockStore()
	executor := &mockActionExecutor{}
	orch := NewOrchestrator(store, executor, nil)

	j, _ := store.CreateJourney(context.Background(), &Journey{
		Name: "simple-journey",
		Steps: []Step{
			{ID: "step1", Type: "action", Next: []string{}, Action: &Action{Kind: "message", Payload: map[string]any{"msg": "welcome"}}},
		},
		Status: "active",
	})

	p := &profile.Profile{CanonicalID: "user1"}
	inst, err := orch.Enter(context.Background(), j.ID, p)
	if err != nil {
		t.Fatalf("enter failed: %v", err)
	}
	if inst.CurrentStep != "step1" {
		t.Errorf("expected current_step=step1, got %s", inst.CurrentStep)
	}
}

func TestOrchestrator_DAGTransition(t *testing.T) {
	store := newMockStore()
	executor := &mockActionExecutor{}

	// 使用自定义 logger（nil 会 panic，用 log/slog 默认）
	orch := NewOrchestrator(store, executor, nil)

	j, _ := store.CreateJourney(context.Background(), &Journey{
		Name: "multi-step",
		Steps: []Step{
			{ID: "s1", Type: "action", Next: []string{"s2"}, Action: &Action{Kind: "tag", Payload: map[string]any{"tags": []any{"welcome"}}}},
			{ID: "s2", Type: "action", Next: []string{}, Action: &Action{Kind: "message", Payload: map[string]any{}}},
		},
		Status: "active",
	})

	p := &profile.Profile{CanonicalID: "user2"}
	inst, err := orch.Enter(context.Background(), j.ID, p)
	if err != nil {
		t.Fatalf("enter failed: %v", err)
	}
	// instance 应当被创建
	if inst == nil {
		t.Fatal("instance should not be nil")
	}
}

func TestBuildStepMap(t *testing.T) {
	steps := []Step{
		{ID: "s1", Type: "action"},
		{ID: "s2", Type: "wait"},
		{ID: "s3", Type: "condition"},
	}
	m := buildStepMap(steps)
	if len(m) != 3 {
		t.Errorf("expected 3 steps in map, got %d", len(m))
	}
	if _, ok := m["s1"]; !ok {
		t.Error("s1 should be in step map")
	}
}
