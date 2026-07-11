# Labubu 项目架构文档

> 最后更新：2026-07-11（基于 develop 分支 HEAD）

## 整体架构概览

Labubu 是一个本地优先的 LLM 可观测平台，接收 OTLP 遥测数据（Traces、Metrics、Logs），派生 token 桶并计算成本（含 prompt-caching 差异化费率），提供 AI 驱动的 Trace 诊断与会话级行为分析，并通过 Vue SPA 提供 Web 看板。

### 全局架构图

```
                          OTLP Sender (SDK / AI Agent)
                                 │
                    ┌────────────┼────────────┐
                    │ gRPC :4317  │  HTTP :4318 │   (端口可配置)
                    └───────┬──────┴──────┬──────┘
                            │             │
                            ▼             ▼
                    ┌───────────────────────────┐
                    │      OTLP Receiver         │  internal/receiver/
                    │  ┌──────┐ ┌──────┐ ┌─────┐ │
                    │  │Trace │ │Metric│ │ Log │ │
                    │  │翻译器│ │翻译器│ │翻译器│ │
                    │  └──┬───┘ └──┬───┘ └─┬───┘ │
                    └───┼────────┼────────┼─────┘
                        │        │        │
           ┌────────────┼        │        ├────────────┐
           │            ▼        │        ▼            │
           │  ┌─────────────┐    │  ┌──────────────┐   │
           │  │   Pipeline   │    │  │ MetricStore  │   │
           │  │ (异步缓冲)   │    │  │  (tstorage)  │   │
           │  └───┬─────────┘    │  └───┬──────────┘   │
           │      ▼              │      │              │
           │  ┌─────────────┐    │      │              │
           │  │    Store     │◄───┼──────┤              │
           │  │ (chDB/SQLite │    │      │              │
           │  │  /memStore)  │    │      │              │
           │  └───┬─────────┘    │      │              │
           │      │              │      │              │
           │      ├──► token 派生 │      │              │
           │      │  (DeriveTokenBuckets)  │              │
           │      ├──► 成本计算  │      │              │
           │      │  (pricing)  │      │              │
           │      │  + cache 费率│      │              │
           │      │              │      │              │
           └──────┼──────────────┼──────┼──────────────┘
                  │              │      │
                  ▼              ▼      ▼
           ┌─────────────────────────────────────┐
           │           HTTP API Server            │  internal/api/ + internal/alerting/
           │   :8080  (可配置)                    │
           │   ┌──────┐ ┌──────┐ ┌──────┐        │
           │   │Trace │ │Metric│ │Session│  ...   │
           │   │Handler│ │Handler│ │Handler│       │
           │   └──┬───┘ └──┬───┘ └──┬───┘        │
           │      │        │       │              │
           │      ├──► LLM 诊断 (Diagnosis)       │
           │      ├──► 告警引擎 (Alerting)         │
           │      ├──► OpenAPI 3.0 spec           │
           │      │                               │
           │   ┌──┴───────────────────────────┐   │
           │   │      SPA Fallback / Embed     │   │
           │   │    Vue 3 前端 (web/dist/)     │   │
           │   └──────────────────────────────┘   │
           └─────────────────────────────────────┘
                        │
                        ▼
                 ┌──────────────┐
                 │   Browser     │
                 │  Vue 3 SPA    │
                 └──────────────┘
```

### 实现技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| **后端语言** | Go 1.25 | 标准库为主，无 ORM |
| **HTTP 路由** | Go 1.22 `http.ServeMux` | 路径参数 `{id}` 原生支持，`strings.TrimPrefix` 手动子路由 |
| **数据存储** | chDB / SQLite / 内存存储 | CGO 构建用 chDB，默认非 CGO 构建用 SQLite，memStore 为 fallback，统一 `Store` 接口 |
| **指标存储** | tstorage | 嵌入式 TSDB，Prometheus 兼容查询 |
| **日志存储** | chDB / SQLite / 内存存储 | 与 Trace 共用 Store 接口 |
| **SQLite 驱动** | modernc.org/sqlite | 纯 Go 实现，无需 CGO，WAL 模式并发读 |
| **前端框架** | Vue 3 + TypeScript | Composition API，无 Vuex/Pinia |
| **前端构建** | Vite | 开发模式 `make dev`，生产嵌入 Go 二进制 |
| **前端嵌入** | go:embed | 生产构建将 `web/dist` 嵌入 Go 二进制，单文件部署 |
| **国际化** | vue-i18n | 中/英双语，`localStorage` 存储语言偏好，16 个 section |
| **OTLP 协议** | gRPC + HTTP | 标准 OpenTelemetry 协议，端口 4317/4318 可配置 |
| **告警通知** | SMTP | 内置邮件通知器，`Notifier` 接口可扩展 |
| **API 文档** | OpenAPI 3.0 + swagger-ui | `/api/v1/openapi.json` + 前端内嵌 Swagger UI |
| **Python 分发** | pip wheel | Go 二进制打包进 Python wheel，`pip install labubu` 即可用 |
| **MCP 集成** | Python MCP SDK (FastMCP) | 暴露 Labubu API 为 AI Agent 工具（5 个工具） |

---

## 模块架构详情

### 1. 入口与启动 — `cmd/labubu/`

#### 架构图

```
labubu serve [flags]
     │
     ▼
 ┌──────────────────────────────────────────────────────┐
 │  main.go                                             │
 │                                                      │
 │  1. 解析 CLI 参数 (见下表)                            │
 │  2. 加载 YAML 配置 (storage.LoadConfig)              │
 │  3. 端口可用性预检 (占用则提示 --port N+1 退出)       │
 │  4. 初始化子系统 (按顺序):                            │
 │     Store → RetentionCleanup → MetricStore → Pipeline│
 │     → Receiver → Alerting → Handlers → Router        │
 │  5. 种子默认定价 (from config.Pricing.Models)        │
 │  6. 启动 HTTP 服务器 (:8080, Read/Write 30s)        │
 │  7. 等待 SIGINT/SIGTERM → 10s graceful shutdown      │
 └──────────────────────────────────────────────────────┘
```

