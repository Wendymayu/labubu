# Labubu 项目架构文档

> 最后更新：2026-06-12

## 整体架构概览

Labubu 是一个本地优先的 LLM 可观测平台，接收 OTLP 遥测数据（Traces、Metrics、Logs），计算 token 成本，提供 AI 驱动的 Trace 诊断，并通过 Vue SPA 提供 Web 看板。

### 全局架构图

```
                          OTLP Sender (SDK / AI Agent)
                                 │
                    ┌────────────┼────────────┐
                    │   gRPC :4317  │  HTTP :4318 │
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
           │      ├──► 成本计算  │      │              │
           │      │  (pricing)  │      │              │
           │      │              │      │              │
           └──────┼──────────────┼──────┼──────────────┘
                  │              │      │
                  ▼              ▼      ▼
           ┌─────────────────────────────────────┐
           │           HTTP API Server            │  internal/api/ + internal/alerting/
           │   :8080                              │
           │   ┌──────┐ ┌──────┐ ┌──────┐        │
           │   │Trace │ │Metric│ │Session│  ...   │
           │   │Handler│ │Handler│ │Handler│       │
           │   └──┬───┘ └──┬───┘ └──┬───┘        │
           │      │        │       │              │
           │      ├──► LLM 诊断 (Diagnosis)       │
           │      ├──► 告警引擎 (Alerting)         │
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
| **数据存储** | chDB / SQLite / 内存存储 | CGO 构建用 chDB，默认非 CGO 构建用 SQLite，memStore 为 fallback，统一 `Store` 接口 |
| **指标存储** | tstorage | 嵌入式 TSDB，Prometheus 兼容查询 |
| **日志存储** | chDB / SQLite / 内存存储 | 与 Trace 共用 Store 接口 |
| **SQLite 驱动** | modernc.org/sqlite | 纯 Go 实现，无需 CGO，WAL 模式并发读 |
| **前端框架** | Vue 3 + TypeScript | Composition API，无 Vuex/Pinia |
| **前端构建** | Vite | 开发模式 `make dev`，生产嵌入 Go 二进制 |
| **前端嵌入** | go:embed | 生产构建将 `web/dist` 嵌入 Go 二进制，单文件部署 |
| **国际化** | vue-i18n | 中/英双语，`localStorage` 存储语言偏好 |
| **OTLP 协议** | gRPC + HTTP | 标准 OpenTelemetry 协议，端口 4317/4318 |
| **告警通知** | SMTP | 内置邮件通知器，`Notifier` 接口可扩展 |
| **Python 分发** | pip wheel | Go 二进制打包进 Python wheel，`pip install labubu` 即可用 |
| **MCP 集成** | Python MCP SDK | 暴露 Labubu API 为 AI Agent 工具 |

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
 │  1. 解析 CLI 参数 (-port, -config, -log-level, ...) │
 │  2. 加载 YAML 配置 (storage.LoadConfig)             │
 │  3. 检查端口可用性                                    │
 │  4. 初始化子系统 (按顺序):                           │
 │     Store → Retention → MetricStore → Pipeline       │
 │     → Receiver → Alerting → Handlers → Router        │
 │  5. 种子默认定价 (from config)                       │
 │  6. 启动 HTTP 服务器 (:8080)                         │
 │  7. 等待 SIGINT/SIGTERM → 10s graceful shutdown     │
 └──────────────────────────────────────────────────────┘
```

#### 技术要点

- **子命令模式**：`labubu serve`、`labubu version`、`labubu help`
- **构建标签系统**：`cgo` / `local_engine` 选择 chDB vs SQLite/memStore；`nosqlite` 选择 memStore vs SQLite；`dev` 选择磁盘读取前端 vs 嵌入
- **版本注入**：`-ldflags "-X main.Version=..."` 由 Makefile 通过 `git describe` 自动注入
- **优雅关停**：捕获 SIGINT/SIGTERM，10 秒超时，依次关闭 Pipeline、Receiver、Store

---

### 2. OTLP 接收器 — `internal/receiver/`

#### 架构图

