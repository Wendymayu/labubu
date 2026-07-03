# Agent Behavior 指标卡 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 trace 详情页 Agent Behavior 面板内，用 8 张指标卡替换原有 4 张，并在底部新增「已用工具」汇总表，全部由前端从 `trace.spans` 客户端计算。

**Architecture:** 单文件改动 `AgentBehaviorTab.vue`（已有组件，已接收 `trace.spans` 并客户端计算）+ 两份 locale 文件。不动后端 / 存储 / API / 类型。无前端测试框架，以 `vue-tsc --noEmit` 类型检查 + 浏览器手动验证作为验证手段。

**Tech Stack:** Vue 3 Composition API + TypeScript + vue-i18n。

## Global Constraints

- TypeScript strict，禁用 `any`。
- 所有用户可见文案走 `vue-i18n`，`en.ts` 与 `zh.ts` 同步新增键，模板用 `t('agentStats.<key>')`。
- 复用已有 CSS 变量与 `.score-card` 等现有样式类，不引入新依赖、不拆文件。
- 保留调用链（`toolCallChain`）与循环告警（`loopWarning`）逻辑不变。
- 提交仅在用户明确要求时执行（默认不自动 git commit）。

**参考 spec:** `docs/superpowers/specs/2026-07-03-agent-behavior-metrics-design.md`

---

## File Structure

- **Modify** `web/src/i18n/locales/en.ts` — `agentStats` 命名空间新增 13 个键（行 229-250 块内）。
- **Modify** `web/src/i18n/locales/zh.ts` — 同步新增对应中文键（行 229-250 块内）。
- **Modify** `web/src/components/AgentBehaviorTab.vue` — 新增指标 computed；模板用 8 张卡替换原 4 张；底部新增「已用工具」表；新增表格样式。

无新建文件。`web/src/api/client.ts` 的 `SpanDetail` 接口已含所需字段（`is_tool_call`、`tool_name`、`gen_ai_system`、`duration_ms`、`input_tokens`、`output_tokens`、`total_tokens`、`attributes`），不改。

---

## Task 1: 新增 i18n 键

**Files:**
- Modify: `web/src/i18n/locales/en.ts:229-250`（`agentStats` 块）
- Modify: `web/src/i18n/locales/zh.ts:229-250`（`agentStats` 块）

**Interfaces:**
- Produces: `agentStats.totalToolCalls` / `avgToolDuration` / `subagentCount` / `skillsUsed` / `llmCallCount` / `avgLlmDuration` / `avgLlmTokens` / `tokenOutputRate` / `toolsUsedTitle` / `tool` / `callCount` / `successRateCol` / `avgDuration` 共 13 个键，供 Task 2/3 模板引用。

- [ ] **Step 1: 在 `en.ts` 的 `agentStats` 块末尾（`maxLoop: 'Max Loop',` 之后、`},` 之前）新增键**

```ts
    totalToolCalls: 'Tool Calls',
    avgToolDuration: 'Avg Tool Duration',
    subagentCount: 'Subagents',
    skillsUsed: 'Skills Used',
    llmCallCount: 'LLM Calls',
    avgLlmDuration: 'Avg LLM Duration',
    avgLlmTokens: 'Avg LLM Tokens',
    tokenOutputRate: 'Token Output Rate',
    toolsUsedTitle: 'Tools Used',
    tool: 'Tool',
    callCount: 'Calls',
    successRateCol: 'Success Rate',
    avgDuration: 'Avg Duration',
```

- [ ] **Step 2: 在 `zh.ts` 的 `agentStats` 块末尾同步新增对应中文**

```ts
    totalToolCalls: '工具调用总数',
    avgToolDuration: '工具调用平均耗时',
    subagentCount: 'Subagents 个数',
    skillsUsed: 'Skill 使用个数',
    llmCallCount: 'LLM 调用次数',
    avgLlmDuration: 'LLM 调用平均耗时',
    avgLlmTokens: 'LLM 调用平均 Token 消耗',
    tokenOutputRate: 'Tokens 输出速率',
    toolsUsedTitle: '已使用工具',
    tool: '工具',
    callCount: '调用次数',
    successRateCol: '成功率',
    avgDuration: '平均耗时',
```

- [ ] **Step 3: 类型检查**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS（无新增类型错误；i18n 键不参与类型检查，但需确认未破坏现有结构）

