<template>
  <div class="panel-chart">
    <div class="panel-header">
      <h3 class="panel-title">{{ panel.title }}</h3>
      <div class="panel-actions">
        <button class="btn-icon" title="Edit" @click="$emit('edit', panel)">&#9998;</button>
        <button class="btn-icon" title="Delete" @click="$emit('delete', panel)">&#10005;</button>
      </div>
    </div>
    <div class="panel-body">
      <div v-if="loading" class="panel-state">Loading...</div>
      <div v-else-if="error" class="panel-state panel-error">{{ error }}</div>
      <div v-else-if="noData" class="panel-state">No data</div>
      <div v-else-if="panel.chartType === 'stat'" class="stat-value">
        <span class="stat-number">{{ formatValue(statValue) }}</span>
        <span class="stat-metric">{{ panel.metric }}</span>
      </div>
      <canvas v-else ref="canvasRef"></canvas>
      <div
        v-if="chartDatasets.length > 1"
        class="chart-legend"
      >
        <div v-for="(ds, idx) in chartDatasets" :key="idx" class="legend-item">
          <span class="legend-color" :style="{ background: ds.borderColor }"></span>
          <span class="legend-label">{{ ds.label }}</span>
        </div>
      </div>
      <div ref="tooltipRef" class="chart-tooltip"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import {
  Chart, LineController, BarController, CategoryScale, LinearScale,
  PointElement, LineElement, BarElement, Tooltip, Legend, Filler
} from 'chart.js'
import type { PanelConfig, QueryResult } from '../api/client'
import { useTheme } from '../composables/useTheme'

// Register Chart.js components.
Chart.register(
  LineController, BarController, CategoryScale, LinearScale,
  PointElement, LineElement, BarElement, Tooltip, Legend, Filler
)

const props = defineProps<{
  panel: PanelConfig
  timeRange: { start: number; end: number }
  refreshKey?: number
}>()