```
              OTLP Sender
                 │
    ┌────────────┼────────────┐
    │ gRPC :4317 │ HTTP :4318 │
    └───────┬────┴─────┬──────┘
            │          │
            ▼          ▼
    ┌────────────────────────┐
    │       Receiver         │
    │                        │
    │  TraceServiceServer    │◄── gRPC TraceService
    │  MetricsServiceServer  │◄── gRPC MetricsService
    │  LogsServiceServer     │◄── gRPC LogsService
    │                        │
    │  /v1/traces  (HTTP)    │◄── HTTP POST protobuf/json
    │  /v1/metrics (HTTP)    │◄── HTTP POST protobuf/json
    │  /v1/logs    (HTTP)    │◄── HTTP POST protobuf/json
    │                        │
    │  ┌──── 翻译层 ─────────┐│
    │  │ translateResource() ││
    │  │ translateScope()    ││
    │  │ translateSpans()    ││──► pipeline.Batch
    │  │ TranslateMetrics()  ││──► []MetricPoint
    │  │ translateLogs()     ││──► []LogRecord
    │  └─────────────────────┘│
    └────────┬────┬────┬──────┘
             │    │    │
             ▼    ▼    ▼
        Pipeline  MS   Store
       (异步缓冲) (直写) (直写)
```

#### 技术要点

- **双协议接入**：gRPC (4317) + HTTP (4318)，标准 OTLP 端口
- **Trace 异步写入**：翻译后通过 Pipeline 缓冲，保护后端不被突发流量压垮
- **Metric/Log 直写**：不走 Pipeline，直接写入对应 Store
- **流量控制**：Pipeline 缓冲区满时返回 `ErrBufferFull` / HTTP 503
- **翻译器**：`metrics_translator.go` 支持 Gauge/Sum/Histogram/Summary 四种指标类型，Histogram 展开为 Prometheus 兼容的 `_bucket`/`_sum`/`_count`

---

### 3. 异步管道 — `internal/pipeline/`

#### 架构图

```
  Receiver.Ingest(batch)
         │
         ▼
  ┌───────────────────┐
  │  Buffered Channel  │  容量: 1000 (可配置)
  │  ┌──┐┌──┐┌──┐┌──┐ │
  │  │B1││B2││B3││..│ │
  │  └──┘└──┘└──┘└──┘ │
  └─────────┬─────────┘
            │
            ▼
  ┌───────────────────┐
  │  Worker Goroutine  │
  │                    │
  │  ticker (200ms)    │
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

- **仅用于 Trace**：Metrics 和 Logs 直接写入，不需要缓冲
- **批量刷写**：Worker 每 200ms 将累积的所有 batch 一次性写入
- **优雅关停**：`Shutdown()` 关闭 channel，等待 Worker 完成，刷写剩余数据
- **背压保护**：缓冲区满时 `Ingest` 返回 `ErrBufferFull`，Receiver 返回 503

---

### 4. 核心存储层 — `internal/storage/`

#### 架构图

```
           Pipeline / Handler
                 │
                 ▼
        ┌────────────────┐
        │  Store 接口     │  19 方法, 6 个数据域
        │                │
        │  Trace 域      │  InsertSpans / ListTraces / GetTrace / GetServices
        │  Session 域    │  ListSessions / GetSession
        │  Log 域        │  InsertLogs / ListLogs / GetLogsByTrace / GetLogEventNames
        │  Pricing 域    │  GetModelPricing / UpsertModelPricing / DeleteModelPricing
        │  LLMConfig 域  │  GetLLMConfigs / CreateLLMConfig / UpdateLLMConfig / DeleteLLMConfig
        │  Cost 域       │  UpdateTraceCost / GetCostSummary
        │  Diagnosis 域  │  GetDiagnosisResult / UpsertDiagnosisResult
        │  Lifecycle     │  Purge / Close
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
│cgo &&  │ │!cgo && │ │!cgo && │ │        │
│local_  │ │!nosqlite│ │nosqlite│ │        │
│engine  │ │ (默认) │ │(fallback)│ │        │
└───┬────┘ └───┬────┘ └───┬────┘ └──┬─────┘
    │          │          │         │
    ▼          ▼          ▼         ▼
 ClickHouse   SQLite    内存切片  labubu.yaml
 嵌入式DB    WAL模式   + maps    + pricing.yaml
 +SQL查询    持久化    +JSON持久 (配置+默认定价)
 列式存储    参数化    Trace/Log
             查询      重启丢失