- [ ] **Step 4: 提交（仅当用户要求时）**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(i18n): add agent behavior metrics keys"
```

---

## Task 2: 新增指标 computed 并用 8 张卡替换原 4 张

**Files:**
- Modify: `web/src/components/AgentBehaviorTab.vue`（`<script setup>` computed 区 + `<template>` score-cards 区）

**Interfaces:**
- Consumes: Task 1 的 8 个指标 i18n 键；`SpanDetail`（`is_tool_call`、`tool_name`、`gen_ai_system`、`duration_ms`、`input_tokens`、`output_tokens`、`total_tokens`、`attributes`）。
- Produces: 模板内 8 张指标卡；新 computed `llmSpans` / `totalLLMCalls` / `avgToolDurationMs` / `avgLLMDurationMs` / `avgLLMTokens` / `tokenOutputRate` / `subagentCount` / `skillCount` / `llmInputTokens` / `llmOutputTokens`。保留 `toolSpans` / `successfulToolCalls` / `totalToolCalls`（被新卡副标题与 Task 3 表格复用）。

- [ ] **Step 1: 在 `<script setup>` 的 `toolSpans` computed 之后、`maxLoopDepth` 之前新增 LLM spans 与指标 computed**

定位锚点（现有代码，`web/src/components/AgentBehaviorTab.vue:97-115`）：

```ts
const toolSpans = computed(() =>
  props.spans.filter(s => s.is_tool_call === true)
)

const toolAndLLMSpans = computed(() =>
  props.spans.filter(s => s.is_tool_call === true || s.gen_ai_system !== undefined)
)

const hasToolCalls = computed(() => toolSpans.value.length > 0)

const totalToolCalls = computed(() => toolSpans.value.length)

const successfulToolCalls = computed(() =>
  toolSpans.value.filter(s => s.status === 'ok').length
)

const toolSuccessRate = computed(() =>
  totalToolCalls.value > 0 ? successfulToolCalls.value / totalToolCalls.value : 1
)
```

在 `toolSuccessRate` computed 之后插入以下代码（保留上述现有代码不变）：

```ts
// --- LLM spans ---
// LLM 调用：gen_ai_system 已设置且非 tool 调用（与 callChainItems 的 isLLM 判定一致）

const llmSpans = computed(() =>
  props.spans.filter(s => s.gen_ai_system !== undefined && !s.is_tool_call)
)

const totalLLMCalls = computed(() => llmSpans.value.length)

const llmInputTokens = computed(() =>
  llmSpans.value.reduce((sum, s) => sum + (s.input_tokens ?? 0), 0)
)

const llmOutputTokens = computed(() =>
  llmSpans.value.reduce((sum, s) => sum + (s.output_tokens ?? 0), 0)
)

// --- 平均耗时 / token / 速率（除零时返回 null，模板显示 —） ---

const avgToolDurationMs = computed(() =>
  totalToolCalls.value > 0
    ? toolSpans.value.reduce((sum, s) => sum + s.duration_ms, 0) / totalToolCalls.value
    : null
)

const avgLLMDurationMs = computed(() =>
  totalLLMCalls.value > 0
    ? llmSpans.value.reduce((sum, s) => sum + s.duration_ms, 0) / totalLLMCalls.value
    : null
)

const avgLLMTokens = computed(() =>
  totalLLMCalls.value > 0
    ? llmSpans.value.reduce((sum, s) => sum + (s.total_tokens ?? 0), 0) / totalLLMCalls.value
    : null
)

const tokenOutputRate = computed(() => {
  const totalDurMs = llmSpans.value.reduce((sum, s) => sum + s.duration_ms, 0)
  return totalDurMs > 0 ? llmOutputTokens.value / (totalDurMs / 1000) : null
})

// --- subagent / skill 计数 ---

const subagentCount = computed(() => {
  const names = new Set<string>()
  for (const s of props.spans) {
    const n = s.attributes?.['gen_ai.agent.name']
    if (n) names.add(n)
  }
  return Math.max(0, names.size - 1)
})

const skillCount = computed(() => {
  const names = new Set<string>()
  let invocations = 0
  for (const s of toolSpans.value) {
    const tn = (s.tool_name ?? '').toLowerCase()
    const isSkillTool = tn === 'skill' || tn === 'use_skill'
    const skillAttr = s.attributes?.['skill.name'] ?? s.attributes?.['gen_ai.skill.name']
    if (!isSkillTool && !skillAttr) continue
    invocations++
    let name = skillAttr ?? ''
    if (!name && isSkillTool) {
      try {
        const args = JSON.parse(s.attributes?.['gen_ai.tool.arguments'] ?? '{}')
        name = args?.name ?? args?.skill ?? args?.skill_name ?? ''
      } catch {
        name = ''
      }
    }
    if (name) names.add(name)
  }
  return names.size > 0 ? names.size : invocations
})

