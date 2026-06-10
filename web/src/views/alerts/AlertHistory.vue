<template>
  <div class="alert-history-page">
    <div class="page-header">
      <h2>{{ t('alerts.history') }}</h2>
    </div>

    <div class="filters">
      <label>{{ t('alerts.statusFilter') }}</label>
      <select v-model="statusFilter" @change="load" class="form-input filter-select">
        <option value="">{{ t('alerts.all') }}</option>
        <option value="firing">{{ t('alerts.firing') }}</option>
        <option value="resolved">{{ t('alerts.resolved') }}</option>
        <option value="pending">{{ t('alerts.pending') }}</option>
      </select>
    </div>

    <table v-if="states.length > 0" class="data-table">
      <thead>
        <tr>
          <th>{{ t('alerts.ruleName') }}</th>
          <th>Trace ID</th>
          <th>{{ t('alerts.status') }}</th>
          <th>{{ t('alerts.triggeredAt') }}</th>
          <th>{{ t('alerts.resolvedAt') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="st in states" :key="st.id">
          <td>{{ ruleNames[st.rule_id] || st.rule_id }}</td>
          <td>
            <router-link :to="`/traces/${st.trace_id_hex}`" class="trace-link">
              {{ st.trace_id_hex.slice(0, 12) }}...
            </router-link>
          </td>
          <td>
            <span :class="statusClass(st.status)">{{ st.status }}</span>
          </td>
          <td>{{ formatTime(st.triggered_at) }}</td>
          <td>{{ st.resolved_at ? formatTime(st.resolved_at) : '-' }}</td>
        </tr>
      </tbody>
    </table>

    <p v-else class="empty-msg">{{ t('alerts.noAlerts') }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listStates, listRules, type AlertState, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const states = ref<AlertState[]>([])
const statusFilter = ref('')
const ruleNames = ref<Record<string, string>>({})

async function load() {
  try {
    const [statesResp, rulesResp] = await Promise.all([
      listStates(statusFilter.value || undefined),
      listRules(),
    ])
    states.value = statesResp.states
    const map: Record<string, string> = {}
    rulesResp.rules.forEach((r: AlertRule) => { map[r.id] = r.name })
    ruleNames.value = map
  } catch (e) {
    console.error('Failed to load alert history:', e)
  }
}

function statusClass(status: string) {
  switch (status) {
    case 'firing': return 'badge-red'
    case 'resolved': return 'badge-green'
    case 'pending': return 'badge-yellow'
    default: return 'badge-gray'
  }
}

function formatTime(ts: string) {
  if (!ts) return '-'
  const d = new Date(ts)
  return d.toLocaleString()
}

onMounted(load)
</script>

<style scoped>
.alert-history-page { max-width: 1000px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 20px; color: var(--text-primary); }
.filters { display: flex; align-items: center; gap: 12px; margin-bottom: 20px; }
.filters label { font-size: 14px; color: var(--text-secondary); }
.filter-select { width: 160px; }
.form-input { padding: 8px 12px; border: 1px solid var(--border-default); border-radius: 6px; font-size: 14px; background: var(--bg-primary); color: var(--text-primary); }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { text-align: left; padding: 12px 8px; border-bottom: 1px solid var(--border-default); font-size: 14px; }
.data-table th { color: var(--text-secondary); font-weight: 600; }
.trace-link { color: var(--accent-blue); text-decoration: none; }
.trace-link:hover { text-decoration: underline; }
.badge-red { background: #f8d7da; color: #721c24; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-green { background: #d4edda; color: #155724; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-yellow { background: #fff3cd; color: #856404; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-gray { background: #e2e3e5; color: #383d41; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.empty-msg { color: var(--text-secondary); margin-top: 40px; text-align: center; }
</style>
