<template>
  <div class="api-docs">
    <div ref="swaggerContainer" class="api-docs-container"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
// swagger-ui ships without bundled types; declare a minimal shim below.
// @ts-ignore - no types available for swagger-ui
import SwaggerUI from 'swagger-ui'

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
