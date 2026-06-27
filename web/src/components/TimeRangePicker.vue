<template>
  <div class="period-bar">
    <button
      v-if="showAll"
      :class="['btn', 'btn-preset', { active: activePeriod === 'all' }]"
      @click="setPeriod('all')"
    >{{ t('timeRange.all') }}</button>
    <button
      v-for="p in periods"
      :key="p"
      :class="['btn', 'btn-preset', { active: activePeriod === p }]"
      @click="setPeriod(p)"
    >{{ t(`timeRange.${p}`) }}</button>
    <div v-if="activePeriod === 'custom'" class="custom-range">
      <input type="datetime-local" v-model="customStart" @change="onCustomChange" />
      <span>{{ t('timeRange.to') }}</span>
      <input type="datetime-local" v-model="customEnd" @change="onCustomChange" />
    </div>
    <span v-if="rangeError" class="range-error">{{ t('timeRange.invalidRange') }}</span>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TimeRangeSelection } from '../api/client'

withDefaults(defineProps<{ showAll?: boolean }>(), {
  showAll: false,
})

const emit = defineEmits<{
  (e: 'change', selection: TimeRangeSelection): void
}>()

const { t } = useI18n()

// Presets always rendered (the "all" button is conditional on showAll).
const periods = ['today', '7d', '30d', 'custom'] as const

const activePeriod = ref<string>('today')
const customStart = ref('')
const customEnd = ref('')
const rangeError = ref(false)

// <input type="datetime-local"> values are local time with no zone suffix.
function toLocalInputValue(ms: number): string {
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// Compute start/end (epoch ms) for a non-custom preset. 'all' → no bounds.
function presetRange(period: string): { start?: number; end?: number } {
  const now = Date.now()
  switch (period) {
    case 'today': {
      const d = new Date()
      return { start: new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime(), end: now }
    }
    case '7d':
      return { start: now - 7 * 24 * 3600 * 1000, end: now }
    case '30d':
      return { start: now - 30 * 24 * 3600 * 1000, end: now }
    default: // 'all' or unknown
      return {}
  }
}

function emitSelection(period: string) {
  rangeError.value = false
  if (period === 'custom') {
    if (!customStart.value || !customEnd.value) return // not fully entered yet
    const start = new Date(customStart.value).getTime()
    const end = new Date(customEnd.value).getTime()
    if (start > end) {
      rangeError.value = true
      return // do not emit an invalid range
    }
    emit('change', { period, start, end })
    return
  }
  const { start, end } = presetRange(period)
  emit('change', { period, start, end })
}

function setPeriod(key: string) {
  if (key === 'custom') {
    // Seed the custom range from the current preset so the user can fine-tune.
    const now = Date.now()
    let start: number
    switch (activePeriod.value) {
      case 'today': {
        const d = new Date()
        start = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
        break
      }
      case '7d': start = now - 7 * 24 * 3600 * 1000; break
      case '30d': start = now - 30 * 24 * 3600 * 1000; break
      default: start = now - 24 * 3600 * 1000 // 'all' or already-custom → last 24h
    }
    customStart.value = toLocalInputValue(start)
    customEnd.value = toLocalInputValue(now)
    activePeriod.value = 'custom'
    emitSelection('custom')
    return
  }
  activePeriod.value = key
  emitSelection(key)
}

function onCustomChange() {
  emitSelection('custom')
}

onMounted(() => {
  // Emit the default 'today' selection so parents run their first fetch with
  // the correct time range. Vue mounts children before parents, so this fires
  // before the parent's onMounted.
  emitSelection(activePeriod.value)
})
</script>

<style scoped>
.period-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
  align-items: center;
  flex-wrap: wrap;
}

.btn-preset {
  padding: 6px 16px;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}

.btn-preset:hover {
  color: var(--text-primary);
}

.btn-preset.active {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}

.custom-range {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: 4px;
}

.custom-range input {
  background: var(--bg-primary);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  padding: 6px 10px;
  font-size: 13px;
}

.custom-range span {
  color: var(--text-secondary);
  font-size: 13px;
}

html[data-theme="dark"] .custom-range input {
  color-scheme: dark;
}

.range-error {
  color: var(--status-error-accent);
  font-size: 13px;
  margin-left: 8px;
}
</style>
