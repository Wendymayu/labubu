<template>
  <div class="app">
    <aside class="sidebar">
      <router-link to="/" class="app-title">Labubu</router-link>
      <nav class="app-nav">
        <router-link to="/traces">Trace</router-link>
      </nav>
    </aside>
    <main class="app-main">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
</script>

<style scoped>
.app { min-height: 100vh; display: flex; }
.sidebar {
  width: 200px;
  flex-shrink: 0;
  background: #000;
  border-right: 1px solid #334155;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.app-title { font-size: 18px; font-weight: 700; color: #38bdf8; text-decoration: none; }
.app-nav { display: flex; flex-direction: column; gap: 8px; }
.app-nav a { color: #94a3b8; text-decoration: none; font-size: 14px; padding: 6px 0; }
.app-nav a:hover { color: #e2e8f0; }
.app-nav a.router-link-active { color: #38bdf8; }
.app-main { flex: 1; padding: 24px; }
</style>
