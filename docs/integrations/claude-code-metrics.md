# Claude Code 可观测数据接入 Labubu

> 最后更新：2026-06-06

本文档记录如何将 Claude Code 的 OTLP metrics 和 traces 数据接入 Labubu 可观测平台。

## 概述

Claude Code 内置 OpenTelemetry 遥测子系统，可发出以下类型的可观测信号：

| 信号类型 | 支持状态 | Labubu 对接 |
|----------|----------|-------------|
| **Metrics** | ✅ 稳定 | ✅ 已对接 — gRPC:4317 |
| **Traces** | 🔬 Beta | ✅ 已对接 — gRPC:4317 |
| **Logs** | ❌ 不支持 | ❌ Labubu 暂无 OTLP Logs 端点 |

## Metrics 数据

Claude Code 发出的 metrics 涵盖以下维度：

- **Token 用量** — `gen_ai.client.token.usage`（按模型、请求类型分组）
- **会话数量** — session 创建/结束计数
- **成本统计** — 按模型统计的费用估算
- **代码行数** — 生成/修改的代码行统计
- **工具决策** — 工具调用次数与类型分布

所有 metrics 携带 `service.name=claude-code` 标签，可在 Labubu 按此筛选。

## Traces 数据（Beta）

Claude Code 的 traces 以 span 形式记录每次交互的调用链路。Trace 开启后会生成以下 span 类型：

| Span 名称 | 说明 |
|-----------|------|
| `claude_code.interaction` | 单轮交互（prompt → response） |
| `claude_code.llm_request` | LLM API 调用（模型、延迟、token 数） |
| `claude_code.tool` | 工具调用 |
| `claude_code.tool.blocked_on_user` | 等待用户授权 |
| `claude_code.tool.execution` | 工具实际执行 |
| `claude_code.hook` | Hook 执行 |

> **注意**：
> - Traces 功能处于 Beta 阶段，数据格式可能随 Claude Code 版本变更。
> - 必须同时设置 `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` 和 `OTEL_TRACES_EXPORTER=otlp` 才会发送 spans。

## Trace 数据内容分析

> 分析日期：2026-06-06，Claude Code v2.1.165

本节记录 Claude Code 实际通过 OTLP Traces 上报的字段，以及**未上报的字段**，便于后续接入其他 Agent 时做对比。

### 各 Span 类型的属性清单

#### `claude_code.interaction`（根 span，每个交互轮次 1 个）

| 属性 key | 类型 | 说明 |
|----------|------|------|
| `interaction.duration_ms` | string | 交互总耗时 |
| `interaction.sequence` | string | 交互序号 |
| `session.id` | string (uuid) | Claude Code 会话 ID |
| `span.type` | string | 固定为 `interaction` |
| `user.id` | string (hash) | 用户标识 |
| `user_prompt` | string | ⚠️ **已被脱敏为 `<REDACTED>`** |
| `user_prompt_length` | string | 用户输入的字符长度 |

#### `claude_code.llm_request`（每次 LLM 调用 1 个）

| 属性 key | 类型 | 说明 |
|----------|------|------|
| `attempt` | string | 重试次数 |
| `cache_creation_tokens` | string | 新写入缓存的 token 数 |
| `cache_read_tokens` | string | 命中缓存的 token 数 |
| `duration_ms` | string | 请求耗时 |
| `gen_ai.request.model` | string | LLM 模型名 |
| `gen_ai.response.finish_reasons` | JSON string | 完成原因（stop/tool_use/end_turn 等） |
| `gen_ai.system` | string | LLM 提供商 |
| `input_tokens` | string | 输入 token 数 |
| `llm_request.context` | string | 上下文类型（interaction/tool 等） |
| `model` | string | 模型名（与 gen_ai 重复） |
| `output_tokens` | string | 输出 token 数 |
| `session.id` | string (uuid) | 会话 ID |
| `span.type` | string | 固定为 `llm_request` |
| `speed` | string | 速度模式（normal/fast） |
| `stop_reason` | string | 停止原因 |
| `success` | string | 是否成功 |
| `ttft_ms` | string | 首 token 耗时 |
| `user.id` | string (hash) | 用户标识 |

Span events：每个 llm_request 有 1 个 `gen_ai.request.attempt` event，携带 `attempt` 序号。

#### `claude_code.tool` + `claude_code.tool.execution`（每次工具调用）

| 属性 key | 类型 | 说明 |
|----------|------|------|
| `duration_ms` | string | 工具耗时 |
| `gen_ai.tool.call.id` | string | 工具调用 ID |
| `session.id` | string (uuid) | 会话 ID |
| `span.type` | string | `tool` / `tool.execution` |
| `tool_name` | string | 工具名称（如 Agent/Bash/Read 等） |
| `tool_use_id` | string | 工具使用 ID |
| `user.id` | string (hash) | 用户标识 |
| `success` | string | 执行是否成功（仅 execution） |

