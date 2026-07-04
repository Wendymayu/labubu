<template>
  <div class="context-chart">
    <h3 class="chart-title">{{ t('traceDetail.contextTitle') }}</h3>

    <!-- Session selector: each agent invocation is an independent context trajectory -->
    <div v-if="sessions.length > 1" class="session-selector" role="tablist">
      <button
        v-for="s in sessionOptions"
        :key="s.id"
        class="session-chip"
        :class="{ active: s.id === selectedId }"
        role="tab"
        :aria-selected="s.id === selectedId"
        @click="selectedId = s.id"
      >
        <span class="session-chip-name">{{ s.label }}</span>
        <span class="session-chip-count">{{ t('traceDetail.contextSessionCalls', { n: s.count }) }}</span>
      </button>
    </div>
    <div v-else-if="sessions.length === 1" class="session-single">
      {{ sessionOptions[0].label }} · {{ t('traceDetail.contextSessionCalls', { n: sessionOptions[0].count }) }}
    </div>

    <div v-if="points.length < 2" class="empty-state">
      {{ t('traceDetail.contextEmpty') }}
    </div>

    <div v-else class="chart-container">
      <canvas ref="canvasRef"></canvas>
    </div>

    <!-- Legend (4 segments) -->
    <div v-if="points.length >= 2" class="segment-legend">
      <div v-for="seg in legend" :key="seg.key" class="legend-item">
        <span class="legend-dot" :style="{ background: seg.color }"></span>
        <span class="legend-label">{{ seg.label }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart, BarController, BarElement, CategoryScale, LinearScale, Tooltip,
} from 'chart.js'
import type { ContextPoint, ContextSession } from '../api/client'
import { useTheme } from '../composables/useTheme'

Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip)

const props = defineProps<{ sessions: ContextSession[] }>()
const emit = defineEmits<{ (e: 'select', spanId: string): void }>()

const { t } = useI18n()
const { theme } = useTheme()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart<'bar'> | null = null

// --- Session selection ---
// Default to the main session; otherwise the first. Reset when the set of
// session ids changes (e.g. navigating to another trace).
const selectedId = ref<string>('')
const knownIds = computed(() => props.sessions.map(s => s.id).join('|'))
watch(knownIds, () => {
  const main = props.sessions.find(s => s.isMain)
  selectedId.value = (main ?? props.sessions[0])?.id ?? ''
}, { immediate: true })

// Display labels: main session is "Main · <agent>", subagents are "<agent> #<n>"
// where n is the 1-based index among non-main sessions (disambiguates repeats).
const sessionOptions = computed(() => {
  let subIdx = 0
  return props.sessions.map(s => {
    let label: string
    if (s.isMain) {
      label = s.agentName ? `${t('traceDetail.contextSessionMain')} · ${s.agentName}` : t('traceDetail.contextSessionMain')
    } else {
      subIdx++
      label = s.agentName ? `${s.agentName} #${subIdx}` : `#${subIdx}`
    }
    return { id: s.id, label, count: s.points.length }
  })
})

const currentSession = computed(() =>
  props.sessions.find(s => s.id === selectedId.value) ?? props.sessions[0]
)
const points = computed(() => currentSession.value?.points ?? [])

// --- Theme-aware colors (reuse existing --chart-pie-* vars) ---
function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

interface SegmentMeta { key: string; label: string; varName: string }

const SEGMENTS: SegmentMeta[] = [
  { key: 'input',         label: 'ctxInput',         varName: '--chart-pie-user' },
  { key: 'cacheRead',     label: 'ctxCacheRead',     varName: '--chart-pie-tool' },
  { key: 'cacheCreation', label: 'ctxCacheCreation', varName: '--chart-pie-assistant' },
  { key: 'output',        label: 'ctxOutput',        varName: '--chart-pie-output' },
]

const legend = computed(() =>
  SEGMENTS.map(s => ({ key: s.key, label: t(`traceDetail.${s.label}`), color: getCSSVar(s.varName) }))
)

function bucketValue(p: ContextPoint, key: string): number {
  if (key === 'input') return p.input
  if (key === 'cacheRead') return p.cacheRead
  if (key === 'cacheCreation') return p.cacheCreation
  return p.output
}

