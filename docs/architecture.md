# adortb-cdp 内部架构文档

## 1. 内部模块架构图

CDP 服务由 5 个核心业务包组成，通过共享的数据层（PostgreSQL + Redis）和消息层（Kafka）协同工作：

```
                          ┌──────────────────────────────────────────────────┐
                          │                  adortb-cdp                      │
                          │                                                  │
   HTTP 请求              │  ┌────────────────────────────────────────────┐  │
  ─────────────────────►  │  │              internal/api                  │  │
                          │  │   server.go (路由)  handler.go (处理)      │  │
                          │  └──────┬──────────┬──────────────┬───────────┘  │
                          │         │          │              │               │
                          │         ▼          ▼              ▼               │
                          │  ┌──────────┐ ┌────────────┐ ┌────────────┐     │
                          │  │ profile/ │ │ audience/  │ │ journey/   │     │
                          │  │          │ │            │ │            │     │
                          │  │ model    │ │ model      │ │ model      │     │
                          │  │ store    │ │ store      │ │ store      │     │
                          │  │ merger   │ │ builder    │ │orchestrator│     │
                          │  │ enricher │ │ evaluator  │ │ trigger    │     │
                          │  └────┬─────┘ └─────┬──────┘ │ executor   │     │
                          │       │             │        └─────┬──────┘     │
                          │       │             │              │             │
  Kafka Topics            │  ┌────────────────────────────────────────────┐  │
  ─────────────────────►  │  │              internal/ingestion            │  │
  adortb.events           │  │   consumer.go (多 Topic)  mapper.go        │  │
  adortb.mmp.installs     │  └──────────────────────┬─────────────────────┘  │
  adortb.billing.*        │                         │                         │
  adortb.dmp.tags         │                         ▼                         │
                          │  ┌────────────────────────────────────────────┐  │
                          │  │              internal/export               │  │
                          │  │   exporter.go (500 条/批，HTTP POST 推送)  │  │
                          │  └────────────────────────────────────────────┘  │
                          │                                                  │
                          │  ┌────────────────────────────────────────────┐  │
                          │  │              internal/metrics               │  │
                          │  │   Prometheus Counter/Gauge/Histogram        │  │
                          │  └────────────────────────────────────────────┘  │
                          └────────────────┬─────────────────────────────────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    ▼                      ▼                      ▼
             ┌────────────┐        ┌─────────────┐        ┌────────────┐
             │ PostgreSQL │        │    Redis     │        │   Kafka    │
             │            │        │              │        │  (消费方)  │
             │ profiles   │        │ cdp:profile: │        └────────────┘
             │ cdp_events │        │ {canonical_id}│
             │ audiences  │        │ TTL: 10min   │
             │ journeys   │        └─────────────┘
             └────────────┘
```

---

## 2. 数据摄入流程

### 2.1 Kafka 消费到 Profile 更新完整链路

```
Kafka Topics
    │
    │  adortb.events          adortb.mmp.installs
    │  adortb.billing.        adortb.dmp.tags
    │  transactions
    ▼
┌─────────────────────────────────────────────────────┐
│              ingestion/consumer.go                   │
│                                                     │
│  Consumer Group: adortb-cdp                         │
│  并发消费多 Topic（每个 Topic 独立 goroutine）        │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│              ingestion/mapper.go                     │
│                                                     │
│  adortb.events         → Profile Event (广告行为)   │
│  adortb.mmp.installs   → Profile Event (安装事件)   │
│  adortb.billing.*      → Profile Event (付费事件)   │
│  adortb.dmp.tags       → Profile Trait  (标签更新)  │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│              profile/store.go                        │
│                                                     │
│  1. 查 Redis cdp:profile:{canonical_id}             │
│     命中 → 反序列化 Profile                         │
│     未中 → 查 PostgreSQL profiles 表                │
│  2. Upsert Profile（external_ids JSONB 合并）       │
│  3. INSERT cdp_events（按 occurred_at 分区）        │
│  4. 回写 Redis，TTL 10min                           │
└──────────────────────┬──────────────────────────────┘
                       │
           ┌───────────┴───────────┐
           ▼                       ▼
┌──────────────────┐   ┌───────────────────────────────┐
│ audience/builder │   │    journey/trigger.go          │
│                  │   │                               │
│ 重新评估该用户   │   │ 检查是否满足 Journey           │
│ 所在的受众条件树 │   │ Entry Trigger（事件/受众/Cron）│
│ → 更新成员列表   │   │ → 若满足则进入 Journey         │
└──────────────────┘   └───────────────────────────────┘
```

