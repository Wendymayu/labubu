<template>
  <div class="diagnosis-tab">
    <!-- State 1: Empty (no result, no error) -->
    <div v-if="!loading && !result && !error" class="diagnosis-empty">
      <div class="empty-icon">🔍</div>
      <p>{{ t('diagnosis.empty') }}</p>
      <button class="btn-diagnose" :disabled="noModel" @click="emit('diagnose')">
        {{ t('diagnosis.start') }}
      </button>
      <p v-if="noModel" class="hint-no-model">
        <router-link to="/llm-configs">{{ t('diagnosis.noModel') }}</router-link>
      </p>
    </div>

    <!-- State 2: Error -->
    <div v-if="!loading && !result && error" class="diagnosis-error">
      <div class="error-icon">⚠️</div>
      <p class="error-title">{{ t('diagnosis.failed') }}</p>
      <p class="error-detail">{{ error }}</p>
      <button class="btn-diagnose" :disabled="noModel" @click="emit('diagnose')">
        {{ t('diagnosis.retry') }}
      </button>
      <p v-if="noModel" class="hint-no-model">
        <router-link to="/llm-configs">{{ t('diagnosis.noModel') }}</router-link>
      </p>
    </div>

    <!-- State 2: Loading -->
    <div v-else-if="loading" class="diagnosis-loading">
      <div class="spinner"></div>
      <p>{{ t('diagnosis.analyzing') }}</p>
      <p class="est-time">{{ t('diagnosis.estTime') }}</p>
    </div>

    <!-- State 3: Result -->
    <div v-else-if="result" class="diagnosis-result">
      <!-- Stale banner -->
      <div v-if="result.stale" class="stale-banner">
        {{ t('diagnosis.stale') }}
      </div>

      <!-- Header -->
      <div class="result-header">
        <span class="overall-score" :class="scoreColorClass(result.overall_score)">
          {{ t('diagnosis.overall') }}: {{ result.overall_score }}/100
        </span>
        <span class="model-name">{{ t('diagnosis.modelLabel') }}: {{ result.model_name }}</span>
        <span class="timestamp">{{ formatTime(result.created_at) }}</span>
        <button class="btn-rediagnose" @click="exportMarkdown">
          {{ t('diagnosis.exportMarkdown') }}
        </button>
        <button class="btn-rediagnose" @click="emit('diagnose')">
          {{ t('diagnosis.rediagnose') }}
        </button>
      </div>

      <!-- Score cards -->
      <div class="score-cards">
        <div v-for="dim in dimensions" :key="dim.key" class="score-card" :class="scoreColorClass(result.scores[dim.key])">
          <div class="score-value">{{ result.scores[dim.key] }}</div>
          <div class="score-label">{{ t(`diagnosis.${dim.key}`) }}</div>
        </div>
      </div>

      <!-- Summary -->
      <p class="diagnosis-summary">{{ result.summary }}</p>

      <!-- Findings grouped by severity -->
      <div v-if="criticalFindings.length" class="findings-section">
        <h4>{{ t('diagnosis.critical') }} ({{ criticalFindings.length }})</h4>
        <div v-for="(f, i) in criticalFindings" :key="'crit-'+i" class="finding-card severity-error" @click="onFindingClick(f)">
          <span class="severity-badge error">{{ f.severity }}</span>
          <span class="dimension-tag">{{ f.dimension }}</span>
          <strong>{{ f.title }}</strong>
          <p>{{ f.description }}</p>
          <p class="suggestion">💡 {{ f.suggestion }}</p>
        </div>
      </div>

      <div v-if="otherFindings.length" class="findings-section">
        <h4>{{ t('diagnosis.suggestions') }} ({{ otherFindings.length }})</h4>
        <div v-for="(f, i) in otherFindings" :key="'other-'+i" class="finding-card" :class="'severity-'+f.severity" @click="onFindingClick(f)">
          <span class="severity-badge" :class="f.severity">{{ f.severity }}</span>
          <span class="dimension-tag">{{ f.dimension }}</span>
          <strong>{{ f.title }}</strong>
          <p>{{ f.description }}</p>
          <p class="suggestion">💡 {{ f.suggestion }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DiagnosisResult, DiagnosisFinding } from '../api/client'

const { t } = useI18n()

const props = defineProps<{
  result: DiagnosisResult | null
  loading: boolean
  noModel: boolean
  error: string
}>()

const emit = defineEmits<{
  diagnose: []
  'navigate-span': [spanIndex: number]
}>()

const dimensions = [
  { key: 'latency' as const },
  { key: 'cost' as const },
  { key: 'error' as const },
  { key: 'efficiency' as const },
]

const criticalFindings = computed(() =>
  props.result?.findings.filter(f => f.severity === 'error') ?? []
)

const otherFindings = computed(() =>
  props.result?.findings.filter(f => f.severity !== 'error') ?? []
)

function scoreColorClass(score: number): string {
  if (score >= 80) return 'score-good'
  if (score >= 60) return 'score-fair'
  return 'score-poor'
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  const now = Date.now()
  const diff = now - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return d.toLocaleDateString()
}

function onFindingClick(finding: { span_index?: number }) {
  if (finding.span_index !== undefined) {
    emit('navigate-span', finding.span_index)
  }
}

