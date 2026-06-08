<template>
  <div class="llm-config">
    <h2>LLM Configs</h2>

    <div class="toolbar">
      <button class="btn btn-primary" @click="openAdd">+ Add Model</button>
    </div>

    <!-- Form modal -->
    <div v-if="showForm" class="form-overlay" @click.self="closeForm">
      <div class="form-box">
        <h3>{{ editing ? 'Edit' : 'Add' }} LLM Model</h3>

        <label>Model Name:
          <input v-model="form.model_name" placeholder="claude-opus-4-8" />
        </label>
        <label>Provider URL:
          <input v-model="form.provider_url" placeholder="https://api.anthropic.com/v1/messages" />
        </label>
        <label>API Key:
          <input v-model="form.api_key" :placeholder="editing ? '(unchanged)' : 'sk-ant-...'" />
        </label>
        <label>Temperature:
          <input v-model.number="form.temperature" type="number" step="0.1" min="0" max="2" />
        </label>
        <label>Max Tokens:
          <input v-model.number="form.max_tokens" type="number" min="1" />
        </label>
        <label class="checkbox-label">
          <input type="checkbox" v-model="form.is_default" />
          Set as default model
        </label>

        <div class="form-actions">
          <button class="btn btn-primary" @click="saveConfig" :disabled="saving">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
          <button class="btn" @click="closeForm">Cancel</button>
        </div>
        <p v-if="saveError" class="form-error">{{ saveError }}</p>
      </div>
    </div>

    <!-- Config table -->
    <table v-if="configs.length > 0" class="config-table">
      <thead>
        <tr>
          <th>Model Name</th>
          <th>Provider URL</th>
          <th>API Key</th>
          <th>Default</th>
          <th>Temp</th>
          <th>Max Tokens</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in configs" :key="c.id">
          <td>{{ c.model_name }}</td>
          <td class="url-cell">{{ c.provider_url }}</td>
          <td><code>{{ c.api_key }}</code></td>
          <td>
            <span v-if="c.is_default" class="default-star">&#9733;</span>
            <button v-else class="btn btn-sm" @click="setDefault(c)">Set Default</button>
          </td>
          <td>{{ c.temperature }}</td>
          <td>{{ c.max_tokens }}</td>
          <td>
            <button class="btn btn-sm" @click="editConfig(c)">Edit</button>
            <button class="btn btn-sm btn-danger" @click="deleteConfig(c)">Delete</button>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-else class="empty">
      No LLM models configured. Add a model to enable LLM-powered trace analysis.
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import type { LlmConfig } from '../api/client'
import {
  listLlmConfigs, createLlmConfig, updateLlmConfig, deleteLlmConfig,
} from '../api/client'

const configs = ref<LlmConfig[]>([])
const showForm = ref(false)
const editing = ref<LlmConfig | null>(null)
const saving = ref(false)
const saveError = ref('')

const form = reactive<Omit<LlmConfig, 'id'> & { id?: string }>({
  model_name: '',
  provider_url: '',
  api_key: '',
  is_default: false,
  temperature: 0.7,
  max_tokens: 4096,
})

async function loadConfigs() {
  try {
    const data = await listLlmConfigs()
    configs.value = data.configs || []
  } catch {
    configs.value = []
  }
}

function openAdd() {
  editing.value = null
  form.model_name = ''
  form.provider_url = ''
  form.api_key = ''
  form.is_default = false
  form.temperature = 0.7
  form.max_tokens = 4096
  form.id = undefined
  saveError.value = ''
  showForm.value = true
}

function editConfig(c: LlmConfig) {
  editing.value = c
  form.model_name = c.model_name
  form.provider_url = c.provider_url
  form.api_key = '***'
  form.is_default = c.is_default
  form.temperature = c.temperature
  form.max_tokens = c.max_tokens
  form.id = c.id
  saveError.value = ''
  showForm.value = true
}

function closeForm() {
  showForm.value = false
  editing.value = null
}

async function saveConfig() {
  saveError.value = ''
  if (!form.model_name || !form.provider_url) {
    saveError.value = 'Model name and provider URL are required.'
    return
  }
  if (!editing.value && !form.api_key) {
    saveError.value = 'API key is required.'
    return
  }
  saving.value = true
  try {
    if (editing.value && form.id) {
      await updateLlmConfig(form.id, form as LlmConfig)
    } else {
      await createLlmConfig(form as Omit<LlmConfig, 'id'>)
    }
    closeForm()
    await loadConfigs()
  } catch (e: any) {
    saveError.value = e.message || 'Save failed'
  } finally {
    saving.value = false
  }
}

async function setDefault(c: LlmConfig) {
  try {
    await updateLlmConfig(c.id, {
      ...c,
      api_key: '***',
      is_default: true,
    })
    await loadConfigs()
  } catch (e: any) {
    alert('Failed to set default: ' + e.message)
  }
}

async function deleteConfig(c: LlmConfig) {
  let msg = `Delete LLM config "${c.model_name}"?`
  if (c.is_default) {
    msg = `"${c.model_name}" is the active default model. Delete anyway?`
  }
  if (!confirm(msg)) return
  try {
    await deleteLlmConfig(c.id)
    await loadConfigs()
  } catch (e: any) {
    alert('Delete failed: ' + e.message)
  }
}

onMounted(loadConfigs)
</script>

<style scoped>
.llm-config { max-width: 960px; }
.llm-config h2 { margin-bottom: 16px; }
.toolbar { margin-bottom: 16px; }

.config-table {
  width: 100%;
  border-collapse: collapse;
}
.config-table th, .config-table td {
  padding: 8px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border-default);
}
.config-table th {
  font-size: 11px;
  color: var(--text-secondary);
  text-transform: uppercase;
}
.url-cell {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.default-star {
  color: var(--accent-blue);
  font-size: 18px;
}

.form-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.form-box {
  background: var(--bg-primary);
  padding: 24px;
  border-radius: 8px;
  min-width: 420px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.form-box h3 { margin-bottom: 8px; }
.form-box label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}
.form-box input {
  padding: 6px 10px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-primary);
  color: var(--text-primary);
}
.checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 8px !important;
}
.checkbox-label input {
  width: auto;
}
.form-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 8px;
}
.form-error {
  color: var(--status-error-accent);
  font-size: 13px;
}

.btn {
  padding: 6px 12px;
  border: 1px solid var(--border-default);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
}
.btn-primary {
  background: var(--accent-blue);
  color: #fff;
  border-color: var(--accent-blue);
}
.btn-danger {
  color: var(--status-error-accent);
  border-color: var(--status-error-accent);
}
.btn-sm { padding: 3px 8px; font-size: 12px; }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }

.empty {
  text-align: center;
  color: var(--text-muted);
  padding: 40px;
}
</style>
