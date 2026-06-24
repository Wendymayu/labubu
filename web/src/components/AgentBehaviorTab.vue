<template>
  <div class="agent-behavior-tab">
    <!-- State 1: Empty — no tool calls detected -->
    <div v-if="!hasToolCalls" class="empty-state">
      <div class="empty-icon">🔧</div>
      <p>{{ t('agentStats.noToolCalls') }}</p>
    </div>

    <!-- State 2-4: Content (has tool calls) -->
    <div v-else class="behavior-content">
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

      <!-- Tool Call Chain -->
      <div v-if="callChainItems.length > 0" class="call-chain-section">
        <h4>{{ t('agentStats.toolCallChain') }}</h4>
        <div class="call-chain">
          <div
            v-for="(item, i) in callChainItems"
            :key="i"
            class="chain-item"
            :class="{
              'chain-item-error': item.status === 'error',
              'chain-item-loop': item.isLoopGroup
            }"
          >
            <span class="chain-icon">{{ item.icon }}</span>
            <div class="chain-main">
              <span class="chain-name">{{ item.name }}</span>
              <span class="chain-attr-label">{{ item.attrLabel }}</span>
            </div>
            <div class="chain-retry-info">
              <template v-if="item.retryAnnotation">
                <span class="retry-annotation">{{ item.retryAnnotation }}</span>
              </template>
            </div>
            <span class="chain-meta">{{ item.meta }}</span>
            <span class="chain-badge" :class="item.status === 'ok' ? 'badge-ok' : 'badge-error'">
              {{ item.status === 'ok' ? 'OK' : 'ERROR' }}
            </span>
          </div>
        </div>
      </div>

      <!-- Loop Warning -->
      <div v-if="maxLoopDepth >= 3" class="loop-warning">
        <div class="loop-warning-title">{{ t('agentStats.loopWarning', { tool: loopToolName, count: maxLoopDepth }) }}</div>
        <div class="loop-warning-desc">{{ t('agentStats.loopWarningDesc', { tool: loopToolName, count: maxLoopDepth }) }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SpanDetail as SpanDetailType } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  spans: SpanDetailType[]
}>()

// --- Core computed properties ---

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

// --- Total retries computation ---
// For each tool group, count consecutive errors followed by a success as a retry pattern

const totalRetries = computed(() => {
  // Group spans by tool_name
  const groups: Record<string, SpanDetailType[]> = {}
  for (const s of toolSpans.value) {
    const key = s.tool_name ?? ''
    if (!groups[key]) groups[key] = []
    groups[key].push(s)
  }

  let retries = 0
  for (const spans of Object.values(groups)) {
    let consecutiveErrors = 0
    for (const s of spans) {
      if (s.status === 'error') {
        consecutiveErrors++
      } else if (s.status === 'ok' && consecutiveErrors > 0) {
        retries += consecutiveErrors
        consecutiveErrors = 0
      } else {
        consecutiveErrors = 0
      }
    }
  }
  return retries
})

// --- Token computation ---
// Sum from ALL spans (not just tool spans)

const inputTokens = computed(() =>
  props.spans.reduce((sum, s) => sum + (s.input_tokens ?? 0), 0)
)

const outputTokens = computed(() =>
  props.spans.reduce((sum, s) => sum + (s.output_tokens ?? 0), 0)
)

const totalTokens = computed(() =>
  props.spans.reduce((sum, s) => sum + (s.total_tokens ?? 0), 0)
)

// --- Call chain items ---
// Ordered list of tool calls and LLM calls with metadata

