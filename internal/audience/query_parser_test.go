package audience

import (
	"testing"
)

func TestParseCondition_Valid(t *testing.T) {
	raw := []byte(`{
		"and": [
			{"trait":"is_vip","op":"=","value":true},
			{"or":[
				{"attr":"country","op":"in","value":["US","UK"]},
				{"tag":"high_value_user"}
			]}
		]
	}`)
	cond, err := ParseCondition(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cond.And) != 2 {
		t.Errorf("expected 2 AND branches, got %d", len(cond.And))
	}
}

func TestParseCondition_Empty(t *testing.T) {
	_, err := ParseCondition(nil)
	if err == nil {
		t.Error("expected error for empty condition")
	}
}

func TestParseCondition_TooDeep(t *testing.T) {
	// 构造超过 maxDepth 的嵌套
	raw := []byte(`{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"and":[{"tag":"x"}]}]}]}]}]}]}]}]}]}]}]}`)
	_, err := ParseCondition(raw)
	if err == nil {
		t.Error("expected error for too deep condition")
	}
}

func TestParseCondition_MissingOp(t *testing.T) {
	raw := []byte(`{"trait":"is_vip","value":true}`)
	_, err := ParseCondition(raw)
	if err == nil {
		t.Error("expected error when op is missing")
	}
}

func TestParseCondition_EventCondition(t *testing.T) {
	raw := []byte(`{"event":"purchase","op":"count","gte":3,"window_days":30}`)
	cond, err := ParseCondition(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cond.Event != "purchase" {
		t.Errorf("expected event=purchase, got %s", cond.Event)
	}
}
