# Labubu Metrics — OTLP Metrics Ingestion with Prometheus Compatibility

> Phase 2: 在现有 OTLP trace 平台上新增 metrics 采集管线，嵌入式 TSDB 存储，Prometheus HTTP API 兼容。

## Overview

**目标：** Labubu 接收探针上报的 OTLP Metrics 数据（聚焦 LLM/GenAI 指标），存入嵌入式 TSDB，对外暴露 Prometheus HTTP API，使 Grafana 等标准工具可直接对接。设计上通过 Store 接口抽象，支持生产环境无缝切换到真正的 Prometheus。

**技术栈：** Go + tstorage (嵌入式 TSDB，纯 Go 零 CGO) + 复用现有 cmux/OTLP receiver

**核心指标维度：**
- Token 维度 — input/output/total token 速率、TTFT、TPOT、总延迟
- 请求维度 — LLM API QPS、错误率、模型使用分布
- 成本维度 — 按 token 计价的费用估算，按 model/provider/customer 拆分

## Architecture

```
OTLP (gRPC/HTTP)  ──▶  cmux (端口 4317/4318)  ──┬──▶  trace_receiver  ──▶  pipeline  ──▶  chDB
                                                  │
                                                  └──▶  metrics_receiver ──▶  Store interface
                                                                                      │
                                                                  ┌───────────────────┴───────────────────┐
                                                                  ▼                                       ▼
                                                            tstorage                             (prometheus remote)
                                                          (嵌入式默认)                            (生产可切换实现)
                                                                  │
                                                                  ▼
                                                       Prometheus HTTP API
                                                       /api/v1/query
                                                       /api/v1/query_range
                                                       /api/v1/labels
```

**要点：**
- cmux 已在端口 4317/4318 同时处理 gRPC 和 HTTP 的 OTLP 请求，metrics 和 trace 共用同一端口
- metrics_receiver 从 OTLP Metrics protobuf 解析出 Sum/Gauge/Histogram/Summary，翻译为 Prometheus 数据模型
- Store interface 定义与现有 trace Store 接口风格一致但独立
- tstorage 纯 Go，零 CGO，嵌入式，重启丢数据（可接受）
- 生产切 Prometheus：实现同一个 Store interface，写入 → Remote Write，查询 → Remote Read

## Data Model & OTLP → Prometheus Translation

### Type Mapping

| OTLP Type | Prometheus Type | Notes |
|-----------|----------------|-------|
| Sum (cumulative=true) | Counter | Monotonically increasing |
| Sum (cumulative=false) | Gauge | Delta sum → gauge |
| Gauge | Gauge | Direct mapping |
| Histogram | Histogram | Bucket boundaries + count + sum |
| Summary | Summary | Quantile + count + sum |

### Prometheus Data Model

Each data point: `metric_name{label1="v1",label2="v2"} = value @ timestamp_ms`

```
// Example: OTLP Histogram translated to Prometheus:
gen_ai_client_token_usage_bucket{model="claude-opus-4-8",service="agent-gateway",le="100"} = 42 @ 1717000000123
gen_ai_client_token_usage_sum{model="claude-opus-4-8",service="agent-gateway"} = 12500 @ 1717000000123
gen_ai_client_token_usage_count{model="claude-opus-4-8",service="agent-gateway"} = 67 @ 1717000000123
```

### Label Extraction

- `resource.attributes` 中的关键键 (`service.name`, `service.namespace` 等) → labels
- `scope.name` / `scope.version` → labels
- OTLP metric `attributes` → labels
- OTLP metric `name` → Prometheus metric name (snake_case, 单位后缀)

### Histogram Expansion

OTLP Histogram with explicit bucket boundaries → Prometheus `_bucket{le="..."}` series:

```
OTLP Histogram: count=100, sum=4500, buckets=[0, 10, 50, 100, 500], bucketCounts=[5, 20, 35, 30, 10]

Expands to:
  metric_bucket{le="10"}   = 5
  metric_bucket{le="50"}   = 20
  metric_bucket{le="100"}  = 35
  metric_bucket{le="500"}  = 30
  metric_bucket{le="+Inf"} = 10
  metric_sum{}             = 4500
  metric_count{}           = 100
```

### Metric Naming Convention