const llmModelLabel = computed(() => {
  for (const s of llmSpans.value) {
    if (s.gen_ai_request_model) return s.gen_ai_request_model
  }
  return ''
})
```

- [ ] **Step 2: 在 `<script setup>` 末尾（`loopClass` 函数之后）新增格式化 helper**

定位锚点（现有代码，`web/src/components/AgentBehaviorTab.vue:319-334`）：

```ts
function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.round(ms)}ms`
}

function rateClass(rate: number): string {
  if (rate >= 0.9) return 'rate-green'
  if (rate >= 0.7) return 'rate-yellow'
  return 'rate-red'
}

function loopClass(depth: number): string {
  if (depth < 3) return 'rate-green'
  if (depth <= 4) return 'rate-yellow'
  return 'rate-red'
}
```

在 `loopClass` 之后追加：

```ts
function formatRatePerSec(rate: number | null): string {
  if (rate === null) return '—'
  return `${rate.toFixed(1)} t/s`
}

function formatNullableDuration(ms: number | null): string {
  return ms === null ? '—' : formatDuration(ms)
}

function formatNullableTokens(tokens: number | null): string {
  return tokens === null ? '—' : formatTokens(tokens)
}
```

- [ ] **Step 3: 用 8 张指标卡替换 `<template>` 中现有 4 张 score-card**

定位锚点（现有代码，`web/src/components/AgentBehaviorTab.vue:11-42`，即 `<div class="score-cards">…</div>` 整块）：

```html
      <!-- Score cards -->
      <div class="score-cards">
        <div class="score-card" :class="rateClass(toolSuccessRate)">
          <div class="score-value">{{ formatRate(toolSuccessRate) }}</div>
          <div class="score-label">{{ t('agentStats.toolSuccessRate') }}</div>
          <div class="score-subtitle">{{ successfulToolCalls }}/{{ totalToolCalls }} calls succeeded</div>
        </div>

        <div class="score-card" :class="loopClass(maxLoopDepth)">
          <div class="score-value">{{ maxLoopDepth }}</div>
          <div class="score-label">{{ t('agentStats.maxLoopDepth') }}</div>
          <div class="score-subtitle">
            <template v-if="maxLoopDepth > 1">{{ loopToolName }} called {{ maxLoopDepth }}&times; in a row</template>
            <template v-else>no loops detected</template>
          </div>
        </div>

        <div class="score-card" :class="totalRetries > 0 ? 'rate-red' : 'rate-green'">
          <div class="score-value">{{ totalRetries }}</div>
          <div class="score-label">{{ t('agentStats.totalRetries') }}</div>
          <div class="score-subtitle">
            <template v-if="totalRetries > 0">{{ totalRetries }} retries across tools</template>
            <template v-else>all calls succeeded first try</template>
          </div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ formatTokens(totalTokens) }}</div>
          <div class="score-label">{{ t('agentStats.tokensUsed') }}</div>
          <div class="score-subtitle">{{ formatTokens(inputTokens) }} in + {{ formatTokens(outputTokens) }} out</div>
        </div>
      </div>
```

整块替换为：

```html
      <!-- Metric cards (8) -->
      <div class="score-cards">
        <div class="score-card rate-green">
          <div class="score-value">{{ totalToolCalls }}</div>
          <div class="score-label">{{ t('agentStats.totalToolCalls') }}</div>
          <div class="score-subtitle">{{ successfulToolCalls }}/{{ totalToolCalls }} succeeded</div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ formatNullableDuration(avgToolDurationMs) }}</div>
          <div class="score-label">{{ t('agentStats.avgToolDuration') }}</div>
          <div class="score-subtitle">{{ totalToolCalls }} calls</div>
        </div>

        <div class="score-card" :class="subagentCount > 0 ? 'rate-yellow' : 'rate-green'">
          <div class="score-value">{{ subagentCount }}</div>
          <div class="score-label">{{ t('agentStats.subagentCount') }}</div>
          <div class="score-subtitle">
            <template v-if="subagentCount > 0">{{ subagentCount }} nested agent(s)</template>
            <template v-else>root only</template>
          </div>
        </div>

        <div class="score-card" :class="skillCount > 0 ? 'rate-green' : 'rate-green'">
          <div class="score-value">{{ skillCount }}</div>
          <div class="score-label">{{ t('agentStats.skillsUsed') }}</div>
          <div class="score-subtitle">
            <template v-if="skillCount > 0">{{ skillCount }} skill(s)</template>
            <template v-else>none</template>
          </div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ totalLLMCalls }}</div>
          <div class="score-label">{{ t('agentStats.llmCallCount') }}</div>
          <div class="score-subtitle">{{ llmModelLabel || '—' }}</div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ formatNullableDuration(avgLLMDurationMs) }}</div>
          <div class="score-label">{{ t('agentStats.avgLlmDuration') }}</div>
          <div class="score-subtitle">{{ totalLLMCalls }} calls</div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ formatNullableTokens(avgLLMTokens) }}</div>
          <div class="score-label">{{ t('agentStats.avgLlmTokens') }}</div>
          <div class="score-subtitle">{{ formatTokens(llmInputTokens) }} in + {{ formatTokens(llmOutputTokens) }} out</div>
        </div>

        <div class="score-card rate-green">
          <div class="score-value">{{ formatRatePerSec(tokenOutputRate) }}</div>
          <div class="score-label">{{ t('agentStats.tokenOutputRate') }}</div>
          <div class="score-subtitle">{{ formatTokens(llmOutputTokens) }} out / {{ formatNullableDuration(avgLLMDurationMs) }}</div>
        </div>
      </div>
```

