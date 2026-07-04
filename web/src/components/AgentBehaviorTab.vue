<template>
  <div class="agent-behavior-tab">
    <!-- State 1: Empty — no agent behavior data at all -->
    <div v-if="!hasAnyData" class="empty-state">
      <div class="empty-icon">🔧</div>
      <p>{{ t('agentStats.noAgentData') }}</p>
    </div>

    <!-- State 2-4: Content (has LLM or tool activity) -->
    <div v-else class="behavior-content">
      <!-- LLM dimension -->
      <section v-if="totalLLMCalls > 0" class="dim-section">
        <h4 class="dim-title">🤖 {{ t('agentStats.dimLlm') }}</h4>
        <div class="score-cards">
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
            <div class="score-value">{{ formatNullableDuration(maxLLMDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.maxLlmDuration') }}</div>
            <div class="score-subtitle">{{ totalLLMCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(minLLMDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.minLlmDuration') }}</div>
            <div class="score-subtitle">{{ totalLLMCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(avgFirstTokenMs) }}</div>
            <div class="score-label">{{ t('agentStats.avgFirstToken') }}</div>
            <div class="score-subtitle">{{ llmFirstTokens.length }}/{{ totalLLMCalls }} samples</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(maxFirstTokenMs) }}</div>
            <div class="score-label">{{ t('agentStats.maxFirstToken') }}</div>
            <div class="score-subtitle">{{ llmFirstTokens.length }}/{{ totalLLMCalls }} samples</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(minFirstTokenMs) }}</div>
            <div class="score-label">{{ t('agentStats.minFirstToken') }}</div>
            <div class="score-subtitle">{{ llmFirstTokens.length }}/{{ totalLLMCalls }} samples</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableTokens(avgLLMTokens) }}</div>
            <div class="score-label">{{ t('agentStats.avgLlmTokens') }}</div>
            <div class="score-subtitle">{{ formatTokens(llmInputTokens) }} in + {{ formatTokens(llmOutputTokens) }} out</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableTokens(maxLLMTokens) }}</div>
            <div class="score-label">{{ t('agentStats.maxLlmTokens') }}</div>
            <div class="score-subtitle">per call max</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatRatePerSec(tokenOutputRate) }}</div>
            <div class="score-label">{{ t('agentStats.tokenOutputRate') }}</div>
            <div class="score-subtitle">{{ formatTokens(llmOutputTokens) }} out / {{ formatNullableDuration(avgLLMDurationMs) }}</div>
          </div>
        </div>
      </section>

      <!-- Tool dimension -->
      <section v-if="hasToolCalls" class="dim-section">
        <h4 class="dim-title">🔧 {{ t('agentStats.dimTool') }}</h4>
        <div class="score-cards">
          <div class="score-card rate-green">
            <div class="score-value">{{ distinctToolCount }}</div>
            <div class="score-label">{{ t('agentStats.toolCount') }}</div>
            <div class="score-subtitle">{{ totalToolCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ totalToolCalls }}</div>
            <div class="score-label">{{ t('agentStats.callCount') }}</div>
            <div class="score-subtitle">{{ successfulToolCalls }}/{{ totalToolCalls }} succeeded</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(avgToolDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.avgDuration') }}</div>
            <div class="score-subtitle">{{ totalToolCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(maxToolDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.maxDuration') }}</div>
            <div class="score-subtitle">{{ maxDurationToolName || '—' }}</div>
          </div>
        </div>

        <!-- Tools Used summary -->
        <div v-if="toolsUsed.length > 0" class="tools-used-section">
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

        <!-- Loop Warning -->
        <div v-if="maxLoopDepth >= 3" class="loop-warning">
          <div class="loop-warning-title">{{ t('agentStats.loopWarning', { tool: loopToolName, count: maxLoopDepth }) }}</div>
          <div class="loop-warning-desc">{{ t('agentStats.loopWarningDesc', { tool: loopToolName, count: maxLoopDepth }) }}</div>
        </div>
      </section>

      <!-- Skill dimension -->
      <section v-if="skillCount > 0" class="dim-section">
        <h4 class="dim-title">🧩 {{ t('agentStats.dimSkill') }}</h4>
        <div class="score-cards">
          <div class="score-card rate-green">
            <div class="score-value">{{ skillCount }}</div>
            <div class="score-label">{{ t('agentStats.skillsUsed') }}</div>
            <div class="score-subtitle">{{ skillTotalCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ skillTotalCalls }}</div>
            <div class="score-label">{{ t('agentStats.callCount') }}</div>
            <div class="score-subtitle">{{ skillSuccessfulCalls }}/{{ skillTotalCalls }} succeeded</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(skillAvgDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.avgDuration') }}</div>
            <div class="score-subtitle">{{ skillTotalCalls }} calls</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(maxSkillDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.maxDuration') }}</div>
            <div class="score-subtitle">{{ maxDurationSkillName || '—' }}</div>
          </div>
        </div>

        <!-- Skills Used summary -->
        <div v-if="skillsUsed.length > 0" class="tools-used-section">
          <table class="tools-used-table">
            <thead>
              <tr>
                <th>{{ t('agentStats.dimSkill') }}</th>
                <th class="num">{{ t('agentStats.callCount') }}</th>
                <th class="num">{{ t('agentStats.successRateCol') }}</th>
                <th class="num">{{ t('agentStats.avgDuration') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in skillsUsed" :key="i">
                <td class="tool-name">{{ row.name }}</td>
                <td class="num">{{ row.calls }}</td>
                <td class="num" :class="rateClass(row.successRate)">{{ formatRate(row.successRate) }}</td>
                <td class="num">{{ formatDuration(row.avgDuration) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Subagent dimension -->
      <section class="dim-section">
        <h4 class="dim-title">🧬 {{ t('agentStats.dimSubagent') }}</h4>
        <div class="score-cards">
          <div class="score-card" :class="subagentCount > 0 ? 'rate-yellow' : 'rate-green'">
            <div class="score-value">{{ subagentCount }}</div>
            <div class="score-label">{{ t('agentStats.subagentCount') }}</div>
            <div class="score-subtitle">
              <template v-if="subagentCount > 0">{{ subagentCount }} nested agent(s)</template>
              <template v-else>root only</template>
            </div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ subagentInvocationCount }}</div>
            <div class="score-label">{{ t('agentStats.callCount') }}</div>
            <div class="score-subtitle">{{ successfulSubagentCalls }}/{{ subagentInvocationCount }} succeeded</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(avgSubagentDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.avgDuration') }}</div>
            <div class="score-subtitle">{{ subagentInvocationCount }} invocations</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(maxSubagentDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.maxDuration') }}</div>
            <div class="score-subtitle">{{ subagentInvocationCount }} invocations</div>
          </div>

          <div class="score-card rate-green">
            <div class="score-value">{{ formatNullableDuration(minSubagentDurationMs) }}</div>
            <div class="score-label">{{ t('agentStats.minDuration') }}</div>
            <div class="score-subtitle">{{ subagentInvocationCount }} invocations</div>
          </div>
        </div>

        <!-- Subagents Used summary -->
        <div v-if="subagentsUsed.length > 0" class="tools-used-section">
          <table class="tools-used-table">
            <thead>
              <tr>
                <th>{{ t('agentStats.dimSubagent') }}</th>
                <th class="num">{{ t('agentStats.callCount') }}</th>
                <th class="num">{{ t('agentStats.successRateCol') }}</th>
                <th class="num">{{ t('agentStats.avgDuration') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in subagentsUsed" :key="i">
                <td class="tool-name">{{ row.name }}</td>
                <td class="num">{{ row.calls }}</td>
                <td class="num" :class="rateClass(row.successRate)">{{ formatRate(row.successRate) }}</td>
                <td class="num">{{ formatDuration(row.avgDuration) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SpanDetail as SpanDetailType } from '../api/client'

interface ParsedToolArgs { name?: string; skill?: string; skill_name?: string }

const { t } = useI18n()

const props = defineProps<{
  spans: SpanDetailType[]
}>()

// --- Core computed properties ---

const toolSpans = computed(() =>
  props.spans.filter(s => s.is_tool_call === true)
)

const hasToolCalls = computed(() => toolSpans.value.length > 0)

// 整体空状态门槛：既无 LLM 调用也无工具调用时才显示空态
const hasAnyData = computed(() => totalLLMCalls.value > 0 || hasToolCalls.value)

const totalToolCalls = computed(() => toolSpans.value.length)

// 去重工具数（按工具名聚合后的种类数）
const distinctToolCount = computed(() => toolsUsed.value.length)

const successfulToolCalls = computed(() =>
  toolSpans.value.filter(s => isStatusOk(s)).length
)

// --- LLM spans ---
// LLM 调用：gen_ai_system 已设置且非 tool 调用
// 注意 gen_ai_system 可能为 null（根 agent span 带有 gen_ai.* 属性但没有 gen_ai.system），
// 需用真值判断排除 null/undefined/空串，否则会把根 span 误算成一次模型调用。

const llmSpans = computed(() =>
  props.spans.filter(s => !!s.gen_ai_system && !s.is_tool_call)
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

const maxToolDurationMs = computed(() =>
  totalToolCalls.value > 0
    ? Math.max(...toolSpans.value.map(s => s.duration_ms))
    : null
)

const maxDurationToolName = computed(() => {
  if (totalToolCalls.value === 0) return ''
  let best = toolSpans.value[0]
  for (const s of toolSpans.value) {
    if (s.duration_ms > best.duration_ms) best = s
  }
  return best.tool_name ?? best.name ?? ''
})

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

// --- LLM 耗时极值 / 首 token 耗时 ---
// 首 token 时间取自 gen_ai.streaming.first_token_ms 属性（浮点字符串，毫秒）。

function firstTokenMs(s: SpanDetailType): number | null {
  const v = s.attributes?.['gen_ai.streaming.first_token_ms']
  if (!v) return null
  const n = parseFloat(v)
  return Number.isFinite(n) ? n : null
}

const llmDurations = computed(() => llmSpans.value.map(s => s.duration_ms))

const maxLLMDurationMs = computed(() =>
  llmDurations.value.length ? Math.max(...llmDurations.value) : null
)

const minLLMDurationMs = computed(() =>
  llmDurations.value.length ? Math.min(...llmDurations.value) : null
)

const llmFirstTokens = computed(() =>
  llmSpans.value.map(firstTokenMs).filter((v): v is number => v != null)
)

const avgFirstTokenMs = computed(() =>
  llmFirstTokens.value.length
    ? llmFirstTokens.value.reduce((sum, v) => sum + v, 0) / llmFirstTokens.value.length
    : null
)

const maxFirstTokenMs = computed(() =>
  llmFirstTokens.value.length ? Math.max(...llmFirstTokens.value) : null
)

const minFirstTokenMs = computed(() =>
  llmFirstTokens.value.length ? Math.min(...llmFirstTokens.value) : null
)

const maxLLMTokens = computed(() => {
  const toks = llmSpans.value.map(s => s.total_tokens ?? 0)
  return toks.length ? Math.max(...toks) : null
})

// --- subagent / skill 计数 ---

// --- 子智能体 ---
// 子智能体调用 = span 名包含 subagent.invoke 的 span（主 agent 通过 subagent 机制派发的调用）。
// 一个子智能体名可被多次调用；个数按去重 agent 名计，调用次数按 span 数计，
// 耗时取每次调用 span 的 duration_ms。

const subagentInvokeSpans = computed(() =>
  props.spans.filter(s => (s.name ?? '').includes('subagent.invoke'))
)

const subagentCount = computed(() => {
  const names = new Set<string>()
  for (const s of subagentInvokeSpans.value) {
    const n = s.attributes?.['gen_ai.agent.name']
    if (n) names.add(n)
  }
  return names.size
})

const subagentInvocationCount = computed(() => subagentInvokeSpans.value.length)

const successfulSubagentCalls = computed(() =>
  subagentInvokeSpans.value.filter(s => isStatusOk(s)).length
)

const subagentDurations = computed(() => subagentInvokeSpans.value.map(s => s.duration_ms))

const avgSubagentDurationMs = computed(() =>
  subagentInvocationCount.value > 0
    ? subagentDurations.value.reduce((sum, v) => sum + v, 0) / subagentInvocationCount.value
    : null
)

const maxSubagentDurationMs = computed(() =>
  subagentInvocationCount.value > 0 ? Math.max(...subagentDurations.value) : null
)

const minSubagentDurationMs = computed(() =>
  subagentInvocationCount.value > 0 ? Math.min(...subagentDurations.value) : null
)

// --- 子智能体汇总（按 agent 名聚合，调用次数降序） ---

interface SubagentRow { name: string; calls: number; ok: number; totalDur: number }

const subagentsUsed = computed(() => {
  const map: Record<string, SubagentRow> = {}
  for (const s of subagentInvokeSpans.value) {
    const name = s.attributes?.['gen_ai.agent.name'] ?? '(unnamed)'
    if (!map[name]) map[name] = { name, calls: 0, ok: 0, totalDur: 0 }
    map[name].calls++
    if (isStatusOk(s)) map[name].ok++
    map[name].totalDur += s.duration_ms
  }
  return Object.values(map)
    .map(r => ({
      name: r.name,
      calls: r.calls,
      successRate: r.calls > 0 ? r.ok / r.calls : 0,
      avgDuration: r.calls > 0 ? r.totalDur / r.calls : 0,
    }))
    .sort((a, b) => b.calls - a.calls)
})

// --- skills used (按技能名聚合，调用次数降序) ---
// Skill 调用：tool_name 为 skill/use_skill，或带 skill.name / gen_ai.skill.name 属性

function isSkillSpan(s: SpanDetailType): boolean {
  const tn = (s.tool_name ?? '').toLowerCase()
  if (tn === 'skill' || tn === 'use_skill') return true
  return !!(s.attributes?.['skill.name'] ?? s.attributes?.['gen_ai.skill.name'])
}

function skillNameOf(s: SpanDetailType): string {
  const skillAttr = s.attributes?.['skill.name'] ?? s.attributes?.['gen_ai.skill.name']
  if (skillAttr) return skillAttr
  const tn = (s.tool_name ?? '').toLowerCase()
  if (tn === 'skill' || tn === 'use_skill') {
    try {
      const args = JSON.parse(s.attributes?.['gen_ai.tool.arguments'] ?? '{}') as ParsedToolArgs
      const n = args?.name ?? args?.skill ?? args?.skill_name
      if (n) return n
    } catch {
      /* ignore */
    }
  }
  return '(unnamed)'
}

interface SkillRow { name: string; calls: number; ok: number; totalDur: number }

const skillSpans = computed(() => toolSpans.value.filter(isSkillSpan))

const skillsUsed = computed(() => {
  const map: Record<string, SkillRow> = {}
  for (const s of skillSpans.value) {
    const name = skillNameOf(s)
    if (!map[name]) map[name] = { name, calls: 0, ok: 0, totalDur: 0 }
    map[name].calls++
    if (isStatusOk(s)) map[name].ok++
    map[name].totalDur += s.duration_ms
  }
  return Object.values(map)
    .map(r => ({
      name: r.name,
      calls: r.calls,
      ok: r.ok,
      successRate: r.calls > 0 ? r.ok / r.calls : 0,
      avgDuration: r.calls > 0 ? r.totalDur / r.calls : 0,
    }))
    .sort((a, b) => b.calls - a.calls)
})

const maxSkillDurationMs = computed(() =>
  skillSpans.value.length ? Math.max(...skillSpans.value.map(s => s.duration_ms)) : null
)

const maxDurationSkillName = computed(() => {
  if (!skillSpans.value.length) return ''
  let best = skillSpans.value[0]
  for (const s of skillSpans.value) {
    if (s.duration_ms > best.duration_ms) best = s
  }
  return skillNameOf(best)
})

const skillCount = computed(() => skillsUsed.value.length)

const skillTotalCalls = computed(() =>
  skillsUsed.value.reduce((sum, r) => sum + r.calls, 0)
)

const skillSuccessfulCalls = computed(() =>
  skillsUsed.value.reduce((sum, r) => sum + r.ok, 0)
)

const skillAvgDurationMs = computed(() =>
  skillTotalCalls.value > 0
    ? skillsUsed.value.reduce((sum, r) => sum + r.avgDuration * r.calls, 0) / skillTotalCalls.value
    : null
)

// --- 已用工具汇总（按工具名聚合，调用次数降序） ---

const toolsUsed = computed(() => {
  const map: Record<string, { name: string; calls: number; ok: number; totalDur: number }> = {}
  for (const s of toolSpans.value) {
    const name = s.tool_name ?? s.name
    if (!map[name]) map[name] = { name, calls: 0, ok: 0, totalDur: 0 }
    map[name].calls++
    if (isStatusOk(s)) map[name].ok++
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

const llmModelLabel = computed(() => {
  for (const s of llmSpans.value) {
    if (s.gen_ai_request_model) return s.gen_ai_request_model
  }
  return ''
})

// --- Max loop depth computation ---
// Spans are already time-sorted from API, so we scan sequentially

const maxLoopDepth = computed(() => {
  if (toolSpans.value.length === 0) return 0
  let max = 1
  let current = 1
  for (let i = 1; i < toolSpans.value.length; i++) {
    if (toolSpans.value[i].tool_name === toolSpans.value[i - 1].tool_name) {
      current++
      if (current > max) max = current
    } else {
      current = 1
    }
  }
  return max
})

const loopToolName = computed(() => {
  if (maxLoopDepth.value <= 1) return ''
  let maxDepth = 1
  let currentDepth = 1
  let resultName = ''
  for (let i = 1; i < toolSpans.value.length; i++) {
    if (toolSpans.value[i].tool_name === toolSpans.value[i - 1].tool_name) {
      currentDepth++
      if (currentDepth > maxDepth) {
        maxDepth = currentDepth
        resultName = toolSpans.value[i].tool_name ?? ''
      }
    } else {
      currentDepth = 1
    }
  }
  return resultName
})

// --- Formatting helpers ---

// OTLP 状态码为大写 "OK"/"ERROR"/"UNSET"；数据里 status 可能是任意大小写，
// 统一用大小写无关判断，避免把 "OK" 误判为失败。
function isStatusOk(s: SpanDetailType): boolean {
  return String(s.status ?? '').toLowerCase() === 'ok'
}

function formatRate(rate: number): string {
  return `${Math.round(rate * 100)}%`
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return String(tokens)
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.round(ms)}ms`
}

function rateClass(rate: number): string {
  if (rate >= 0.9) return 'rate-green'
  if (rate >= 0.7) return 'rate-yellow'
  return 'rate-red'
}

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
</script>

<style scoped>
.agent-behavior-tab {
  padding: 20px;
}

.empty-state {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.behavior-content {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* Dimension sections */
.dim-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dim-pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

.dim-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

/* Score cards */
.score-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
}

.score-card {
  text-align: center;
  padding: 16px 8px;
  border-radius: 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
}

.score-value {
  font-size: 32px;
  font-weight: 700;
}

.score-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.score-subtitle {
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 2px;
}

/* Rate color classes */
.rate-green .score-value { color: #22c55e; }
.rate-yellow .score-value { color: #eab308; }
.rate-red .score-value { color: #ef4444; }

.rate-green { border-left: 3px solid #22c55e; }
.rate-yellow { border-left: 3px solid #eab308; }
.rate-red { border-left: 3px solid #ef4444; }

/* Loop warning */
.loop-warning {
  background: #fef3c7;
  border: 1px solid #fbbf24;
  border-radius: 8px;
  padding: 14px 18px;
}

.loop-warning-title {
  font-size: 14px;
  font-weight: 600;
  color: #92400e;
}

.loop-warning-desc {
  font-size: 13px;
  color: #92400e;
  margin-top: 4px;
}

/* Tools used summary table */
.tools-used-section {
  overflow-x: auto;
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

.tools-used-table td.rate-green { color: #22c55e; border-left: none; }
.tools-used-table td.rate-yellow { color: #eab308; border-left: none; }
.tools-used-table td.rate-red { color: #ef4444; border-left: none; }
</style>
