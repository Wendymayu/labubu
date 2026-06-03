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

// Register Chart.js components.
Chart.register(
  LineController, BarController, CategoryScale, LinearScale,
  PointElement, LineElement, BarElement, Tooltip, Legend, Filler
)

const props = defineProps<{
  panel: PanelConfig
  timeRange: { start: number; end: number }
}>()

defineEmits<{
  edit: [panel: PanelConfig]
  delete: [panel: PanelConfig]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const loading = ref(false)
const error = ref('')
const noData = ref(false)
const statValue = ref(0)
let chart: Chart | null = null

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
      if (results.length === 0 || !results[0].values || results[0].values.length === 0) {
        noData.value = true
        return
      }
      await nextTick()
      renderChart(results[0].values)
    }
  } catch (e: any) {
    error.value = e.message || 'Failed to fetch data'
  } finally {
    loading.value = false
  }
}

function renderChart(values: Array<[number, string]>) {
  if (!canvasRef.value) return

  if (chart) {
    chart.destroy()
    chart = null
  }

  const labels = values.map(v => {
    const d = new Date(v[0] * 1000)
    return d.toLocaleTimeString()
  })
  const data = values.map(v => parseFloat(v[1]))

  chart = new Chart(canvasRef.value, {
    type: props.panel.chartType === 'bar' ? 'bar' : 'line',
    data: {
      labels,
      datasets: [{
        label: props.panel.title,
        data,
        borderColor: '#38bdf8',
        backgroundColor: props.panel.chartType === 'bar' ? '#38bdf888' : '#38bdf822',
        borderWidth: 2,
        fill: props.panel.chartType === 'line',
        tension: 0.3,
        pointRadius: 0,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          mode: 'index',
          intersect: false,
        },
      },
      scales: {
        x: {
          ticks: { color: '#94a3b8', maxTicksLimit: 8, font: { size: 10 } },
          grid: { color: '#1e293b' },
        },
        y: {
          ticks: { color: '#94a3b8', font: { size: 10 } },
          grid: { color: '#1e293b' },
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

onMounted(fetchData)
watch(() => [props.timeRange, props.panel], fetchData, { deep: true })

onUnmounted(() => {
  if (chart) {
    chart.destroy()
    chart = null
  }
})
</script>

<style scoped>
.panel-chart {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  overflow: hidden;
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid #334155;
}
.panel-title { font-size: 14px; font-weight: 600; color: #e2e8f0; margin: 0; }
.panel-actions { display: flex; gap: 4px; }
.btn-icon {
  background: none; border: none; color: #64748b; cursor: pointer;
  font-size: 14px; padding: 4px; border-radius: 4px; line-height: 1;
}
.btn-icon:hover { color: #e2e8f0; background: #334155; }
.panel-body { padding: 16px; height: 280px; position: relative; }
.panel-body canvas { width: 100% !important; height: 100% !important; }
.panel-state {
  display: flex; align-items: center; justify-content: center;
  height: 100%; color: #94a3b8; font-size: 14px;
}
.panel-error { color: #f87171; }
.stat-value {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; height: 100%;
}
.stat-number { font-size: 48px; font-weight: 700; color: #38bdf8; line-height: 1.2; }
.stat-metric { font-size: 12px; color: #94a3b8; margin-top: 8px; }
</style>