#### CLI 参数（`serve` 子命令）

| flag | 默认值 | 说明 |
|------|--------|------|
| `--port` | 8080 | API + UI 监听端口 |
| `--otlp-grpc-port` | 4317 | OTLP gRPC 监听端口 |
| `--otlp-http-port` | 4318 | OTLP HTTP 监听端口 |
| `--data-dir` | `data` | 持久化目录（空 = 纯内存） |
| `--buffer-size` | 1000 | Pipeline 缓冲容量 |
| `--flush-interval` | 200ms | Pipeline flush 间隔 |
| `--metrics-enabled` | true | 是否启用 metrics 摄取 |
| `--metrics-data-dir` | `""` | tstorage 目录（空 = 纯内存） |
| `--log-level` | `info` | debug/info/warn/error |
| `--config` | `labubu.yaml` | YAML 配置文件路径 |

#### 技术要点

- **子命令模式**：`labubu serve`、`labubu version`、`labubu help`
- **构建标签系统**：`cgo` / `local_engine` 选择 chDB vs SQLite/memStore；`nosqlite` 选择 memStore vs SQLite；`dev` 选择磁盘读取前端 vs 嵌入
- **版本注入**：`-ldflags "-X main.Version=..."` 由 Makefile 通过 `git describe` 自动注入
- **端口分离**：OTLP gRPC / OTLP HTTP / API 三个端口独立可配置（原单端口拆分）
- **Retention 清理**：独立 goroutine `runRetentionCleanup` 按 `CleanupInterval` 周期调用 `store.Purge` + `store.PurgeLogs`，trace/log/metric 各自可配 7 天保留
- **告警容错**：`alerting.InitAlerting` 失败时仅禁用告警，服务继续运行
- **优雅关停**：捕获 SIGINT/SIGTERM，10 秒超时，依次 `httpSrv.Shutdown` → `store.Close` → `pipe.Shutdown(5s)` → `recv.Shutdown(5s)` → `alertSub.Shutdown`

---

### 2. OTLP 接收器 — `internal/receiver/`

#### 架构图

```
              OTLP Sender
                 │
    ┌────────────┼────────────┐
    │ gRPC :4317 │ HTTP :4318 │   (端口可配置, 监听 0.0.0.0)
    └───────┬────┴─────┬──────┘
            │          │
            ▼          ▼
    ┌────────────────────────┐
    │       Receiver         │
    │                        │
    │  TraceServiceServer    │◄── gRPC TraceService (无条件)
    │  MetricsServiceServer  │◄── gRPC MetricsService (metricStore!=nil)
    │  LogsServiceServer     │◄── gRPC LogsService (store!=nil)
    │                        │
    │  /v1/traces  (HTTP)    │◄── protobuf 或 JSON (protojson)
    │  /v1/metrics (HTTP)    │◄── 仅 protobuf
    │  /v1/logs    (HTTP)    │◄── protobuf 或 JSON (protojson)
    │                        │
    │  ┌──── 翻译层 ─────────┐│
    │  │ normalizeAttributes │  属性键归一化 (多 Agent 兼容)
    │  │ translateSpans()    │──► storage.DeriveTokenBuckets (派生 token)
    │  │ TranslateMetrics()  │──► []MetricPoint
    │  │ translateLogs()     │──► []LogRecord
    │  └─────────────────────┘│
    └────────┬────┬────┬──────┘
             │    │    │
             ▼    ▼    ▼
        Pipeline  MS   Store
       (异步缓冲) (直写) (直写)
```

#### 技术要点

- **双协议接入**：gRPC (4317) + HTTP (4318)，端口可通过 `--otlp-grpc-port` / `--otlp-http-port` 配置；监听地址固定 `0.0.0.0`
- **JSON OTLP 支持**：HTTP `/v1/traces` 与 `/v1/logs` 检测 `Content-Type: application/json` 走 `protojson.Unmarshal`，否则 `proto.Unmarshal`；HTTP `/v1/metrics` 仅接受 protobuf
- **Token 派生**：`translateSpan` 中先 `normalizeAttributes` 归一化属性键，再调 `storage.DeriveTokenBuckets(attrs)` 派生不相交 token 桶（input/output/cacheCreation/cacheRead），**忽略自报的 total**，`total = input + output + cacheCreation + cacheRead`
- **Trace 异步写入**：翻译后通过 Pipeline 缓冲，保护后端不被突发流量压垮；缓冲满返回 OTLP `PartialSuccess`（gRPC）或 503（HTTP）
- **Metric/Log 直写**：不走 Pipeline，直接写入对应 Store
- **翻译器**：`metrics_translator.go` 支持 Gauge / Sum（当作 Gauge，忽略单调性与时间性）/ Histogram（展开为 `_bucket`/`_sum`/`_count`）/ Summary（展开为 `_sum`/`_count`/`quantile`）；ExponentialHistogram 静默跳过
- **logs_translator**：`TimeUnixNano` fallback `ObservedTimeUnixNano`（ns→ms），`SeverityNumber` 优先于 `SeverityText`，`event.name` 属性提取为 `EventName`

---

### 3. 异步管道 — `internal/pipeline/`

#### 架构图

