import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

/** Use IPv4 loopback: Node often resolves `localhost` to ::1 while the Go server is only reachable via 127.0.0.1, which causes proxy "socket hang up" / ECONNRESET. */
const FYMS_BACKEND = 'http://127.0.0.1:8961'

const embyProxy = {
  target: FYMS_BACKEND,
  changeOrigin: true,
} as const

// SSE 专用代理：禁用 Node http-proxy 的自动压缩/缓冲，否则 EventSource 会收到
// 残缺包断开。要点是 selfHandleResponse:false + ws:false，vite 默认配置已兼容，
// 显式写一次以便将来检索。
const sseProxy = {
  target: FYMS_BACKEND,
  changeOrigin: true,
  ws: false,
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
      '/Tasks/stream': sseProxy,
      '/Tasks': embyProxy,
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
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('naive-ui') || id.includes('vueuc') || id.includes('vdirs')) return 'vendor-naive'
          if (id.includes('swiper')) return 'vendor-swiper'
          if (id.includes('echarts') || id.includes('zrender') || id.includes('vue-echarts')) return 'vendor-charts'
          if (id.includes('artplayer') || id.includes('hls.js')) return 'vendor-player'
          if (id.includes('sql.js')) return 'vendor-sql'
          if (id.includes('vue') || id.includes('vue-router') || id.includes('pinia') || id.includes('@vueuse')) return 'vendor-vue'
          return 'vendor'
        },
      },
    },
  },
})