### 2.2 Kafka 消费偏移提交策略

业务处理（Profile Upsert + 受众评估 + Journey 触发）**全部成功**后才提交偏移量，保证 at-least-once 语义。下游操作需保证幂等（Profile Upsert 天然幂等，Journey 进入须检查重复）。

---

## 3. Journey 状态机

### 3.1 Journey 生命周期

```
         创建
          │
          ▼
      ┌────────┐   activate
      │ DRAFT  │ ──────────────► ┌──────────┐
      └────────┘                 │  ACTIVE  │
                                 └─────┬────┘
                                       │
                    ┌──────────────────┼──────────────────┐
                    │                  │                  │
                    ▼                  ▼                  ▼
              Entry Trigger        pause           deactivate
              满足条件             │                  │
                    │              ▼                  ▼
                    │         ┌─────────┐        ┌──────────┐
                    │         │ PAUSED  │        │ INACTIVE │
                    │         └─────────┘        └──────────┘
                    ▼
           ┌────────────────┐
           │ JourneyInstance │  (每个用户独立实例)
           └────────┬───────┘
                    │
                    ▼
              ┌───────────┐
              │  ENTERED  │
              └─────┬─────┘
                    │
          ┌─────────┴──────────┐
          ▼                    ▼
   ┌────────────┐      ┌───────────────┐
   │  执行 Step │      │  Wait Step    │
   │  (action)  │      │  等待条件/时间 │
   └──────┬─────┘      └───────┬───────┘
          │                    │
          │   ◄────────────────┘
          │   满足条件后继续
          ▼
   ┌────────────┐
   │ COMPLETED  │
   └────────────┘
          │ 或
          ▼
   ┌────────────┐
   │   FAILED   │  (执行异常/超时)
   └────────────┘
          │ 或
          ▼
   ┌────────────┐
   │  EXITED    │  (主动退出/受众条件不再满足)
   └────────────┘
```

### 3.2 Step 类型与执行逻辑

```
Step DAG 示例：

  Entry Trigger
       │
       ▼
  [action: tag]          ← 给用户打标签（调用 DMP 接口）
       │
       ▼
  [wait: 24h]            ← 等待 24 小时
       │
       ▼
  [condition: 是否付费]   ← 评估条件
      / \
    YES   NO
    /       \
[action:   [action:
 message]   webhook]     ← 发消息 or 触发 Webhook
    \       /
     \     /
      \   /
       ▼
  [COMPLETED]
```

### 3.3 Entry Trigger 类型

| Trigger 类型 | 说明 | 示例 |
|-------------|------|------|
| `event` | 用户触发指定事件时进入 | 发生 `app_install` 事件 |
| `audience` | 用户进入指定受众时触发 | 加入「高价值用户」受众 |
| `cron` | 定时触发（对满足条件的用户批量执行） | 每天 09:00 给活跃用户推送 |

---

## 4. 受众条件树

### 4.1 条件树结构

受众由一棵条件树定义，支持嵌套 AND/OR，最大深度 10 层：

```
Audience: "高付费活跃用户"
│
└── AND
    ├── OR
    │   ├── Trait: platform = "iOS"
    │   └── Trait: platform = "Android"
    │
    ├── Event: purchase (近 30 天发生过)
    │   └── 附加条件: amount > 100
    │
    ├── Tag: "dmp:high_value" = true
    │
    └── AND
        ├── Attr: country IN ["CN", "JP", "KR"]
        └── Attr: age >= 18
```

### 4.2 叶节点类型

| 类型 | 描述 | 示例 |
|------|------|------|
| `Trait` | 用户固有属性（Profile 字段） | `platform = "iOS"` |
| `Attr` | 自定义用户属性（JSONB） | `country IN ["CN","JP"]` |
| `Tag` | DMP 标签（来自 adortb.dmp.tags） | `"high_value" = true` |
| `Event` | 行为事件（来自 cdp_events） | `purchase 在近 30 天内发生` |