```
  Receiver.Ingest(batch)
         │
         ▼
  ┌───────────────────┐
  │  Buffered Channel  │  容量: --buffer-size (默认 1000)
  │  ┌──┐┌──┐┌──┐┌──┐ │
  │  │B1││B2││B3││..│ │
  │  └──┘└──┘└──┘└──┘ │
  └─────────┬─────────┘
            │
            ▼
  ┌───────────────────┐
  │  Worker Goroutine  │
  │                    │
  │  ticker (--flush-   │
  │   interval 默认200ms)│
  │      │             │
  │      ▼             │
  │  flush 累积的      │
  │  batch 到          │
  │  store.InsertSpans │
  └───────────────────┘
            │
            ▼
         Store
```

#### 技术要点

- **仅用于 Trace**：`Batch` 仅含 `Spans []storage.Span`；Metrics 和 Logs 直接写入，不需要缓冲
- **批量刷写**：Worker 每 `--flush-interval`（默认 200ms）将累积的所有 batch 一次性写入；channel 关闭时也触发 flush
- **可配置容量与间隔**：`--buffer-size` / `--flush-interval`
- **背压保护**：非阻塞 `select` + `default`，缓冲满时 `Ingest` 返回 `ErrBufferFull`，Receiver 返回 503/PartialSuccess；`closed` flag 防止 shutdown 后新 ingest
- **单 Worker**：仅一个 goroutine 调用 `store.InsertSpans`

---

### 4. 核心存储层 — `internal/storage/`

#### 架构图

```
           Pipeline / Handler
                 │
                 ▼
        ┌────────────────┐
        │  Store 接口     │  28 方法, 8 数据域
        │                │
        │  Trace 域      │  InsertSpans / ListTraces / GetTrace / GetServices
        │  Session 域    │  ListSessions / GetSession (带分页)
        │  Retention 域  │  Purge / PurgeLogs / DeleteTraces
        │  Log 域        │  InsertLogs / ListLogs (含 span_id 过滤) / GetLogsByTrace
        │                │    / GetLogCountsByTrace / GetLogEventNames
        │  Pricing 域    │  GetModelPricing / UpsertModelPricing / DeleteModelPricing
        │  LLMConfig 域  │  GetLLMConfigs / CreateLLMConfig / UpdateLLMConfig / DeleteLLMConfig
        │  Cost 域       │  UpdateTraceCost / GetCostSummary (group_by model|service)
        │  Diagnosis 域  │  GetDiagnosisResult / UpsertDiagnosisResult
        │  AgentStats 域 │  GetSessionAgentStats / GetSessionContextSpans
        │  Lifecycle     │  Close
        └──────┬─────────┘
               │
    ┌──────────┼──────────┬──────────┐
    │          │          │          │
    ▼          ▼          ▼          ▼
┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ chDB   │ │SQLite  │ │memStore│ │ config │
│ Store  │ │ Store  │ │ Store  │ │ 加载器 │
│ (CGO)  │ │(纯Go)  │ │(纯Go)  │ │        │
│        │ │        │ │        │ │        │
│构建标签 │ │构建标签 │ │构建标签 │ │        │
│cgo &&  │ │!local_ │ │!local_ │ │        │
│local_  │ │engine &&│ │engine &&│ │        │
│engine  │ │!nosqlite│ │nosqlite│ │        │
│        │ │ (默认) │ │(fallback)│        │
└───┬────┘ └───┬────┘ └───┬────┘ └──┬─────┘
    │          │          │         │
    ▼          ▼          ▼         ▼
 ClickHouse   SQLite    内存切片  labubu.yaml
 嵌入式DB    WAL模式   + maps    + pricing
 列式存储    持久化    +JSON持久   (配置+默认
            参数化    Trace/Log    定价模型)
            查询      重启丢失
```

#### 构建标签选择逻辑

| 构建方式 | 编译的 Store | 数据持久化 | 适用场景 |
|----------|-------------|-----------|----------|
| `CGO_ENABLED=1 -tags local_engine` | chDB Store | ✅ ClickHouse 文件 | Linux/Mac 生产 |
| `CGO_ENABLED=0`（默认非 CGO） | SQLite Store | ✅ `data/labubu.db` | **Windows / 跨平台默认** |
| `CGO_ENABLED=0 -tags nosqlite` | memStore | ❌ 进程重启丢失 | 最简开发调试 |

#### Schema 与迁移

- **SQLite schema** (`sqlite_schema.sql`)：`spans` 表直接含 `cache_creation_tokens` / `cache_read_tokens` / `gen_ai_request_model` / `cost` / `cost_currency` 列；`traces` 表含 `cost` / `cost_currency`；`model_pricing` 含 `context_window`
- **chDB schema** (`schema.sql`)：cache token 与 trace cost 列通过 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 增量添加；`spans` 表无 cost 列（chDB 仅写 trace 级 cost）
- **运行时迁移**（SQLite `NewChDBStore` 内）：为旧库 `ALTER` 补 `provider_type` / cache token / span cost / `context_window` 列；`backfillCacheTokens` 为旧 span 重算 cache 列与 trace 总量；`backfillSpanCost` 为旧 span 填充 cost（幂等，跳过非 LLM span）

#### 技术要点

