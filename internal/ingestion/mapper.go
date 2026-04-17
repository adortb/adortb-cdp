package ingestion

import (
	"fmt"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// Mapper 将不同来源的 Kafka 消息转换为 profile.UpsertRequest + profile.Event。
type Mapper struct{}

// NewMapper 构造 Mapper。
func NewMapper() *Mapper { return &Mapper{} }

// Map 根据 topic 分发到对应处理器。
func (m *Mapper) Map(topic string, raw map[string]any) (*profile.UpsertRequest, *profile.Event, error) {
	switch topic {
	case "adortb.events":
		return mapAdEvent(raw)
	case "adortb.mmp.installs":
		return mapMMPInstall(raw)
	case "adortb.billing.transactions":
		return mapBillingTx(raw)
	case "adortb.dmp.tags":
		return mapDMPTags(raw)
	default:
		return nil, nil, fmt.Errorf("unknown topic: %s", topic)
	}
}

func mapAdEvent(raw map[string]any) (*profile.UpsertRequest, *profile.Event, error) {
	uid := strField(raw, "uid")
	idfa := strField(raw, "idfa")
	gaid := strField(raw, "gaid")

	extIDs := map[string]string{}
	if uid != "" {
		extIDs["uid"] = uid
	}
	if idfa != "" {
		extIDs["idfa"] = idfa
	}
	if gaid != "" {
		extIDs["gaid"] = gaid
	}

	req := &profile.UpsertRequest{
		ExternalIDs: extIDs,
		Attributes: map[string]any{
			"last_ad_source": strField(raw, "source"),
		},
	}
	req.CanonicalID = profile.GenerateCanonicalID(extIDs)

	ev := &profile.Event{
		CanonicalID: req.CanonicalID,
		EventType:   strField(raw, "event_type"),
		Properties:  raw,
		Source:      "adortb.events",
		OccurredAt:  parseTime(raw, "timestamp"),
	}
	return req, ev, nil
}

func mapMMPInstall(raw map[string]any) (*profile.UpsertRequest, *profile.Event, error) {
	idfa := strField(raw, "idfa")
	gaid := strField(raw, "gaid")
	extIDs := map[string]string{}
	if idfa != "" {
		extIDs["idfa"] = idfa
	}
	if gaid != "" {
		extIDs["gaid"] = gaid
	}

	req := &profile.UpsertRequest{
		ExternalIDs: extIDs,
		Traits: map[string]any{
			"install_source": strField(raw, "source"),
			"install_at":     parseTime(raw, "install_at").Format(time.RFC3339),
		},
	}
	req.CanonicalID = profile.GenerateCanonicalID(extIDs)

	ev := &profile.Event{
		CanonicalID: req.CanonicalID,
		EventType:   "install",
		Properties:  raw,
		Source:      "mmp",
		OccurredAt:  parseTime(raw, "install_at"),
	}
	return req, ev, nil
}

func mapBillingTx(raw map[string]any) (*profile.UpsertRequest, *profile.Event, error) {
	uid := strField(raw, "uid")
	extIDs := map[string]string{}
	if uid != "" {
		extIDs["uid"] = uid
	}

	req := &profile.UpsertRequest{
		ExternalIDs: extIDs,
		Traits: map[string]any{
			"has_purchase": true,
		},
	}
	req.CanonicalID = profile.GenerateCanonicalID(extIDs)

	ev := &profile.Event{
		CanonicalID: req.CanonicalID,
		EventType:   "purchase",
		Properties:  raw,
		Source:      "billing",
		OccurredAt:  parseTime(raw, "created_at"),
	}
	return req, ev, nil
}

func mapDMPTags(raw map[string]any) (*profile.UpsertRequest, *profile.Event, error) {
	uid := strField(raw, "uid")
	extIDs := map[string]string{}
	if uid != "" {
		extIDs["uid"] = uid
	}

	var tags []string
	if t, ok := raw["tags"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				tags = append(tags, s)
			}
		}
	}

	req := &profile.UpsertRequest{
		ExternalIDs: extIDs,
		Tags:        tags,
	}
	req.CanonicalID = profile.GenerateCanonicalID(extIDs)
	return req, nil, nil
}

func strField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func parseTime(m map[string]any, key string) time.Time {
	v, ok := m[key]
	if !ok {
		return time.Now().UTC()
	}
	switch t := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", time.RFC3339Nano} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed.UTC()
			}
		}
	case float64:
		return time.Unix(int64(t), 0).UTC()
	}
	return time.Now().UTC()
}
