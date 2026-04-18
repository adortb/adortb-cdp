# adortb-cdp — Customer Data Platform

adortb-cdp 是 adortb 广告技术栈的客户数据平台（CDP），负责统一客户身份、受众管理与客户旅程编排。它从广告事件、安装、计费、DMP 标签等多个上游系统消费数据，通过 canonical_id 建立跨设备统一画像，并将受众结果输出给下游 DMP、DSP 及 Admin Dashboard。

---

## 系统架构总览

```
              ┌─────────────────────────────────────────┐
              │     Web SDK / iOS / Android / CTV       │
              └────────────────┬────────────────────────┘
                               ↓
                       ┌───────────────┐
                       │   ADX Core    │◀──外部 DSP─┐
                       └───────┬───────┘            │
                   ┌───────────┼───────────┐        │
                   ↓           ↓           ↓        │
              ┌────────┐ ┌────────┐ ┌────────┐     │
              │  DSP   │ │  MMP   │ │  SSAI  │─────┘
              └───┬────┘ └───┬────┘ └────────┘
                  ↓          ↓
              ┌───────────────────────────┐
              │  Event Pipeline (Kafka)   │
              └───────┬───────────────────┘
        ┌─────────────┼─────────────┐
        ↓             ↓             ↓
  ┌─────────┐   ┌──────────┐   ┌──────────┐
  │ Billing │   │   DMP    │   │  ★CDP   │
  └─────────┘   └──────────┘   └──────────┘
                       ↓
                  ┌──────────┐
                  │  Admin   │◀── Frontend
                  └──────────┘
```

CDP 在整个平台中处于核心位置：
- **上游消费**：ADX Core 广告事件、MMP 安装事件、Billing 交易事件、DMP 标签事件
- **下游输出**：受众/旅程数据提供给 Admin Dashboard；导出至 DMP、DSP 及自定义 Webhook

---

## 技术栈

| 组件 | 版本 |
|------|------|
| Go | 1.25.3 |
| PostgreSQL (lib/pq) | 1.10.9 |
| Kafka (segmentio/kafka-go) | 0.4.47 |
| Redis (go-redis) | v9.18.0 |
| Prometheus | v1.23.2 |

---

## 项目结构

```
adortb-cdp/
├── cmd/cdp/main.go
├── internal/
│   ├── api/server.go           # HTTP 路由（12 个端点）
│   ├── api/handler.go          # 请求处理
│   ├── profile/                # 客户统一画像
│   │   ├── model.go            # Profile/Event 数据模型
│   │   ├── store.go            # PostgreSQL + Redis 缓存
│   │   ├── merger.go           # 跨设备合并
│   │   └── enricher.go         # 外部数据补充
│   ├── audience/               # 受众管理
│   │   ├── model.go            # Audience/Condition/Member
│   │   ├── store.go            # PostgreSQL 存储
│   │   ├── builder.go          # 受众评估和成员管理
│   │   └── evaluator.go        # 条件树递归评估
│   ├── journey/                # 客户旅程编排
│   │   ├── model.go            # Journey/Step/Instance
│   │   ├── store.go            # PostgreSQL 存储
│   │   ├── orchestrator.go     # 状态机推进
│   │   ├── trigger.go          # 触发器评估
│   │   └── executor.go         # Action 执行（tag/webhook/message）
│   ├── ingestion/              # 数据摄入
│   │   ├── consumer.go         # Kafka 消费（多 topic）
│   │   └── mapper.go           # Topic 消息映射
│   ├── export/                 # 受众导出
│   │   └── exporter.go         # 批量导出（500 条/批）
│   └── metrics/metrics.go      # Prometheus 指标
└── migrations/001_cdp.up.sql
```

---

## 快速启动

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8102` | HTTP 监听地址 |
| `POSTGRES_DSN` | `postgres://postgres:postgres@localhost:5432/adortb_cdp?sslmode=disable` | PostgreSQL 连接串 |
| `REDIS_ADDR` | `localhost:6379` | Redis 地址 |
| `KAFKA_BROKER` | `localhost:9092` | Kafka Broker 地址 |

