# Labubu MCP Server Design

> 2026-06-10 | Status: Draft

## Overview

为 Labubu 可观测平台构建 MCP (Model Context Protocol) Server，使 Claude Code 能通过自然语言查询 traces、logs、metrics 等可观测数据，辅助代码与环境问题定位。

**Phase 1 聚焦人工诊断助手场景**：开发者在 Claude Code 中提问（如"为什么这个 trace 是失败的？"），Claude 通过 MCP 工具查询 Labubu 数据并输出诊断结论。

## Architecture

```
Claude Code (MCP Client)
    │ stdio (JSON-RPC)
    ▼
Labubu MCP Server (Python, 独立进程)
    │ HTTP (REST API)
    ▼
Labubu Go Backend (labubu serve)
    │
    ▼
Store (chDB / memstore)
```

### 关键设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 语言 | Python | MCP SDK 官方最成熟，学习 MCP 开发最佳起点 |
| 部署形态 | 独立进程（非嵌入 Labubu） | 解耦，可独立迭代，可远程连接 Labubu |
| Transport | stdio（Phase 1） | local-first 天然优势，零网络配置 |
| 数据获取 | Labubu REST API（非直连存储） | 不依赖 Labubu 内部实现，升级互不影响 |
| 分析职责 | Claude Code 端 | MCP Server 只暴露数据，分析推理全在 Claude |

## MCP Tools

### 1. search_traces

搜索 trace 列表，返回摘要信息（不含完整 span 树）。

**Input:**
```json
{
  "status": "ERROR",        // OK | ERROR | UNSET | 空=全部
  "service": "my-service",  // 服务名过滤
  "query": "chat",          // trace 名模糊搜索
  "start_time": "2026-06-10T14:00:00Z",
  "end_time": "2026-06-10T15:00:00Z",
  "min_duration_ms": 1000,
  "max_duration_ms": 30000,
  "limit": 20,              // 默认 20，最大 50
  "offset": 0               // 分页偏移
}
```

**Output:** CSV 风格表格，包含 trace_id, name, status, duration_ms, span_count, service, start_time。

**Token control:** 最多返回 50 条，超出返回 `next_offset` 游标。

### 2. get_trace_detail

获取单个 trace 的完整 span 树、events 和属性。

**Input:**
```json
{
  "trace_id": "abc123def456...",
  "include_events": true,
  "include_attributes": true
}
```

**Output:** 缩进 YAML 格式，包含 trace 元数据 + 递归 span 树（含 children、events、attributes）。

**Token control:** events > 100 个时截断，保留前 100 + 提示剩余数量。

### 3. search_logs

按条件搜索日志，支持按 trace_id 关联。

**Input:**
```json
{
  "trace_id": "abc123...",
  "severity": "ERROR",       // DEBUG | INFO | WARN | ERROR | FATAL
  "event_name": "exception",
  "query": "timeout",
  "start_time": "...",
  "end_time": "...",
  "limit": 20,
  "offset": 0
}
```

**Output:** 表格，包含 timestamp, severity, event, body。

**Token control:** 最多 50 条，超出返回 `next_offset`。

### 4. query_metrics

PromQL 即时查询。

**Input:**
```json
{
  "query": "rate(http_requests_total[5m])",
  "time": "2026-06-10T14:30:00Z"  // 可选
}
```

**Output:** 简洁键值对列表 `{labels} => value`。

**Token control:** 最多 20 个时间序列，按值排序取 top 20。

### 5. list_services

列出所有已知服务名（零参数）。

**Output:** 服务名列表。

---

### 典型诊断流程

```
用户: "为什么这个 trace 是失败的？"
→ search_traces(status="ERROR", limit=10)
  → 发现 trace abc123 "POST /api/chat", 2.34s, ERROR
→ get_trace_detail(trace_id="abc123")
  → span "call_llm" ERROR, event "gen_ai.error" → "rate limit exceeded"
→ search_logs(trace_id="abc123", severity="ERROR")
  → 确认 API key 配额在 14:32:05 耗尽
→ Claude 综合输出诊断结论
```

## Output Format Rules