```

#### 构建标签选择逻辑

| 构建方式 | 编译的 Store | 数据持久化 | 适用场景 |
|----------|-------------|-----------|----------|
| `CGO_ENABLED=1 -tags local_engine` | chDB Store | ✅ ClickHouse 文件 | Linux/Mac 生产 |
| `CGO_ENABLED=0`（默认非 CGO） | SQLite Store | ✅ `data/labubu.db` | **Windows / 跨平台默认** |
| `CGO_ENABLED=0 -tags nosqlite` | memStore | ❌ 进程重启丢失 | 最简开发调试 |

#### 技术要点

- **三实现策略**：CGO 构建用 chDB（生产级列式存储），默认非 CGO 构建用 SQLite（持久化+SQL查询），memStore 保留为最简 fallback
- **统一接口**：所有 Handler 仅依赖 `Store` 接口，`NewChDBStore` 函数名由构建标签决定编译哪个实现
- **SQLite Store**：WAL 模式并发读 + `sync.Mutex` 写保护，参数化查询防 SQL 注入，`go:embed` 嵌入 Schema，数据持久化到 `data/labubu.db`
- **chDB Store**：线程安全（`sync.Mutex`），Schema 从 `schema.sql` 初始化，SQL 由 `chdb_query.go` 字符串拼接生成
- **memStore**：`sync.RWMutex` 保护，LLMConfig/Pricing/Diagnosis 持久化到 `data/memstore.json`，Trace/Log 重启丢失
- **成本计算触发**：`InsertSpans` 后异步调用 `UpdateTraceCost` goroutine（chDB/SQLite Store），memStore 内联计算
- **会话分组**：Trace 含 `jiuwenclaw.session.id` 属性时自动归入 Session
- **数据清理**：`Purge` 定期删除过期 Trace（默认 24h / 10000 条），配置驱动。SQLite 的 Purge 使用标准 `DELETE FROM` 即时生效，比 chDB 的异步 `ALTER TABLE DELETE` 更精确

---

### 5. 指标存储 — `internal/metrics/`

#### 架构图

```
  Receiver (Metric翻译器)
         │
         ▼
  ┌──────────────────┐
  │  metrics.Store   │  接口: Insert / Select / LabelNames / LabelValues / Close
  │  接口             │
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
  │  │ labelIdx     ││  label名称 → 值集合
  │  │ metricIdx    ││  metric名称 → 已知label组合
  │  └──────────────┘│
  │                  │
  │  配置:           │
  │  - 内存/磁盘持久 ││
  │  - 保留期        ││
  │  - 数据目录      ││
  └──────────────────┘
```

#### 技术要点

- **基于 tstorage**：嵌入式 TSDB，无需外部依赖
- **Prometheus 兼容**：`/api/v1/query`、`/query_range`、`/labels` 返回标准 Prometheus JSON
- **二级索引**：`labelIdx` 和 `metricIdx` 支持高效标签查询
- **保留期配置**：默认 24h，支持磁盘持久化

---

### 6. HTTP API 层 — `internal/api/`

#### 架构图

```
          Browser / API Client
                 │
                 │  :8080
                 ▼
        ┌────────────────┐
        │   Router        │  http.ServeMux (Go 1.22 路径路由)
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
│Trace   │ │Metrics │ │Session│Log│Pricing │ │LLM   │ │Cost  │ │Alert │
│Handler │ │Handler │ │Handler│  │Handler │ │Config│ │Handler│ │Handler│
└───┬────┘ └──┬────┘ └──┬────┘  └──┬─────┘ └──┬───┘ └──┬───┘ └──┬───┘
    │         │         │          │          │       │       │
    ▼         ▼         ▼          ▼          ▼       ▼       ▼
  Store    MStore     Store     Store      Store   Store   AlertStore
    │                                  │
    ├──► DiagnoseTrace                 │
    │    (LLM诊断, 调用外部模型)       │
    │                                  │
    ├──► ExportTraces                  │
    │    (OTLP JSON导出)               │
    │                                  │
    └──► Services                      │
         (distinct service名称)        │
