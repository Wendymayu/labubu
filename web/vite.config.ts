import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': resolve(__dirname, 'src') },
    // swagger-ui pulls react/react-dom transitively; without dedupe Vite can
    // bundle two mismatched copies (top-level 18 vs swagger-ui's nested 19),
    // which crashes swagger-ui's React root at render. Force a single copy.
    dedupe: ['react', 'react-dom']
  },
  server: {
    port: 3001,
    proxy: {
      // Narrow to /api/v1: the SPA route /api-docs must NOT be proxied to the
      // backend (it would 404 on browser refresh). /api-docs falls through to
      // Vite's SPA fallback, which serves index.html so vue-router can render it.
      '/api/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets'
  }
})
