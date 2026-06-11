<template>
  <div class="dashboard-page">
    <!-- Tab bar -->
    <div class="tab-bar">
      <div class="tab-list">
        <button
          v-for="dash in dashboards"
          :key="dash.id"
          :class="['tab-item', { active: dash.id === activeDashboardId }]"
          @click="switchDashboard(dash.id)"
        >{{ dash.name }}</button>
        <button class="tab-item tab-add" @click="showCreateDashboard = true" :title="t('dashboard.newDashboard')">+</button>
      </div>
    </div>

    <!-- Create dashboard popover -->
    <div v-if="showCreateDashboard" class="popover-overlay" @click.self="closeCreateDashboard">
      <div class="popover-box">
        <input
          ref="createInputRef"
          v-model="newDashboardName"
          type="text"
          :placeholder="t('dashboard.dashboardName')"
          class="popover-input"
          @keyup.enter="doCreateDashboard"
          @keyup.escape="closeCreateDashboard"
        />
        <div class="popover-actions">
          <button class="btn btn-sm" @click="closeCreateDashboard">{{ t('common.cancel') }}</button>
          <button class="btn btn-sm btn-primary" @click="doCreateDashboard" :disabled="!newDashboardName.trim()">{{ t('dashboard.createDashboard') }}</button>
        </div>
      </div>
    </div>

    <!-- Rename dashboard popover -->
    <div v-if="showRenameDashboard" class="popover-overlay" @click.self="closeRenameDashboard">
      <div class="popover-box">
        <input
          ref="renameInputRef"
          v-model="renameName"
          type="text"
          :placeholder="t('dashboard.dashboardName')"
          class="popover-input"
          @keyup.enter="doRenameDashboard"
          @keyup.escape="closeRenameDashboard"
        />
        <div class="popover-actions">
          <button class="btn btn-sm" @click="closeRenameDashboard">{{ t('common.cancel') }}</button>
          <button class="btn btn-sm btn-primary" @click="doRenameDashboard" :disabled="!renameName.trim()">{{ t('dashboard.renameDashboard') }}</button>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="dashboard-toolbar" v-if="activeDashboardId">
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
        <button class="btn-preset btn-refresh" @click="refreshAll">&#8635;</button>
      </div>
      <div class="toolbar-actions">
        <button class="btn" @click="openRenameDashboard">{{ t('dashboard.renameDashboard') }}</button>
        <button class="btn" @click="confirmDeleteDashboard">{{ t('dashboard.deleteDashboard') }}</button>
        <button class="btn btn-primary" @click="openCreatePanel">+ New Panel</button>
      </div>
    </div>

    <!-- Content -->
    <div v-if="loading" class="page-state">Loading...</div>
    <div v-else-if="loadError" class="page-state page-error">{{ loadError }}</div>
    <div v-else-if="dashboards.length === 0" class="page-state">
      <p>{{ t('dashboard.noDashboards') }}</p>
      <button class="btn btn-primary" @click="showCreateDashboard = true">{{ t('dashboard.createFirstDashboard') }}</button>
    </div>
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
        :refreshKey="refreshKey"
        @edit="openEditPanel"
        @delete="confirmDeletePanel"
      />
    </div>

    <!-- Panel form modal -->
    <PanelForm
      v-if="showPanelForm"
      :panel="editingPanel"
      :dashboardId="activeDashboardId"
      @saved="onPanelSaved"
      @cancel="closePanelForm"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { listDashboards, createDashboard, renameDashboard, deleteDashboard, deletePanel, type DashboardItem, type PanelConfig } from '../api/client'
import { useI18n } from 'vue-i18n'
import PanelChart from '../components/PanelChart.vue'
import PanelForm from '../components/PanelForm.vue'

const { t } = useI18n()

const dashboards = ref<DashboardItem[]>([])
const activeDashboardId = ref<string>('')
const loading = ref(true)
const loadError = ref('')

const panels = computed(() => {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  return dash?.panels || []
})

// Dashboard CRUD state
const showCreateDashboard = ref(false)
const newDashboardName = ref('')
const createInputRef = ref<HTMLInputElement | null>(null)

const showRenameDashboard = ref(false)
const renameName = ref('')
const renameInputRef = ref<HTMLInputElement | null>(null)

// Panel form state
const showPanelForm = ref(false)
const editingPanel = ref<PanelConfig | null>(null)

// Time presets
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

const refreshKey = ref(0)

function refreshAll() { refreshKey.value++ }

function setPreset(label: string, duration: number) {
  activePreset.value = label
  if (label === 'custom') {
    const end = new Date()
    const start = new Date(end.getTime() - 3600000)
    customEnd.value = end.toISOString().slice(0, 16)
    customStart.value = start.toISOString().slice(0, 16)
  }
}

async function loadAll() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await listDashboards()
    dashboards.value = data.dashboards || []
    if (dashboards.value.length === 0) {
      activeDashboardId.value = ''
    } else if (!dashboards.value.find(d => d.id === activeDashboardId.value)) {
      activeDashboardId.value = dashboards.value[0].id
    }
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load dashboards'
  } finally {
    loading.value = false
  }
}

function switchDashboard(id: string) {
  activeDashboardId.value = id
}

// Create dashboard
async function doCreateDashboard() {
  const name = newDashboardName.value.trim()
  if (!name) return
  try {
    const dash = await createDashboard(name)
    dashboards.value.push(dash)
    activeDashboardId.value = dash.id
    closeCreateDashboard()
  } catch (e: any) {
    alert(`Create dashboard failed: ${e.message}`)
  }
}