```

#### 路由表

| 路径 | 方法 | Handler | 功能 |
|------|------|---------|------|
| `/api/v1/traces` | GET | TraceHandler | 分页查询 Trace 列表 |
| `/api/v1/traces/{id}` | GET | TraceHandler | Trace 详情 (支持 `?format=otlp`) |
| `/api/v1/traces/{id}/diagnosis` | GET | TraceHandler | 获取诊断结果 |
| `/api/v1/traces/{id}/diagnose` | POST | TraceHandler | 触发 LLM 诊断 |
| `/api/v1/traces/export` | POST | TraceHandler | 批量导出 OTLP JSON |
| `/api/v1/services` | GET | TraceHandler | 服务名称列表 |
| `/api/v1/query` | GET | MetricsHandler | Prometheus 即时查询 |
| `/api/v1/query_range` | GET | MetricsHandler | Prometheus 范围查询 |
| `/api/v1/labels` | GET | MetricsHandler | 标签名称列表 |
| `/api/v1/label/{name}/values` | GET | MetricsHandler | 标签值列表 |
| `/api/v1/metric-names` | GET | MetricsHandler | 指标名称列表 |
| `/api/v1/dashboards` | GET/POST | DashboardHandler | 看板 CRUD |
| `/api/v1/dashboards/{id}` | GET/PUT/DELETE | DashboardHandler | 看板详情 |
| `/api/v1/dashboards/{id}/panels` | GET/POST | DashboardHandler | 面板 CRUD |
| `/api/v1/sessions` | GET | SessionHandler | Session 列表 |
| `/api/v1/sessions/{id}` | GET | SessionHandler | Session 详情 |
| `/api/v1/logs` | GET | LogHandler | Log 列表 |
| `/api/v1/logs/{id}` | GET | LogHandler | Trace 关联日志 |
| `/api/v1/model-pricing` | GET/POST | PricingHandler | 定价 CRUD |
| `/api/v1/model-pricing/recalc` | POST | PricingHandler | 重算全部成本 |
| `/api/v1/llm-configs` | GET/POST | LLMConfigHandler | LLM 配置 CRUD |
| `/api/v1/llm-configs/{id}` | PUT/DELETE | LLMConfigHandler | LLM 配置更新/删除 |
| `/api/v1/cost-summary` | GET | CostHandler | 成本汇总 (today/7d/30d) |
| `/api/v1/alerts/rules` | GET/POST | AlertHandler | 告警规则 CRUD |
| `/api/v1/alerts/rules/{id}` | PUT/DELETE | AlertHandler | 规则更新/删除 |
| `/api/v1/alerts/rules/{id}/state` | GET | AlertHandler | 规则当前状态 |
| `/api/v1/alerts/notifications` | GET | AlertHandler | 通知历史 |
| `/api/health` | GET | inline | 健康检查 |

#### 技术要点

- **Go 1.22 ServeMux**：路径参数 `{id}` 支持，无需第三方路由库
- **SPA 嵌入**：生产构建将 `web/dist` 嵌入二进制，`serveSPA` 处理所有非 API 路径
- **LLM 诊断**：`DiagnoseTrace` → 构建 prompt → 调用 OpenAI 兼容 API → 存储 DiagnosisResult
  - 去重：`inFlight` map 防止同一 Trace 重复诊断
  - 过期检测：`computeSpanSnapshot` 检测 Trace 数据变化，标记诊断结果过期
  - 多语言 prompt：中/英双语诊断提示
- **API Key 安全**：GET 响应中 API Key 显示为 `***`，更新请求中 `***` 表示保留原值

---

### 7. 告警引擎 — `internal/alerting/`

#### 架构图

```
        ┌──────────────────────────────┐
        │      Alerting Engine         │
        │                              │
        │  ┌────────────────────────┐  │
        │  │  Polling Goroutine     │  │  每 15s 评估一次
        │  │                        │  │
        │  │  Evaluate():           │  │
        │  │  1. 获取近期 Trace     │  │◄── TraceFetcher (使用 Store.ListTraces)
        │  │  2. 检查每个 Rule      │  │
        │  │  3. 条件匹配判断       │  │
        │  │  4. 驱动状态机         │  │
        │  └────────────────────────┘  │
        │                              │
        │  状态机:                     │
        │  OK ──► Pending ──► Firing ──► Resolved
        │  (条件开始)  (持续for_duration)  (条件消失)
        │                              │
        │  ┌────────────────────────┐  │
        │  │  RuleStore             │  │  JSON 文件: alerting.json
        │  │  (规则+状态+通知历史)  │  │
        │  └────────────────────────┘  │
        │                              │
        │  ┌────────────────────────┐  │
        │  │  Notifier Registry     │  │
        │  │                        │  │
        │  │  Notifier 接口:        │  │  Type() / Fire() / Resolve()
        │  │  ├── EmailNotifier     │  │◄── SMTP 发送
        │  │  └ (可扩展更多)        │  │
        │  └────────────────────────┘  │
        └──────────────────────────────┘