- **三实现策略**：CGO 构建用 chDB（生产级列式存储），默认非 CGO 构建用 SQLite（持久化+SQL查询），memStore 保留为最简 fallback。三个实现共享同一构造函数名 `NewChDBStore(dataDir) (Store, error)`，由构建标签选择编译哪个
- **统一接口**：所有 Handler 仅依赖 `Store` 接口
- **Token 派生**：`DeriveTokenBuckets`（`tokens.go`）从属性 map 提取不相交 token 桶，是文档化的唯一扩展点；在 receiver 翻译 span 时调用，确保 token 统一口径
- **Span cost 持久化**：SQLite 在 `InsertSpans` 时写入 per-span `cost`/`cost_currency`，`UpdateTraceCost` 重算 trace 级 `total_tokens` 与 cost；memStore 内联计算；chDB 仅写 trace 级 cost
- **成本汇总 group_by**：`GetCostSummary(q CostQuery)` 支持 `GroupBy: "model"|"service"`，service 模式 `JOIN spans JOIN traces` 按 `resource_attributes.service.name` 分组；含 cache token breakdown（input/output/cacheCreation/cacheRead）
- **Trace 过滤增强**：`TraceQuery` 新增 `MinSpanCount`/`MaxSpanCount`/`MinCost`/`MaxCost`（及 `MinDuration`/`MaxDuration`），三实现均支持
- **Log 过滤增强**：`LogQuery.SpanID [8]byte` 零值不过滤；`GetLogCountsByTrace` 返回 `map[spanIDHex]count`
- **Trace 删除**：`DeleteTraces(traceIDs [][16]byte)` 事务级联删除 `diagnosis_results` → `logs` → `spans` → `traces`，返回删除的 trace 数与 log 数
- **会话分组**：Trace 含 `jiuwenclaw.session.id` 属性时自动归入 Session；`GetSession` 对 session 内 traces 分页；`TraceListItem.InputMessages` 携带 root span 的 `gen_ai.input.messages`
- **Agent 统计与上下文**：`GetSessionAgentStats` 计算 trace 成功率/span 数/工具调用与成功率/循环深度/`ToolUsage` 列表 + 最多 4 条洞察；`GetSessionContextSpans` 筛选 session 主 agent 的 LLM span（沿父链回溯到 trace root，排除 subagent）
- **数据清理**：`Purge` 定期删除过期 Trace（默认 7 天 / 10000 条），`PurgeLogs` 清理过期 Log，配置驱动。SQLite 的 Purge 用标准 `DELETE FROM` 即时生效，比 chDB 的异步 `ALTER TABLE DELETE` 更精确
- **默认定价模型**：`claude-opus-4-8`（15/75）、`claude-sonnet-4-6`（3/15）、`claude-haiku-4-5`（0.8/4）（per 1M token）

> 注：截至本次扫描，chDB 实现尚未补齐 `GetCostSummary` 方法（仅 SQLite 与 memStore 实现），`cgo && local_engine` 构建当前无法通过编译。SQLite 为跨平台默认实现。

---

### 5. 指标存储 — `internal/metrics/`

#### 架构图

```
  Receiver (Metric翻译器)
         │
         ▼
  ┌──────────────────┐
  │  metrics.Store   │  接口: Insert / Select / LabelNames / LabelValues / Close
  │  接口 (5 方法)    │
  └──────┬───────────┘
         │
         ▼
  ┌──────────────────┐
  │  TStorageStore   │
  │                  │
  │  tstorage (嵌入式) │
  │  TSDB            │
  │                  │
  │  ┌──────────────┐│
  │  │ labelIdx     ││  label名称 → 值集合 (含合成 __name__)
  │  │ metricIdx    ││  metric名称 → 已知label组合
  │  └──────────────┘│
  │                  │
  │  配置:           │
  │  - 内存/磁盘持久 ││
  │  - 保留期 (默认7d)││
  │  - 数据目录      ││
  └──────────────────┘
```

#### 技术要点

- **基于 tstorage**：嵌入式 TSDB，无需外部依赖，时间戳精度毫秒
- **Prometheus 兼容**：`/api/v1/query`、`/query_range`、`/labels` 返回标准 Prometheus JSON
- **二级索引**：`labelIdx`（含合成 `__name__` label）和 `metricIdx` 支持高效标签查询；空 labels 的 `Select` 遍历 `metricIdx` 合并
- **保留期配置**：默认 7 天，支持磁盘持久化（`--metrics-data-dir` 非空时）

---

### 6. HTTP API 层 — `internal/api/`

#### 架构图

```
          Browser / API Client
                 │
                 │  :8080 (可配置)
                 ▼
        ┌────────────────┐
        │   Router        │  http.ServeMux + strings.TrimPrefix 手动子路由
        │                │
        │  /api/v1/*     │──► 各 Handler
        │  /             │──► SPA 静态文件 / Fallback
        │  /api/health   │──► 健康检查
        └──────┬─────────┘
               │
    ┌──────────┼──────────────────────────┐
    │          │                           │
    ▼          ▼                           ▼
┌────────┐ ┌────────┐ ┌─────────────────── ┐ ┌───────┐ ┌──────┐
│Trace   │ │Metrics │ │Session│Log│Pricing │ │LLM   │ │Cost  │ │OpenAPI│
│Handler │ │Handler │ │Handler│  │Handler │ │Config│ │Handler│ │Handler│
└───┬────┘ └──┬────┘ └──┬────┘  └──┬─────┘ └──┬───┘ └──┬───┘ └──┬───┘
    │         │         │          │          │       │       │
    ▼         ▼         ▼          ▼          ▼       ▼       ▼
  Store    MStore     Store     Store      Store   Store   (spec)
    │                                  │
    ├──► DiagnoseTrace                 │
    │    (LLM诊断, openai/anthropic)   │
    ├──► ExportTraces / ImportTraces   │
    │    / DeleteTraces (OTLP JSON)    │
    └──► Services                      │
```

#### 路由表