function createChart() {
  if (chart) {
    chart.destroy()
    chart = null
  }

  if (!canvasRef.value || points.value.length < 2) return

  const labels = points.value.map(p => String(p.index))
  const borderColor = getCSSVar('--chart-pie-border')
  const tooltipBg = getCSSVar('--bg-secondary')
  const tooltipTitleColor = getCSSVar('--text-primary')
  const tooltipBodyColor = getCSSVar('--text-secondary')
  const tooltipBorderColor = getCSSVar('--border-group')

  const datasets = SEGMENTS.map(s => ({
    label: t(`traceDetail.${s.label}`),
    data: points.value.map(p => bucketValue(p, s.key)),
    backgroundColor: getCSSVar(s.varName),
    borderColor,
    borderWidth: 1,
    stack: 'tokens',
  }))

  // Capture for click handler closure.
  const pointsSnapshot = points.value

  // 内联插件：每根柱顶绘制上下文使用率百分比（仅当该模型配置了 context_window）
  const usageLabelPlugin = {
    id: 'usageLabel',
    afterDatasetsDraw(chart: any) {
      const meta = chart.getDatasetMeta(0)
      if (!meta || !meta.data) return
      const yScale = chart.scales.y
      const ctx2 = chart.ctx
      ctx2.save()
      ctx2.fillStyle = getCSSVar('--text-primary')
      ctx2.font = '600 11px sans-serif'
      ctx2.textAlign = 'center'
      ctx2.textBaseline = 'bottom'
      for (let i = 0; i < pointsSnapshot.length; i++) {
        const p = pointsSnapshot[i]
        if (p.usagePct == null) continue
        const bar = meta.data[i]
        if (!bar) continue
        const total = p.input + p.cacheRead + p.cacheCreation + p.output
        const y = yScale.getPixelForValue(total)
        ctx2.fillText(`${Math.round(p.usagePct * 100)}%`, bar.x, y - 4)
      }
      ctx2.restore()
    },
  }

  chart = new Chart(canvasRef.value, {
    type: 'bar',
    data: { labels, datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: tooltipBg,
          titleColor: tooltipTitleColor,
          bodyColor: tooltipBodyColor,
          borderColor: tooltipBorderColor,
          borderWidth: 1,
          padding: 12,
          cornerRadius: 6,
          callbacks: {
            title: (items: any[]) => {
              const idx = items[0]?.dataIndex ?? 0
              const p = pointsSnapshot[idx]
              if (!p) return ''
              return p.spanName + (p.model ? ` (${p.model})` : '')
            },
            label: (ctx: any) => {
              const p = pointsSnapshot[ctx.dataIndex]
              if (!p) return ''
              const key = SEGMENTS[ctx.datasetIndex].key
              const val = bucketValue(p, key).toLocaleString()
              return ` ${t(`traceDetail.${SEGMENTS[ctx.datasetIndex].label}`)}: ${val}`
            },
            footer: (items: any[]) => {
              const idx = items[0]?.dataIndex ?? 0
              const p = pointsSnapshot[idx]
              if (!p) return ''
              const total = (p.input + p.cacheRead + p.cacheCreation + p.output).toLocaleString()
              if (p.usagePct != null && p.contextWindow) {
                return `${t('traceDetail.ctxTotal')}: ${total}  ·  ${Math.round(p.usagePct * 100)}% / ${p.contextWindow.toLocaleString()}`
              }
              return `${t('traceDetail.ctxTotal')}: ${total}`
            },
          },
        },
      },
      scales: {
        x: { stacked: true, grid: { display: false }, ticks: { color: getCSSVar('--text-secondary') } },
        y: { stacked: true, beginAtZero: true, grid: { color: getCSSVar('--border-group') }, ticks: { color: getCSSVar('--text-secondary') } },
      },
      onClick: (_evt: any, elements: any[]) => {
        if (elements.length > 0) {
          const el = elements[0]
          const p = pointsSnapshot[el.index]
          if (p) emit('select', p.spanId)
        }
      },
      animation: { duration: 400 },
    },
    plugins: [usageLabelPlugin],
  })
}

watch(points, () => { requestAnimationFrame(createChart) }, { deep: true })
watch(theme, () => { requestAnimationFrame(createChart) })

onMounted(() => { requestAnimationFrame(createChart) })
onBeforeUnmount(() => {
  if (chart) { chart.destroy(); chart = null }
})
</script>

<style scoped>
.context-chart {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
}
.chart-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 12px;
}
.session-selector {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}
.session-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}
.session-chip:hover {
  border-color: var(--accent-blue, #3b82f6);
  color: var(--text-primary);
}
.session-chip.active {
  background: var(--accent-blue, #3b82f6);
  border-color: var(--accent-blue, #3b82f6);
  color: #fff;
}
.session-chip-name { font-weight: 600; }
.session-chip-count { opacity: 0.8; }
.session-single {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 14px;
}
.chart-container {
  position: relative;
  width: 100%;
  height: 320px;
}
.empty-state {
  color: var(--text-secondary);
  font-size: 13px;
  padding: 24px 0;
  text-align: center;
}
.segment-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--border-default);
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  flex-shrink: 0;
}
</style>