### 运行

```bash
# 数据库迁移
psql $POSTGRES_DSN -f migrations/001_cdp.up.sql

# 启动服务
go run ./cmd/cdp/...

# 或自定义配置
LISTEN_ADDR=:8102 \
POSTGRES_DSN="postgres://user:pass@host:5432/adortb_cdp?sslmode=disable" \
REDIS_ADDR=localhost:6379 \
KAFKA_BROKER=localhost:9092 \
go run ./cmd/cdp/...
```

---

## HTTP API

### Profile 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/profiles` | 创建或更新 Profile（Upsert） |
| `GET` | `/v1/profiles/{canonical_id}` | 获取 Profile（含最近 20 条事件） |
| `POST` | `/v1/profiles/{canonical_id}/events` | 写入事件 |
| `POST` | `/v1/profiles/identify` | 跨设备身份合并 |

### Audience 受众

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/audiences` | 创建受众（含条件树定义） |
| `GET` | `/v1/audiences/{id}/members` | 分页获取受众成员 |
| `POST` | `/v1/audiences/{id}/export` | 触发受众导出 |

### Journey 旅程

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/journeys` | 创建 Journey（DAG 格式） |
| `POST` | `/v1/journeys/{id}/activate` | 激活 Journey |
| `GET` | `/v1/journeys/{id}/instances/{canonical_id}` | 查询用户执行状态 |

### 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/metrics` | Prometheus 指标 |
| `GET` | `/health` | 健康检查 |

---

## Kafka 消费

- **Consumer Group**: `adortb-cdp`
- **消费 Topics**:

| Topic | 数据类型 |
|-------|---------|
| `adortb.events` | ADX Core 广告事件 |
| `adortb.mmp.installs` | MMP 安装事件 |
| `adortb.billing.transactions` | Billing 交易事件 |
| `adortb.dmp.tags` | DMP 标签事件 |

---

## Prometheus 监控指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `cdp_profile_upserts_total` | Counter | Profile Upsert 总次数 |
| `cdp_events_written_total` | Counter | 写入事件总次数 |
| `cdp_audience_evaluations_total` | Counter | 受众评估总次数 |
| `cdp_audience_size{audience_id}` | Gauge | 各受众当前成员数 |
| `cdp_journey_enters_total` | Counter | Journey 进入总次数 |
| `cdp_kafka_messages_processed_total{topic}` | Counter | 各 Topic 消息处理总次数 |
| `cdp_api_request_duration_seconds{method,path,status}` | Histogram | API 请求延迟 |

---

## 核心特性说明

### 统一身份（Profile）
- 使用 `canonical_id` 作为跨系统的统一用户身份标识
- `external_ids` 字段为 JSONB 格式，支持多系统 ID 映射（IDFA、GAID、email、phone 等）
- GIN 索引加速 JSONB 查询

### 受众条件树
- 支持嵌套 AND/OR 逻辑，最大嵌套深度 10 层
- 叶节点类型：Trait（用户属性）、Attr（自定义属性）、Tag（DMP 标签）、Event（行为事件）

### 客户旅程（Journey）
- Entry Trigger 支持：事件触发、受众进入、Cron 定时
- Steps 以 DAG 形式定义，支持 action（标签/Webhook/消息）、wait（等待）、condition（分支）节点
- 状态机驱动用户在旅程中逐步推进

### Redis 二级缓存
- 缓存键：`cdp:profile:{canonical_id}`
- TTL：10 分钟

### 数据分区
- `cdp_events` 表按 `occurred_at` 时间范围分区，提升大数据量下的查询性能

### 受众导出
- 分批处理，每批 500 条
- 通过 HTTP POST 推送至目标端点（DMP、DSP、Webhook）

---

## 详细架构文档

请参阅 [docs/architecture.md](docs/architecture.md) 获取内部架构图、数据流、Journey 状态机及受众条件树详细说明。
