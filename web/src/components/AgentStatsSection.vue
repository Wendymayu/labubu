<template>
  <div class="agent-stats-section" v-if="stats">
    <h3 class="section-title">
      🤖 {{ t('agentStats.agentStats') }}
      <span class="section-subtitle">— Is this agent reliable?</span>
    </h3>

    <!-- Overview cards -->
    <div class="overview-cards">
      <div class="overview-card" :class="rateClass(stats.trace_success_rate)">
        <div class="card-value">{{ formatRate(stats.trace_success_rate) }}</div>
        <div class="card-label">{{ t('agentStats.traceSuccessRate') }}</div>
      </div>

      <div class="overview-card" :class="rateClass(stats.avg_tool_success_rate)">
        <div class="card-value">{{ formatRate(stats.avg_tool_success_rate) }}</div>
        <div class="card-label">{{ t('agentStats.avgToolSuccess') }}</div>
      </div>

      <div class="overview-card" :class="stats.avg_retries > 1.0 ? 'rate-red' : ''">
        <div class="card-value">{{ stats.avg_retries.toFixed(1) }}</div>
        <div class="card-label">{{ t('agentStats.avgRetries') }}</div>
      </div>

      <div class="overview-card">
        <div class="card-value">{{ stats.span_per_trace.toFixed(1) }}</div>
        <div class="card-label">{{ t('agentStats.avgSpanPerTrace') }}</div>
      </div>
    </div>

    <!-- Tool usage table -->
    <div v-if="stats.tool_usage && stats.tool_usage.length > 0" class="tool-table-section">
      <h4 class="subsection-title">{{ t('agentStats.toolUsage') }}</h4>
      <table class="data-table">
        <thead>
          <tr>
            <th>Tool</th>
            <th>{{ t('agentStats.calls') }}</th>
            <th>{{ t('agentStats.successRate') }}</th>
            <th>{{ t('agentStats.avgRetriesCol') }}</th>
            <th>{{ t('agentStats.maxLoop') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(item, i) in stats.tool_usage" :key="i">
            <td class="td-tool">{{ item.tool_name }}</td>
            <td>{{ item.call_count }}</td>
            <td :class="rateCellClass(item.success_rate)">{{ formatRate(item.success_rate) }}</td>
            <td>{{ item.avg_retries.toFixed(1) }}</td>
            <td>
              {{ item.max_loop }}
              <span v-if="item.max_loop >= 3" class="loop-warning-icon">⚠️</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Insight card -->
    <div v-if="stats.insights && stats.insights.length > 0" class="insight-card">
      <div class="insight-header">
        <span class="insight-icon">💡</span>
        <span class="insight-title">{{ t('agentStats.insight') }}</span>
      </div>
      <ul class="insight-list">
        <li v-for="(insight, i) in stats.insights" :key="i">{{ insight }}</li>
      </ul>
    </div>
  </div>

  <div v-else-if="loading" class="loading-state">{{ t('common.loading') }}</div>

  <div v-else-if="error" class="error-state">{{ error }}</div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { AgentStats } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  stats: AgentStats | null
  loading: boolean
  error: string
}>()

function rateClass(rate: number): string {
  if (rate >= 0.9) return 'rate-green'
  if (rate >= 0.7) return 'rate-yellow'
  return 'rate-red'
}

function rateCellClass(rate: number): string {
  if (rate >= 0.9) return 'cell-green'
  if (rate >= 0.7) return 'cell-yellow'
  return 'cell-red'
}

function formatRate(rate: number): string {
  return `${Math.round(rate * 100)}%`
}
</script>

<style scoped>
.agent-stats-section {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.section-subtitle {
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 400;
}

/* Overview cards */
.overview-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.overview-card {
  text-align: center;
  padding: 16px 8px;
  border-radius: 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
}

.card-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-primary);
}

.card-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

/* Rate color classes for cards */
.rate-green .card-value { color: #22c55e; }
.rate-green { border-left: 3px solid #22c55e; }
.rate-yellow .card-value { color: #f59e0b; }
.rate-yellow { border-left: 3px solid #f59e0b; }
.rate-red .card-value { color: #ef4444; }
.rate-red { border-left: 3px solid #ef4444; }

/* Tool table */
.tool-table-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.subsection-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th,
.data-table td {
  text-align: left;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-default);
  font-size: 14px;
  color: var(--text-primary);
}

.data-table th {
  color: var(--text-secondary);
  font-weight: 600;
  font-size: 12px;
}

.td-tool {
  font-weight: 600;
}

/* Rate color classes for table cells */
.cell-green { color: #22c55e; }
.cell-yellow { color: #f59e0b; }
.cell-red { color: #ef4444; }

.loop-warning-icon {
  margin-left: 4px;
}

/* Insight card */
.insight-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  padding: 14px 18px;
}

.insight-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.insight-icon {
  font-size: 18px;
}

.insight-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}

.insight-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.insight-list li {
  font-size: 13px;
  color: var(--text-secondary);
  padding-left: 16px;
  position: relative;
}

.insight-list li::before {
  content: '•';
  position: absolute;
  left: 0;
  color: var(--text-muted);
}

/* Loading & error states */
.loading-state {
  padding: 40px 20px;
  text-align: center;
  color: var(--text-secondary);
}

.error-state {
  padding: 40px 20px;
  text-align: center;
  color: #ef4444;
}
</style>
