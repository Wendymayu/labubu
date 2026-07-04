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
import { useTheme } from '../composables/useTheme'

Chart.register(DoughnutController, ArcElement, Tooltip)

const props = defineProps<{
  items: PieSlice[]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart<'doughnut'> | null = null

// --- Theme-aware colors ---
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function getColors(): Record<string, string> {
  return {
    system:           getCSSVar('--chart-pie-system'),
    assistant:        getCSSVar('--chart-pie-assistant'),
    user:             getCSSVar('--chart-pie-user'),
    tool:             getCSSVar('--chart-pie-tool'),
    tool_definitions: getCSSVar('--chart-pie-tool-defs'),
    skill:            getCSSVar('--chart-pie-skill'),
    output:           getCSSVar('--chart-pie-output'),
  }
}

const { theme } = useTheme()

const KEY_PATTERNS: { patterns: string[]; key: string; label: string }[] = [
  { patterns: ['gen_ai.context.system_prompt',       'system_prompt_tokens'],  key: 'system',           label: 'System' },
  { patterns: ['gen_ai.context.assistant_messages',  'assistant_messages_tokens'], key: 'assistant',        label: 'Assistant History' },
  { patterns: ['gen_ai.context.user_messages',       'user_messages_tokens'],  key: 'user',             label: 'User' },
  { patterns: ['gen_ai.context.tool_results',        'tool_results_tokens'],   key: 'tool',             label: 'Tool Results' },
  { patterns: ['gen_ai.context.tool_definitions',    'tool_definitions_tokens'], key: 'tool_definitions', label: 'Tool Definitions' },
  { patterns: ['gen_ai.context.skill',               'skill_tokens'],          key: 'skill',            label: 'Skill' },
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
      out.push({ key, label, value, color: getColors()[key] || getCSSVar('--chart-internal'), pct: '' })
    }
  }

  // Compute percentages based on input context tokens only.
  // Output tokens are shown in the summary below, not in the context window pie.
  const total = out.reduce((sum, s) => sum + s.value, 0)
  for (const s of out) {
    s.pct = total > 0 ? ((s.value / total) * 100).toFixed(1) : '0'
  }

  return out
})

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
  const borderColor = getCSSVar('--chart-pie-border')
  const tooltipBg = getCSSVar('--bg-secondary')
  const tooltipTitle = getCSSVar('--text-primary')
  const tooltipBody = getCSSVar('--text-secondary')
  const tooltipBorder = getCSSVar('--border-group')

  chart = new Chart(canvasRef.value, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{
        data,
        backgroundColor: colors,
        borderColor: borderColor,
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
          backgroundColor: tooltipBg,
          titleColor: tooltipTitle,
          bodyColor: tooltipBody,
          borderColor: tooltipBorder,
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

watch(theme, () => {
  requestAnimationFrame(createChart)
})

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
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 12px;
}
.chart-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 12px;
}
.chart-container {
  position: relative;
  width: 100%;
  max-width: 240px;
  margin: 0 auto;
}

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
  color: var(--text-secondary);
}
.legend-value {
  font-weight: 600;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.legend-pct {
  color: var(--text-secondary);
  font-size: 11px;
  width: 40px;
  text-align: right;
}
</style>