| 路径 | 方法 | Handler | 功能 |
|------|------|---------|------|
| `/api/v1/traces` | GET | TraceHandler | 分页+多维过滤（service/status/q/start/end/min/max duration/spans/cost） |
| `/api/v1/traces/{id}` | GET | TraceHandler | Trace 详情（`?format=otlp` 返回 OTLP JSON） |
| `/api/v1/traces/{id}/diagnosis` | GET | TraceHandler | 获取诊断结果（含 staleness 检测） |
| `/api/v1/traces/{id}/diagnose` | POST | TraceHandler | 触发 LLM 诊断（`?force=true&locale=zh`） |
| `/api/v1/traces/export` | POST | TraceHandler | 批量导出 OTLP JSON（≤100） |
| `/api/v1/traces/import` | POST | TraceHandler | 导入 OTLP JSON（TracesData 或 ExportTraceServiceRequest，10MB 上限，已存 trace 整体跳过） |
| `/api/v1/traces/delete` | POST | TraceHandler | 批量删除 trace+关联 logs+diagnosis（≤100） |
| `/api/v1/services` | GET | TraceHandler | 服务名称列表 |
| `/api/v1/query` | GET | MetricsHandler | Prometheus 即时查询 |
| `/api/v1/query_range` | GET | MetricsHandler | Prometheus 范围查询 |
| `/api/v1/labels` | GET | MetricsHandler | 标签名称列表 |
| `/api/v1/label/{name}/values` | GET | MetricsHandler | 标签值列表 |
| `/api/v1/metadata` | GET | MetricsHandler | metric 元数据 |
| `/api/v1/metric-names` | GET | MetricsHandler | 指标名称列表 |
| `/api/v1/otlp/v1/metrics` | POST | MetricsHandler | OTLP metrics HTTP 摄取 |
| `/api/v1/dashboards` | GET/POST | DashboardHandler | 看板 list / create |
| `/api/v1/dashboards/{id}` | PUT/DELETE | DashboardHandler | 看板重命名 / 删除 |
| `/api/v1/dashboards/{id}/panels` | GET/POST | DashboardHandler | 面板 list / create |
| `/api/v1/dashboards/{id}/panels/{panelId}` | PUT/DELETE | DashboardHandler | 面板更新 / 删除 |
| `/api/v1/sessions` | GET | SessionHandler | Session 列表 |
| `/api/v1/sessions/{id}` | GET | SessionHandler | Session 详情（分页） |
| `/api/v1/sessions/{id}/agent-stats` | GET | SessionHandler | Agent 行为统计 |
| `/api/v1/sessions/{id}/context` | GET | SessionHandler | 主 agent LLM span（token 分解，排除 subagent） |
| `/api/v1/logs` | GET | LogHandler | Log 列表（含 `span_id` 过滤） |
| `/api/v1/logs/{traceId}` | GET | LogHandler | Trace 关联日志 |
| `/api/v1/logs/{traceId}/counts` | GET | LogHandler | 每 span 的 log 计数 |
| `/api/v1/log-event-names` | GET | LogHandler | distinct event 名 |
| `/api/v1/model-pricing` | GET/POST | PricingHandler | 定价 list / upsert |
| `/api/v1/model-pricing/recalc` | POST | PricingHandler | 重算全部成本 |
| `/api/v1/model-pricing/{name}` | DELETE | PricingHandler | 删除定价 |
| `/api/v1/llm-configs` | GET/POST | LLMConfigHandler | LLM 配置 list / create（校验 provider_type） |
| `/api/v1/llm-configs/{id}` | PUT/DELETE | LLMConfigHandler | LLM 配置更新 / 删除 |
| `/api/v1/alerts/*` | * | AlertHandler | 告警规则 / 状态 / 通知（见 §7） |
| `/api/v1/cost-summary` | GET | CostHandler | 成本汇总（`?group_by=model\|service&period=today\|7d\|30d` 或 `start/end`） |
| `/api/v1/openapi.json` | GET | OpenAPIHandler | OpenAPI 3.0.3 spec |
| `/api/health` | GET | inline | `{"status":"ok"}` |
| `/` | * | serveSPA | 嵌入/磁盘 SPA，未构建时返回 devFallbackHTML |

#### 技术要点

- **路由**：`http.ServeMux` + `strings.TrimPrefix` 手动子路由；metrics/dashboard/session/log/pricing/llm-config/alert/cost 均 nil-safe（条件注册）
- **SPA 嵌入**：生产构建将 `web/dist` 嵌入二进制，`serveSPA` 处理所有非 API 路径
- **Trace 导入/导出/删除**：均走 OTLP JSON；导入用 `protojson.Unmarshal` + hex→base64 预处理，支持 `TracesData` 与 `ExportTraceServiceRequest` 两种形态；删除级联清理 logs/diagnosis
- **LLM 诊断**：`DiagnoseTrace` → 构建 prompt → 按 provider_type 调用适配器 → 存储 DiagnosisResult
  - **多提供商适配器**：`provider_type` 字段（`openai` / `anthropic`）决定调用哪个适配器，空串默认 openai
  - **OpenAI 适配器**：兼容 OpenAI、DeepSeek、Qwen、ZhipuAI/GLM 等，`buildOpenAIURL` 智能补 `/chat/completions`，`extractContent` 处理多态 content（string 或数组），`Authorization: Bearer` 鉴权
  - **Anthropic 适配器**：Claude Messages API，`system` 顶级字段，**同时设置 `x-api-key` 与 `Authorization: Bearer`**（兼容 DashScope 等 Anthropic-compatible 提供方），`anthropic-version: 2023-06-01`
  - **去重**：`inFlight` map 防止同一 Trace 重复诊断，重复请求返回 409
  - **过期检测**：`computeSpanSnapshot`（spanCount + 每个 span 的 SpanID:Status:DurationMS 指纹）比对 `result.SpansSnapshot` 标记 `Stale`
  - **detached context**：LLM 调用用 60s timeout 的 detached context，客户端断开也完成调用
  - 多语言 prompt：中/英双语诊断提示
