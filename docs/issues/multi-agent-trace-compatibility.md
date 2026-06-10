# 多 Agent 接入 Trace 字段兼容性问题

> 创建日期：2026-06-05
> 状态：部分修复，Agent 耦合待解耦

## 问题一：chDB 后端 TraceDetail 缺少 attributes/events/links（已修复）

### 根因

`internal/storage/chdb.go` 中的 `mapToSpanDetail` 函数（第 519 行）从 chDB JSONEachRow 结果中构造 `SpanDetail` 时，**未读取 SQL 查询已返回的 `attributes`、`events`、`links` 三个字段**。

SQL 查询（`chdb_query.go:148`）正确 SELECT 了这些列：

```sql
SELECT ..., attributes, events, links, ... FROM spans WHERE trace_id = ...
```

但 `mapToSpanDetail` 的 struct literal 中直接忽略了它们，导致 API 返回的每个 span 的 `attributes` 为空 map，`events`/`links` 为空数组。

此外 `parseTraceDetail` 还存在以下问题：
- `TraceIDHex` 硬编码为 `""`
- `ResourceAttrs` 硬编码为空 map（未查询 traces 表）
- `DurationMS` 仅取 root span 自身 duration，而非整个 trace 的时间窗口
- `Scope` 未填充

**影响**：使用 chDB 后端（CGO 生产构建）时，TraceDetail 页面看不到：
- 所有 span 属性（`gen_ai.*`、`http.*` 等全部不可见）
- Span events（tool call/result 的输入输出数据不可见）
- Trace 摘要中的 service name

### 修复方式

**文件变更**（commit 待提交）：

| 文件 | 变更内容 |
|------|----------|
| `internal/storage/chdb.go:184-201` | `GetTrace` — 新增 traces 表元数据查询，传递 traceIDHex |
| `internal/storage/chdb.go:396-475` | `parseTraceDetail` — 解析 resource_attributes/scope，正确计算 trace duration |
| `internal/storage/chdb.go:595-617` | `mapToSpanDetail` — 新增 attributes (Map→map[string]string)、events、links 解析 |
| `internal/storage/chdb_query.go:174-186` | 新增 `buildGetTraceMetaSQL` — 查询 traces 表获取 resource/scope 元数据 |
| `internal/storage/storage.go` | 新增 `parseJSONArray`（从 memstore.go 移至共享位置，解决构建标签互斥问题） |
| `internal/storage/memstore.go` | 移除重复的 `parseJSONArray` 及未使用的 `encoding/json` import |

**关键实现细节**：
- chDB 的 `Map(String, String)` 列在 JSONEachRow 中返回为 JSON 对象 → `map[string]interface{}` → 转换为 `map[string]string`
- chDB 的 `events`/`links` 列存储为 JSON 字符串 → 通过 `parseJSONArray` 反序列化
- `parseJSONArray` 从 `memstore.go`（`!cgo || !local_engine` 构建标签）移至 `storage.go`（无构建标签），使两个 Store 实现共享

---

## 问题二：Agent 属性键名硬编码耦合（待解决）

### 根因

Labubu 当前对 AI Agent 上报的 trace 数据做了**语义提取** —— 将特定 OTLP attribute key 提升为数据库的一级列（`input_tokens`、`output_tokens`、`gen_ai_request_model`）和 UI 专用组件（`TokenPieChart`）。这些提取代码中硬编码了特定的属性键名，导致不同 Agent 使用不同命名约定时功能降级或失效。

### 耦合点清单

#### 1. Session ID 提取（最关键）

**位置**：`internal/storage/chdb_query.go:346` + `memstore.go`

```go
if sid, ok := span.Attributes["jiuwenclaw.session.id"]; ok {
    t.SessionID = sid
}
```

**硬编码值**：`jiuwenclaw.session.id`

**影响**：Codex、OpenClaw 或其他 Agent 若使用不同的 session ID 属性键，其 session 数据将完全不可见（SessionList 为空）。

**潜在的其他 Agent 键名**：`session.id`、`codex.session.id`、`openclaw.session.id`、`claude.session.id`

#### 2. Token 用量提取

