# Accurate Token & Cost Aggregation with Cache Breakdown — Design Spec

> 创建日期：2026-06-25
> 状态：设计已确认，待写实现计划
> 关联：`docs/issues/multi-agent-trace-compatibility.md`、`docs/superpowers/specs/2026-06-22-attribute-normalization-design.md`

## 背景

成本页面(`/api/v1/cost-summary`)的 `overview.total_tokens` 不等于 `total_input_tokens + total_output_tokens`,且 `by_model` 的 `input_tokens`/`output_tokens` 恒为 0、`tokens` 膨胀。

## 根因(已用 live DB `data/labubu.db` 证实)

1. **span 层是对的,与 agent 无关**:
   - `claude-code`(调 GLM,经 OpenAI 兼容代理):`span.total_tokens == input + output + cache_creation + cache_read`,逐行 0 不符;input 才几百(非缓存),cache_read 巨大且**分桶**。
   - `jiuwenclaw`(调 glm-5.1):`cache_creation = cache_read = 0`,`span.total_tokens == input + output`。
   - 两个 agent 都用**不相交分桶**,谁也没有"input 已含 cache"。所谓 agent 语义差异在当前数据里不存在。

2. **overview 对不上**:`total_tokens` 取 `sum(traces.total_tokens)`,而这列**不可靠**——`aggregateTraces` 只聚合当前 OTLP 批次的 spans,多轮对话分批到达则残缺;`UpdateTraceCost`/recalc 不重算 `total_tokens`。`input`/`output` 直接从 spans 聚合(对),`total` 从残缺列取(错)。
   - 实测(全量 cost>0):`traces.total_tokens` 之和 = 69.3M,而 `sum(spans input+output+cache)` = 213.7M;逐 trace `t.total_tokens != sum(spans.total_tokens)`。

