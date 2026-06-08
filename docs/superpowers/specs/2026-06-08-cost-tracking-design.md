# AI 成本追踪 — 设计文档

> 对应 roadmap #17  
> 日期：2026-06-08  
> 状态：已确认

## 目标

在 Labubu 中实现 AI 模型调用成本追踪：支持任意模型的通用定价配置，在 Trace/Session 级别展示金钱成本（非仅 token 数），并通过 Web UI 管理定价表。

## 数据模型

### 新增表：`model_pricing`

| 列 | 类型 | 说明 |
|----|------|------|
| model_name | String | 模型标识，如 `claude-opus-4-8` |
| input_price | Float64 | 每 1M input token 价格 |
| output_price | Float64 | 每 1M output token 价格 |
| currency | String | 货币单位，`USD` 或 `CNY` |
| updated_at | DateTime | 最后修改时间 |

主键：`model_name`。

### 现有表加列

- **traces** 表：`cost` (Float64, nullable) + `cost_currency` (String)
- **sessions** 表：`cost` (Float64, nullable) + `cost_currency` (String)

### 成本计算公式

```
span_cost = (input_tokens × model.input_price + output_tokens × model.output_price) / 1_000_000
trace_cost = sum(所有 span 的 span_cost)
session_cost = sum(所有 trace 的 trace_cost)
```

- 有 token 但模型名匹配不到定价的 span：不计入成本，计入 `unpriced_spans` 计数
- 货币单位取第一个匹配模型的 currency（一个 Trace 内通常同模型）

## 默认定价配置

`pricing.yaml`（打包进二进制，启动时加载为初始默认值）：

```yaml
models:
  - name: claude-opus-4-8
    input_price: 15.0
    output_price: 75.0
    currency: USD
  - name: claude-sonnet-4-6
    input_price: 3.0
    output_price: 15.0
    currency: USD
  - name: claude-haiku-4-5
    input_price: 0.80
    output_price: 4.0
    currency: USD
```

## API 设计

### 新增接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/model-pricing` | 获取所有模型定价列表 |
| `POST` | `/api/v1/model-pricing` | 新增或覆盖一个模型定价 |
| `DELETE` | `/api/v1/model-pricing/{model_name}` | 删除一个模型定价 |
| `POST` | `/api/v1/model-pricing/recalc` | 触发全量成本重算 |

### 现有接口响应变更

**`TraceListItem`**（列表/详情/会话内 traces）新增：
```json
{ "cost": 0.042, "cost_currency": "USD" }
```

**`TraceDetailResponse`** 新增：
```json
{ "cost": 0.042, "cost_currency": "USD", "unpriced_spans": 3 }
```

**`SessionListItem`** 和 **`SessionDetail.session`** 同理新增 `cost` / `cost_currency`。

## 成本计算时机

1. **新数据**：`InsertSpans` 完成后异步触发成本重算（goroutine）：
   - 先算 Trace 成本（sum 该 trace 所有 span）
   - 再算 Session 成本（sum 该 session 所有 trace）
2. **存量回填**：启动时检测 `traces.cost IS NULL`，后台批量计算（限速，避免 CPU 打满）。先回填所有 trace，再回填所有 session
3. **定价变更**：管理员在 Web UI 修改/删除价格后手动调用 `POST /api/v1/model-pricing/recalc` 触发全量重算：
   - 遍历所有 trace → 重算 cost
   - 遍历所有 session → 重算 cost（聚合 trace）
   - 返回重算结果：`{ traces_updated: N, sessions_updated: N }`

三种场景的 session 成本都是**从 trace 聚合**，不直接从 span 计算，保证一致性。

## 后端结构

```
internal/
  pricing/              # 新增 package
    pricing.go          # 定价加载、存储、计算
    handler.go          # HTTP handler：list/create/delete/recalc
```

路由注册在 `internal/api/router.go` 中，挂载到 `/api/v1/model-pricing` 和 `/api/v1/model-pricing/recalc`。

存储接口 `Store` 不新增方法——定价 CRUD 直接操作 chDB/memstore，成本字段在已有 `Trace`/`TraceDetail`/`TraceListItem`/`SessionListItem`/`SessionDetail` 结构体中体现。

## 前端设计

### 改动页面

| 页面 | 变更 |
|------|------|
| **TraceList** | 表格新增 Cost 列，格式 `$0.042` |
| **SessionList** | 表格新增 Cost 列 |
| **TraceDetail** | 统计栏显示总成本：`· $0.042 USD` |
| **SessionDetail** | 汇总区显示总成本 |

### 新增页面：Settings > Model Pricing

- 路由：`/settings/pricing`
- 模型定价表格：Model Name | Input Price | Output Price | Currency | Actions
- Add Model 按钮 → 弹出表单
- 每行 Edit / Delete 操作
- "Recalculate All Costs" 按钮（调用 recalc API）

### 公共工具

- `web/src/utils/format.ts` 新增 `formatCost(cost: number, currency: string): string`
- `web/src/api/client.ts` 新增 `getModelPricing` / `saveModelPricing` / `deleteModelPricing` / `recalcCosts`

## 错误处理

- 模型名匹配不到定价 → span 计为 `unpriced`，成本为 0，不影响其他 span
- 存量回填中某条 Trace 计算失败 → 跳过，日志记录，不阻塞后续
- recalc API 调用时服务重启 → 幂等，下次启动继续检测 `cost IS NULL`
- 前端显示成本为 0 且有 `unpriced_spans > 0` → 提示 "N spans without pricing"

## 测试要点

- 定价 CRUD API 正确性
- 成本计算精度（浮点累加误差在 $0.001 以内）
- 存量回填不阻塞服务启动
- 前端各页面成本字段正确展示
- pricing.yaml 默认值正确加载
- 定价变更后 recalc 触发全量重算