| 数据类型 | 格式 | 原因 |
|----------|------|------|
| 列表（trace 列表、日志列表） | CSV 风格表格 | 比 JSON 省 ~50% token |
| 树形结构（span 树） | 缩进 YAML | 比 JSON 省 ~20% token，可读性好 |
| 键值对（metrics、服务列表） | 简单列表 | 最紧凑 |
| 大段内容（log body、event attrs） | 保留原文 + 截断提示 | 避免丢失关键信息 |

## Error Handling

**核心原则**：每个错误都附带 Claude 可以用来修正调用的具体建议。

| 场景 | 响应 |
|------|------|
| Labubu 不可达 | `Cannot connect to Labubu at http://localhost:8080. Is 'labubu serve' running?` |
| 超时 | `Request timed out after 30s. Try narrowing the time range or reducing the limit.` |
| Trace 不存在 | `Trace "xyz789" not found. It may have been purged (retention: 7 days).` |
| 参数错误 | `Invalid status 'ERRROR'. Valid values: OK, ERROR, UNSET. (Did you mean 'ERROR'?)` |
| 结果截断 | `Results truncated at 50 records and ~8000 tokens. Use more specific filters to narrow down. next_offset=50` |

## Token Budget Control

| 工具 | 硬上限 | 超出行为 |
|------|--------|----------|
| search_traces | 50 条 | 返回前 N 条 + next_offset |
| search_logs | 50 条 | 同上 |
| get_trace_detail | 100 events | 截断 + 剩余数量提示 |
| query_metrics | 20 序列 | top 20 by value |

## Project Structure

```
labubu-python/
├── pyproject.toml                ← 新增 mcp extra: [mcp, httpx]
├── labubu/
│   ├── __init__.py
│   ├── cli.py                    ← 新增 mcp_main CLI 入口
│   ├── otlp_exporter.py
│   └── mcp/                      ← 新包
│       ├── __init__.py
│       ├── __main__.py           ← python -m labubu.mcp 入口
│       ├── server.py             ← MCP Server 创建 + Tool 注册
│       ├── tools/
│       │   ├── __init__.py
│       │   ├── traces.py         ← search_traces, get_trace_detail
│       │   ├── logs.py           ← search_logs
│       │   ├── metrics.py        ← query_metrics
│       │   └── services.py       ← list_services
│       └── api_client.py         ← Labubu REST API HTTP 客户端
└── tests/
    └── mcp/
        ├── __init__.py
        ├── conftest.py           ← 共享 mock fixtures + sample data
        ├── test_traces.py
        ├── test_logs.py
        ├── test_metrics.py
        └── test_api_client.py
```

## Dependencies

仅 2 个新依赖：
- `mcp>=1.0.0` — MCP Python SDK（官方）
- `httpx>=0.27` — 异步 HTTP 客户端

## CLI Usage

```bash
# 开发模式
python -m labubu.mcp --api-url http://localhost:8080

# 安装后
labubu-mcp --api-url http://localhost:8080

# Claude Code 配置
claude mcp add labubu -- python -m labubu.mcp --api-url http://localhost:8080
```

## Testing Strategy

### 三层测试金字塔

1. **单元测试**（最多）：输出格式化逻辑、参数校验、错误分支处理
2. **集成测试**：pytest-httpx mock HTTP 层，验证 API client + tool 实现的完整链路
3. **E2E 测试**（Phase 2）：真实 MCP Client 连接，端到端验证

### Mock 策略

使用 `pytest-httpx` 的 `MockTransport` 拦截 HTTP 调用，配合预定义的 sample fixtures 模拟各种场景（正常、错误、边界）。不依赖真实 Labubu 实例。

### Phase 1 手动验证

```bash
# 1. 启动 Labubu + 注入测试数据
labubu serve
python -m labubu otlp-send --file test_trace.json

# 2. 启动 MCP server
python -m labubu.mcp --api-url http://localhost:8080

# 3. 用 MCP Inspector 验证
npx @modelcontextprotocol/inspector
```

## Out of Scope (Phase 2+)

- Streamable HTTP transport（团队远程访问）
- MCP Resources（`labubu://traces/{id}` 资源 URI）
- 自动化告警分析
- Session 相关 tool（session 列表、详情）
- Trace diff 能力
- `query_metrics_range`（PromQL range query）
- 直连 chDB 存储（性能优化）
- Docker 部署支持
