# OTLP Logs 接收与展示 — 设计规范

> 创建日期：2026-06-06
> 状态：已批准
> 关联路线图：#16

## 目标

为 Labubu 新增 OTLP Logs 信号的接收与展示能力。Logs 通过 `trace_id`/`span_id` 与 Traces 关联，弥补当前 Trace 缺少 I/O 内容（prompt、response、tool input/output）导致的诊断能力不足问题。

## 范围

- Backend：OTLP Logs 接收端点（gRPC + HTTP）、存储接口、API 处理器
- Frontend：独立 Log 列表页（含搜索过滤）、TraceDetail 内嵌 Log 面板
- Log 保留策略与 Trace 保持一致（YAML 配置的 `max_age` / `max_count`）

### 不做

- Log 独立保留策略（后续迭代）
- Log 统计分析 / Dashboard（后续迭代）

---

## 架构概览

```
Claude Code / Agent → OTLP gRPC/HTTP → Receiver → Store (chDB/memstore)
                                                       ↓
Browser ← API (/api/v1/logs) ← LogHandler ← Store.ListLogs/GetLogsByTrace
```

Log 写入直接调用 Store（不经过 Pipeline），与 Metrics 处理方式一致。Pipeline 主要用于 Span→Trace 聚合，Log 是独立记录无需批处理。

---

## 1. 数据模型

### LogRecord（Go 内部类型）

```go
type LogRecord struct {
    TraceID    [16]byte          // 关联 Trace
    SpanID     [8]byte           // 关联 Span（零值 = 无关联）
    Timestamp  uint64            // Unix 毫秒
    Severity   string            // TRACE / DEBUG / INFO / WARN / ERROR / FATAL
    EventName  string            // 从 attributes["event.name"] 提取
    Body       string            // 日志正文（JSON 字符串）
    Attributes map[string]string // 其他所有 OTLP 属性
}
```

### chDB Schema

```sql
CREATE TABLE IF NOT EXISTS logs (
    trace_id    FixedString(16),
    span_id     FixedString(8),
    timestamp   UInt64,
    severity    String,
    event_name  String,
    body        String,
    attributes  Map(String, String)
)
ENGINE = MergeTree
ORDER BY (trace_id, timestamp);
```

- `body` 存完整 JSON 字符串，不解析内部结构（方案 B：提取关键字段 + 保留原始 body）
- `event_name` 从 OTLP attributes 的 `event.name` 提取为一列列，方便过滤
- Log 与 Trace 共享保留策略，Purge 时同步删除

### LogQuery（查询参数）

```go
type LogQuery struct {
    Page       int
    PageSize   int
    Severity   string   // "" = all
    EventName  string   // "" = all
    Query      string   // 全文搜索 body
    TraceID    [16]byte // 零值 = 不按 trace 过滤
    StartTime  uint64
    EndTime    uint64
}
```

---

## 2. 存储接口扩展

`Store` 接口新增四个方法：

```go
// InsertLogs 写入一批日志记录。
InsertLogs(ctx context.Context, logs []LogRecord) error

// ListLogs 返回分页的日志列表。
ListLogs(ctx context.Context, q LogQuery) (*LogListResult, error)

// GetLogsByTrace 返回某条 Trace 关联的所有日志。
GetLogsByTrace(ctx context.Context, traceID [16]byte) ([]LogRecord, error)

// GetLogEventNames 返回所有 distinct event_name 值。
GetLogEventNames(ctx context.Context) ([]string, error)
```

### memstore 实现

- `[]LogRecord` 切片，互斥锁保护
- 全文搜索使用与 `containsSubstring` 相同的子串匹配

### chDB 实现

- `InsertLogs`：`INSERT INTO logs` 逐行写入
- `ListLogs`：`SELECT ... FROM logs WHERE ... LIKE ... ORDER BY timestamp DESC LIMIT ...`
- 全文搜索使用 SQL `body LIKE '%keyword%'`
- Purge 时同步清理：`ALTER TABLE logs DELETE WHERE trace_id NOT IN (SELECT trace_id FROM traces ...)`

