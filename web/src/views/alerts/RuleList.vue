<template>
  <div class="rule-list-page">
    <div class="page-header">
      <h2>{{ t('alerts.rules') }}</h2>
      <router-link to="/alerts/rules/new" class="btn-primary">{{ t('alerts.newRule') }}</router-link>
    </div>

    <table v-if="rules.length > 0" class="data-table">
      <thead>
        <tr>
          <th>{{ t('alerts.name') }}</th>
          <th>{{ t('alerts.metric') }}</th>
          <th>{{ t('alerts.status') }}</th>
          <th>{{ t('alerts.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="rule in rules" :key="rule.id">
          <td>{{ rule.name }}</td>
          <td>{{ rule.metric }}</td>
          <td>
            <span :class="rule.enabled ? 'badge-green' : 'badge-gray'">
              {{ rule.enabled ? t('alerts.enabled') : t('alerts.disabled') }}
            </span>
          </td>
          <td class="actions-cell">
            <router-link :to="`/alerts/rules/${rule.id}/edit`" class="btn-sm">{{ t('alerts.edit') }}</router-link>
            <button @click="toggleRule(rule)" class="btn-sm">{{ rule.enabled ? t('alerts.disable') : t('alerts.enable') }}</button>
            <button @click="confirmDelete(rule)" class="btn-sm btn-danger">{{ t('alerts.delete') }}</button>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-else class="empty-msg">{{ t('alerts.noRules') }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listRules, deleteRule, updateRule, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const rules = ref<AlertRule[]>([])

async function load() {
  try {
    const resp = await listRules()
    rules.value = resp.rules
  } catch (e) {
    console.error('Failed to load alert rules:', e)
  }
}

async function toggleRule(rule: AlertRule) {
  try {
    await updateRule(rule.id, { enabled: !rule.enabled })
    await load()
  } catch (e) {
    console.error('Failed to toggle rule:', e)
  }
}

async function confirmDelete(rule: AlertRule) {
  if (!confirm(`${t('alerts.confirmDelete')} "${rule.name}"?`)) return
  try {
    await deleteRule(rule.id)
    await load()
  } catch (e) {
    console.error('Failed to delete rule:', e)
  }
}

onMounted(load)
</script>

<style scoped>
.rule-list-page { max-width: 900px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 20px; color: var(--text-primary); }
.btn-primary {
  background: var(--accent-blue); color: #fff; border: none; padding: 8px 16px;
  border-radius: 6px; text-decoration: none; font-size: 14px; cursor: pointer;
}
.btn-primary:hover { opacity: 0.9; }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { text-align: left; padding: 12px 8px; border-bottom: 1px solid var(--border-default); font-size: 14px; }
.data-table th { color: var(--text-secondary); font-weight: 600; }
.actions-cell { display: flex; gap: 8px; }
.btn-sm {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 4px 10px; border-radius: 4px; font-size: 13px; cursor: pointer; text-decoration: none;
}
.btn-sm:hover { background: var(--bg-hover); }
.btn-danger { color: var(--danger-red); border-color: var(--danger-red); }
.btn-danger:hover { background: var(--danger-red); color: #fff; }
.badge-green { background: #d4edda; color: #155724; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.badge-gray { background: #e2e3e5; color: #383d41; padding: 2px 8px; border-radius: 10px; font-size: 12px; }
.empty-msg { color: var(--text-secondary); margin-top: 40px; text-align: center; }
</style>