- [ ] **Step 4: 类型检查**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS。若报 `totalTokens` / `inputTokens` / `outputTokens` / `toolSuccessRate` / `maxLoopDepth` / `totalRetries` 等已不使用变量未使用错误，进入 Step 5 清理；若报其它类型错误，修正。

- [ ] **Step 5: 清理仅被旧 4 卡引用、现已不用的 computed**

检查以下 computed 是否仍在模板中被引用，仅删除**完全不再引用**的：
- `toolSuccessRate` —— 仅旧卡用，删除其 computed 定义。
- `maxLoopDepth` —— **保留**（callChainItems 的 `isLoopGroup` 与循环告警 `maxLoopDepth >= 3` 仍用）。
- `totalRetries` —— 仅旧卡用？检查：callChainItems 不用 `totalRetries`，循环告警不用。删除其 computed 定义。
- `inputTokens` / `outputTokens` / `totalTokens` —— 旧 tokensUsed 卡用；新卡用 `llmInputTokens` / `llmOutputTokens`。检查是否仍被引用，若无则删除。

对每个删除项：删除 `<script setup>` 中对应 `const xxx = computed(...)` 块。删后重跑 `cd web && npx vue-tsc --noEmit` 确认 PASS。

- [ ] **Step 6: 浏览器手动验证**

Run: `make run`（后端 8080）+ 另开 `cd web && npm run dev`（或直接用 8080 嵌入式前端）
访问 `http://localhost:8080/traces/71273b594b914fd6d1c8904019259e9c`，点击「Agent Behavior」按钮，核对 8 张卡数值：
- 工具调用总数 = 1
- 工具调用平均耗时 = 55ms
- Subagents 个数 = 0（root only）
- Skill 使用个数 = 0（none）
- LLM 调用次数 = 2（subtitle: glm-5.2）
- LLM 调用平均耗时 = 4.7s
- LLM 调用平均 Token 消耗 = 13.9K
- Tokens 输出速率 = 31.0 t/s（(233+59)/(9.4s)）

调用链与循环告警仍正常渲染。

- [ ] **Step 7: 提交（仅当用户要求时）**

```bash
git add web/src/components/AgentBehaviorTab.vue
git commit -m "feat(web): agent behavior metrics cards (8) replacing old 4"
```

---

## Task 3: 底部新增「已用工具」汇总表

**Files:**
- Modify: `web/src/components/AgentBehaviorTab.vue`（`<script setup>` 新增 `toolsUsed` computed；`<template>` 在 call-chain-section 之前插入表格；`<style>` 新增表格样式）

**Interfaces:**
- Consumes: Task 1 的 `toolsUsedTitle` / `tool` / `callCount` / `successRateCol` / `avgDuration` 键；Task 2 的 `toolSpans`。
- Produces: `toolsUsed` computed（数组：`{ name, calls, successRate, avgDuration }`），模板表格。

- [ ] **Step 1: 在 `<script setup>` 的 `skillCount` computed 之后新增 `toolsUsed` computed**

```ts
// --- 已用工具汇总（按工具名聚合，调用次数降序） ---

const toolsUsed = computed(() => {
  const map: Record<string, { name: string; calls: number; ok: number; totalDur: number }> = {}
  for (const s of toolSpans.value) {
    const name = s.tool_name ?? s.name
    if (!map[name]) map[name] = { name, calls: 0, ok: 0, totalDur: 0 }
    map[name].calls++
    if (s.status === 'ok') map[name].ok++
    map[name].totalDur += s.duration_ms
  }
  return Object.values(map)
    .map(t => ({
      name: t.name,
      calls: t.calls,
      successRate: t.calls > 0 ? t.ok / t.calls : 0,
      avgDuration: t.calls > 0 ? t.totalDur / t.calls : 0,
    }))
    .sort((a, b) => b.calls - a.calls)
})
```