---

## 3. 接收器（Receiver）

在 `internal/receiver/otlp.go` 中新增：

### gRPC

实现 `col logs pb.LogServiceServer` 接口：

```go
type logsService struct {
    collogspb.UnimplementedLogsServiceServer
    store storage.Store
}

func (s *logsService) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
    // 遍历 ResourceLogs → ScopeLogs → LogRecords
    // 每个 LogRecord 转换为 storage.LogRecord
    // 调用 store.InsertLogs(ctx, logs)
}
```

### HTTP

新增 `POST /v1/logs` 端点，处理 JSON 和 protobuf 两种 Content-Type。

### 依赖

项目已有 `go.opentelemetry.io/proto/otlp v0.20.0`，包含 `collector/logs/v1` 的 proto 定义，无需额外依赖。

---

## 4. API 设计

### 4.1 日志列表 `GET /api/v1/logs`

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 默认 1 |
| `page_size` | int | 默认 20，最大 100 |
| `severity` | string | ERROR / WARN / INFO / DEBUG |
| `event_name` | string | 如 `gen_ai.prompt` |
| `q` | string | 全文搜索 body |
| `trace_id` | string | 按 Trace ID 过滤 |
| `start` | int64 | 起始时间（毫秒时间戳） |
| `end` | int64 | 结束时间（毫秒时间戳） |

响应：

```json
{
  "logs": [
    {
      "trace_id_hex": "abc...",
      "span_id_hex": "def...",
      "timestamp": 1717600000000,
      "severity": "DEBUG",
      "event_name": "gen_ai.prompt",
      "body": "{\"content\":\"...\"}",
      "attributes": {}
    }
  ],
  "pagination": { "page": 1, "page_size": 20, "total": 42 }
}
```

### 4.2 Trace 关联日志 `GET /api/v1/logs/:traceIdHex`

返回该 Trace 所有日志，按 timestamp 正序排列。

### 4.3 事件名枚举 `GET /api/v1/log-event-names`

```json
{ "event_names": ["gen_ai.prompt", "gen_ai.completion", "tool.call", "..."] }
```

### 路由注册

在 `router.go` 中新增：

```go
mux.HandleFunc("/api/v1/logs/", logHandler.ServeHTTP)
mux.HandleFunc("/api/v1/logs", logHandler.ServeHTTP)
mux.HandleFunc("/api/v1/log-event-names", logHandler.GetEventNames)
```

---

## 5. 前端设计

### 5.1 路由与导航

- 新增路由 `/logs` → `LogList.vue`
- 侧边栏新增 "Logs" 导航项（`App.vue` 中加 `<router-link to="/logs">`）
- i18n 中加 `nav.logs` 键

### 5.2 LogList 页面 (`web/src/views/LogList.vue`)

布局：顶部工具栏（搜索框 + 过滤器）+ Log 表格 + 分页

- **搜索框**：全文搜索 body 内容，输入后 debounce 500ms 自动搜索
- **Severity 下拉**：All / ERROR / WARN / INFO / DEBUG
- **Event 下拉**：从 `/api/v1/log-event-names` 加载选项
- **时间范围**：可选，复用 Dashboard 的日期选择模式
- **表格列**：Timestamp | Severity（带颜色徽章） | Event | Body 预览 | Trace（可点击链接）
- **Body 预览**：截断显示前 80 字符，点击行展开完整 JSON（格式化缩进显示）
- **Trace 列**：`router-link` 到 `/traces/:traceIdHex`
- **分页**：复用 `TraceList.vue` 的分页组件模式

样式完全使用现有 CSS 变量，沿用 TraceList 的表格风格。

### 5.3 TraceDetail Log 面板 (`web/src/views/TraceDetail.vue` 增强)

**Waterfall Span 行徽章**：

```
<span v-if="logCount > 0" class="log-badge" @click.stop="filterLogsBySpan(span.span_id)">
  📋 {{ logCount }}
</span>
```