**位置**：`internal/receiver/otlp.go:341-357`

| 一级列 | 硬编码键 | OTel 标准？ |
|--------|----------|-------------|
| `input_tokens` | `gen_ai.usage.input_tokens` | ✅ GenAI semconv v1.27+ |
| `output_tokens` | `gen_ai.usage.output_tokens` | ✅ GenAI semconv v1.27+ |
| `total_tokens` | `gen_ai.usage.total_tokens` | ✅（fallback: input+output 求和） |
| `gen_ai_request_model` | `gen_ai.request.model` | ✅ GenAI semconv v1.27+ |

**影响**：如果 Agent 使用 OpenInference 的 `llm.usage.input_tokens` / `llm.usage.output_tokens` 或其他约定，则：
- 一级 token 列在 DB 中为 NULL
- 前端 SpanDetail 的 Token 摘要区显示为空
- 但 **原始属性保留在 attributes map 中**，用户仍可在属性列表中看到

#### 3. Context Window 饼图

**位置**：`web/src/components/TokenPieChart.vue:86-93` + `web/src/views/TraceDetail.vue:96-102`

| 硬编码键 | 饼图标签 |
|----------|----------|
| `gen_ai.context.system_tokens` | System |
| `gen_ai.context.assistant_tokens` | Assistant History |
| `gen_ai.context.user_tokens` | User |
| `gen_ai.context.tool_tokens` | Tool Results |
| `gen_ai.context.tool_definitions_tokens` | Tool Definitions |
| `gen_ai.context.skill_tokens` | Skill |

**影响**：这些键名 **不属于 OTel 标准**，是自定义扩展。不同 Agent 几乎必然使用不同的键名，导致饼图为空。

#### 4. UI 属性分组

**位置**：`web/src/components/SpanDetail.vue:125-129`

属性按前缀分组：`gen_ai.` → "Gen AI"、`http.`/`url.`/`net.` → "HTTP"、`service.`/`telemetry.` → "Service"

**影响**：部分降级。非 `gen_ai.` 前缀的 LLM 属性（如 `llm.*`）会进入 "Other" 组，但**全部可见**。

### 现状总结

| 功能 | 通用性 | 原因 |
|------|--------|------|
| Trace 摄入 & Waterfall | ✅ 完全通用 | 纯 OTLP 结构 |
| 所有属性存储 & 查看 | ✅ 完全通用 | attributes Map 列保留一切 |
| 服务名提取 | ✅ 完全通用 | OTel 标准 `service.name` |
| Events/Links | ✅ 完全通用 | JSON 序列化存储 |
| Session 列表/详情 | ❌ 仅 JiuwenClaw | `jiuwenclaw.session.id` 硬编码 |
| Token 摘要（一级列） | ⚠️ 仅 `gen_ai.*` | OTel 标准，但其他约定不兼容 |
| Context 窗口饼图 | ❌ 仅 `gen_ai.context.*` | 非标准扩展，几乎无 Agent 兼容 |
| 属性分组 | ⚠️ 仅 `gen_ai.` 前缀高亮 | 其他 LLM 属性进 "Other" |

### 建议方案（待讨论）

1. **Session ID**：改为优先列表匹配（`jiuwenclaw.session.id` → `session.id` → `codex.session.id` → ...），或通过 YAML 配置注入
2. **Token 提取**：增加 fallback 键名列表，覆盖 OpenInference（`llm.usage.*`）等主流框架
3. **Context 饼图**：改为动态扫描 `*.context.*_tokens` 模式，降低新增 Agent 的接入成本
4. **属性分组**：增加 `llm.` 前缀到 "Gen AI" 组

### 相关文件

- `internal/receiver/otlp.go` — OTLP 摄入 + 属性提取
- `internal/storage/chdb.go` — chDB 存储实现
- `internal/storage/chdb_query.go` — SQL 构建 + session ID 提取
- `internal/storage/memstore.go` — 内存存储实现
- `web/src/components/TokenPieChart.vue` — Context 窗口饼图
- `web/src/components/SpanDetail.vue` — Span 详情 + 属性分组
- `web/src/views/TraceDetail.vue` — Trace 详情页