- **OpenAPI 3.0 spec**：`/api/v1/openapi.json` 提供机器可读 spec，`openapi_test.go` drift 测试保持 spec 与路由同步（无服务端 Swagger UI，UI 由前端 `ApiDocs.vue` 内嵌 swagger-ui 客户端渲染）
- **API Key 安全**：`storage.MaskAPIKey`（≤8 位返回 `***`，否则前 3 + `***` + 后 2）；list 时统一 mask，`***` 哨兵"保留原值"语义在 `store.UpdateLLMConfig` 内部处理

---

### 7. 告警引擎 — `internal/alerting/`

> 独立包，不在 `internal/api/` 下。文件：`api.go`（HTTP handler）、`engine.go`（轮询引擎）、`notifier.go`（Email SMTP）、`setup.go`（Subsystem 初始化）、`store.go`（JSON RuleStore）、`types.go`。

#### 架构图

```
        ┌──────────────────────────────┐
        │      Alerting Engine         │
        │                              │
        │  ┌────────────────────────┐  │
        │  │  Polling Goroutine     │  │  硬编码 15s ticker
        │  │                        │  │
        │  │  Evaluate():           │  │
        │  │  1. ListTraces 取近期  │  │◄── TraceFetcher (store.ListTraces)
        │  │  2. 检查每个 Rule      │  │     对每个 trace GetTrace 提取模型
        │  │  3. 条件匹配判断       │  │
        │  │  4. 驱动状态机         │  │
        │  └────────────────────────┘  │
        │                              │
        │  状态机:                     │
        │  OK ──► Pending ──► Firing ──► Resolved
        │  (条件开始)  (持续for_duration)  (条件消失)│
        │                              │
        │  ┌────────────────────────┐  │
        │  │  RuleStore             │  │  JSON 文件: alerting.json
        │  │  (规则+状态+通知历史)  │  │  (内存 map + 全量重写)
        │  └────────────────────────┘  │
        │                              │
        │  ┌────────────────────────┐  │
        │  │  Notifier Registry     │  │
        │  │  Notifier 接口:        │  │  Type() / Fire() / Resolve()
        │  │  ├── EmailNotifier     │  │◄── SMTP 发送
        │  │  └ (可扩展更多)        │  │
        │  └────────────────────────┘  │
        └──────────────────────────────┘
```

#### 路由表

| 路径 | 方法 | 功能 |
|------|------|------|
| `/api/v1/alerts/rules` | GET/POST | 规则 list / create |
| `/api/v1/alerts/rules/{id}` | GET/PUT/DELETE | 规则 get / update / delete |
| `/api/v1/alerts/states` | GET | firing/resolved 状态（`?status=` 过滤） |
| `/api/v1/alerts/notifications` | GET | 通知历史（`?rule_id=` 过滤） |

#### 技术要点

- **独立包**：`InitAlerting(dbPath, traceStore)` 返回 `*Subsystem`（Store/Engine/Handler），失败时 main 不阻塞服务
- **条件组合**：多个 `Condition`（field + operator + value）AND 组合；operator 支持 gt/gte/lt/lte/eq/neq；field 支持 `total_tokens`/`input_tokens`/`output_tokens`/`model`
- **防抖机制**：`for_duration`（默认 300s）持续满足条件后才从 Pending 转 Firing
- **评估频率**：Polling ticker 硬编码 15s，但每条 Rule 有自己的 `Interval`（默认 60s）控制实际评估频率
- **状态机**：4 状态 OK / Pending / Firing / Resolved，`driveStateMachine` 驱动转换，仅 Firing/Resolved 转换触发通知
- **Notifier 接口**：`NotifierFactory` 按 type 注册，当前仅支持 `email`（SMTP），无 webhook/slack/PagerDuty
- **文件持久化**：`RuleStore` 是内存 map + JSON 文件（`alerting.json`），每次变更全量重写（非 Store 接口，注释提到可后续换 SQLite）

---

### 8. 成本计算 — `internal/pricing/`

#### 架构图

```
  Store.InsertSpans()
         │
         ▼ (SQLite: 写 per-span cost + 异步重算 trace total)
  ┌────────────────────────┐
  │   Pricing 模块          │
  │                        │
  │  CalculateSpanCost()   │  cost = (input×inPrice
  │  CalculateTraceCost()  │        + cacheCreate×inPrice×1.25
  │                        │        + cacheRead×inPrice×0.1
  │                        │        + output×outPrice) / 1M
  │  价格来源:              │◄── Store.GetModelPricing()
  │  ModelPricing 表        │    (InputPrice/OutputPrice/Currency/
  │                        │     ContextWindow)
  └────────────────────────┘
         │
         ▼
  Store.UpdateTraceCost()
```

#### 技术要点

- **纯函数计算**：`CalculateSpanCost(span, pricings) *float64` 与 `CalculateTraceCost(spans, pricings) (*float64, currency string, unpriced int)` 无副作用
- **Cache 差异化费率**（硬编码常量）：cache 创建 = 1.25× input price，cache 读取 = 0.1× input price；cache 费率非 `ModelPricing` 字段，从 `InputPrice` 派生
- **Per-1M-token 定价**：`ModelPricing.InputPrice`/`OutputPrice` 单位为 per 1M token，结果四舍五入到 6 位小数
- **unpriced 统计**：`CalculateTraceCost` 返回有 token 但无定价的 span 数，便于前端提示未计价 trace
- **异步触发**：Store 在 `InsertSpans` 后异步调用成本计算与 `UpdateTraceCost`
- **重算**：`POST /api/v1/model-pricing/recalc` 遍历全量 traces 调 `UpdateTraceCost`

---

### 9. Vue 前端 — `web/src/`

#### 架构图