#### `claude_code.tool.blocked_on_user`（等待用户授权）

| 属性 key | 类型 | 说明 |
|----------|------|------|
| `decision` | string | 用户决策（accept/deny） |
| `duration_ms` | string | 等待耗时 |
| `session.id` | string (uuid) | 会话 ID |
| `source` | string | 权限来源（config 等） |
| `span.type` | string | 固定为 `tool.blocked_on_user` |
| `user.id` | string (hash) | 用户标识 |

### 未上报的内容（重要）

Claude Code Traces **不包含**以下实际文本内容，这是 Claude Code 遥测的隐私设计，**非 Labubu 的问题**：

| 缺失内容 | 对应字段 | 原因 |
|----------|----------|------|
| 用户输入原文 | `user_prompt` = `<REDACTED>` | Claude Code 主动脱敏，保护用户隐私 |
| LLM 调用的完整 prompt | 无 `gen_ai.prompt` 或类似属性 | Beta 阶段，未包含上下文文本 |
| 模型的响应文本 | 无 `gen_ai.completion` 或 `gen_ai.response.content` | Beta 阶段，未包含响应内容 |
| 工具调用的入参 | 无 `tool.input` 或类似属性 | 仅记录工具名和 ID，不记录参数 |
| 工具执行的结果 | 无 `tool.output` 或类似属性 | 仅记录耗时和成功/失败 |
| Context window 分项 token | 无 `gen_ai.context.*_tokens` | Claude Code 不使用此语义约定 |

### Trace 可诊断性差距

传统分布式追踪的核心价值是**在每个 span 节点记录输入输出**，出问题时沿调用链逐层溯源。当前 Claude Code Traces 只有元数据，缺少 I/O 内容，砍掉了可观测性最核心的诊断能力。

**当前能做的（性能分析）**：

| 诊断能力 | 可用字段 | 说明 |
|----------|----------|------|
| 哪个 span 最慢 | `duration_ms` | ✅ 可以 |
| 哪次 LLM 调用 token 最多 | `input_tokens`, `output_tokens` | ✅ 可以 |
| 工具调用成功率 | `success` | ✅ 可以 |
| 首 token 响应时间 | `ttft_ms` | ✅ 可以 |
| 缓存命中率 | `cache_read_tokens` | ✅ 可以 |

**不能做的（内容级诊断）**：

| 要排查的问题 | 需要的字段 | 缺失 |
|-------------|-----------|------|
| LLM 为什么返回了错误的代码 | `gen_ai.prompt` + `gen_ai.completion` | ❌ 无原文 |
| 工具参数传错导致执行失败 | `tool.input` 入参 | ❌ 无 |
| 工具执行返回了什么结果 | `tool.output` 结果 | ❌ 无 |
| 为什么模型理解错了用户意图 | System prompt + full context | ❌ 无 |
| 缓存命中了什么具体内容 | 缓存 key / 命中内容摘要 | ❌ 只有 token 数 |
| Memory 里存了什么导致幻觉 | Memory 读写内容 | ❌ 无 |

**业界解决思路**：

| 方案 | 做法 | 参考案例 |
|------|------|----------|
| 内容哈希关联 | 发 `input_sha256`，本地存原文，按哈希关联 | — |
| 截断摘要 | 发前后各 N 字符，足够定位问题不泄露完整内容 | Langfuse mask 策略 |
| 结构化片段 | 用结构化描述代替明文（如"读文件 X 的第 Y 行"） | OpenInference |
| 调试开关 | `CLAUDE_CODE_TELEMETRY_DEBUG=1` 发完整内容，默认脱敏 | 常见 CLI 实践 |
| **OTLP Logs 独立通道** | Traces 保持轻量，Logs 走独立通道携带详细内容，按日志级别过滤 | **OTel 标准实践** |

> **Labubu 后续方向**：OTLP Logs 是实现完整可诊断性的关键路径。Traces 定位性能瓶颈，Logs 提供内容级上下文，两者通过 `trace_id` 关联后，可以在 TraceDetail 页面中同时查看 span 瀑布图和关联的日志内容。详见 `docs/roadmap.md`。

### 与 Labubu 期望字段的差异

Claude Code 使用的属性键名与 Labubu 后端提取逻辑存在差异：

