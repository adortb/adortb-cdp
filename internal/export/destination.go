// Package export 负责将受众导出到 DMP/DSP/邮件系统等外部目标。
package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/adortb/adortb-cdp/internal/audience"
)

// DestinationType 目标系统类型。
type DestinationType string

const (
	DestDMP     DestinationType = "dmp"
	DestDSP     DestinationType = "dsp"
	DestWebhook DestinationType = "webhook"
)

// ExportRequest 描述一次导出任务。
type ExportRequest struct {
	AudienceID  int64           `json:"audience_id"`
	Destination DestinationType `json:"destination"`
	Endpoint    string          `json:"endpoint"` // 目标 URL 或系统标识
}

// ExportResult 描述导出结果。
type ExportResult struct {
	AudienceID  int64  `json:"audience_id"`
	Exported    int    `json:"exported"`
	Destination string `json:"destination"`
}

// Exporter 将受众成员批量导出到外部系统。
type Exporter struct {
	audienceStore audience.Store
	httpClient    *http.Client
	logger        *slog.Logger
}

// NewExporter 构造 Exporter。
func NewExporter(audienceStore audience.Store, logger *slog.Logger) *Exporter {
	return &Exporter{
		audienceStore: audienceStore,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		logger:        logger,
	}
}

// Export 导出指定受众到外部系统。
func (e *Exporter) Export(ctx context.Context, req ExportRequest) (*ExportResult, error) {
	var cursor string
	exported := 0
	const batchSize = 500

	for {
		members, nextCursor, err := e.audienceStore.ListMembers(ctx, req.AudienceID, batchSize, cursor)
		if err != nil {
			return nil, fmt.Errorf("list members: %w", err)
		}

		if len(members) > 0 {
			if err := e.sendBatch(ctx, req, members); err != nil {
				return nil, fmt.Errorf("send batch: %w", err)
			}
			exported += len(members)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	e.logger.Info("export completed", "audience_id", req.AudienceID, "destination", req.Destination, "exported", exported)
	return &ExportResult{
		AudienceID:  req.AudienceID,
		Exported:    exported,
		Destination: string(req.Destination),
	}, nil
}

func (e *Exporter) sendBatch(ctx context.Context, req ExportRequest, members []*audience.Member) error {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.CanonicalID
	}

	payload := map[string]any{
		"audience_id":  req.AudienceID,
		"canonical_ids": ids,
		"timestamp":    time.Now().UTC(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.Endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("destination returned %d", resp.StatusCode)
	}
	return nil
}
