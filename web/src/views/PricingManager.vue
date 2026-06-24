<template>
  <div class="pricing-manager">
    <div class="pricing-toolbar">
      <button class="btn btn-primary" @click="openAdd">{{ t('pricingManager.addModel') }}</button>
      <button class="btn" @click="handleRecalc" :disabled="recalcing">
        {{ recalcing ? t('pricingManager.recalculating') : t('pricingManager.recalcAll') }}
      </button>
    </div>

    <div v-if="showForm" class="pricing-form-overlay" @click.self="showForm = false">
      <div class="pricing-form">
        <h3>{{ editingModel ? t('pricingManager.editTitle') : t('pricingManager.addTitle') }}</h3>
        <label>{{ t('pricingManager.modelName') }}:
          <input v-model="form.model_name" placeholder="claude-opus-4-8" />
        </label>
        <label>{{ t('pricingManager.inputPriceHint') }}:
          <input v-model.number="form.input_price" type="number" step="0.01" min="0" />
        </label>
        <label>{{ t('pricingManager.outputPriceHint') }}:
          <input v-model.number="form.output_price" type="number" step="0.01" min="0" />
        </label>
        <label>{{ t('pricingManager.currency') }}:
          <select v-model="form.currency">
            <option value="USD">USD ($)</option>
            <option value="CNY">CNY (¥)</option>
          </select>
        </label>
        <div class="form-actions">
          <button class="btn btn-primary" @click="saveModel">{{ t('pricingManager.save') }}</button>
          <button class="btn" @click="showForm = false">{{ t('pricingManager.cancel') }}</button>
        </div>
      </div>
    </div>

    <table class="pricing-table" v-if="models.length > 0">
      <thead>
        <tr>
          <th>{{ t('pricingManager.modelName') }}</th>
          <th>{{ t('pricingManager.inputPrice') }}</th>
          <th>{{ t('pricingManager.outputPrice') }}</th>
          <th>{{ t('pricingManager.currency') }}</th>
          <th>{{ t('pricingManager.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in models" :key="m.model_name">
          <td>{{ m.model_name }}</td>
          <td>${{ m.input_price }}/1M</td>
          <td>${{ m.output_price }}/1M</td>
          <td>{{ m.currency }}</td>
          <td>
            <button class="btn btn-sm" @click="editModel(m)">{{ t('pricingManager.edit') }}</button>
            <button class="btn btn-sm btn-danger" @click="deleteModel(m.model_name)">{{ t('pricingManager.delete') }}</button>
          </td>
        </tr>
      </tbody>
    </table>
    <div v-else class="empty">{{ t('pricingManager.empty') }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getModelPricing, saveModelPricing, deleteModelPricing, recalcCosts, type ModelPricing } from '../api/client'

const { t } = useI18n()

const models = ref<ModelPricing[]>([])
const showForm = ref(false)
const editingModel = ref<ModelPricing | null>(null)
const recalcing = ref(false)

const form = ref<ModelPricing>({ model_name: '', input_price: 0, output_price: 0, currency: 'USD' })

async function fetchModels() {
  const result = await getModelPricing()
  models.value = result.models
}

function openAdd() {
  editingModel.value = null
  form.value = { model_name: '', input_price: 0, output_price: 0, currency: 'USD' }
  showForm.value = true
}

function editModel(m: ModelPricing) {
  editingModel.value = m
  form.value = { ...m }
  showForm.value = true
}

async function saveModel() {
  await saveModelPricing(form.value)
  showForm.value = false
  editingModel.value = null
  await fetchModels()
}

async function deleteModel(name: string) {
  if (!confirm(t('pricingManager.deleteConfirm', { name }))) return
  await deleteModelPricing(name)
  await fetchModels()
}

async function handleRecalc() {
  recalcing.value = true
  try {
    const result = await recalcCosts()
    alert(t('pricingManager.recalcDone', { count: result.traces_updated }))
  } catch (e: any) {
    alert(t('pricingManager.recalcFailed', { error: e.message }))
  } finally {
    recalcing.value = false
  }
}

onMounted(fetchModels)
</script>

<style scoped>
.pricing-manager { max-width: 800px; margin: 0 auto; }
.pricing-toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
.pricing-table { width: 100%; border-collapse: collapse; }
.pricing-table th, .pricing-table td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--border-default); }
.pricing-table th { font-size: 11px; color: var(--text-secondary); text-transform: uppercase; }
.pricing-form-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.pricing-form {
  background: var(--bg-primary); padding: 24px; border-radius: 8px;
  min-width: 360px; display: flex; flex-direction: column; gap: 12px;
}
.pricing-form h3 { margin-bottom: 8px; }
.pricing-form label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
.pricing-form input, .pricing-form select {
  padding: 6px 10px; border: 1px solid var(--border-default); border-radius: 4px;
  background: var(--bg-primary); color: var(--text-primary);
}
.form-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 8px; }
.btn { padding: 6px 12px; border: 1px solid var(--border-default); border-radius: 4px;
  background: var(--bg-secondary); color: var(--text-primary); cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--accent-blue); color: #fff; border-color: var(--accent-blue); }
.btn-danger { color: var(--status-error-accent); border-color: var(--status-error-accent); }
.btn-sm { padding: 3px 8px; font-size: 12px; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.empty { text-align: center; color: var(--text-secondary); padding: 40px; }
</style>