- 徽章显示在 `col-tokens` 列旁边
- `logCount` 来自 `logsBySpan` 的 computed map
- 点击徽章设置 `logSpanFilter`，下方 Log 面板自动过滤

**Tab 切换**：

Waterfall 下方改为 Tab 结构：

```
[Spans] [Logs (N)]     ← "Spans" = 现有 SpanDetail drawer，"Logs (N)" = Log 面板
```

- "Spans" tab：现有的 Span 点击 → Drawer 行为不变
- "Logs" tab：显示该 Trace 所有关联 Log 的列表
- 当 `logSpanFilter` 激活时，面板顶部显示过滤标签 "Filtered: span_name ✕"

**Log 行展开**：

- 点击 Log 行展开，显示格式化 JSON body
- 类似 SpanDetail 的 Attributes 展开方式

### 5.4 API 客户端 (`web/src/api/client.ts`)

新增 TypeScript 类型和 API 函数：

```typescript
export interface LogRecord {
  trace_id_hex: string
  span_id_hex: string
  timestamp: number
  severity: string
  event_name: string
  body: string
  attributes: Record<string, string>
}

export interface LogListResponse {
  logs: LogRecord[]
  pagination: Pagination
}

export async function listLogs(query: LogQuery): Promise<LogListResponse>
export async function getLogsByTrace(traceIdHex: string): Promise<{ logs: LogRecord[] }>
export async function getLogEventNames(): Promise<string[]>
```

---

## 6. 国际化

新增 i18n 键：

| 键 | 英文 | 中文 |
|----|------|------|
| `nav.logs` | Logs | 日志 |
| `logs.title` | Logs | 日志 |
| `logs.search` | Search body... | 搜索正文... |
| `logs.severity` | Severity | 级别 |
| `logs.event` | Event | 事件 |
| `logs.noLogs` | No logs found | 暂无日志 |
| `logs.traceLink` | View Trace | 查看 Trace |

---

## 7. 测试

### Backend

- `internal/receiver/otlp_logs_test.go`：gRPC + HTTP 日志接收测试
- `internal/storage/memstore_logs_test.go`：内存存储 InsertLogs/ListLogs/GetLogsByTrace 测试
- `internal/api/log_handler_test.go`：API 端点测试

### Frontend

- TypeScript 类型检查 `npx vue-tsc --noEmit`

---

## 8. 文件变更清单

| 操作 | 文件 |
|------|------|
| 新增 | `internal/api/log_handler.go` |
| 新增 | `internal/api/log_handler_test.go` |
| 新增 | `internal/receiver/logs_translator.go` |
| 新增 | `internal/storage/logs_test.go`（memstore 集成） |
| 修改 | `internal/receiver/otlp.go` |
| 修改 | `internal/storage/storage.go` |
| 修改 | `internal/storage/memstore.go` |
| 修改 | `internal/storage/chdb.go` |
| 修改 | `internal/storage/chdb_query.go` |
| 修改 | `internal/storage/schema.sql` |
| 修改 | `internal/api/router.go` |
| 修改 | `cmd/labubu/main.go` |
| 新增 | `web/src/views/LogList.vue` |
| 修改 | `web/src/views/TraceDetail.vue` |
| 修改 | `web/src/router.ts` |
| 修改 | `web/src/api/client.ts` |
| 修改 | `web/src/App.vue` |
| 修改 | `web/src/i18n/locales/en.ts` |
| 修改 | `web/src/i18n/locales/zh.ts` |
| 修改 | `web/src/components/WaterfallChart.vue` |

---

## 关键决策记录

1. **方案 B**：提取关键字段 + 保留原始 body（与现有 Span 存储模式一致，不绑定 body schema）
2. **方案 C**：Log 列表独立页面 + TraceDetail 内嵌（两者兼有）
3. **方案 C**：Span 行徽章 + 下方 Log 面板（快速定位 + 灵活过滤）
4. **方案 B**：基础浏览 + 全文搜索（够用，统计后续迭代）
5. Log 保留策略与 Trace 一致（保持简单，后续可差异化）
6. Log 写入不经过 Pipeline，直接写 Store（与 Metrics 一致）