```
  ┌─────────────────────────────────────────────────────────┐
  │                    Vue 3 SPA                            │
  │                                                         │
  │  main.ts ──► App.vue                                    │
  │              │                                           │
  │              ├── Sidebar 导航                            │
  │              │   ├── Traces                              │
  │              │   ├── Sessions                            │
  │              │   ├── Logs                                │
  │              │   ├── Metrics Group (默认展开)            │
  │              │   │   ├── Dashboard                       │
  │              │   │   └── Cost                            │
  │              │   ├── Alerts Group (默认收起)             │
  │              │   │   ├── Alert Rules                     │
  │              │   │   └── Alert History                   │
  │              │   ├── Settings Group (默认收起)           │
  │              │   │   ├── Model Pricing                   │
  │              │   │   └── LLM Configs                     │
  │              │   └── Footer: API Docs + ThemeToggle     │
  │              │                  + LanguageToggle         │
  │              └── <router-view> ──► 各 View               │
  │                                                         │
  │  ┌─── 核心层 ──────────────────────────────────────┐    │
  │  │ api/client.ts  │ 统一 HTTP 客户端 (721行), 类型    │    │
  │  │ router.ts      │ 路由定义 (15 条)                 │    │
  │  │ i18n/          │ vue-i18n 中/英双语 (16 section) │    │
  │  │ composables/   │ useTheme / usePageSize          │    │
  │  │ styles/theme.css│ CSS 变量主题系统 (30+ 变量)     │    │
  │  └──────────────────────────────────────────────────┘    │
  │                                                         │
  │  ┌─── 视图层 (13 个) ─────────────────────────────┐    │
  │  │ views/                                         │    │
  │  │ ├── TraceList.vue     Trace 列表+多维过滤+多选 │    │
  │  │ │                      下载/删除/导入 JSON      │    │
  │  │ ├── TraceDetail.vue   Trace 详情+OTLP下载+overlay│    │
  │  │ │                      (logs/diagnosis/agent/   │    │
  │  │ │                       context)                │    │
  │  │ ├── SessionList.vue   Session 列表              │    │
  │  │ ├── SessionDetail.vue Session 详情+AgentStats  │    │
  │  │ │                      +ContextBarChart         │    │
  │  │ ├── Dashboard.vue     自定义看板+面板管理       │    │
  │  │ ├── CostDashboard.vue 成本看板(model/service)  │    │
  │  │ ├── LogList.vue       Log 列表                  │    │
  │  │ ├── PricingManager.vue 模型定价管理+重算        │    │
  │  │ ├── LlmConfig.vue     LLM 配置管理              │    │
  │  │ ├── ApiDocs.vue       内嵌 Swagger UI           │    │
  │  │ └── alerts/                                    │    │
  │  │     ├── RuleList.vue    告警规则列表            │    │
  │  │     ├── RuleForm.vue    告警规则表单            │    │
  │  │     └── AlertHistory.vue 告警历史               │    │
  │  └──────────────────────────────────────────────────┘    │
  │                                                         │
  │  ┌─── 组件层 (12 个) ─────────────────────────────┐    │
  │  │ components/                                    │    │
  │  │ ├── WaterfallChart.vue   Trace 瀑布图           │    │
  │  │ ├── SpanDetail.vue       Span 详情(含cache token)│   │
  │  │ ├── DiagnosisTab.vue     LLM 诊断面板           │    │
  │  │ ├── AgentBehaviorTab.vue Agent 行为(LLM/Tool)   │    │
  │  │ ├── AgentStatsSection.vue Agent 统计概览        │    │
  │  │ ├── ContextBarChart.vue  上下文 token 条形图    │    │
  │  │ ├── TokenPieChart.vue    Token 占比饼图         │    │
  │  │ ├── TimeRangePicker.vue  时间范围选择器         │    │
  │  │ ├── PanelForm.vue        看板面板编辑           │    │
  │  │ ├── PanelChart.vue       看板面板图表           │    │
  │  │ ├── ThemeToggle.vue      主题切换按钮           │    │
  │  │ └── LanguageToggle.vue   语言切换按钮           │    │
  │  └──────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────┘
```

#### 技术要点

- **Composition API**：`ref` / `reactive` / `computed`，无全局状态管理库
- **统一 API 客户端**：`client.ts`（721 行）封装所有后端 API，类型与 Go 结构体镜像对应；覆盖 traces（含 import/delete/export）、sessions（含 agent-stats/context）、metrics、dashboards、logs（含 counts）、pricing、cost（group_by）、llm-configs、diagnosis
- **Trace 详情 overlay 面板**：`TraceDetail.vue` 用 overlay 切换 logs / diagnosis / agent behavior / context 四个洞察面板，替代内联展开
- **多选与导入**：`TraceList.vue` 支持多选下载（OTLP）、多选删除、导入 JSON 文件
- **分页大小持久化**：`usePageSize` composable 按 scope（traces/sessions/logs）独立记住分页大小
- **CSS 变量主题**：抽离到独立 `styles/theme.css`（约 30+ 变量，`[data-theme="light"]` 覆盖），`useTheme` composable 管理模块级单例 ref + `<html data-theme>` 属性 + 全局 0.25s transition
- **i18n 双语**：16 个 section（common/dashboard/panelForm/nav/traceList/sessionList/traceDetail/sessionDetail/logList/alerts/timeRange/costDashboard/diagnosis/agentStats/pricingManager/llmConfig），所有 UI 文本通过 `t('section.key')` 引用
- **API 文档页**：`ApiDocs.vue` 内嵌 swagger-ui，fetch `/api/v1/openapi.json` 客户端渲染交互式文档
- **SPA 嵌入**：生产构建通过 `go:embed` 嵌入 Go 二进制，开发模式从磁盘读取

---

### 10. Python 分发包 — `labubu-python/`

#### 架构图

