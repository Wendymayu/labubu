# Labubu Trace Platform Design

> 可观测领域 Agent Trace 平台第一阶段：接收 OTLP trace 协议数据，存储到嵌入式 chDB，提供 trace 列表和 waterfall 详情展示。

## Overview

**目标：** 构建一个最小可用的 Agent trace 可观测平台。后端为 Go 服务，接收探针通过 OpenTelemetry 协议上报的 trace 数据，存入嵌入式 chDB；前端为 Vue SPA，提供 trace 列表搜索和 waterfall 详情展示。

**技术栈：** Go + chDB (CGO) + Vue 3 + Vite

**部署形态：** Go 服务内嵌 Vue 构建产物，单二进制部署，零外部依赖（除 libchdb.so）。

## Architecture

```
┌────────────────────────────────────────────────────┐
│                  Go 主服务 (:8080)                   │
│                                                    │
│  OTLP (gRPC/HTTP)  ──▶  receiver  ──▶  pipeline    │
│                            │               │       │
│                            ▼               ▼       │
│                        otlp_translator   chDB      │
│                        (proto → model)   写入       │
│                                            │       │
│                                            ▼       │
│                                       trace API     │
│                                    (列表 + 详情)     │
│                                            │       │
│                               embed 静态文件        │
│                               (Vue dist)           │
└────────────────────────────────────────────────────┘
```

- **cmux** 同一端口多路复用 OTLP HTTP 和 gRPC
- **pipeline** 异步批量写入，双缓冲 + 定时刷盘
- **chDB** 通过 CGO 调用，封装在 storage 包内

## Data Model

### traces 表

Trace 级聚合信息，用于列表查询（单表命中，不 JOIN spans）。

```sql
CREATE TABLE traces (
    trace_id               FixedString(16),
    trace_id_hex           String,
    root_span_id           FixedString(8),
    root_name              String,
    span_count             UInt16,
    start_time_ms          UInt64,
    end_time_ms            UInt64,
    duration_ms            UInt64,
    resource_attributes    Map(String, String),
    resource_schema_url    String,
    scope_name             String,
    scope_version          String,
    scope_attributes       Map(String, String),
    scope_schema_url       String,
    trace_state            String,
    dropped_span_count     UInt32,
    status_code            Enum8('UNSET'=0, 'OK'=1, 'ERROR'=2),
    status_message         String,
    total_tokens           Nullable(UInt32)
)
ENGINE = MergeTree
ORDER BY (start_time_ms);
```

### spans 表

Span 明细，用于 trace 详情查询（单表命中，不 JOIN traces）。

```sql
CREATE TABLE spans (
    trace_id               FixedString(16),
    span_id                FixedString(8),
    parent_span_id         FixedString(8),          -- 全 0 = root
    trace_state            String,
    name                   String,
    kind                   Enum8('UNSPECIFIED'=0, 'INTERNAL'=1, 'SERVER'=2, 'CLIENT'=3, 'PRODUCER'=4, 'CONSUMER'=5),
    start_time_ms          UInt64,
    end_time_ms            UInt64,
    duration_ms            UInt64,
    attributes             Map(String, String),
    dropped_attributes_count UInt32,
    events                 String,                   -- JSON array
    dropped_events_count   UInt32,
    links                  String,                   -- JSON array
    dropped_links_count    UInt32,
    status_code            Enum8('UNSET'=0, 'OK'=1, 'ERROR'=2),
    status_message         String,

    -- LLM observability (nullable, only for LLM spans)
    input_tokens           Nullable(UInt32),
    output_tokens          Nullable(UInt32),
    total_tokens           Nullable(UInt32),
    gen_ai_request_model   Nullable(String)
)
ENGINE = MergeTree
ORDER BY (trace_id, start_time_ms);
```

**设计要点：**
- traces 和 spans 表各自独立查询，永不 JOIN
- trace 级字段在 pipeline 写入时聚合到 traces 表（冗余换查询性能）
- 时间字段使用毫秒精度（从 OTel 纳秒 ÷ 1e6）
- token 字段为 UInt32 Nullable，仅 LLM span 有值，支持 SUM/AVG/PERCENTILE 聚合

## API Design

### GET /api/v1/traces