function closeCreateDashboard() {
  showCreateDashboard.value = false
  newDashboardName.value = ''
}

// Rename dashboard
function openRenameDashboard() {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  if (!dash) return
  renameName.value = dash.name
  showRenameDashboard.value = true
  nextTick(() => renameInputRef.value?.focus())
}

async function doRenameDashboard() {
  const name = renameName.value.trim()
  if (!name || !activeDashboardId.value) return
  try {
    await renameDashboard(activeDashboardId.value, name)
    const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
    if (dash) dash.name = name
    closeRenameDashboard()
  } catch (e: any) {
    alert(`Rename failed: ${e.message}`)
  }
}

function closeRenameDashboard() {
  showRenameDashboard.value = false
  renameName.value = ''
}

// Delete dashboard
async function confirmDeleteDashboard() {
  const dash = dashboards.value.find(d => d.id === activeDashboardId.value)
  if (!dash) return
  if (!confirm(t('dashboard.deleteDashboardConfirm', { name: dash.name }))) return
  try {
    await deleteDashboard(dash.id)
    dashboards.value = dashboards.value.filter(d => d.id !== dash.id)
    if (dashboards.value.length > 0) {
      activeDashboardId.value = dashboards.value[0].id
    } else {
      activeDashboardId.value = ''
    }
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

// Panel operations
function openCreatePanel() {
  editingPanel.value = null
  showPanelForm.value = true
}

function openEditPanel(panel: PanelConfig) {
  editingPanel.value = panel
  showPanelForm.value = true
}

function closePanelForm() {
  showPanelForm.value = false
  editingPanel.value = null
}

function onPanelSaved() {
  closePanelForm()
  loadAll()
}

async function confirmDeletePanel(panel: PanelConfig) {
  if (!confirm(`Delete panel "${panel.title}"?`)) return
  try {
    await deletePanel(activeDashboardId.value, panel.id)
    loadAll()
  } catch (e: any) {
    alert(`Delete failed: ${e.message}`)
  }
}

onMounted(loadAll)
</script>

<style scoped>
.dashboard-page { max-width: 1400px; margin: 0 auto; }

/* Tab bar */
.tab-bar {
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border-default);
}
.tab-list {
  display: flex;
  align-items: center;
  gap: 2px;
  overflow-x: auto;
}
.tab-item {
  padding: 8px 16px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  white-space: nowrap;
  transition: color 0.15s, border-color 0.15s;
}
.tab-item:hover:not(.tab-add) {
  color: var(--text-primary);
}
.tab-item.active {
  color: var(--accent-blue);
  border-bottom-color: var(--accent-blue);
}
.tab-add {
  font-size: 18px;
  padding: 8px 12px;
  color: var(--text-secondary);
}
.tab-add:hover {
  color: var(--accent-blue);
}

/* Popover */
.popover-overlay {
  position: fixed; inset: 0; z-index: 1000;
  display: flex; align-items: flex-start; justify-content: center;
  padding-top: 80px;
}
.popover-box {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
  min-width: 300px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.3);
}
.popover-input {
  width: 100%;
  padding: 8px 12px;
  background: var(--bg-surface-deep);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 14px;
  box-sizing: border-box;
}
.popover-input:focus { border-color: var(--accent-blue); outline: none; }
.popover-actions {
  display: flex; gap: 8px; justify-content: flex-end;
  margin-top: 12px;
}

/* Toolbar */
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.time-presets { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.btn-preset {
  padding: 6px 16px; border: 1px solid var(--border-group); background: var(--bg-secondary);
  color: var(--text-secondary); border-radius: 6px; cursor: pointer; font-size: 13px;
}
.btn-preset.active { background: var(--accent-blue); color: var(--bg-primary); border-color: var(--accent-blue); }
.btn-preset:hover:not(.active) { border-color: var(--accent-blue); color: var(--accent-blue); }
.custom-range { display: flex; align-items: center; gap: 8px; }
.custom-range input {
  background: var(--bg-primary); border: 1px solid var(--border-group); border-radius: 6px;
  color: var(--text-primary); padding: 6px 10px; font-size: 13px;
}
.custom-range span { color: var(--text-secondary); font-size: 13px; }
.toolbar-actions {
  display: flex; gap: 8px; align-items: center;
}
.btn {
  padding: 8px 16px; border: 1px solid var(--border-strong); border-radius: 6px;
  background: var(--bg-surface-hover); color: var(--text-primary); cursor: pointer; font-size: 14px;
}
.btn:hover { background: var(--border-strong); }
.btn-sm { padding: 4px 12px; font-size: 13px; }
.btn-primary { background: var(--accent-blue); border-color: var(--accent-blue); color: var(--bg-primary); }
.btn-primary:hover { background: var(--accent-light); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-refresh {
  background: var(--bg-secondary); border: 1px solid var(--border-group); color: var(--text-primary);
}
.btn-refresh:hover { background: var(--bg-surface-hover-subtle); border-color: var(--accent-blue); }

/* Panel grid */
.panel-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}
@media (max-width: 960px) {
  .panel-grid { grid-template-columns: 1fr; }
}

/* Page states */
.page-state {
  text-align: center; padding: 80px 20px; color: var(--text-secondary); font-size: 15px;
}
.page-error { color: var(--status-error-accent); }
</style>
