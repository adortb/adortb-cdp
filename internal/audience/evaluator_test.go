package audience

import (
	"context"
	"testing"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// mockEventCounter 测试用事件计数器。
type mockEventCounter struct {
	counts map[string]int
}

func (m *mockEventCounter) CountEvents(_ context.Context, _, eventType string, _ time.Time) (int, error) {
	return m.counts[eventType], nil
}

func baseProfile() *profile.Profile {
	return &profile.Profile{
		CanonicalID: "test-user",
		Traits: map[string]any{
			"is_vip":         true,
			"lifetime_value": float64(1500),
			"country":        "US",
		},
		Attributes: map[string]any{
			"country": "US",
		},
		Tags: []string{"high_value_user", "active"},
	}
}

func intPtr(v int) *int { return &v }

func TestEvaluator_TraitEqual(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Trait: "is_vip", Op: "=", Value: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("is_vip=true should match")
	}
}

func TestEvaluator_TraitNotEqual(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Trait: "is_vip", Op: "=", Value: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("is_vip should not equal false")
	}
}

func TestEvaluator_AttributeIn(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Attr: "country", Op: "in", Value: []any{"US", "UK"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("country=US should be in [US, UK]")
	}
}

func TestEvaluator_TagMatch(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{Tag: "high_value_user"})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("should match high_value_user tag")
	}
}

func TestEvaluator_TagNotMatch(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, _ := e.Matches(context.Background(), p, Condition{Tag: "churned"})
	if matched {
		t.Error("should not match churned tag")
	}
}

func TestEvaluator_NestedAndOr(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	// {"and": [is_vip=true, {"or": [country=US, country=UK]}]}
	cond := Condition{
		And: []Condition{
			{Trait: "is_vip", Op: "=", Value: true},
			{
				Or: []Condition{
					{Attr: "country", Op: "=", Value: "US"},
					{Attr: "country", Op: "=", Value: "UK"},
				},
			},
		},
	}
	matched, err := e.Matches(context.Background(), p, cond)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("nested and/or should match")
	}
}

func TestEvaluator_EventCount(t *testing.T) {
	counter := &mockEventCounter{counts: map[string]int{"purchase": 5}}
	e := NewEvaluator(counter)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Event: "purchase", Op: "count", Gte: intPtr(3), WindowDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("5 purchases should satisfy gte=3")
	}
}

func TestEvaluator_EventCount_NotEnough(t *testing.T) {
	counter := &mockEventCounter{counts: map[string]int{"purchase": 2}}
	e := NewEvaluator(counter)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Event: "purchase", Op: "count", Gte: intPtr(3), WindowDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("2 purchases should NOT satisfy gte=3")
	}
}

func TestEvaluator_AndShortCircuit(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	// 第一个条件失败，整个 AND 应短路
	cond := Condition{
		And: []Condition{
			{Trait: "is_vip", Op: "=", Value: false}, // false
			{Tag: "high_value_user"},                  // true (but won't reach)
		},
	}
	matched, err := e.Matches(context.Background(), p, cond)
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Error("AND with first condition false should not match")
	}
}

func TestEvaluator_NumericGte(t *testing.T) {
	e := NewEvaluator(nil)
	p := baseProfile()

	matched, err := e.Matches(context.Background(), p, Condition{
		Trait: "lifetime_value", Op: ">=", Value: float64(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("lifetime_value=1500 should be >= 1000")
	}
}
