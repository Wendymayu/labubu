<template>
  <div class="dashboard-page">
    <div class="dashboard-toolbar">
      <div class="time-presets">
        <button
          v-for="p in presets"
          :key="p.label"
          :class="['btn-preset', { active: activePreset === p.label }]"
          @click="setPreset(p.label, p.duration)"
        >{{ p.label }}</button>
        <div v-if="activePreset === 'custom'" class="custom-range">
          <input type="datetime-local" v-model="customStart" />
          <span>to</span>
          <input type="datetime-local" v-model="customEnd" />
        </div>
      </div>
      <button class="btn btn-primary" @click="openCreate">+ New Panel</button>
    </div>

    <div v-if="loading" class="page-state">Loading...</div>
    <div v-else-if="loadError" class="page-state page-error">{{ loadError }}</div>
    <div v-else-if="panels.length === 0" class="page-state">
      <p>No panels yet.</p>
      <p>Click "+ New Panel" to create your first dashboard panel.</p>
    </div>
    <div v-else class="panel-grid">
      <PanelChart
        v-for="panel in panels"
        :key="panel.id"
        :panel="panel"
        :timeRange="computedTimeRange"
        @edit="openEdit"
        @delete="confirmDelete"
      />
    </div>

    <PanelForm
      v-if="showForm"
      :panel="editingPanel"
      @saved="onSaved"
      @cancel="closeForm"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { listDashboards, deleteDashboard } from '../api/client'
import type { PanelConfig } from '../api/client'
import PanelChart from '../components/PanelChart.vue'
import PanelForm from '../components/PanelForm.vue'

const panels = ref<PanelConfig[]>([])
const loading = ref(true)
const loadError = ref('')

const showForm = ref(false)
const editingPanel = ref<PanelConfig | null>(null)

const presets = [
  { label: '1h', duration: 3600 * 1000 },
  { label: '6h', duration: 6 * 3600 * 1000 },
  { label: '24h', duration: 24 * 3600 * 1000 },
  { label: 'custom', duration: 0 },
]

const activePreset = ref('1h')
const customStart = ref('')
const customEnd = ref('')

const computedTimeRange = computed(() => {
  if (activePreset.value === 'custom') {
    const start = customStart.value ? new Date(customStart.value).getTime() : Date.now() - 3600000
    const end = customEnd.value ? new Date(customEnd.value).getTime() : Date.now()
    return { start, end }
  }
  const end = Date.now()
  const preset = presets.find(p => p.label === activePreset.value)
  const start = end - (preset?.duration || 3600000)
  return { start, end }
})

function setPreset(label: string, duration: number) {
  activePreset.value = label
  if (label === 'custom') {
    const end = new Date()
    const start = new Date(end.getTime() - 3600000)
    customEnd.value = end.toISOString().slice(0, 16)
    customStart.value = start.toISOString().slice(0, 16)
  }
}

async function loadPanels() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await listDashboards()
    panels.value = data.panels || []
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load dashboards'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingPanel.value = null
  showForm.value = true
}

function openEdit(panel: PanelConfig) {
  editingPanel.value = panel
  showForm.value = true
}

function closeForm() {
  showForm.value = false
  editingPanel.value = null
}

function onSaved(_panel: PanelConfig) {
  closeForm()
  loadPanels()
}

async function confirmDelete(panel: PanelConfig) {
  if (!confirm(`Delete panel "${panel.title}"?`)) return
  try {
    await deleteDashboard(panel.id)
    panels.value = panels.value.filter(p => p.id !== panel.id)
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

onMounted(loadPanels)
</script>

<style scoped>
.dashboard-page { max-width: 1400px; margin: 0 auto; }
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.time-presets { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-preset {
  padding: 6px 16px; border: 1px solid #334155; background: #1e293b;
  color: #94a3b8; border-radius: 6px; cursor: pointer; font-size: 13px;
}
.btn-preset.active { background: #38bdf8; color: #000; border-color: #38bdf8; }
.btn-preset:hover:not(.active) { border-color: #38bdf8; color: #38bdf8; }
.custom-range { display: flex; align-items: center; gap: 8px; }
.custom-range input {
  background: #0f172a; border: 1px solid #334155; border-radius: 6px;
  color: #e2e8f0; padding: 6px 10px; font-size: 13px;
}
.custom-range span { color: #94a3b8; font-size: 13px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: #38bdf8; color: #000; }
.btn-primary:hover { background: #7dd3fc; }
.panel-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}
@media (max-width: 960px) {
  .panel-grid { grid-template-columns: 1fr; }
}
.page-state {
  text-align: center; padding: 80px 20px; color: #94a3b8; font-size: 15px;
}
.page-error { color: #f87171; }
</style>