```

#### 技术要点

- **条件组合**：多个 `Condition`（field + operator + value）AND 组合
- **防抖机制**：`for_duration` 持续满足条件后才从 Pending 转 Firing
- **Notifier 接口**：`NotifierFactory` 按 type 注册，当前支持 `email`，可扩展
- **文件持久化**：规则、状态、通知历史存储在 `alerting.json`

---

### 8. 成本计算 — `internal/pricing/`

#### 架构图

```
  Store.InsertSpans()
         │
         ▼ (异步 goroutine, chDB Store)
  ┌────────────────────────┐
  │   Pricing 模块          │
  │                        │
  │  CalculateSpanCost()   │  (inputTokens × inputPrice
  │  CalculateTraceCost()  │   + outputTokens × outputPrice)
  │                        │   / 1,000,000
  │  价格来源:              │◄── Store.GetModelPricing()
  │  ModelPricing 表        │
  └────────────────────────┘
         │
         ▼
  Store.UpdateTraceCost()
```

#### 技术要点

- **纯函数计算**：`CalculateSpanCost` 和 `CalculateTraceCost` 无副作用
- **异步触发**：chDB Store 在 `InsertSpans` 后异步 goroutine 调用成本计算
- **内联计算**：memStore 在 `InsertSpans` 中直接计算成本，无需异步
- **定价配置**：`ModelPricing` 通过 API 管理，种子数据从 `pricing.yaml` 加载
- **精度**：6 位小数，单位 USD

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
  │              │   ├── Trace                               │
  │              │   ├── Sessions                            │
  │              │   ├── Metrics Group                       │
  │              │   │   ├── Dashboard                       │
  │              │   │   └── Cost                            │
  │              │   ├── Logs                                │
  │              │   ├── Alerts Group                        │
  │              │   │   ├── Rules                           │
  │              │   │   └── History                         │
  │              │   ├── Settings Group                      │
  │              │   │   ├── Model Pricing                   │
  │              │   │   └── LLM Configs                     │
  │              │   └── Footer: ThemeToggle + LanguageToggle│
  │              │                                           │
  │              └── <router-view> ──► 各 View               │
  │                                                         │
  │  ┌─── 核心层 ──────────────────────────────────────┐    │
  │  │ api/client.ts  │ 统一 HTTP 客户端, 类型定义      │    │
  │  │ router.ts      │ 路由定义                         │    │
  │  │ i18n/          │ vue-i18n 中/英双语               │    │
  │  │ composables/   │ useTheme (暗/亮主题切换)        │    │
  │  │ styles/        │ CSS 变量主题系统                  │    │
  │  └──────────────────────────────────────────────────┘    │
  │                                                         │
  │  ┌─── 视图层 ──────────────────────────────────────┐    │
  │  │ views/                                         │    │
  │  │ ├── TraceList.vue     Trace 列表 + 搜索/筛选   │    │
  │  │ ├── TraceDetail.vue   Trace 详情 + Waterfall   │    │
  │  │ ├── SessionList.vue   Session 列表              │    │
  │  │ ├── SessionDetail.vue Session 详情 + 上下文图  │    │
  │  │ ├── Dashboard.vue     自定义看板 + 面板编辑    │    │
  │  │ ├── CostDashboard.vue 成本看板                  │    │
  │  │ ├── LogList.vue       Log 列表                  │    │
  │  │ ├── PricingManager.vue 模型定价管理             │    │
  │  │ ├── LlmConfig.vue     LLM 配置管理              │    │
  │  │ ├── RuleList.vue       告警规则列表              │    │
  │  │ ├── RuleForm.vue       告警规则表单              │    │
  │  │ └── AlertHistory.vue   告警历史                  │    │
  │  └──────────────────────────────────────────────────┘    │
  │                                                         │
  │  ┌─── 组件层 ──────────────────────────────────────┐    │
  │  │ components/                                    │    │
  │  │ ├── WaterfallChart.vue  Trace 瀑布图           │    │
  │  │ ├── SpanDetail.vue      Span 详情 Drawer       │    │
  │  │ ├── DiagnosisTab.vue    LLM 诊断面板           │    │
  │  │ ├── TokenPieChart.vue   Token 分布饼图         │    │
  │  │ ├── PanelForm.vue       看板面板编辑           │    │
  │  │ ├── PanelChart.vue      看板面板图表           │    │
  │  │ ├── ThemeToggle.vue     主题切换按钮           │    │
  │  │ ├── LanguageToggle.vue  语言切换按钮           │    │
  │  └──────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────┘
```

