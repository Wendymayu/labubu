<template>
  <div class="api-docs">
    <div ref="swaggerContainer" class="api-docs-container"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
// swagger-ui ships without bundled types; declare a minimal shim below.
import SwaggerUI from 'swagger-ui'
import 'swagger-ui/dist/swagger-ui.css'

const swaggerContainer = ref<HTMLElement | null>(null)

onMounted(() => {
  SwaggerUI({
    domNode: swaggerContainer.value,
    url: '/api/v1/openapi.json',
    docExpansion: 'list',
    tryItOutEnabled: false,
    supportedSubmitMethods: [],
    persistAuth: false,
  })
})
</script>

<style scoped>
.api-docs {
  background: var(--bg-primary);
  min-height: calc(100vh - 48px);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  overflow: hidden;
}
.api-docs-container :deep(.swagger-ui) {
  background: var(--bg-primary);
}
</style>

<style>
/* swagger-ui ships a light theme; in dark mode its #3b4151 text is dark-on-dark
   and unreadable. Override the main surfaces to dark bg + light text (using the
   app's theme vars). Light mode is left to swagger-ui's native theme. */
html[data-theme="dark"] .api-docs .swagger-ui { color: var(--text-primary); }
html[data-theme="dark"] .api-docs .swagger-ui .wrapper,
html[data-theme="dark"] .api-docs .swagger-ui .scheme-container,
html[data-theme="dark"] .api-docs .swagger-ui .info,
html[data-theme="dark"] .api-docs .swagger-ui .opblock,
html[data-theme="dark"] .api-docs .swagger-ui .opblock-section,
html[data-theme="dark"] .api-docs .swagger-ui .opblock-section-header,
html[data-theme="dark"] .api-docs .swagger-ui .responses-inner,
html[data-theme="dark"] .api-docs .swagger-ui .params,
html[data-theme="dark"] .api-docs .swagger-ui table thead tr td,
html[data-theme="dark"] .api-docs .swagger-ui table thead tr th {
  background: var(--bg-surface);
  color: var(--text-primary);
  border-color: var(--border-default);
}
html[data-theme="dark"] .api-docs .swagger-ui .opblock-summary-path,
html[data-theme="dark"] .api-docs .swagger-ui .opblock-summary-description,
html[data-theme="dark"] .api-docs .swagger-ui .parameter__name,
html[data-theme="dark"] .api-docs .swagger-ui .parameter__type,
html[data-theme="dark"] .api-docs .swagger-ui .response-col_description,
html[data-theme="dark"] .api-docs .swagger-ui .info__title,
html[data-theme="dark"] .api-docs .swagger-ui .info p,
html[data-theme="dark"] .api-docs .swagger-ui .info li,
html[data-theme="dark"] .api-docs .swagger-ui .markdown p,
html[data-theme="dark"] .api-docs .swagger-ui .model-title {
  color: var(--text-primary);
}
html[data-theme="dark"] .api-docs .swagger-ui pre {
  background: var(--bg-surface-deep);
  color: var(--text-primary);
}
html[data-theme="dark"] .api-docs .swagger-ui input[type="text"],
html[data-theme="dark"] .api-docs .swagger-ui select,
html[data-theme="dark"] .api-docs .swagger-ui textarea {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border-color: var(--border-default);
}
html[data-theme="dark"] .api-docs .swagger-ui a { color: var(--accent-blue); }
/* expanded opblock: parameter/response descriptions, table cells, models, response code */
html[data-theme="dark"] .api-docs .swagger-ui .opblock-body,
html[data-theme="dark"] .api-docs .swagger-ui .parameters-col_description,
html[data-theme="dark"] .api-docs .swagger-ui .parameter__in,
html[data-theme="dark"] .api-docs .swagger-ui .parameter__deprecated,
html[data-theme="dark"] .api-docs .swagger-ui table.params td,
html[data-theme="dark"] .api-docs .swagger-ui .response-col_status,
html[data-theme="dark"] .api-docs .swagger-ui .response-col_links,
html[data-theme="dark"] .api-docs .swagger-ui .model-container,
html[data-theme="dark"] .api-docs .swagger-ui .model-box,
html[data-theme="dark"] .api-docs .swagger-ui .models,
html[data-theme="dark"] .api-docs .swagger-ui .prop-type,
html[data-theme="dark"] .api-docs .swagger-ui .prop-format {
  color: var(--text-primary);
}
html[data-theme="dark"] .api-docs .swagger-ui .microlight,
html[data-theme="dark"] .api-docs .swagger-ui .highlight-code {
  background: var(--bg-surface-deep);
  color: var(--text-primary);
}
/* section headers (h4 "Parameters"/"Responses" has hardcoded #3b4151) + op description */
html[data-theme="dark"] .api-docs .swagger-ui h1,
html[data-theme="dark"] .api-docs .swagger-ui h2,
html[data-theme="dark"] .api-docs .swagger-ui h3,
html[data-theme="dark"] .api-docs .swagger-ui h4,
html[data-theme="dark"] .api-docs .swagger-ui h5,
html[data-theme="dark"] .api-docs .swagger-ui h6,
html[data-theme="dark"] .api-docs .swagger-ui .opblock-description,
html[data-theme="dark"] .api-docs .swagger-ui .opblock-description p {
  color: var(--text-primary);
}
</style>