defineEmits<{
  edit: [panel: PanelConfig]
  delete: [panel: PanelConfig]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const tooltipRef = ref<HTMLDivElement | null>(null)
const loading = ref(false)
const error = ref('')
const noData = ref(false)
const statValue = ref(0)
const chartDatasets = ref<any[]>([])
let chart: Chart | null = null
let tooltipHovering = false

function buildPromQL(): string {
  const labels = props.panel.labels || {}
  const labelPairs = Object.entries(labels)
    .map(([k, v]) => `${k}="${v}"`)
    .join(',')
  if (labelPairs) {
    return `${props.panel.metric}{${labelPairs}}`
  }
  return props.panel.metric
}

async function fetchData() {
  loading.value = true
  error.value = ''
  noData.value = false

  try {
    const query = buildPromQL()

    if (props.panel.chartType === 'stat') {
      const res = await fetch(`/api/v1/query?query=${encodeURIComponent(query)}`)
      const data: QueryResult = await res.json()
      if (data.status === 'error') {
        error.value = data.error || 'Query error'
        return
      }
      const results = data.data?.result || []
      if (results.length === 0 || !results[0].value) {
        noData.value = true
        return
      }
      statValue.value = parseFloat(results[0].value[1])
    } else {
      const step = props.panel.step || 60
      const startSec = Math.floor(props.timeRange.start / 1000)
      const endSec = Math.floor(props.timeRange.end / 1000)
      const url = `/api/v1/query_range?query=${encodeURIComponent(query)}&start=${startSec}&end=${endSec}&step=${step}`
      const res = await fetch(url)
      const data: QueryResult = await res.json()
      if (data.status === 'error') {
        error.value = data.error || 'Query error'
        return
      }
      const results = data.data?.result || []
      const hasData = results.some(r => r.values && r.values.length > 0)
      if (!hasData) {
        noData.value = true
        return
      }
      // Must set loading false first so canvas renders in DOM before Chart.js targets it.
      loading.value = false
      await nextTick()
      renderChart(results)
      return // skip the finally block's loading=false
    }
  } catch (e: any) {
    error.value = e.message || 'Failed to fetch data'
  } finally {
    loading.value = false
  }
}

function getCSSVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

const { theme } = useTheme()

const COLORS = [
  '#38bdf8', '#f472b6', '#a78bfa', '#fb923c', '#4ade80',
  '#facc15', '#fb7185', '#2dd4bf', '#e2e8f0', '#94a3b8',
]

function seriesLabel(result: any): string {
  const metric = result.metric || {}
  const pairs = Object.entries(metric)
    .filter(([k]) => k !== '__name__')
    .map(([k, v]) => `${k}=${v}`)
  return pairs.length > 0 ? pairs.join(',') : (metric.__name__ || 'value')
}

function externalTooltipHandler(context: any) {
  const { chart, tooltip } = context
  const el = tooltipRef.value
  if (!el) return

  if (tooltip.opacity === 0) {
    if (!tooltipHovering) {
      el.style.opacity = '0'
    }
    return
  }

  // Don't reposition the tooltip while the user is interacting with it.
  if (tooltipHovering) return

  const dataPoints = tooltip.dataPoints || []
  if (dataPoints.length === 0) {
    el.style.opacity = '0'
    return
  }

  // Build tooltip HTML.
  let html = ''
  html += `<div class="tt-time">${dataPoints[0].label}</div>`
  html += '<div class="tt-body">'
  for (const dp of dataPoints) {
    const ds = chart.data.datasets[dp.datasetIndex]
    html += `<div class="tt-item" style="border-left:3px solid ${ds.borderColor}">`
    html += `<span class="tt-color" style="background:${ds.borderColor}"></span>`
    html += '<div class="tt-labels">'
    // Show each label key=value pair on its own line.
    const rawLabel = (ds.label || '') as string
    const parts = rawLabel.split(',').map((s: string) => s.trim()).filter(Boolean)
    for (const line of parts) {
      html += `<div class="tt-label">${line}</div>`
    }
    html += '</div>'
    html += `<span class="tt-value">${parseFloat(dp.formattedValue).toFixed(2)}</span>`
    html += '</div>'
  }
  html += '</div>'

  el.innerHTML = html
  el.style.opacity = '1'

  // Position tooltip relative to the chart canvas.
  const rect = chart.canvas.getBoundingClientRect()
  const caretX = tooltip.caretX
  const caretY = tooltip.caretY

  // Default: place above caret, centered horizontally.
  let left = rect.left + window.scrollX + caretX - el.offsetWidth / 2
  let top = rect.top + window.scrollY + caretY - el.offsetHeight - 12

  // Clamp horizontally.
  if (left < 0) left = 4
  const maxLeft = window.innerWidth - el.offsetWidth - 4
  if (left > maxLeft) left = maxLeft

  // If too high, flip below.
  if (top < 0) {
    top = rect.top + window.scrollY + caretY + 16
  }

  el.style.left = left + 'px'
  el.style.top = top + 'px'
}

function renderChart(results: any[]) {
  if (!canvasRef.value) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  // Use the first series' timestamps as shared labels for the x-axis.
  const firstValues = results[0]?.values || []
  const labels = firstValues.map((v: any) => {
    const d = new Date(v[0] * 1000)
    return d.toLocaleTimeString()
  })

  const datasets = results.map((r, idx) => {
    const data = (r.values || []).map((v: any) => parseFloat(v[1]))
    const color = COLORS[idx % COLORS.length]
    return {
      label: seriesLabel(r),
      data,
      borderColor: color,
      backgroundColor: props.panel.chartType === 'bar' ? color + '88' : 'transparent',
      borderWidth: 2,
      fill: props.panel.chartType === 'bar',
      tension: 0.3,
      pointRadius: 0,
    }
  })

  chartDatasets.value = datasets

  chart = new Chart(canvasRef.value, {
    type: props.panel.chartType === 'bar' ? 'bar' : 'line',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: {
          display: false,
        },
        tooltip: {
          enabled: false,
          mode: 'index',
          intersect: false,
          position: 'nearest',
          external: externalTooltipHandler,
        },
      },
      scales: {
        x: {
          ticks: { color: getCSSVar('--text-secondary'), maxTicksLimit: 8, font: { size: 10 } },
          grid: { color: getCSSVar('--border-subtle') },
        },
        y: {
          ticks: { color: getCSSVar('--text-secondary'), font: { size: 10 } },
          grid: { color: getCSSVar('--border-subtle') },
        },
      },
    },
  })
}