#### 技术要点

- **Composition API**：`ref` / `reactive` / `computed`，无全局状态管理库
- **统一 API 客户端**：`client.ts` 封装所有后端 API，类型与 Go 结构体镜像对应
- **CSS 变量主题**：`--bg-primary`、`--text-secondary` 等，`useTheme` composable 管理 `data-theme` 属性
- **i18n 双语**：所有 UI 文本通过 `t('section.key')` 引用，locale 文件按功能域组织
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
  │  labubu/cli.py        │  薄壳: 定位 bin/labubu.exe
  │                      │     └ subprocess.run 传递所有参数
  │  labubu/bin/          │◄── Makefile wheel: Go 二进制拷入
  │    labubu.exe         │
  │                      │
  │  pyproject.toml       │  setuptools, 声明 CLI entry_point
  │  tests/test_cli.py    │  CLI 包装测试
  └──────────────────────┘
```

#### 技术要点

- **薄壳模式**：Python 包仅做二进制定位 + 参数传递，无业务逻辑
- **平台 wheel**：非 purelib，包含编译好的 Go 二进制
- **一键安装**：`pip install labubu` → `labubu serve` 即可运行

---

### 11. MCP Server — `labubu-mcp/`

#### 架构图

```
  AI Agent (Claude, GPT, etc.)
         │
         │  MCP Protocol
         ▼
  ┌──────────────────────┐
  │  labubu-mcp/          │
  │                      │
  │  server.py            │  MCP Server 设置
  │                      │
  │  api_client.py        │  HTTP 客户端 ──► Labubu API (:8080)
  │                      │
  │  formatters.py        │  输出格式化
  │                      │
  │  tools/               │  MCP Tool 定义
  │  ├── traces.py        │  Trace 查询工具
  │  ├── logs.py          │  Log 查询工具
  │  ├── metrics.py       │  Metric 查询工具
  │  └── services.py      │  Service 列表工具
  └──────────────────────┘
```

#### 技术要点

- **MCP 协议**：Model Context Protocol，让 AI Agent 通过标准化工具调用访问 Labubu 数据
- **HTTP 客户端**：`api_client.py` 调用 Labubu REST API，与前端共享同一 API
- **输出格式化**：`formatters.py` 将 API 响应转为 Agent 可理解的文本摘要

---

## 关键设计模式总结

| 模式 | 说明 | 应用位置 |
|------|------|----------|
| **接口抽象** | 统一 `Store` 接口，Handler 不直接访问底层存储 | `internal/storage/storage.go` |
| **三实现切换** | 构建标签选择 chDB (CGO生产) / SQLite (非CGO默认) / memStore (fallback)，同一接口 | `chdb.go` / `sqlite_store.go` / `memstore.go` |
| **异步缓冲** | Pipeline 为 Trace 提供背压保护，Metric/Log 直写 | `internal/pipeline/pipeline.go` |
| **单二进制部署** | `go:embed` 嵌入前端，Go + Vue = 一个可执行文件 | `web/embed.go` |
| **去重保护** | `inFlight` map 防止重复 LLM 诊断调用 | `trace_handler.go` |
| **过期检测** | `computeSpanSnapshot` 检测数据变化，标记诊断过期 | `trace_handler.go` |
| **Prometheus 兼容** | Metrics API 返回标准 Prometheus JSON，支持 Grafana 集成 | `metrics_handler.go` |
| **Notifier 插件** | `Notifier` 接口 + `NotifierFactory` 注册，告警通知可扩展 | `alerting/notifier.go` |
| **薄壳分发** | Python 包仅定位+调用 Go 二进制，无业务逻辑 | `labubu-python/labubu/cli.py` |
| **配置驱动** | YAML 配置保留期、定价、清理间隔等，种子数据不覆盖已有 | `internal/storage/config.go` |
