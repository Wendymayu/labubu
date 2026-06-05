<template>
  <div class="modal-overlay" @click.self="$emit('cancel')">
    <div class="modal-content">
      <h3 class="modal-title">{{ isEdit ? 'Edit Panel' : 'New Panel' }}</h3>

      <form @submit.prevent="handleSubmit">
        <div class="form-group">
          <label for="pf-title">Title</label>
          <input id="pf-title" v-model="form.title" type="text" required placeholder="e.g. Token Usage" />
        </div>

        <div class="form-group">
          <label for="pf-metric">Metric</label>
          <select id="pf-metric" v-model="form.metric" required>
            <option value="" disabled>Select a metric...</option>
            <option v-for="m in metricNames" :key="m" :value="m">{{ m }}</option>
          </select>
        </div>

        <div class="form-group">
          <label>Labels (optional)</label>
          <div v-for="(item, idx) in form.labelEntries" :key="idx" class="label-row">
            <select v-model="item.key" @change="onLabelKeyChange(idx)">
              <option value="">-- key --</option>
              <option v-for="ln in sortedLabelNames" :key="ln" :value="ln">{{ ln }}</option>
            </select>
            <select v-model="item.value">
              <option value="">-- value --</option>
              <option v-for="lv in labelValuesCache[item.key] || []" :key="lv" :value="lv">{{ lv }}</option>
            </select>
            <button type="button" class="btn-remove" @click="removeLabel(idx)">x</button>
          </div>
          <button type="button" class="btn-add-label" @click="addLabel">+ Add label</button>
        </div>

        <div class="form-group">
          <label for="pf-charttype">Chart Type</label>
          <select id="pf-charttype" v-model="form.chartType" required>
            <option value="line">Line</option>
            <option value="bar">Bar</option>
            <option value="stat">Stat Card</option>
          </select>
        </div>

        <div class="form-group" v-if="form.chartType !== 'stat'">
          <label for="pf-step">Step (seconds)</label>
          <input id="pf-step" v-model.number="form.step" type="number" min="1" />
        </div>

        <div class="modal-actions">
          <button type="button" class="btn btn-secondary" @click="$emit('cancel')">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
        </div>
        <p v-if="saveError" class="form-error">{{ saveError }}</p>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { getMetricNames, getLabels, getLabelValues, createDashboard, updateDashboard } from '../api/client'
import type { PanelConfig } from '../api/client'

const props = defineProps<{
  panel?: PanelConfig | null
}>()

const emit = defineEmits<{
  saved: [panel: PanelConfig]
  cancel: []
}>()

const isEdit = computed(() => !!props.panel)

const form = reactive({
  title: props.panel?.title || '',
  metric: props.panel?.metric || '',
  labelEntries: [] as Array<{ key: string; value: string }>,
  chartType: (props.panel?.chartType || 'line') as 'line' | 'bar' | 'stat',
  step: props.panel?.step || 60,
})

// Populate initial label entries from panel.
if (props.panel?.labels) {
  form.labelEntries = Object.entries(props.panel.labels).map(([k, v]) => ({ key: k, value: v }))
}

const metricNames = ref<string[]>([])
const allLabelNames = ref<string[]>([])
const labelValuesCache = reactive<Record<string, string[]>>({})
const saving = ref(false)
const saveError = ref('')

const sortedLabelNames = computed(() => {
  return allLabelNames.value.filter(n => n !== '__name__').sort()
})

function addLabel() {
  form.labelEntries.push({ key: '', value: '' })
}

function removeLabel(idx: number) {
  form.labelEntries.splice(idx, 1)
}

async function onLabelKeyChange(idx: number) {
  const key = form.labelEntries[idx].key
  if (key && !labelValuesCache[key]) {
    try {
      const values = await getLabelValues(key)
      labelValuesCache[key] = values
    } catch {
      labelValuesCache[key] = []
    }
  }
}

async function handleSubmit() {
  saveError.value = ''
  if (!form.title || !form.metric) return

  const labels: Record<string, string> = {}
  for (const entry of form.labelEntries) {
    if (entry.key && entry.value) {
      labels[entry.key] = entry.value
    }
  }

  const panel: Omit<PanelConfig, 'id'> = {
    title: form.title,
    metric: form.metric,
    labels,
    chartType: form.chartType,
  }
  if (form.chartType !== 'stat') {
    panel.step = form.step
  }

  saving.value = true
  try {
    let result: PanelConfig
    if (isEdit.value && props.panel) {
      result = await updateDashboard({
        ...panel,
        id: props.panel.id,
      })
    } else {
      result = await createDashboard(panel)
    }
    emit('saved', result)
  } catch (e: any) {
    saveError.value = e.message || 'Save failed'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    const [names, labels] = await Promise.all([getMetricNames(), getLabels()])
    metricNames.value = names.sort()
    allLabelNames.value = labels
  } catch {
    // populating dropdowns is best-effort
  }
})
</script>

<style scoped>
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.7);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000;
}
.modal-content {
  background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 12px;
  padding: 24px; width: 480px; max-height: 90vh; overflow-y: auto;
}
.modal-title { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0 0 20px 0; }
.form-group { margin-bottom: 16px; }
.form-group label { display: block; font-size: 13px; color: var(--text-secondary); margin-bottom: 6px; }
.form-group input, .form-group select {
  width: 100%; padding: 8px 12px; background: var(--bg-surface-deep); border: 1px solid var(--border-default);
  border-radius: 6px; color: var(--text-primary); font-size: 14px; box-sizing: border-box;
}
.form-group input:focus, .form-group select:focus { border-color: var(--accent-blue); outline: none; }
.label-row { display: flex; gap: 8px; margin-bottom: 8px; }
.label-row select { flex: 1; }
.btn-remove {
  background: none; border: none; color: var(--status-error-accent); cursor: pointer;
  font-size: 16px; padding: 0 8px;
}
.btn-add-label {
  background: none; border: 1px dashed var(--border-default); color: var(--text-secondary);
  padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; width: 100%;
}
.btn-add-label:hover { border-color: var(--accent-blue); color: var(--accent-blue); }
.modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px; }
.btn {
  padding: 8px 20px; border-radius: 6px; border: none;
  font-size: 14px; cursor: pointer; font-weight: 500;
}
.btn-primary { background: var(--accent-blue); color: var(--bg-primary); }
.btn-primary:hover { background: var(--accent-light); }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-secondary { background: var(--bg-surface-hover); color: var(--text-primary); }
.btn-secondary:hover { background: var(--border-strong); }
.form-error { color: var(--status-error-accent); font-size: 13px; margin-top: 8px; }
</style>
