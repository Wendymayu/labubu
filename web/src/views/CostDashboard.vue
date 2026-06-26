<template>
  <div class="cost-dashboard">
    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-else-if="loadError" class="error">{{ loadError }}</div>
    <div v-else-if="noPricing" class="no-pricing">
      <p>{{ t('costDashboard.noPricing') }}</p>
      <router-link to="/settings/pricing" class="btn btn-primary">{{ t('nav.modelPricing') }}</router-link>
    </div>
    <div v-else>
      <!-- Period selector -->
      <div class="period-bar">
        <button
          v-for="p in periods"
          :key="p.key"
          :class="['btn', 'btn-preset', { active: activePeriod === p.key }]"
          @click="setPeriod(p.key)"
        >{{ t(`costDashboard.${p.key}`) }}</button>
      </div>

      <!-- Overview cards -->
      <div class="overview-cards">
        <div class="card">
          <div class="card-label">{{ t('costDashboard.totalCost') }}</div>
          <div class="card-value">{{ formatCost(summary.overview.total_cost, summary.currency) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.totalTokens') }}</div>
          <div class="card-value">{{ formatNumber(summary.overview.total_tokens) }}</div>
          <div class="card-sub">{{ formatNumber(summary.overview.total_input_tokens) }} in / {{ formatNumber(summary.overview.total_output_tokens) }} out</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.cache') }}</div>
          <div class="card-value">{{ formatNumber(summary.overview.total_cache_read_tokens + summary.overview.total_cache_creation_tokens) }}</div>
          <div class="card-sub">{{ t('costDashboard.cacheRead') }}: {{ formatNumber(summary.overview.total_cache_read_tokens) }} / {{ t('costDashboard.cacheWrite') }}: {{ formatNumber(summary.overview.total_cache_creation_tokens) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.avgCostPerTrace') }}</div>
          <div class="card-value">{{ formatCost(summary.overview.avg_cost_per_trace, summary.currency) }}</div>
        </div>
        <div class="card">
          <div class="card-label">{{ t('costDashboard.traceCount') }}</div>
          <div class="card-value">{{ summary.overview.trace_count }}</div>
        </div>
      </div>

      <!-- Cost breakdown table (by model / by service) -->
      <h3>{{ breakdownTitle }}</h3>
      <div class="breakdown-toggle">
        <button
          :class="['btn', 'btn-preset', { active: groupBy === 'model' }]"
          @click="setGroupBy('model')"
        >{{ t('costDashboard.byModel') }}</button>
        <button
          :class="['btn', 'btn-preset', { active: groupBy === 'service' }]"
          @click="setGroupBy('service')"
        >{{ t('costDashboard.byService') }}</button>
      </div>
      <table v-if="breakdownRows.length > 0" class="cost-table">
        <thead>
          <tr>
            <th>{{ breakdownDimensionLabel }}</th>
            <th>{{ t('costDashboard.cost') }}</th>
            <th>{{ t('costDashboard.tokens') }}</th>
            <th>{{ t('costDashboard.cache') }}</th>
            <th>{{ t('costDashboard.traces') }}</th>
            <th>{{ t('costDashboard.avgCost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in breakdownRows" :key="r.name">
            <td>{{ r.name }}</td>
            <td>{{ formatCost(r.cost, summary.currency) }}</td>
            <td>{{ formatNumber(r.tokens) }}</td>
            <td>{{ formatNumber(r.cache_read_tokens + r.cache_creation_tokens) }}</td>
            <td>{{ r.trace_count }}</td>
            <td>{{ formatCost(r.avg_cost, summary.currency) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">{{ t('costDashboard.noData') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCostSummary, getModelPricing, type CostSummary } from '../api/client'
import { formatCost, formatNumber } from '../utils/format'

const { t } = useI18n()

const periods = [
  { key: 'today' },
  { key: '7d' },
  { key: '30d' },
]

const activePeriod = ref('today')
const groupBy = ref<'model' | 'service'>('model')

function setGroupBy(dim: 'model' | 'service') {
  if (groupBy.value === dim) return
  groupBy.value = dim
  fetchData()
}

const summary = ref<CostSummary>({
  period: 'today',
  currency: 'USD',
  group_by: 'model',
  overview: {
    total_cost: 0,
    total_tokens: 0,
    total_input_tokens: 0,
    total_cache_creation_tokens: 0,
    total_cache_read_tokens: 0,
    total_output_tokens: 0,
    avg_cost_per_trace: 0,
    trace_count: 0,
  },
  by_model: [],
  by_service: [],
})
const loading = ref(true)
const loadError = ref('')
const noPricing = ref(false)

const breakdownRows = computed(() => {
  const normalize = (r: { name: string; cost: number; tokens: number; cache_read_tokens: number; cache_creation_tokens: number; trace_count: number; avg_cost: number }) => r
  if (summary.value.group_by === 'service') {
    return (summary.value.by_service ?? []).map(s => normalize({
      name: s.service, cost: s.cost, tokens: s.tokens,
      cache_read_tokens: s.cache_read_tokens, cache_creation_tokens: s.cache_creation_tokens,
      trace_count: s.trace_count, avg_cost: s.avg_cost,
    }))
  }
  return (summary.value.by_model ?? []).map(m => normalize({
    name: m.model, cost: m.cost, tokens: m.tokens,
    cache_read_tokens: m.cache_read_tokens, cache_creation_tokens: m.cache_creation_tokens,
    trace_count: m.trace_count, avg_cost: m.avg_cost,
  }))
})

const breakdownTitle = computed(() =>
  summary.value.group_by === 'service' ? t('costDashboard.costByService') : t('costDashboard.costByModel')
)

const breakdownDimensionLabel = computed(() =>
  summary.value.group_by === 'service' ? t('costDashboard.service') : t('costDashboard.model')
)

async function fetchData() {
  loading.value = true
  loadError.value = ''
  try {
    const result = await getCostSummary(activePeriod.value, groupBy.value)
    summary.value = result
  } catch (e: any) {
    loadError.value = e.message || 'Failed to load cost data'
  } finally {
    loading.value = false
  }
}

async function checkPricing() {
  try {
    const pricingResult = await getModelPricing()
    noPricing.value = pricingResult.models.length === 0
  } catch {
    // If pricing API fails, still show dashboard with whatever data we have
    noPricing.value = false
  }
}

function setPeriod(key: string) {
  activePeriod.value = key
  fetchData()
}

onMounted(() => {
  checkPricing()
  fetchData()
})
</script>

<style scoped>
.cost-dashboard {
  max-width: 1200px;
  margin: 0 auto;
  padding: 24px;
}

.period-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
}

.breakdown-toggle {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
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

.overview-cards {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}

.card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 16px;
}

.card-label {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  margin-bottom: 8px;
}

.card-value {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.card-sub {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.cost-dashboard h3 {
  margin-bottom: 12px;
}

.cost-table {
  width: 100%;
  border-collapse: collapse;
}

.cost-table th, .cost-table td {
  padding: 10px 16px;
  text-align: left;
  border-bottom: 1px solid var(--border-default);
}

.cost-table th {
  font-size: 11px;
  color: var(--text-secondary);
  text-transform: uppercase;
}

.cost-table td {
  font-size: 14px;
}

.loading, .error, .empty, .no-pricing {
  text-align: center;
  padding: 40px;
  color: var(--text-secondary);
}

.no-pricing .btn {
  margin-top: 12px;
}

@media (max-width: 960px) {
  .overview-cards {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>