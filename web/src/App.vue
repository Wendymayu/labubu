<template>
  <div class="app">
    <header class="app-header">
      <h1 class="app-title">Labubu</h1>
      <nav class="app-nav">
        <router-link to="/traces">Traces</router-link>
      </nav>
    </header>
    <main class="app-main">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
</script>

<style scoped>
.app { min-height: 100vh; display: flex; flex-direction: column; }
.app-header { display: flex; align-items: center; gap: 24px; padding: 12px 24px; background: #1e293b; border-bottom: 1px solid #334155; }
.app-title { font-size: 18px; font-weight: 700; color: #38bdf8; }
.app-nav a { color: #94a3b8; text-decoration: none; font-size: 14px; }
.app-nav a:hover { color: #e2e8f0; }
.app-nav a.router-link-active { color: #38bdf8; }
.app-main { flex: 1; padding: 24px; }
</style>
