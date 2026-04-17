// Package ingestion 订阅 Kafka 多 topic，将事件映射到 profile 更新。
package ingestion

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/adortb/adortb-cdp/internal/audience"
	"github.com/adortb/adortb-cdp/internal/journey"
	"github.com/adortb/adortb-cdp/internal/profile"
	"github.com/segmentio/kafka-go"
)

// Topics 定义 CDP 监听的 Kafka topic 列表。
var Topics = []string{
	"adortb.events",
	"adortb.mmp.installs",
	"adortb.billing.transactions",
	"adortb.dmp.tags",
}

// Consumer 多 topic Kafka 消费者，每 topic 独立 goroutine。
type Consumer struct {
	brokers       []string
	topics        []string
	mapper        *Mapper
	profileStore  profile.Store
	audienceBuilder *audience.Builder
	journeyTrigger  *journey.TriggerEvaluator
	logger        *slog.Logger
	readers       []*kafka.Reader
	mu            sync.Mutex
}

// NewConsumer 构造 Consumer。
func NewConsumer(
	brokers, topics []string,
	mapper *Mapper,
	profileStore profile.Store,
	audienceBuilder *audience.Builder,
	journeyTrigger *journey.TriggerEvaluator,
	logger *slog.Logger,
) *Consumer {
	if len(topics) == 0 {
		topics = Topics
	}
	return &Consumer{
		brokers:         brokers,
		topics:          topics,
		mapper:          mapper,
		profileStore:    profileStore,
		audienceBuilder: audienceBuilder,
		journeyTrigger:  journeyTrigger,
		logger:          logger,
	}
}

// Start 在后台启动所有 topic 的消费 goroutine。
func (c *Consumer) Start(ctx context.Context) {
	for _, topic := range c.topics {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:        c.brokers,
			Topic:          topic,
			GroupID:        "adortb-cdp",
			MinBytes:       1e3,
			MaxBytes:       10e6,
			MaxWait:        500 * time.Millisecond,
			CommitInterval: time.Second,
		})
		c.mu.Lock()
		c.readers = append(c.readers, r)
		c.mu.Unlock()

		go c.consumeTopic(ctx, r, topic)
	}
}

// Close 优雅关闭所有 reader。
func (c *Consumer) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.readers {
		_ = r.Close()
	}
}

func (c *Consumer) consumeTopic(ctx context.Context, r *kafka.Reader, topic string) {
	c.logger.Info("kafka consumer started", "topic", topic)
	for {
		msg, err := r.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Warn("kafka read error", "topic", topic, "error", err)
			continue
		}
		c.handleMessage(ctx, topic, msg.Value)
	}
}

func (c *Consumer) handleMessage(ctx context.Context, topic string, data []byte) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		c.logger.Warn("unmarshal kafka message failed", "topic", topic, "error", err)
		return
	}

	req, ev, err := c.mapper.Map(topic, raw)
	if err != nil {
		c.logger.Warn("map kafka message failed", "topic", topic, "error", err)
		return
	}

	p, err := c.profileStore.Upsert(ctx, *req)
	if err != nil {
		c.logger.Warn("upsert profile failed", "topic", topic, "error", err)
		return
	}

	if ev != nil {
		if err := c.profileStore.WriteEvent(ctx, ev); err != nil {
			c.logger.Warn("write event failed", "topic", topic, "error", err)
		}
	}

	// 实时受众评估
	if err := c.audienceBuilder.EvaluateProfile(ctx, p); err != nil {
		c.logger.Warn("audience evaluate failed", "canonical_id", p.CanonicalID, "error", err)
	}

	// Journey 触发检查
	if ev != nil {
		c.journeyTrigger.CheckEvent(ctx, p.CanonicalID, ev.EventType)
	}
}
