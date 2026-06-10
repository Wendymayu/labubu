<template>
  <div class="rule-form-page">
    <h2>{{ isEdit ? t('alerts.editRule') : t('alerts.newRule') }}</h2>

    <form @submit.prevent="save" class="rule-form">
      <div class="form-group">
        <label>{{ t('alerts.name') }}</label>
        <input v-model="form.name" type="text" required class="form-input" />
      </div>

      <div class="form-group">
        <label>{{ t('alerts.metric') }}</label>
        <select v-model="form.metric" class="form-input">
          <option value="total_tokens">Total Tokens</option>
          <option value="input_tokens">Input Tokens</option>
          <option value="output_tokens">Output Tokens</option>
        </select>
      </div>

      <div class="form-group">
        <label>{{ t('alerts.conditions') }}</label>
        <div v-for="(cond, i) in form.conditions" :key="i" class="condition-row">
          <select v-model="cond.field" class="cond-field form-input">
            <option value="total_tokens">Total Tokens</option>
            <option value="input_tokens">Input Tokens</option>
            <option value="output_tokens">Output Tokens</option>
            <option value="model">Model</option>
          </select>
          <select v-model="cond.op" class="cond-op form-input">
            <option value="gt">&gt;</option>
            <option value="gte">&gt;=</option>
            <option value="lt">&lt;</option>
            <option value="lte">&lt;=</option>
            <option value="eq">=</option>
            <option value="neq">!=</option>
          </select>
          <input v-model="cond.value" type="text" class="cond-value form-input" placeholder="Value" />
          <button type="button" @click="removeCondition(i)" class="btn-sm btn-danger">&times;</button>
          <span v-if="i < form.conditions.length - 1" class="and-label">AND</span>
        </div>
        <button type="button" @click="addCondition" class="btn-sm add-cond-btn">{{ t('alerts.addCondition') }}</button>
      </div>

      <div class="form-row">
        <div class="form-group flex-1">
          <label>{{ t('alerts.forDuration') }} (s)</label>
          <input v-model.number="form.for_duration" type="number" min="0" class="form-input" />
        </div>
        <div class="form-group flex-1">
          <label>{{ t('alerts.interval') }} (s)</label>
          <input v-model.number="form.interval" type="number" min="15" class="form-input" />
        </div>
      </div>

      <fieldset class="smtp-section">
        <legend>{{ t('alerts.emailConfig') }}</legend>
        <div class="form-row">
          <div class="form-group flex-2">
            <label>SMTP Host</label>
            <input v-model="form.notifier.smtp_host" type="text" class="form-input" placeholder="smtp.gmail.com" />
          </div>
          <div class="form-group flex-1">
            <label>Port</label>
            <input v-model.number="form.notifier.smtp_port" type="number" class="form-input" placeholder="587" />
          </div>
        </div>
        <div class="form-row">
          <div class="form-group flex-1">
            <label>Username</label>
            <input v-model="form.notifier.username" type="text" class="form-input" />
          </div>
          <div class="form-group flex-1">
            <label>Password</label>
            <input v-model="form.notifier.password" type="password" class="form-input" />
          </div>
        </div>
        <div class="form-group">
          <label>{{ t('alerts.recipients') }}</label>
          <input v-model="recipientsStr" type="text" class="form-input" placeholder="a@b.com, c@d.com" />
        </div>
      </fieldset>

      <div class="form-actions">
        <button type="submit" class="btn-primary" :disabled="saving">{{ t('alerts.save') }}</button>
        <router-link to="/alerts/rules" class="btn-cancel">{{ t('alerts.cancel') }}</router-link>
      </div>

      <p v-if="error" class="error-msg">{{ error }}</p>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { createRule, updateRule, getRule, type AlertCondition, type AlertRule } from '../../api/alerts'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const isEdit = computed(() => !!route.params.id)
const saving = ref(false)
const error = ref('')

const defaultForm = () => ({
  name: '',
  metric: 'total_tokens',
  conditions: [{ field: 'total_tokens', op: 'gt', value: '' }] as AlertCondition[],
  for_duration: 300,
  interval: 60,
  notifier: {
    type: 'email',
    smtp_host: '',
    smtp_port: 587,
    username: '',
    password: '',
    recipients: [] as string[],
  },
})

const form = ref(defaultForm())
const recipientsStr = ref('')

onMounted(async () => {
  if (isEdit.value) {
    try {
      const rule = await getRule(route.params.id as string)
      form.value = {
        name: rule.name,
        metric: rule.metric,
        conditions: rule.conditions.length > 0 ? rule.conditions : [{ field: 'total_tokens', op: 'gt', value: '' }],
        for_duration: rule.for_duration,
        interval: rule.interval,
        notifier: { ...rule.notifier },
      }
      recipientsStr.value = (rule.notifier.recipients || []).join(', ')
    } catch (e: any) {
      error.value = e.message
    }
  }
})

function addCondition() {
  form.value.conditions.push({ field: 'total_tokens', op: 'gt', value: '' })
}

function removeCondition(i: number) {
  if (form.value.conditions.length > 1) {
    form.value.conditions.splice(i, 1)
  }
}

async function save() {
  saving.value = true
  error.value = ''

  form.value.notifier.recipients = recipientsStr.value
    .split(',')
    .map(s => s.trim())
    .filter(s => s.length > 0)

  try {
    if (isEdit.value) {
      await updateRule(route.params.id as string, form.value as any)
    } else {
      await createRule(form.value as any)
    }
    router.push('/alerts/rules')
  } catch (e: any) {
    error.value = e.message
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.rule-form-page { max-width: 700px; }
.rule-form-page h2 { margin-bottom: 24px; color: var(--text-primary); }
.rule-form { display: flex; flex-direction: column; gap: 16px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-group label { font-size: 13px; color: var(--text-secondary); font-weight: 600; }
.form-input { padding: 8px 12px; border: 1px solid var(--border-default); border-radius: 6px; font-size: 14px; background: var(--bg-primary); color: var(--text-primary); }
.form-input:focus { outline: none; border-color: var(--accent-blue); }
.form-row { display: flex; gap: 16px; }
.flex-1 { flex: 1; }
.flex-2 { flex: 2; }
.condition-row { display: flex; gap: 8px; align-items: center; margin-bottom: 6px; }
.cond-field { width: 130px; }
.cond-op { width: 70px; }
.cond-value { flex: 1; }
.and-label { color: var(--text-secondary); font-size: 12px; font-weight: 700; margin: 0 4px; white-space: nowrap; }
.add-cond-btn { margin-top: 4px; align-self: flex-start; }
.smtp-section { border: 1px solid var(--border-default); border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.smtp-section legend { font-size: 14px; font-weight: 600; color: var(--text-primary); padding: 0 8px; }
.form-actions { display: flex; gap: 12px; align-items: center; margin-top: 8px; }
.btn-primary {
  background: var(--accent-blue); color: #fff; border: none; padding: 10px 24px;
  border-radius: 6px; font-size: 14px; cursor: pointer;
}
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-cancel {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 10px 24px; border-radius: 6px; font-size: 14px; text-decoration: none;
}
.btn-sm {
  background: var(--bg-secondary); border: 1px solid var(--border-default); color: var(--text-primary);
  padding: 4px 10px; border-radius: 4px; font-size: 13px; cursor: pointer;
}
.btn-danger { color: var(--danger-red); border-color: var(--danger-red); }
.error-msg { color: var(--danger-red); font-size: 14px; }
</style>
