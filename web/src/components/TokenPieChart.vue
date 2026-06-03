<template>
  <div class="token-chart" v-if="segments.length > 0">
    <h3 class="chart-title">Context Window</h3>

    <div class="chart-container">
      <canvas ref="canvasRef"></canvas>
    </div>

    <!-- Legend -->
    <div class="token-legend">
      <div
        v-for="seg in segments"
        :key="seg.key"
        class="legend-item"
      >
        <span class="legend-dot" :style="{ background: seg.color }"></span>
        <span class="legend-label">{{ seg.label }}</span>
        <span class="legend-value">{{ seg.value.toLocaleString() }}</span>
        <span class="legend-pct">{{ seg.pct }}%</span>
      </div>
    </div>

    <!-- Summary stats -->
    <div class="token-summary">
      <div class="summary-item">
        <div class="s-label">Input Tokens</div>
        <div class="s-value purple">{{ inputTokens.toLocaleString() }}</div>
      </div>
      <div class="summary-item">
        <div class="s-label">Output Tokens</div>
        <div class="s-value green">{{ outputTokens.toLocaleString() }}</div>
      </div>
      <div class="summary-item">
        <div class="s-label">Total Tokens</div>
        <div class="s-value blue">{{ (inputTokens + outputTokens).toLocaleString() }}</div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
export interface PieSlice {
  name: string
  tokens: number
}
</script>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { Chart, DoughnutController, ArcElement, Tooltip } from 'chart.js'

Chart.register(DoughnutController, ArcElement, Tooltip)

const props = withDefaults(defineProps<{
  items: PieSlice[]
  inputTokens: number
  outputTokens: number
}>(), {
  inputTokens: 0,
  outputTokens: 0,
})

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart<'doughnut'> | null = null

// --- Colors (matching reference) ---
const COLORS: Record<string, string> = {
  system:            '#8b5cf6',
  assistant:         '#ec4899',
  user:              '#3b82f6',
  tool:              '#06b6d4',
  tool_definitions:  '#f59e0b',
  skill:             '#10b981',
  output:            '#ef4444',
}

const KEY_PATTERNS: { pattern: string; key: string; label: string }[] = [
  { pattern: 'gen_ai.context.system_tokens',           key: 'system',           label: 'System' },
  { pattern: 'gen_ai.context.assistant_tokens',        key: 'assistant',        label: 'Assistant History' },
  { pattern: 'gen_ai.context.user_tokens',             key: 'user',             label: 'User' },
  { pattern: 'gen_ai.context.tool_tokens',             key: 'tool',             label: 'Tool Results' },
  { pattern: 'gen_ai.context.tool_definitions_tokens', key: 'tool_definitions', label: 'Tool Definitions' },
  { pattern: 'gen_ai.context.skill_tokens',            key: 'skill',            label: 'Skill' },
]

interface Segment {
  key: string
  label: string
  value: number
  color: string
  pct: string
}

const segments = computed<Segment[]>(() => {
  const out: Segment[] = []

  for (const { key, label } of KEY_PATTERNS) {
    const found = props.items.find(s => s.name === label)
    const value = found ? found.tokens : 0
    if (value > 0) {
      out.push({ key, label, value, color: COLORS[key], pct: '' })
    }
  }

  // Also add output as a separate segment if present.
  const outputFound = props.items.find(s => s.name === 'Output (completion)')
  const outputVal = outputFound ? outputFound.tokens : props.outputTokens
  if (outputVal > 0 && !out.find(s => s.key === 'output')) {
    out.push({ key: 'output', label: 'Output Tokens', value: outputVal, color: COLORS['output'], pct: '' })
  }

  // Compute percentages.
  const total = out.reduce((sum, s) => sum + s.value, 0)
  for (const s of out) {
    s.pct = total > 0 ? ((s.value / total) * 100).toFixed(1) : '0'
  }

  return out
})

const inputTokens = computed(() => props.inputTokens)
const outputTokens = computed(() => props.outputTokens)

// --- Chart.js rendering ---
function createChart() {
  if (!canvasRef.value || segments.value.length === 0) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  const labels = segments.value.map(s => s.label)
  const data = segments.value.map(s => s.value)
  const colors = segments.value.map(s => s.color)

  chart = new Chart(canvasRef.value, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{
        data,
        backgroundColor: colors,
        borderColor: '#1a1d27',
        borderWidth: 2,
        hoverOffset: 6,
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: true,
      cutout: '55%',
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: '#1a1d27',
          titleColor: '#e4e4e7',
          bodyColor: '#9ca3af',
          borderColor: '#2d3140',
          borderWidth: 1,
          padding: 12,
          cornerRadius: 6,
          callbacks: {
            label: (ctx: any) => {
              const total = ctx.dataset.data.reduce((a: number, b: number) => a + b, 0)
              const pct = ((ctx.parsed / total) * 100).toFixed(1)
              return ` ${ctx.label}: ${ctx.parsed.toLocaleString()} (${pct}%)`
            }
          }
        }
      },
      animation: {
        animateRotate: true,
        duration: 600,
      }
    }
  })
}

watch(segments, () => {
  requestAnimationFrame(createChart)
}, { deep: true })

onMounted(() => {
  requestAnimationFrame(createChart)
})

onUnmounted(() => {
  if (chart) {
    chart.destroy()
    chart = null
  }
})
</script>

<style scoped>
.token-chart {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 12px;
}
.chart-title {
  font-size: 14px;
  font-weight: 600;
  color: #e2e8f0;
  margin-bottom: 12px;
}
.chart-container {
  position: relative;
  width: 100%;
  max-width: 240px;
  margin: 0 auto;
}

/* Legend */
.token-legend {
  margin-top: 14px;
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}
.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.legend-label {
  flex: 1;
  color: #94a3b8;
}
.legend-value {
  font-weight: 600;
  color: #e2e8f0;
  font-variant-numeric: tabular-nums;
}
.legend-pct {
  color: #64748b;
  font-size: 11px;
  width: 40px;
  text-align: right;
}

/* Summary stats */
.token-summary {
  display: flex;
  gap: 10px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid #334155;
}
.summary-item {
  flex: 1;
  text-align: center;
}
.s-label {
  font-size: 10px;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.s-value {
  font-size: 16px;
  font-weight: 700;
  margin-top: 2px;
  font-variant-numeric: tabular-nums;
}
.s-value.purple { color: #c4b5fd; }
.s-value.green { color: #6ee7b7; }
.s-value.blue { color: #60a5fa; }
</style>