Following Prometheus [naming best practices](https://prometheus.io/docs/practices/naming/):
- `gen_ai_client_token_usage` — token consumption counter
- `gen_ai_client_request_duration_seconds` — request latency histogram
- `gen_ai_client_cost_dollars_total` — cost estimate counter
- Probes name metrics; the receiver uses names as-is

## Store Interface

```go
// internal/metrics/store.go

type MetricPoint struct {
    Name      string
    Labels    map[string]string
    Value     float64
    Timestamp int64              // milliseconds
}

type MetricSeries struct {
    Name   string
    Labels map[string]string
    Points []MetricPoint
}

type Store interface {
    Insert(ctx context.Context, points []MetricPoint) error
    Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]MetricSeries, error)
    LabelNames(ctx context.Context) ([]string, error)
    LabelValues(ctx context.Context, name string) ([]string, error)
    Close() error
}
```

### Implementations

| | tstorage (default) | prometheus remote (production) |
|---|---|---|
| `Insert` | Write memory partition, optional WAL | Call Remote Write API |
| `Select` | In-memory index + disk segment scan | Call Remote Read / Query API |
| `LabelNames/Values` | In-memory inverted index | Call /api/v1/labels |
| Dependencies | Pure Go, zero CGO | Requires Prometheus endpoint config |

## HTTP API

New metrics routes registered on the existing router with `/api/v1/` prefix (coexisting with trace routes):

| Endpoint | Parameters | Description |
|----------|-----------|-------------|
| `GET /api/v1/query` | `query`, `time` | Instant query |
| `GET /api/v1/query_range` | `query`, `start`, `end`, `step` | Range query |
| `GET /api/v1/labels` | (optional `start`, `end`) | List all label names |
| `GET /api/v1/label/:name/values` | (optional `start`, `end`) | List values for a label |
| `GET /api/v1/metadata` | (optional `metric`) | Metric metadata |

Response format is fully compatible with [Prometheus HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/), enabling Grafana to connect directly as a Prometheus data source.

Query support: initial scope covers exact metric name + simple label filtering (`metric{label="value"}`). Full PromQL parsing can be added later via existing Go PromQL libraries for functions like `rate()`, `sum()`, etc.

## Metrics Receiver

### cmux Multiplexing

The existing cmux already handles gRPC vs HTTP. Metrics and traces share the same OTLP ports (4317/4318). The receiver extends to handle both `ExportTraceServiceRequest` and `ExportMetricsServiceRequest`:

```
OTLP Request ──▶  cmux  ──▶  receiver/otlp.go
                              │
                              ├── ExportTraceServiceRequest  →  otlpToTraces  →  pipeline
                              │
                              └── ExportMetricsServiceRequest →  otlpToMetrics →  metricStore.Insert
```

### OTLP Metrics Translator (`internal/receiver/metrics_translator.go`)

```
ExportMetricsServiceRequest
  └── ResourceMetrics[]
        └── ScopeMetrics[]
              └── Metric {
                    name, description, unit,
                    data: Gauge | Sum | Histogram | Summary
                  }
```

Translation: iterate ResourceMetrics → ScopeMetrics → Metrics, extract resource/scope/attribute labels, expand histograms, produce `[]MetricPoint`.

Translated points are directly batch-inserted into the metric store (synchronous — bypassing the trace pipeline which has batch/flush logic; metrics are more latency-sensitive).

## Error Handling

| Stage | Error | Behavior |
|-------|-------|----------|
| OTLP parse | Unknown metric data type | Skip that metric, warn log, continue |
| OTLP parse | Protobuf deserialization failure | Return gRPC InvalidArgument / HTTP 400 |
| Translation | Unknown metric data type (future extension) | Skip + warn log, don't block |
| Store Insert | tstorage write failure | Error log, return gRPC Internal / HTTP 500 |
| API Query | Invalid label / time parameters | Return HTTP 400 + error JSON |
| API Query | Store query timeout | Return HTTP 504 |
| Startup | tstorage init failure | Fatal exit |

Core principle: **single metric corruption does not block collection** — skip broken ones, log, continue.

## Configuration

All metrics-related flags prefixed with `--metrics-`:

| Flag | Default | Description |
|------|---------|-------------|
| `--metrics-enabled` | `true` | Enable/disable metrics ingestion |
| `--metrics-data-dir` | `./data/metrics` | tstorage data directory (empty = pure memory) |
| `--metrics-retention` | `2h` | tstorage retention duration for in-memory partitions |
| `--metrics-prometheus-addr` | (empty) | Production Prometheus remote write/read address (empty = use tstorage) |

When `--metrics-prometheus-addr` is set, the Store implementation switches to remote write/read mode; tstorage is not initialized.

## Code Structure

New and modified files:

```
labubu/
├── cmd/labubu/main.go                  # [MODIFIED] add metrics flags, init metricStore
├── internal/
│   ├── receiver/
│   │   ├── otlp.go                     # [MODIFIED] route metrics alongside traces
│   │   ├── metrics_translator.go       # ★ NEW: OTLP → Prometheus translation
│   │   └── metrics_translator_test.go  # ★ NEW
│   ├── metrics/
│   │   ├── store.go                    # ★ NEW: Store interface + MetricPoint types
│   │   ├── tstorage_store.go           # ★ NEW: tstorage embedded implementation
│   │   └── tstorage_store_test.go      # ★ NEW
│   ├── api/
│   │   ├── router.go                   # [MODIFIED] add /api/v1/ metrics routes
│   │   ├── metrics_handler.go          # ★ NEW: Prometheus HTTP API handlers
│   │   └── metrics_handler_test.go     # ★ NEW
```

## Key Design Decisions

1. **Independent Store interface** — mirrors existing trace Store pattern, keeps metrics storage isolated from chDB traces
2. **tstorage as default** — pure Go, zero CGO, matches Labubu's "single binary, zero external dependencies" philosophy
3. **Prometheus data model from day one** — metric name + labels model aligns with Prometheus ecosystem; production migration means swapping one Store implementation
4. **No pipeline buffering for metrics** — metrics are more latency-sensitive than traces; direct synchronous writes
5. **No built-in visualization** — API-first approach; Grafana and other Prometheus-compatible tools handle visualization
6. **Error isolation** — single malformed metric doesn't block the batch; skip, log, continue
