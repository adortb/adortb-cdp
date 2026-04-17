package profile

import (
	"context"
	"log/slog"
)

// Enricher 从其他服务拉取数据补充到 profile。
// 实现为异步"读时补全"模式，避免同步调用增加延迟。
type Enricher struct {
	store  Store
	logger *slog.Logger
	// 扩展点：可注入外部 client（dmp/identity/mmp/billing）
}

// NewEnricher 构造 Enricher。
func NewEnricher(store Store, logger *slog.Logger) *Enricher {
	return &Enricher{store: store, logger: logger}
}

// EnrichFromDMP 将 DMP 标签写入 profile。
func (e *Enricher) EnrichFromDMP(ctx context.Context, canonicalID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	if err := e.store.UpdateTags(ctx, canonicalID, tags); err != nil {
		e.logger.Warn("enrich from dmp failed", "canonical_id", canonicalID, "error", err)
		return err
	}
	return nil
}

// EnrichTraits 合并外部 traits（如 billing 写入 LTV、MMP 写入 install_source）。
func (e *Enricher) EnrichTraits(ctx context.Context, canonicalID string, traits map[string]any) error {
	if len(traits) == 0 {
		return nil
	}
	_, err := e.store.Upsert(ctx, UpsertRequest{
		CanonicalID: canonicalID,
		Traits:      traits,
	})
	if err != nil {
		e.logger.Warn("enrich traits failed", "canonical_id", canonicalID, "error", err)
	}
	return err
}

// EnrichAttributes 合并外部 attributes（如首次来源、地理信息等）。
func (e *Enricher) EnrichAttributes(ctx context.Context, canonicalID string, attrs map[string]any) error {
	if len(attrs) == 0 {
		return nil
	}
	_, err := e.store.Upsert(ctx, UpsertRequest{
		CanonicalID: canonicalID,
		Attributes:  attrs,
	})
	if err != nil {
		e.logger.Warn("enrich attributes failed", "canonical_id", canonicalID, "error", err)
	}
	return err
}
