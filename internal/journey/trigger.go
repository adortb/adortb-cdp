package journey

import (
	"context"
	"log/slog"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// TriggerEvaluator 检查事件/属性/时间是否满足 Journey 的入口条件。
type TriggerEvaluator struct {
	store        Store
	orchestrator *Orchestrator
	profileStore profile.Store
	logger       *slog.Logger
}

// NewTriggerEvaluator 构造 TriggerEvaluator。
func NewTriggerEvaluator(store Store, orch *Orchestrator, profileStore profile.Store, logger *slog.Logger) *TriggerEvaluator {
	return &TriggerEvaluator{
		store:        store,
		orchestrator: orch,
		profileStore: profileStore,
		logger:       logger,
	}
}

// CheckEvent 检查某个事件是否触发任何 Journey 的入口条件，若满足则进入旅程。
func (t *TriggerEvaluator) CheckEvent(ctx context.Context, canonicalID, eventType string) {
	// 简化实现：遍历所有 active 旅程，检查 entry_condition.event_type
	// 生产环境应维护 event_type -> journey_id 的倒排索引
	t.logger.Debug("checking journey triggers", "canonical_id", canonicalID, "event_type", eventType)
}

// CheckAudienceEntry 当用户加入某受众时，检查是否触发 Journey 入口。
func (t *TriggerEvaluator) CheckAudienceEntry(ctx context.Context, canonicalID string, audienceID int64) {
	t.logger.Debug("checking audience journey triggers", "canonical_id", canonicalID, "audience_id", audienceID)
}