### 4.3 评估流程（audience/evaluator.go）

```
evaluateNode(node, profile):
  if node.type == "AND":
    return ALL children pass
  if node.type == "OR":
    return ANY child passes
  if node.type == "leaf":
    return evaluateLeaf(node, profile)

evaluateLeaf(node, profile):
  switch node.leafType:
    case "Trait":  → 查 profile.traits[key] op value
    case "Attr":   → 查 profile.attributes[key] op value
    case "Tag":    → 查 profile.tags 是否包含 tag
    case "Event":  → 查 cdp_events WHERE canonical_id AND event_type AND 时间窗口
```

递归深度保护：评估栈深度超过 10 时返回 `false` 并记录告警。

---

## 5. 数据库表结构概览

```sql
-- 统一用户画像
profiles (
  canonical_id    UUID PRIMARY KEY,
  external_ids    JSONB,          -- GIN 索引，存储 {idfa, gaid, email, ...}
  traits          JSONB,
  attributes      JSONB,
  tags            TEXT[],
  created_at      TIMESTAMPTZ,
  updated_at      TIMESTAMPTZ
)

-- 用户行为事件（按 occurred_at 时间范围分区）
cdp_events (
  id              BIGSERIAL,
  canonical_id    UUID,
  event_type      TEXT,
  properties      JSONB,
  occurred_at     TIMESTAMPTZ    -- 分区键
) PARTITION BY RANGE (occurred_at)

-- 受众定义
audiences (
  id              UUID PRIMARY KEY,
  name            TEXT,
  condition_tree  JSONB,          -- 条件树 JSON 序列化
  created_at      TIMESTAMPTZ
)

-- 受众成员
audience_members (
  audience_id     UUID,
  canonical_id    UUID,
  joined_at       TIMESTAMPTZ,
  PRIMARY KEY (audience_id, canonical_id)
)

-- Journey 定义
journeys (
  id              UUID PRIMARY KEY,
  name            TEXT,
  status          TEXT,           -- DRAFT/ACTIVE/PAUSED/INACTIVE
  entry_trigger   JSONB,
  steps           JSONB,          -- DAG 结构
  created_at      TIMESTAMPTZ
)

-- Journey 用户实例
journey_instances (
  id              UUID PRIMARY KEY,
  journey_id      UUID,
  canonical_id    UUID,
  current_step    TEXT,
  status          TEXT,           -- ENTERED/COMPLETED/FAILED/EXITED
  entered_at      TIMESTAMPTZ,
  updated_at      TIMESTAMPTZ
)
```

---

## 6. Redis 缓存策略

```
键格式：cdp:profile:{canonical_id}
值格式：JSON 序列化的 Profile 对象（含 traits、attributes、tags）
TTL：600 秒（10 分钟）

读取流程：
  GET cdp:profile:{canonical_id}
    命中 → JSON 反序列化，直接返回
    未中 → SELECT FROM profiles WHERE canonical_id = ?
           → SET cdp:profile:{canonical_id} EX 600

写入流程（Upsert）：
  BEGIN TRANSACTION
    UPSERT profiles ...
    INSERT cdp_events ...
  COMMIT
  DEL cdp:profile:{canonical_id}   ← 先删后写，避免脏缓存
```

---

## 7. 受众导出流程

```
POST /v1/audiences/{id}/export
  │
  ▼
export/exporter.go
  │
  ├── SELECT canonical_id FROM audience_members WHERE audience_id = ?
  │   ORDER BY joined_at
  │
  ├── 分批处理（每批 500 条）
  │   ├── 批次 1: [id_001 ... id_500]   → HTTP POST → 目标端点
  │   ├── 批次 2: [id_501 ... id_1000]  → HTTP POST → 目标端点
  │   └── ...
  │
  ├── 每批携带 batch_index 和 total_count，支持目标端点幂等接收
  │
  └── 导出结果记录（成功批次数、失败批次数、总成员数）
         → cdp_audience_size{audience_id} Gauge 同步更新

导出目标：
  - DMP  (adortb.dmp)
  - DSP  (adortb.dsp)
  - 自定义 Webhook（由 Audience 定义时配置）
```
