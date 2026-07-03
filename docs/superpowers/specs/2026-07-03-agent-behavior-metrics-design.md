# Agent Behavior 指标卡设计

**日期:** 2026-07-03
**状态:** 待审阅
**改动范围:** `web/src/components/AgentBehaviorTab.vue` + `web/src/i18n/locales/{en,zh}.ts`

## 概述

trace 详情页已有「Agent Behavior」面板（`AgentBehaviorTab.vue`），目前展示 4 张分析卡（工具成功率、最大循环深度、总重试、tokens 使用）+ 工具调用链 + 循环告警。本设计在该面板内用 8 张指标卡替换原有 4 张，并在底部新增「本次 trace 已用工具」汇总表。所有指标均由前端从 `trace.spans` 客户端计算，**不改动后端 / 存储 / API / 类型定义**。

## 目标指标

8 张指标卡，分两组：

### Tool & Agent 组

| 指标 | 计算规则 |
|---|---|
| 工具调用总数 | `spans.filter(is_tool_call === true).length` |
| 工具调用平均耗时 | `sum(duration_ms of tool spans) / 工具调用总数`；为 0 时显示 `—` |
| subagents 个数 | `max(0, distinct(attributes['gen_ai.agent.name']) − 1)`；根 agent 计为 1，每个额外不同 agent 名 = 一个 subagent。无该属性时为 0 |
| skill 使用个数 | 去重 skill 名称数。候选拉取自：`tool_name ∈ {Skill, skill, use_skill}` 的 tool span，从其 `attributes['gen_ai.tool.arguments']`（JSON）解析 skill 名；解析不出时回退为该类 tool span 的调用次数。也包含携带 `skill.name` / `gen_ai.skill.name` 属性的 span。无则 0 |

### LLM 组

| 指标 | 计算规则 |
|---|---|
| llm 调用次数 | `spans.filter(gen_ai_system !== undefined && !is_tool_call).length`（与现有 `isLLM` 判定一致） |
| llm 调用平均耗时 | `sum(duration_ms of llm spans) / llm 调用次数`；为 0 时 `—` |
| llm 调用平均 token 消耗 | `sum(total_tokens of llm spans) / llm 调用次数`；为 0 时 `—` |
| tokens 输出速率 | `sum(output_tokens of llm spans) / (sum(duration_ms of llm spans) / 1000)`，单位 tokens/s；分母为 0 时 `—` |

## 底部：已用工具汇总表

`下方展示这次trace所有已使用的工具` —— 按工具名聚合的表格，按调用次数降序：

| 列 | 取值 |
|---|---|
| 工具名 | `tool_name` |
| 调用次数 | 该工具 span 数 |
| 成功率 | `ok 数 / 总数`，百分比 |
| 平均耗时 | 该工具 span 的 `duration_ms` 均值 |

无 tool span 时整表隐藏（沿用现有 `!hasToolCalls` 空态）。

## 布局

```
[8 张指标卡：两行 4 列 grid，复用现有 .score-card 样式]
  行1 Tool & Agent：工具调用总数 · 工具调用平均耗时 · subagents 个数 · skill 使用个数
  行2 LLM        ：llm 调用次数 · llm 调用平均耗时 · llm 调用平均 token 消耗 · tokens 输出速率

[已用工具汇总表]  ← 新增，底部

[工具调用链]      ← 保留
[循环告警]        ← 保留（maxLoopDepth >= 3）
```

原有 4 张分析卡（成功率/循环/重试/tokens）**移除**，由 8 张新卡替换。调用链与循环告警保留不变。

## i18n

在 `agentStats.*` 命名空间下新增以下键，中英两份 locale 同步：

- `totalToolCalls` / `avgToolDuration` / `subagentCount` / `skillsUsed`
- `llmCallCount` / `avgLlmDuration` / `avgLlmTokens` / `tokenOutputRate`
- `toolsUsedTitle`（表标题）
- 列头：`tool` / `callCount` / `successRateCol` / `avgDuration`
- 副标题提示（如「X in + Y out」「tokens/s」等）

移除仅服务于旧 4 卡且不再被引用的键（若存在），避免遗留死键。

## 边界情况

- 无 tool span：沿用现有空态（`!hasToolCalls`），新指标卡不渲染或显示 0；汇总表隐藏。
- 无 LLM span：avg 耗时 / avg token / 输出速率显示 `—`，避免除零。
- `gen_ai.tool.arguments` 非合法 JSON 或无 skill 名：回退为 Skill 工具调用次数。
- subagent 计数为 0 是常态（多数 trace 只有一个根 agent），卡片正常显示 0。

## 不做的事（YAGNI）

- 不改后端：不加新端点、不扩展 `GetTrace` 响应、不动 `otlp.go` 归一化、不加 subagent/skill 的 typed 字段。
- 不改 `client.ts` 类型（`SpanDetail` 已含所需字段：`is_tool_call`、`tool_name`、`gen_ai_system`、`duration_ms`、`input_tokens`、`output_tokens`、`total_tokens`、`attributes`）。
- 不碰 session 级 `AgentStats` / `getAgentStats`。
- 不引入新组件、不拆文件（仍单文件 `AgentBehaviorTab.vue`）。

## 验证

- `cd web && npx vue-tsc --noEmit` 通过。
- `make run` 后访问 `http://localhost:8080/traces/<id>`，打开 Agent Behavior 面板，核对 8 张卡 + 汇总表渲染正确。
- 用本次 `71273b59...` trace 核对：工具调用总数=1、平均耗时=55ms、subagents=0、llm 调用次数=2、llm 平均耗时=4.7s（(7049+2351)/2）、平均 token=13935（(13855+14015)/2）、输出速率≈31 tokens/s（(233+59)/(9.4s)）、skill=0；汇总表 1 行 todo_list。