/** Render the diagnosis result as a Markdown report, mirroring the on-screen layout. */
function diagnosisToMarkdown(result: DiagnosisResult): string {
  const L = (k: string) => t(`diagnosis.${k}`)
  const out: string[] = []

  out.push(`# ${L('tab')}`)
  out.push('')
  out.push(`**${L('traceId')}**: \`${result.trace_id_hex}\``)
  out.push(`**${L('modelLabel')}**: ${result.model_name}`)
  out.push(`**${L('generated')}**: ${new Date(result.created_at).toLocaleString()}`)
  out.push(`**${L('overall')}**: ${result.overall_score}/100`)
  out.push('')

  if (result.stale) {
    out.push(`> ⚠️ ${L('stale')}`)
    out.push('')
  }

  out.push(`## ${L('dimensionScores')}`)
  out.push('')
  for (const d of dimensions) {
    out.push(`- **${L(d.key)}**: ${result.scores[d.key]}/100`)
  }
  out.push('')

  out.push(`## ${L('summary')}`)
  out.push('')
  out.push(result.summary)
  out.push('')

  const writeFindings = (label: string, items: DiagnosisFinding[]) => {
    if (!items.length) return
    out.push(`## ${label} (${items.length})`)
    out.push('')
    for (const f of items) {
      out.push(`### ${f.title}`)
      const meta = [`\`${f.severity}\``, `\`${f.dimension}\``]
      if (f.span_name) meta.push(`${L('span')}: ${f.span_name}`)
      out.push(meta.join(' · '))
      out.push('')
      out.push(f.description)
      out.push('')
      out.push(`💡 ${f.suggestion}`)
      out.push('')
    }
  }
  writeFindings(L('critical'), result.findings.filter(f => f.severity === 'error'))
  writeFindings(L('suggestions'), result.findings.filter(f => f.severity !== 'error'))

  return out.join('\n')
}

function downloadBlob(content: string, filename: string) {
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

function exportMarkdown() {
  const r = props.result
  if (!r) return
  downloadBlob(diagnosisToMarkdown(r), `diagnosis-${r.trace_id_hex}.md`)
}
</script>

<style scoped>
.diagnosis-tab {
  padding: 20px;
}

.diagnosis-empty {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.btn-diagnose {
  margin-top: 16px;
  padding: 10px 24px;
  background: var(--accent-blue);
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 14px;
  cursor: pointer;
}

.btn-diagnose:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.hint-no-model {
  margin-top: 8px;
  font-size: 12px;
}

.hint-no-model a {
  color: var(--accent-blue);
}

.diagnosis-error {
  text-align: center;
  padding: 40px 20px;
  color: var(--text-secondary);
}

.error-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.error-title {
  font-size: 16px;
  font-weight: 600;
  color: #ef4444;
  margin-bottom: 8px;
}

.error-detail {
  font-size: 13px;
  color: var(--text-secondary);
  max-width: 500px;
  margin: 0 auto 20px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  padding: 12px 16px;
  word-break: break-word;
}

.diagnosis-loading {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-secondary);
}

.spinner {
  width: 40px;
  height: 40px;
  margin: 0 auto 16px;
  border: 3px solid var(--border-default);
  border-top-color: var(--accent-blue);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.est-time {
  font-size: 12px;
  margin-top: 8px;
}

.result-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.overall-score {
  font-size: 20px;
  font-weight: 700;
  padding: 6px 14px;
  border-radius: 8px;
}

.model-name {
  color: var(--text-secondary);
  font-size: 13px;
}

.timestamp {
  color: var(--text-secondary);
  font-size: 12px;
  margin-left: auto;
}

.btn-rediagnose {
  padding: 6px 14px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

.btn-rediagnose:hover {
  background: var(--bg-hover);
}

.score-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 20px;
}

.score-card {
  text-align: center;
  padding: 16px 8px;
  border-radius: 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
}

.score-value {
  font-size: 32px;
  font-weight: 700;
}

.score-label {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.score-good .score-value { color: #22c55e; }
.score-fair .score-value { color: #eab308; }
.score-poor .score-value { color: #ef4444; }

.score-good { border-left: 3px solid #22c55e; }
.score-fair { border-left: 3px solid #eab308; }
.score-poor { border-left: 3px solid #ef4444; }

.stale-banner {
  background: #fef3c7;
  color: #92400e;
  padding: 8px 14px;
  border-radius: 6px;
  font-size: 13px;
  margin-bottom: 16px;
}

.diagnosis-summary {
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.5;
  margin-bottom: 24px;
  padding: 12px;
  background: var(--bg-surface);
  border-radius: 6px;
}

.findings-section {
  margin-bottom: 20px;
}

.findings-section h4 {
  font-size: 14px;
  margin-bottom: 10px;
  color: var(--text-primary);
}

.finding-card {
  padding: 14px;
  margin-bottom: 10px;
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: 6px;
  cursor: default;
}

.finding-card.severity-error {
  border-left: 3px solid #ef4444;
}

.finding-card.severity-warning {
  border-left: 3px solid #eab308;
}

.finding-card.severity-info {
  border-left: 3px solid #3b82f6;
}

.finding-card strong {
  display: inline;
  font-size: 14px;
}

.finding-card p {
  margin: 6px 0 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.suggestion {
  color: var(--text-primary) !important;
  font-style: italic;
}

.severity-badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  margin-right: 8px;
}

.severity-badge.error { background: #fecaca; color: #991b1b; }
.severity-badge.warning { background: #fef3c7; color: #92400e; }
.severity-badge.info { background: #dbeafe; color: #1e40af; }

.dimension-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  background: var(--bg-hover);
  color: var(--text-secondary);
  margin-right: 8px;
}
</style>