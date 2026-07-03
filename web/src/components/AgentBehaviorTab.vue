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
      </section>

      <!-- Tool dimension -->
      <section v-if="hasToolCalls" class="dim-section">
        <h4 class="dim-title">🔧 {{ t('agentStats.dimTool') }}</h4>
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

      <!-- Skill & Subagent dimensions share a row -->
      <div class="dim-pair">
        <section class="dim-section">
          <h4 class="dim-title">🧩 {{ t('agentStats.dimSkill') }}</h4>
          <div class="score-cards">
            <div class="score-card rate-green">
              <div class="score-value">{{ skillCount }}</div>
              <div class="score-label">{{ t('agentStats.skillsUsed') }}</div>
              <div class="score-subtitle">
                <template v-if="skillCount > 0">{{ skillCount }} skill(s)</template>
                <template v-else>none</template>
              </div>
            </div>
          </div>
        </section>

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
          </div>
        </section>
      </div>
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

const successfulToolCalls = computed(() =>
  toolSpans.value.filter(s => s.status === 'ok').length
)

// --- LLM spans ---
// LLM 调用：gen_ai_system 已设置且非 tool 调用

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
        const args = JSON.parse(s.attributes?.['gen_ai.tool.arguments'] ?? '{}') as ParsedToolArgs
        name = args?.name ?? args?.skill ?? args?.skill_name ?? ''
      } catch {
        name = ''
      }
    }
    if (name) names.add(name)
  }
  return names.size > 0 ? names.size : invocations
})

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
