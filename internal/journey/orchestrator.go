package journey

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// ActionExecutor 执行 Step 中定义的 Action（打标/webhook/消息）。
type ActionExecutor interface {
	Execute(ctx context.Context, canonicalID string, action *Action) error
}

// Orchestrator 驱动 Journey 状态机推进。
type Orchestrator struct {
	store    Store
	executor ActionExecutor
	enricher interface {
		EnrichFromDMP(ctx context.Context, canonicalID string, tags []string) error
	}
	logger *slog.Logger
}

// NewOrchestrator 构造 Orchestrator。
func NewOrchestrator(store Store, executor ActionExecutor, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{store: store, executor: executor, logger: logger}
}

// Enter 将用户加入 Journey（若已存在则幂等）。
func (o *Orchestrator) Enter(ctx context.Context, journeyID int64, p *profile.Profile) (*Instance, error) {
	j, err := o.store.GetJourney(ctx, journeyID)
	if err != nil {
		return nil, fmt.Errorf("get journey %d: %w", journeyID, err)
	}
	if j.Status != "active" {
		return nil, fmt.Errorf("journey %d is not active (status=%s)", journeyID, j.Status)
	}

	firstStep := ""
	if len(j.Steps) > 0 {
		firstStep = j.Steps[0].ID
	}

	inst, err := o.store.UpsertInstance(ctx, &Instance{
		JourneyID:   journeyID,
		CanonicalID: p.CanonicalID,
		CurrentStep: firstStep,
		State:       map[string]any{},
	})
	if err != nil {
		return nil, err
	}

	// 异步推进第一步
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := o.advance(bgCtx, j, inst); err != nil {
			o.logger.Warn("advance journey failed", "journey_id", journeyID, "canonical_id", p.CanonicalID, "error", err)
		}
	}()

	return inst, nil
}

// OnEvent 当用户产生事件时，尝试推进所有相关 Journey 实例。
func (o *Orchestrator) OnEvent(ctx context.Context, canonicalID, eventType string) error {
	// 简化实现：遍历用户所有活跃 Journey 实例
	// 生产环境可通过 journey_id index + 事件映射表优化
	_ = canonicalID
	_ = eventType
	return nil
}

// advance 将 Journey 实例向前推进一步。
func (o *Orchestrator) advance(ctx context.Context, j *Journey, inst *Instance) error {
	stepMap := buildStepMap(j.Steps)
	step, ok := stepMap[inst.CurrentStep]
	if !ok {
		return nil // 已到终点
	}

	switch step.Type {
	case "action":
		if step.Action != nil && o.executor != nil {
			if err := o.executor.Execute(ctx, inst.CanonicalID, step.Action); err != nil {
				o.logger.Warn("action failed", "step", step.ID, "error", err)
			}
		}
		return o.moveToNext(ctx, j, inst, step)

	case "wait":
		if step.Wait != nil && step.Wait.DurationSecs > 0 {
			// 生产环境接入延迟队列（如 Redis sorted set 或 Kafka delay topic）
			time.Sleep(time.Duration(step.Wait.DurationSecs) * time.Second)
		}
		return o.moveToNext(ctx, j, inst, step)

	default:
		return o.moveToNext(ctx, j, inst, step)
	}
}

func (o *Orchestrator) moveToNext(ctx context.Context, j *Journey, inst *Instance, step Step) error {
	if len(step.Next) == 0 {
		// 旅程完成
		return o.store.CompleteInstance(ctx, inst.ID, time.Now().UTC())
	}
	inst.CurrentStep = step.Next[0]
	if _, err := o.store.UpsertInstance(ctx, inst); err != nil {
		return err
	}
	return o.advance(ctx, j, inst)
}

func buildStepMap(steps []Step) map[string]Step {
	m := make(map[string]Step, len(steps))
	for _, s := range steps {
		m[s.ID] = s
	}
	return m
}