```
  pip install labubu
         │
         ▼
  ┌──────────────────────┐
  │  labubu-python/       │
  │                      │
  │  labubu/__init__.py   │  版本: 0.1.0
  │  labubu/__main__.py   │  python -m labubu 入口
  │  labubu/cli.py        │  薄壳: 定位 bin/labubu(.exe)
  │                      │     └ subprocess.run 透传全部 argv
  │                      │     └ sys.exit 透传退出码
  │  labubu/bin/          │◄── Makefile wheel: Go 二进制拷入
  │    labubu(.exe)        │
  │                      │
  │  pyproject.toml       │  setuptools, MIT license,
  │                      │  requires-python >=3.9,
  │  tests/test_cli.py    │  entry_point: labubu.cli:main
  └──────────────────────┘
```

#### 技术要点

- **薄壳模式**：Python 包仅做二进制定位 + 参数传递，无业务逻辑，无运行时依赖
- **平台 wheel**：非 purelib，包含编译好的 Go 二进制（`labubu/bin/*`）
- **一键安装**：`pip install labubu` → `labubu serve` 即可运行
- **元数据**：name=labubu, version=0.1.0, MIT license, author=Wendymayu, requires-python>=3.9
- **测试**：unittest + mock，覆盖二进制路径解析（Unix/Windows）、argv 透传、退出码透传、缺失提示

---

### 11. MCP Server — `labubu-mcp/`

#### 架构图

```
  AI Agent (Claude, GPT, etc.)
         │
         │  MCP Protocol (stdio transport)
         ▼
  ┌──────────────────────┐
  │  labubu-mcp/          │
  │                      │
  │  server.py            │  FastMCP (官方 mcp SDK)
  │                      │  --api-url 默认 localhost:8080
  │                      │
  │  api_client.py        │  异步 httpx 客户端 ──► Labubu API
  │                      │  (无认证, 本机/可信网络)
  │                      │
  │  formatters.py        │  5 个输出格式化器
  │                      │  (trace 列表/详情树/log/metric/service)
  │                      │
  │  tools/               │  5 个 MCP Tool
  │  ├── traces.py        │  search_traces / get_trace_detail
  │  ├── logs.py          │  search_logs
  │  ├── metrics.py       │  query_metrics
  │  └── services.py      │  list_services
  └──────────────────────┘
```

#### 技术要点

- **MCP 协议**：Model Context Protocol，让 AI Agent 通过标准化工具调用访问 Labubu 数据；用官方 `mcp` SDK 的 FastMCP 高层 API，stdio transport
- **5 个工具**：`search_traces`（status/service/query/时间/duration/limit/offset）、`get_trace_detail`（include_events/include_attributes）、`search_logs`（severity/event_name/query/时间）、`query_metrics`（query/time）、`list_services`
- **HTTP 客户端**：`api_client.py` 异步 httpx，参数名映射（query→q、start_time→start、min_duration_ms→min_duration、limit→page_size+page），错误返回 `{"error":...}` 字典而非抛异常
- **输出格式化**：`formatters.py` 将 API 响应转为 Agent 可读文本（CSV 表格、YAML 风格 trace 树、span 嵌套树、metric `{labels} => value`）
- **依赖**：`mcp>=1.0.0`、`httpx>=0.27`，requires-python>=3.8

---

## 关键设计模式总结

| 模式 | 说明 | 应用位置 |
|------|------|----------|
| **接口抽象** | 统一 `Store` 接口（28 方法），Handler 不直接访问底层存储 | `internal/storage/storage.go` |
| **三实现切换** | 构建标签选择 chDB (CGO生产) / SQLite (非CGO默认) / memStore (fallback)，同一构造函数名 | `chdb.go` / `sqlite_store.go` / `memstore.go` |
| **Token 派生** | `DeriveTokenBuckets` 在 receiver 翻译时计算不相交 token 桶，忽略自报 total，统一全平台口径 | `internal/storage/tokens.go`（receiver 调用） |
| **属性键归一化** | `normalizeAttributes` 将多 Agent 别名键映射到规范 GenAI 键，保证多 Agent 兼容 | `internal/receiver/otlp.go` |
| **异步缓冲** | Pipeline 为 Trace 提供背压保护，Metric/Log 直写 | `internal/pipeline/pipeline.go` |
| **单二进制部署** | `go:embed` 嵌入前端，Go + Vue = 一个可执行文件 | `web/embed.go` |
| **多提供商适配器** | `callLLMForDiagnosis` dispatcher 按 `provider_type` 路由到 OpenAI 或 Anthropic 适配器 | `diagnosis_llm.go` |
| **多态 Content 解析** | `extractContent` 处理 OpenAI 格式（string）和 ZhipuAI 格式（array）的 `content` 字段 | `diagnosis_llm.go` |
| **过期检测** | `computeSpanSnapshot` 检测数据变化，标记诊断过期 | `trace_handler.go` |
| **Cache 差异化费率** | cache 创建 1.25× / 读取 0.1× input price，硬编码常量从 InputPrice 派生 | `internal/pricing/pricing.go` |
| **Prometheus 兼容** | Metrics API 返回标准 Prometheus JSON，支持 Grafana 集成 | `metrics_handler.go` |
| **Notifier 插件** | `Notifier` 接口 + `NotifierFactory` 注册，告警通知可扩展 | `alerting/notifier.go` |
| **OpenAPI drift 测试** | spec 与路由同步的自动化测试，防文档漂移 | `internal/api/openapi_test.go` |
| **薄壳分发** | Python 包仅定位+调用 Go 二进制，无业务逻辑 | `labubu-python/labubu/cli.py` |
| **配置驱动** | YAML 配置 trace/log/metric retention、定价、清理间隔等，种子数据不覆盖已有 | `internal/storage/config.go` |
