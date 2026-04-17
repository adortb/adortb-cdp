package ingestion

import (
	"testing"
	"time"
)

func TestMapper_AdEvent(t *testing.T) {
	m := NewMapper()
	raw := map[string]any{
		"uid":        "u123",
		"idfa":       "idfa-abc",
		"event_type": "click",
		"source":     "dsp",
		"timestamp":  time.Now().Format(time.RFC3339),
	}
	req, ev, err := m.Map("adortb.events", raw)
	if err != nil {
		t.Fatal(err)
	}
	if req.ExternalIDs["uid"] != "u123" {
		t.Error("uid should be in external_ids")
	}
	if req.ExternalIDs["idfa"] != "idfa-abc" {
		t.Error("idfa should be in external_ids")
	}
	if ev == nil {
		t.Fatal("event should not be nil")
	}
	if ev.EventType != "click" {
		t.Errorf("expected event_type=click, got %s", ev.EventType)
	}
}

func TestMapper_MMPInstall(t *testing.T) {
	m := NewMapper()
	raw := map[string]any{
		"idfa":       "idfa-xyz",
		"source":     "facebook",
		"install_at": "2024-01-01T00:00:00Z",
	}
	req, ev, err := m.Map("adortb.mmp.installs", raw)
	if err != nil {
		t.Fatal(err)
	}
	if req.Traits["install_source"] != "facebook" {
		t.Error("install_source should be set as trait")
	}
	if ev.EventType != "install" {
		t.Errorf("expected event_type=install, got %s", ev.EventType)
	}
}

func TestMapper_BillingTx(t *testing.T) {
	m := NewMapper()
	raw := map[string]any{
		"uid":        "u999",
		"amount":     9.99,
		"created_at": "2024-06-01T12:00:00Z",
	}
	req, ev, err := m.Map("adortb.billing.transactions", raw)
	if err != nil {
		t.Fatal(err)
	}
	if req.Traits["has_purchase"] != true {
		t.Error("has_purchase trait should be set")
	}
	if ev.EventType != "purchase" {
		t.Errorf("expected event_type=purchase, got %s", ev.EventType)
	}
}

func TestMapper_DMPTags(t *testing.T) {
	m := NewMapper()
	raw := map[string]any{
		"uid":  "u555",
		"tags": []any{"vip", "sports"},
	}
	req, ev, err := m.Map("adortb.dmp.tags", raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Error("dmp.tags should not produce an event")
	}
	if len(req.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(req.Tags))
	}
}

func TestMapper_UnknownTopic(t *testing.T) {
	m := NewMapper()
	_, _, err := m.Map("unknown.topic", map[string]any{})
	if err == nil {
		t.Error("should return error for unknown topic")
	}
}

func TestParseTime_RFC3339(t *testing.T) {
	expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := map[string]any{"ts": "2024-01-01T00:00:00Z"}
	result := parseTime(raw, "ts")
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestParseTime_UnixTimestamp(t *testing.T) {
	ts := float64(1704067200) // 2024-01-01T00:00:00Z
	raw := map[string]any{"ts": ts}
	result := parseTime(raw, "ts")
	expected := time.Unix(int64(ts), 0).UTC()
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}