- [ ] **Step 2: 在 `<template>` 的 call-chain-section 之前插入表格**

定位锚点（现有代码，`web/src/components/AgentBehaviorTab.vue:44-46`）：

```html
      <!-- Tool Call Chain -->
      <div v-if="callChainItems.length > 0" class="call-chain-section">
        <h4>{{ t('agentStats.toolCallChain') }}</h4>
```

在该 `<!-- Tool Call Chain -->` 之前插入：

```html
      <!-- Tools Used summary -->
      <div v-if="toolsUsed.length > 0" class="tools-used-section">
        <h4>{{ t('agentStats.toolsUsedTitle') }}</h4>
        <table class="tools-used-table">
          <thead>
            <tr>
              <th>{{ t('agentStats.tool') }}</th>
              <th class="num">{{ t('agentStats.callCount') }}</th>
              <th class="num">{{ t('agentStats.successRateCol') }}</th>
              <th class="num">{{ t('agentStats.avgDuration') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in toolsUsed" :key="i">
              <td class="tool-name">{{ row.name }}</td>
              <td class="num">{{ row.calls }}</td>
              <td class="num" :class="rateClass(row.successRate)">{{ formatRate(row.successRate) }}</td>
              <td class="num">{{ formatDuration(row.avgDuration) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

```

- [ ] **Step 3: 在 `<style scoped>` 末尾（`</style>` 之前）新增表格样式**

```css
/* Tools used summary table */
.tools-used-section h4 {
  font-size: 14px;
  margin-bottom: 10px;
  color: var(--text-primary);
}

.tools-used-table {
  width: 100%;
  border-collapse: collapse;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  overflow: hidden;
  font-size: 13px;
}

.tools-used-table th,
.tools-used-table td {
  padding: 8px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border-default);
}

.tools-used-table th {
  font-weight: 600;
  color: var(--text-secondary);
  background: var(--bg-surface);
}

.tools-used-table td.num,
.tools-used-table th.num {
  text-align: right;
}

.tools-used-table .tool-name {
  font-weight: 600;
  color: var(--text-primary);
}

.tools-used-table tr:last-child td {
  border-bottom: none;
}

.tools-used-table td.rate-green { color: #22c55e; }
.tools-used-table td.rate-yellow { color: #eab308; }
.tools-used-table td.rate-red { color: #ef4444; }
```

- [ ] **Step 4: 类型检查**

Run: `cd web && npx vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 5: 浏览器手动验证**

访问同一 trace 的 Agent Behavior 面板，核对底部表格：
- 1 行：工具名 `todo_list`，调用次数 1，成功率 0%（该 tool span status=error），平均耗时 55ms。
- 调用链在表格下方仍正常。

- [ ] **Step 6: 提交（仅当用户要求时）**

```bash
git add web/src/components/AgentBehaviorTab.vue
git commit -m "feat(web): tools-used summary table in agent behavior panel"
```

---

## Self-Review

**1. Spec coverage:**
- 8 张指标卡 → Task 2 ✓
- 替换原有 4 张 → Task 2 Step 3 整块替换 + Step 5 清理 unused ✓
- 底部已用工具表 → Task 3 ✓
- 保留调用链与循环告警 → Task 2 不动 call-chain-section / loop-warning ✓
- i18n 双 locale → Task 1 ✓
- subagent/skill 检测规则 → Task 2 Step 1 `subagentCount`/`skillCount` ✓
- 边界（除零 → `—`，无 tool span 沿用空态，表无 tool span 隐藏）→ Task 2 `formatNullable*` + `v-if toolsUsed.length>0` ✓
- 不动后端/存储/API/类型 → 全计划无 Go 改动 ✓

**2. Placeholder scan:** 无 TBD/TODO；每步含完整代码与命令。✓

**3. Type consistency:** `toolSpans`/`successfulToolCalls`/`totalToolCalls` 跨 Task 2/3 复用一致；`formatDuration`/`formatTokens`/`formatRate`/`rateClass` 为既有 helper，新 `formatNullable*`/`formatRatePerSec` 在 Task 2 Step 2 定义、Step 3 引用；`toolsUsed` 在 Task 3 Step 1 定义、Step 2 引用。✓

无遗留问题。
