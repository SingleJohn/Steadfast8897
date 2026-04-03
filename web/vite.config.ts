import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

/** Use IPv4 loopback: Node often resolves `localhost` to ::1 while the Go server is only reachable via 127.0.0.1, which causes proxy "socket hang up" / ECONNRESET. */
const FYMS_BACKEND = 'http://127.0.0.1:8961'

const embyProxy = {
  target: FYMS_BACKEND,
  changeOrigin: true,
} as const

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 3001,
    proxy: {
      '/api': embyProxy,
      '/Users': embyProxy,
      '/System': embyProxy,
      '/Items': embyProxy,
      '/Videos': embyProxy,
      '/Sessions': embyProxy,
      '/Library': embyProxy,
      '/Startup': embyProxy,
      '/Branding': embyProxy,
      '/DisplayPreferences': embyProxy,
      '/emby': embyProxy,
      '/Genres': embyProxy,
      '/ApiKeys': embyProxy,
      '/user_usage_stats': embyProxy,
      '/Stats': embyProxy,
      '/Auth': embyProxy,
      '/Webhook': embyProxy,
      '/Gateway': embyProxy,
      '/Plugins': embyProxy,
      '/Shows': embyProxy,
    },
  },
  build: {
    outDir: 'dist',
  },
})