3. **by_model 全错**([sqlite_store.go:1242-1264](internal/storage/sqlite_store.go#L1242-L1264)):`LEFT JOIN spans` 扇出,`sum(t.cost)`/`sum(t.total_tokens)` 按 span 数重复累加(cost 膨胀 ~56×,tokens ~15×);SELECT 根本没选 input/output,Scan 也没扫,故恒为 0。

## 目标

- `overview.total_tokens == total_input_tokens + total_cache_creation_tokens + total_cache_read_tokens + total_output_tokens`,**按构造成立**。
- `by_model` 各行之和 == overview;无扇出;每行含 input/cache_creation/cache_read/output。
- 成本页展示缓存 token 分项(cache read / cache write)。
- `traces.total_tokens` 列可靠(trace 列表/session 列表也准确)。
- token 派生逻辑集中在 receiver 单一函数,未来新增 agent 的语义规则有明确扩展点。

## 非目标(YAGNI)

- per-agent 归一化 registry:当前无 agent 需要(input 都是非缓存分桶)。单函数扩展点足够,不预建。
- trace 列表/session 列表展示 cache 分项:用户只要求成本页。
- 重写 chDB 成本路径(chDB 为可选 CGO,用户跑 sqlite)。chDB 若已实现 `GetCostSummary` 则同步等价改动,否则不动。

## 设计

### 核心原则:spans 表为唯一事实源

所有 token 聚合从 `spans` 的不相交分桶计算:`total = input + cache_creation + cache_read + output`。`traces.cost` 仍可靠(UpdateTraceCost 重算),继续用于总成本与 trace 计数。新增 **span 级成本列**,使 by_model 的成本与 token 同源、无扇出。

### 1. Schema:span 级成本列

- `spans` 表新增 `cost REAL`、`cost_currency TEXT`(sqlite_schema.sql、schema.sql、memstore `Span` 结构)。
- 迁移:`ALTER TABLE spans ADD COLUMN cost REAL`、`ADD COLUMN cost_currency TEXT`(参照 [sqlite_store.go:62-64](internal/storage/sqlite_store.go#L62-L64) 的 `ALTER` 模式)。回填由下文 `UpdateTraceCost` 写入 span 成本后,通过启动期迁移对 `cost IS NULL` 的 span 所在 trace 调一次 `UpdateTraceCost`(幂等)。
- `spans.cost` 语义:该 span 的成本(含差异化缓存费率),由 `UpdateTraceCost` 在计算 `spanCost` 时落库。`sum(spans.cost) == traces.cost`(按构造,允许 1e-6 级舍入误差)。

### 2. Receiver:集中 token 派生 + 去掉 override

把 [otlp.go:428-446](internal/receiver/otlp.go#L428-L446) 的 token 计算抽成单一函数。**注意包边界**:`receiver` 依赖 `storage`(import 单向),故共享 helper 放 `storage` 包,`receiver` 与 storage backfill 迁移([sqlite_store.go:159](internal/storage/sqlite_store.go#L159) 附近)都调它,避免重复与 import cycle:

```go
// Package storage.
// DeriveTokenBuckets extracts disjoint token buckets from an attribute map.
// total = input + output + cacheCreation + cacheRead (always; any self-reported
// gen_ai.usage.total_tokens is IGNORED). Assumes input_tokens is non-cached
// (cache reported in separate buckets). If a future agent reports input that
// already includes cache (OpenAI-style prompt_tokens), add a rule HERE to derive
// non-cached input before summing — this function is the single extension point.
func DeriveTokenBuckets(attrs map[string]string) (input, output, cacheCreation, cacheRead, total *uint32)
```

- 删除 [otlp.go:443-445](internal/receiver/otlp.go#L443-L445) 的 override(`if tt := ...; tt != nil { sum = *tt }`)。`total` 恒为四桶之和。
- `translateSpan`(receiver)调用 `storage.DeriveTokenBuckets` 填充 `Span` 字段。
- storage backfill 迁移改用同一函数(替换其内联的 parse+sum 逻辑),保证新 span 与历史 span 回填口径一致。

### 3. Storage:重写 GetCostSummary(sqlite + memstore;chdb 若有则同步)

#### Overview —— 两条查询

(a) 从 `traces`(无 join,无扇出):总成本、trace 数、currency:
```sql
SELECT COALESCE(sum(cost),0), count(*), COALESCE(max(cost_currency),'USD')
FROM traces
WHERE start_time_ms >= ? AND start_time_ms <= ?
  AND cost IS NOT NULL AND cost > 0
```

(b) 从 `spans`(trace 子查询过滤 cost>0 + 时间):四桶:
```sql
SELECT
  COALESCE(sum(input_tokens),0),
  COALESCE(sum(cache_creation_tokens),0),
  COALESCE(sum(cache_read_tokens),0),
  COALESCE(sum(output_tokens),0)
FROM spans
WHERE total_tokens IS NOT NULL
  AND trace_id_hex IN (
    SELECT trace_id_hex FROM traces
    WHERE start_time_ms >= ? AND start_time_ms <= ?
      AND cost IS NOT NULL AND cost > 0
  )
```
Go 侧 `TotalTokens = input + cacheCreation + cacheRead + output`(按构造 = 四桶和,不再读 `traces.total_tokens`)。`AvgCostPerTrace = total_cost / trace_count`(不变)。

#### by_model —— 一条查询,直接 group spans(无扇出)
```sql
SELECT
  COALESCE(gen_ai_request_model,'(unknown)'),
  COALESCE(sum(cost),0),
  COALESCE(sum(input_tokens),0),
  COALESCE(sum(cache_creation_tokens),0),
  COALESCE(sum(cache_read_tokens),0),
  COALESCE(sum(output_tokens),0),
  count(DISTINCT trace_id_hex)
FROM spans
WHERE total_tokens IS NOT NULL
  AND trace_id_hex IN (
    SELECT trace_id_hex FROM traces
    WHERE start_time_ms >= ? AND start_time_ms <= ?
      AND cost IS NOT NULL AND cost > 0
  )
GROUP BY gen_ai_request_model
ORDER BY sum(cost) DESC
```
每行 `tokens = input + cacheCreation + cacheRead + output`(Go);`avg_cost = cost / trace_count`。`trace_count` 为该 model 出现的 distinct trace 数(一条 trace 用多模型会在多行各计一次,这是 per-span 归属的固有结果,可接受)。每个 span 恰落一个 model 桶(NULL model → `(unknown)`),故 `sum(by_model.cost) == overview.total_cost`(舍入误差内,与模型数无关)。

> 依赖 `spans.cost` 已回填;迁移未跑完的旧 span 其 cost 为 NULL,`COALESCE(...,0)` 兜底,recalc 后补齐。

### 4. API 类型

`internal/storage/storage.go`:
- `CostOverview` 增 `TotalCacheCreationTokens uint64`、`TotalCacheReadTokens uint64`(`json:"total_cache_creation_tokens"`/`"total_cache_read_tokens"`)。
- `ModelCostItem` 增 `CacheCreationTokens uint64`、`CacheReadTokens uint64`(字段已有 `InputTokens`/`OutputTokens`,补两个)。

`web/src/api/client.ts`:`CostOverview`、`ModelCost` 同步加字段。

### 5. Frontend

`web/src/views/CostDashboard.vue`:
- 总 token 卡 sub 行由 `"in / out"` 改为展示 cache 分项(或新增两张小卡:Cache Read、Cache Write)。
- by_model 表新增 "Cache" 列(展示 `cache_read + cache_creation`,或在 tokens 列下用副标 `cr/cc`)。
- 可选:缓存命中率 = `cache_read / (input + cache_read)`,作为概览小卡或副标。

`web/src/i18n/locales/en.ts` + `zh.ts`:新增 `costDashboard.cacheRead` / `cacheWrite` / `cache` / `cacheHitRate` 等键。

### 6. traces.total_tokens 列可靠性(trace 列表/session 列表准确)

- **`aggregateTraces` 合并累加**([chdb_query.go:528-536](internal/storage/chdb_query.go#L528-L536)、sqlite merge [sqlite_store.go:328](internal/storage/sqlite_store.go#L328)、memstore [memstore.go:84-89](internal/storage/memstore.go#L84-L89)、chdb [chdb.go:174-179](internal/storage/chdb.go#L174-L179)):现有 trace 在新批次到达时,`total_tokens` 从"新批次 nil 才保留旧值"改为**累加**(`existing + newBatchSum`)。注:同一 span 被 `INSERT OR REPLACE` 重发会双重计数,属罕见边界,接受并注释。
- **`UpdateTraceCost` 重算 `traces.total_tokens`**:在其 span 循环中对**所有** `total_tokens IS NOT NULL` 的 span 累加 `totalTokens`(不过滤 unpriced,与成本计算独立),写回 `UPDATE traces SET total_tokens = ?, cost = ?, cost_currency = ? ...`。recalc(`/api/v1/model-pricing/recalc`)后全量刷新。

## 测试目标(可验证)

- `GetCostSummary`:overview `total == input + cache_creation + cache_read + output`;by_model 各行 `tokens`/`input`/`cache`/`output` 之和 == overview 对应字段;by_model `cost` 之和 == overview `total_cost`(舍入误差内,与模型数无关);无扇出(by_model tokens 不再是 overview 的多倍)。
- 两种 fixture 对账:claude-code 式(有 cache,分桶不相交)、jiuwenclaw 式(无 cache)。
- `deriveTokenBuckets`:自报 `total_tokens=999` 且 input=100/output=50 → 返回 total=150(忽略自报)。原有 `TestTranslateSpanCacheTokens` 仍通过。
- recalc 后逐 trace `traces.total_tokens == sum(spans.total_tokens)`。
- `sum(spans.cost) == traces.cost`(舍入误差内)。
- TypeScript `vue-tsc --noEmit` 通过。

## 受影响文件

| 层 | 文件 | 改动 |
|----|------|------|
| receiver | `internal/receiver/otlp.go`、`otlp_test.go` | 抽 `deriveTokenBuckets`、删 override、补测试 |
| storage | `internal/storage/sqlite_store.go`、`memstore.go`、`chdb*.go` | schema 迁移、`UpdateTraceCost` 写 span 成本+重算 total、`GetCostSummary` 重写、`aggregateTraces` 累加 |
| storage | `internal/storage/storage.go`、`sqlite_schema.sql`、`schema.sql` | 类型字段、schema 列 |
| api | (无新端点) | `CostHandler` 不变,store 返回值扩字段 |
| 前端 | `web/src/api/client.ts`、`web/src/views/CostDashboard.vue`、`i18n/locales/{en,zh}.ts` | 类型、展示、文案 |
| 测试 | `internal/storage/*_test.go`、`internal/receiver/otlp_test.go` | 对账用例 |

## 迁移与回滚

- 启动期迁移幂等:`ALTER` 失败(列已存在)忽略;span 成本回填对 `cost IS NULL` 的 trace 调 `UpdateTraceCost`。
- 回滚:字段/列新增不影响旧查询;`GetCostSummary` 改动可还原。`deriveTokenBuckets` 删 override 后,旧 `total_tokens` 自报值不再生效——已知且符合设计。
