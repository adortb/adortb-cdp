# adortb-cdp — CLAUDE.md

## 项目概述

adortb-cdp 是 adortb 广告技术栈的客户数据平台（CDP）。使用 Go 1.25.3 编写，HTTP 端口 8102，依赖 PostgreSQL、Redis、Kafka。

详细架构参见 [README.md](README.md) 和 [docs/architecture.md](docs/architecture.md)。

---

## 关键约束

### Go 版本
- 使用 **Go 1.25.3**，严格遵循该版本语法，不使用更新版本引入的特性

### 代码风格
- 文件不超过 800 行，函数不超过 50 行
- 按功能模块组织（`profile/`、`audience/`、`journey/`、`ingestion/`、`export/`）
- 错误必须显式处理，禁止 `_` 忽略非 EOF 类错误
- 使用不可变模式：返回新对象而非原地修改

### 并发与性能
- 高 QPS 场景需考虑锁粒度，优先使用分片或 `sync.RWMutex`
- Kafka Consumer 并发消费多 Topic，注意 goroutine 生命周期管理
- 受众评估（audience/evaluator.go）可能对全量用户运行，须异步批量执行
- 禁止在热路径上持有粗粒度锁

---

## 模块职责速览

| 包 | 文件 | 职责 |
|----|------|------|
| `internal/api` | server.go, handler.go | HTTP 路由与请求处理 |
| `internal/profile` | model/store/merger/enricher | 统一身份画像、Redis 缓存、跨设备合并 |
| `internal/audience` | model/store/builder/evaluator | 受众定义、条件树评估、成员管理 |
| `internal/journey` | model/store/orchestrator/trigger/executor | 旅程状态机、触发器、Action 执行 |
| `internal/ingestion` | consumer/mapper | Kafka 多 Topic 消费与消息映射 |
| `internal/export` | exporter | 受众批量导出（500 条/批）|
| `internal/metrics` | metrics.go | Prometheus 指标注册 |

---

## 数据层规范

### PostgreSQL
- 连接串通过 `POSTGRES_DSN` 环境变量注入，不可硬编码
- `cdp_events` 按 `occurred_at` 时间范围分区，查询须带分区键
- `external_ids` 为 JSONB 字段，已建 GIN 索引，使用 `@>` 操作符查询
- 所有 SQL 使用参数化查询，严禁字符串拼接 SQL

### Redis
- 缓存键格式：`cdp:profile:{canonical_id}`
- TTL：10 分钟（固定值，修改需同步更新文档）
- 缓存失效时回源 PostgreSQL，需处理缓存击穿（singleflight 或 mutex）

### Kafka
- Consumer Group: `adortb-cdp`
- 消费 Topics：`adortb.events`、`adortb.mmp.installs`、`adortb.billing.transactions`、`adortb.dmp.tags`
- 消费偏移需在业务处理成功后才提交，避免丢消息

---

## API 规范

### 端点清单（共 12 个）

```
POST   /v1/profiles
GET    /v1/profiles/{canonical_id}
POST   /v1/profiles/{canonical_id}/events
POST   /v1/profiles/identify
POST   /v1/audiences
GET    /v1/audiences/{id}/members
POST   /v1/audiences/{id}/export
POST   /v1/journeys
POST   /v1/journeys/{id}/activate
GET    /v1/journeys/{id}/instances/{canonical_id}
GET    /metrics
GET    /health
```

### 响应格式
- 成功：`{"data": ..., "error": null}`
- 失败：`{"data": null, "error": "描述"}`
- 分页：`{"data": [...], "meta": {"total": N, "page": P, "limit": L}, "error": null}`

---

## Prometheus 指标

新增业务逻辑时，须同步在 `internal/metrics/metrics.go` 注册对应指标：

- Profile upsert/event 写入用 Counter
- 受众大小用 Gauge（带 `audience_id` label）
- API 延迟用 Histogram（带 `method`、`path`、`status` labels）
- Kafka 消费用 Counter（带 `topic` label）

---

## 环境变量

| 变量 | 默认值 |
|------|--------|
| `LISTEN_ADDR` | `:8102` |
| `POSTGRES_DSN` | `postgres://postgres:postgres@localhost:5432/adortb_cdp?sslmode=disable` |
| `REDIS_ADDR` | `localhost:6379` |
| `KAFKA_BROKER` | `localhost:9092` |

启动时必须校验所有必要环境变量，缺失时 fail-fast 并输出明确错误信息。

---

## 测试要求

- 单元测试覆盖率 >= 80%
- 所有新功能须先写测试（TDD）
- 关键路径测试：
  - `profile/merger.go` 的跨设备合并逻辑
  - `audience/evaluator.go` 的条件树递归评估（含边界：空树、最大深度、AND/OR 混合）
  - `journey/orchestrator.go` 的状态机转换
  - `ingestion/mapper.go` 的各 Topic 消息映射
- 数据层测试使用 testcontainers 或 mock，不依赖真实外部服务

---

## 安全检查清单

提交前确认：
- [ ] 无硬编码密钥、密码、Token
- [ ] 所有 SQL 使用参数化查询
- [ ] 对外 API 有输入校验
- [ ] 错误信息不泄露内部细节（stack trace、SQL 语句等）
- [ ] Kafka 消费者处理异常时不崩溃（defer + recover 或显式错误处理）