Trace 列表查询，直接读 traces 表。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 默认 1 |
| `page_size` | int | 默认 20，最大 100 |
| `service` | string | resource_attributes['service.name'] |
| `status` | string | OK / ERROR / UNSET |
| `q` | string | root_name 模糊搜索 |
| `start` / `end` | uint64 | 时间范围（毫秒时间戳） |
| `min_duration` / `max_duration` | uint64 | 耗时范围 |

**响应：**

```json
{
  "traces": [{
    "trace_id_hex": "a1b2c3d4e5f6...",
    "root_span_id": "1a2b3c4d5e6f7g8h",
    "root_name": "POST /chat/completions",
    "root_service": "agent-gateway",
    "start_time_ms": 1700000000000,
    "duration_ms": 2340,
    "span_count": 12,
    "status": "OK",
    "total_tokens": 4500
  }],
  "pagination": { "page": 1, "page_size": 20, "total": 156 }
}
```

### GET /api/v1/traces/:traceIdHex

Trace 详情，直接读 spans 表，按 start_time_ms 排序。

**响应：**

```json
{
  "trace": {
    "trace_id_hex": "a1b2c3d4e5f6...",
    "root_span_id": "1a2b3c4d5e6f7g8h",
    "span_count": 12,
    "start_time_ms": 1700000000000,
    "duration_ms": 2340,
    "resource_attributes": { "service.name": "agent-gateway" },
    "scope": { "name": "..." },
    "spans": [{
      "span_id": "1a2b3c4d5e6f7g8h",
      "parent_span_id": "",
      "name": "handle_chat",
      "kind": "SERVER",
      "start_time_ms": 1700000000000,
      "duration_ms": 2340,
      "attributes": {},
      "events": [],
      "links": [],
      "status": "OK",
      "input_tokens": 3200,
      "output_tokens": 1300,
      "total_tokens": 4500,
      "gen_ai_request_model": "claude-opus-4-8"
    }]
  }
}
```

### GET /api/v1/services

返回可用服务列表，供前端筛选器使用。

## Code Structure

```
labubu/
├── cmd/labubu/main.go           # 入口，依赖注入
├── internal/
│   ├── receiver/                # OTLP 接收 (gRPC + HTTP, cmux)
│   │   ├── otlp.go
│   │   └── otlp_test.go
│   ├── pipeline/                # 批量写入 + trace 聚合
│   │   ├── pipeline.go
│   │   └── pipeline_test.go
│   ├── storage/                 # chDB 存储 (CGO 隔离)
│   │   ├── storage.go           # Store interface
│   │   ├── chdb.go              # chDB CGO 实现
│   │   ├── chdb_query.go        # SQL 构建
│   │   ├── chdb_test.go
│   │   └── schema.sql
│   ├── api/                     # REST API + 静态文件 serve
│   │   ├── router.go
│   │   ├── trace_handler.go
│   │   └── trace_handler_test.go
│   └── mcp/
│       └── interface.go         # 预留 MCP (第一阶段不实现)
├── web/                         # Vue 前端
│   ├── src/
│   │   ├── views/
│   │   │   ├── TraceList.vue
│   │   │   └── TraceDetail.vue
│   │   ├── components/
│   │   │   ├── WaterfallChart.vue
│   │   │   └── SpanDetail.vue
│   │   ├── api/client.ts
│   │   └── router.ts
│   ├── index.html
│   ├── vite.config.ts
│   └── package.json
├── go.mod
├── Makefile
└── README.md
```

## Key Design Decisions

1. **traces 和 spans 表永不 JOIN** — 列表走 traces，详情走 spans，各自单表查询
2. **CGO 封装在 storage 包内** — 外部只依赖 Store interface，不感知 C 类型
3. **cmux 同端口复用 HTTP/gRPC** — 单端口承载两种 OTLP 传输协议
4. **前端内嵌 Go binary** — Go 1.16+ embed 嵌入 Vue dist，单二进制部署
5. **Token 专用列** — LLM span token 数据从 attributes Map 提升到专用列，支持数值聚合
6. **纳秒转毫秒** — 所有时间字段使用毫秒精度，在 receiver 层转换
7. **MCP 预留不实现** — interface 留好，第一阶段不包含 AI 功能
