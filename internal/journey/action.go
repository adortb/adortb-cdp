package journey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// defaultExecutor 内置 ActionExecutor 实现。
type defaultExecutor struct {
	profileStore profile.Store
	httpClient   *http.Client
	logger       *slog.Logger
}

// NewDefaultExecutor 构造内置 ActionExecutor。
func NewDefaultExecutor(profileStore profile.Store, logger *slog.Logger) ActionExecutor {
	return &defaultExecutor{
		profileStore: profileStore,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
	}
}

func (e *defaultExecutor) Execute(ctx context.Context, canonicalID string, action *Action) error {
	switch action.Kind {
	case "tag":
		return e.executeTag(ctx, canonicalID, action.Payload)
	case "webhook":
		return e.executeWebhook(ctx, canonicalID, action.Payload)
	case "message":
		e.logger.Info("send message action", "canonical_id", canonicalID, "payload", action.Payload)
		return nil
	default:
		return fmt.Errorf("unknown action kind: %s", action.Kind)
	}
}

func (e *defaultExecutor) executeTag(ctx context.Context, canonicalID string, payload map[string]any) error {
	tagsRaw, ok := payload["tags"]
	if !ok {
		return fmt.Errorf("tag action missing 'tags' field")
	}
	rawSlice, ok := tagsRaw.([]any)
	if !ok {
		return fmt.Errorf("tag action 'tags' must be array")
	}
	tags := make([]string, 0, len(rawSlice))
	for _, t := range rawSlice {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	return e.profileStore.UpdateTags(ctx, canonicalID, tags)
}

func (e *defaultExecutor) executeWebhook(ctx context.Context, canonicalID string, payload map[string]any) error {
	urlRaw, ok := payload["url"]
	if !ok {
		return fmt.Errorf("webhook action missing 'url'")
	}
	url, ok := urlRaw.(string)
	if !ok {
		return fmt.Errorf("webhook url must be string")
	}

	body := map[string]any{
		"canonical_id": canonicalID,
		"payload":      payload,
		"timestamp":    time.Now().UTC(),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}