function formatValue(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M'
  if (v >= 1_000) return (v / 1_000).toFixed(1) + 'k'
  if (v === Math.floor(v)) return v.toString()
  return v.toFixed(2)
}

function setupTooltipListeners() {
  const el = tooltipRef.value
  if (!el || !canvasRef.value) return

  el.addEventListener('mouseenter', () => {
    tooltipHovering = true
  })
  el.addEventListener('mouseleave', () => {
    tooltipHovering = false
    el.style.opacity = '0'
  })

  // Also hide tooltip when mouse leaves the chart canvas entirely.
  canvasRef.value.addEventListener('mouseleave', () => {
    if (!tooltipHovering) {
      el.style.opacity = '0'
    }
  })
}

onMounted(() => {
  fetchData()
  setupTooltipListeners()
})
watch(() => [props.timeRange, props.panel, props.refreshKey], fetchData, { deep: true })

watch(theme, () => {
  if (chart) {
    chart.destroy()
    chart = null
  }
  fetchData()
})

onUnmounted(() => {
  if (chart) {
    chart.destroy()
    chart = null
  }
})
</script>

<style scoped>
.panel-chart {
  background: var(--bg-primary);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  overflow: hidden;
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-strong);
}
.panel-title { font-size: 14px; font-weight: 600; color: var(--text-primary); margin: 0; }
.panel-actions { display: flex; gap: 4px; }
.btn-icon {
  background: none; border: none; color: var(--text-secondary); cursor: pointer;
  font-size: 14px; padding: 4px; border-radius: 4px; line-height: 1;
}
.btn-icon:hover { color: var(--text-primary); background: var(--bg-surface-hover-subtle); }
.panel-body { padding: 16px; height: 280px; position: relative; display: flex; flex-direction: column; }
.panel-body canvas { width: 100% !important; flex: 1; min-height: 0; }
.panel-state {
  display: flex; align-items: center; justify-content: center;
  height: 100%; color: var(--text-secondary); font-size: 14px;
}
.panel-error { color: var(--status-error-accent); }
.stat-value {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; height: 100%;
}
.stat-number { font-size: 48px; font-weight: 700; color: var(--accent-blue); line-height: 1.2; }
.stat-metric { font-size: 12px; color: var(--text-secondary); margin-top: 8px; }

.chart-tooltip {
  position: fixed;
  pointer-events: auto;
  opacity: 0;
  z-index: 9999;
  background: var(--bg-secondary);
  border: 1px solid var(--border-group);
  border-radius: 6px;
  padding: 8px 10px;
  min-width: 160px;
  max-width: 280px;
  max-height: 220px;
  overflow-y: auto;
  font-size: 12px;
  box-shadow: 0 4px 16px var(--shadow-tooltip);
  transition: opacity 0.15s;
}
.chart-tooltip::-webkit-scrollbar { width: 4px; }
.chart-tooltip::-webkit-scrollbar-track { background: transparent; }
.chart-tooltip::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }

.tt-time {
  color: var(--text-secondary);
  font-size: 11px;
  margin-bottom: 6px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--border-group);
}
.tt-body {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.tt-item {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 6px 6px;
  border-radius: 4px;
}
.tt-item:nth-child(even) {
  background: var(--bg-surface-hover-subtle);
}
.tt-color {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 3px;
  flex-shrink: 0;
}
.tt-labels {
  flex: 1;
  min-width: 0;
}
.tt-label {
  color: var(--text-primary);
  line-height: 1.5;
  word-break: break-all;
}
.tt-value {
  color: var(--accent-blue);
  font-weight: 600;
  flex-shrink: 0;
  margin-left: auto;
}

/* Scrollable legend */
.chart-legend {
  margin-top: 8px;
  max-height: 80px;
  overflow-y: auto;
  font-size: 11px;
}
.chart-legend::-webkit-scrollbar { width: 4px; }
.chart-legend::-webkit-scrollbar-track { background: transparent; }
.chart-legend::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 2px; }
.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 0;
}
.legend-color {
  width: 10px;
  height: 3px;
  border-radius: 2px;
  flex-shrink: 0;
}
.legend-label {
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