| 数据用途 | Labubu 提取的键名 | Claude Code 实际键名 | 影响 |
|----------|-------------------|---------------------|------|
| Token 一级列 | `gen_ai.usage.input_tokens` | `input_tokens` | typed 列为 NULL，token 摘要为空 |
| Token 一级列 | `gen_ai.usage.output_tokens` | `output_tokens` | typed 列为 NULL，token 摘要为空 |
| Session 分组 | `jiuwenclaw.session.id` | `session.id` | session 无法分组 |
| 模型名 | `gen_ai.request.model` | ✅ `gen_ai.request.model` + `model` | 正常工作 |

> **注意**：虽然 typed 列（`input_tokens`/`output_tokens`）为 NULL，但原始数据仍然存在于 span 的 attributes map 中，可在 TraceDetail 页面的属性列表中查看。多 Agent 属性键名兼容性问题的详细分析见 `docs/issues/multi-agent-trace-compatibility.md`。

## 配置方式

### 必需的环境变量

```bash
# 总开关
CLAUDE_CODE_ENABLE_TELEMETRY=1

# Traces Beta 开关（开启后才会发送 trace spans）
CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1

# Metrics（稳定）
OTEL_METRICS_EXPORTER=otlp

# Traces（Beta）
OTEL_TRACES_EXPORTER=otlp

# OTLP 公共配置（所有信号共用）
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
```

> **关键**：`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` 是 traces 工作的前提。只设 `OTEL_TRACES_EXPORTER` 不够，必须同时开启此开关。
>
> `OTEL_EXPORTER_OTLP_PROTOCOL` 默认值即为 `grpc`，显式写出便于理解。

### 推荐：全局配置（所有项目生效）

将环境变量写入 `~/.claude/settings.json` 的 `env` 字段：

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"
  }
}
```

> **注意**：如果 `env` 字段已存在其他变量，直接追加即可，不要覆盖已有配置。

### 项目级配置（仅当前项目生效）

写入项目根目录的 `.claude/settings.local.json`（或 `settings.json`），格式同上。

### 配置优先级

Claude Code 合并三个层级的配置：
1. `~/.claude/settings.json` — 用户级（全局）
2. `.claude/settings.json` — 项目级（commit 可共享）
3. `.claude/settings.local.json` — 项目本地（不 commit）

## Labubu 侧要求

Labubu OTLP 接收器配置（`internal/receiver/otlp.go`）：

| 服务 | gRPC（4317） | HTTP（4318） |
|------|-------------|-------------|
| Metrics | `MetricsService.Export` | `POST /v1/metrics` |
| Traces | `TraceService.Export` | `POST /v1/traces` |

- **认证**：无（本地优先设计）
- **TLS**：无

启动 Labubu 服务：

```bash
labubu serve
```

## 验证方法

### Metrics

```bash
# 检查 metric 名称列表
curl -s http://localhost:8080/api/v1/metric-names

# 查询 token 用量
curl -s "http://localhost:8080/api/v1/query?query=gen_ai_client_token_usage"

# 查看 labels（确认 service.name=claude-code）
curl -s http://localhost:8080/api/v1/labels
```

### Traces

```bash
# 查询 trace 列表
curl -s "http://localhost:8080/api/v1/traces?page=1&page_size=10"

# 在浏览器中打开 Labubu 前端查看 TraceList 页面
```

## 已知限制

1. **Trace 不包含文本内容**：Claude Code Traces 仅上报元数据（token 数、耗时、模型名等），不包含用户输入原文（`user_prompt` 已脱敏为 `<REDACTED>`）、LLM prompt/response 文本、工具调用的入参和返回值。详见上方「未上报的内容」。
2. **属性键名差异**：Claude Code 使用的属性键名（如 `input_tokens`、`session.id`）与 Labubu 期望的键名（`gen_ai.usage.input_tokens`、`jiuwenclaw.session.id`）不一致，导致 typed 列和 session 分组功能失效。详见 `docs/issues/multi-agent-trace-compatibility.md`。
3. **无 OTLP Logs 支持**：Claude Code 暂无 OTLP Logs 导出，log-level 的详细信息（API 请求详情、prompt 原文等）无法通过 Logs 通道采集。Labubu 也没有日志端点。
4. **Traces 为 Beta**：Claude Code traces 功能尚未稳定，数据格式可能随版本变更。
5. **无认证/加密**：Labubu 是本地优先设计，不支持 Auth/TLS，不适合公网暴露。
6. **重启生效**：修改 `settings.json` 后需要重启 Claude Code 才能使环境变量生效。
7. **协议选择**：Claude Code OTLP 默认使用 gRPC，与 Labubu gRPC:4317 天然兼容。如果改用 `http/protobuf`，需使用 HTTP:4318 端口。