const callChainItems = computed(() => {
  const items: Array<{
    icon: string
    name: string
    attrLabel: string
    status: string
    meta: string
    retryAnnotation: string | null
    isLoopGroup: boolean
  }> = []

  // Track consecutive same-tool groups for loop border highlighting
  let prevToolName = ''
  let consecutiveCount = 0

  // Track retry annotations per tool group
  const toolRetryTracker: Record<string, { attempt: number; maxAttempt: number }> = {}

  // Pre-compute retry sequences per tool
  const toolGroups: Record<string, SpanDetailType[]> = {}
  for (const s of toolAndLLMSpans.value) {
    const key = s.is_tool_call ? (s.tool_name ?? '') : '__llm__'
    if (!toolGroups[key]) toolGroups[key] = []
    toolGroups[key].push(s)
  }

  // For each tool group, determine if there's a retry pattern (consecutive errors then ok)
  const retrySequences: Record<string, Array<{ start: number; total: number }>> = {}
  for (const [key, spans] of Object.entries(toolGroups)) {
    const sequences: Array<{ start: number; total: number }> = []
    let seqStart = -1
    let seqLen = 0
    for (let i = 0; i < spans.length; i++) {
      if (spans[i].status === 'error') {
        if (seqStart === -1) seqStart = i
        seqLen++
      } else if (spans[i].status === 'ok' && seqStart !== -1) {
        // errors followed by ok = retry sequence
        sequences.push({ start: seqStart, total: seqLen + 1 })
        seqStart = -1
        seqLen = 0
      } else {
        seqStart = -1
        seqLen = 0
      }
    }
    if (sequences.length > 0) retrySequences[key] = sequences
  }

  // Build per-span retry annotation lookup
  const spanRetryAnnotation: Record<string, string> = {}
  for (const [key, sequences] of Object.entries(retrySequences)) {
    const spans = toolGroups[key]
    for (const seq of sequences) {
      for (let i = seq.start; i < seq.start + seq.total; i++) {
        const span = spans[i]
        const attemptNum = i - seq.start + 1
        const annotation = span.status === 'error'
          ? `❌ attempt ${attemptNum}/${seq.total}`
          : `✅ attempt ${attemptNum}/${seq.total}`
        spanRetryAnnotation[span.span_id] = annotation
      }
    }
  }

  // Build chain items from tool+LLM spans
  for (const s of toolAndLLMSpans.value) {
    const isTool = s.is_tool_call
    const isLLM = s.gen_ai_system !== undefined && !s.is_tool_call

    // Loop detection: consecutive same tool_name
    const currentToolName = s.tool_name ?? ''
    if (isTool && currentToolName === prevToolName) {
      consecutiveCount++
    } else {
      consecutiveCount = 1
    }
    prevToolName = currentToolName

    const icon = isLLM ? '🤖' : '🔧'
    const name = isTool ? (s.tool_name ?? s.name) : (s.gen_ai_system ?? s.name)
    const attrLabel = isTool ? 'gen_ai.tool.name' : 'gen_ai.system'

    // Meta: duration for tools, tokens for LLM
    let meta = ''
    if (isTool) {
      meta = formatDuration(s.duration_ms)
    } else if (isLLM) {
      const tokens = s.total_tokens ?? (s.input_tokens ?? 0) + (s.output_tokens ?? 0)
      meta = formatTokens(tokens) + ' tokens'
    }

    items.push({
      icon,
      name,
      attrLabel,
      status: s.status || 'ok',
      meta,
      retryAnnotation: spanRetryAnnotation[s.span_id] ?? null,
      isLoopGroup: isTool && consecutiveCount >= 2
    })
  }

  return items
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

function loopClass(depth: number): string {
  if (depth < 3) return 'rate-green'
  if (depth <= 4) return 'rate-yellow'
  return 'rate-red'
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

/* Score cards */
.score-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
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

/* Call chain section */
.call-chain-section h4 {
  font-size: 14px;
  margin-bottom: 10px;
  color: var(--text-primary);
}

.call-chain {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.chain-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  font-size: 13px;
}

.chain-item-error {
  background: #fef2f2;
  border-left: 3px solid #ef4444;
}

.chain-item-loop {
  border-left: 3px solid #eab308;
}

.chain-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.chain-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.chain-name {
  font-weight: 600;
  color: var(--text-primary);
}

.chain-attr-label {
  font-size: 11px;
  color: var(--text-secondary);
}

.chain-retry-info {
  flex-shrink: 0;
}

.retry-annotation {
  font-size: 11px;
  color: var(--text-secondary);
}

.chain-meta {
  font-size: 12px;
  color: var(--text-secondary);
  margin-left: auto;
  flex-shrink: 0;
}

.chain-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  flex-shrink: 0;
}

.badge-ok {
  background: #dcfce7;
  color: #166534;
}

.badge-error {
  background: #fecaca;
  color: #991b1b;
}

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
</style>
