// 单例 SSE + 响应式历史，同 useTaskStream 的引用计数模型。
// 所有使用者（Overview 三张卡、AdminLayout 顶栏胶囊）共享一条连接。
import { onBeforeUnmount, reactive, ref } from 'vue'

import {
  getSystemMetrics,
  systemMetricsStreamUrl,
  type SysMetricsSnapshot,
} from '@/api/sysmetrics'

const HISTORY_LIMIT = 60

const history = reactive<SysMetricsSnapshot[]>([])
const current = ref<SysMetricsSnapshot | null>(null)
const connected = ref(false)
const lastError = ref('')

let es: EventSource | null = null
let refCount = 0
let retryTimer: number | null = null
let retryDelay = 1000
let closeTimer: number | null = null

function getToken(): string | null {
  return localStorage.getItem('accessToken')
}

function pushSample(s: SysMetricsSnapshot) {
  current.value = s
  history.push(s)
  while (history.length > HISTORY_LIMIT) history.shift()
}

function clearRetry() {
  if (retryTimer != null) {
    window.clearTimeout(retryTimer)
    retryTimer = null
  }
}

function scheduleReconnect() {
  clearRetry()
  retryTimer = window.setTimeout(() => {
    retryTimer = null
    if (refCount > 0) open()
  }, retryDelay)
  retryDelay = Math.min(retryDelay * 2, 30_000)
}

function open() {
  if (es) return
  const url = systemMetricsStreamUrl(getToken())
  try {
    es = new EventSource(url)
  } catch (e) {
    lastError.value = (e as Error).message
    scheduleReconnect()
    return
  }

  es.addEventListener('metric', (ev: MessageEvent) => {
    try {
      pushSample(JSON.parse(ev.data) as SysMetricsSnapshot)
    } catch {
      /* 忽略单条解析错误 */
    }
  })

  es.onopen = () => {
    connected.value = true
    lastError.value = ''
    retryDelay = 1000
  }

  es.onerror = () => {
    connected.value = false
    lastError.value = 'stream disconnected'
    try { es?.close() } catch { /* ignore */ }
    es = null
    if (refCount > 0) scheduleReconnect()
  }
}

function close() {
  clearRetry()
  if (es) { try { es.close() } catch { /* ignore */ } es = null }
  connected.value = false
}

export function useSystemMetrics() {
  refCount++
  if (closeTimer != null) {
    window.clearTimeout(closeTimer)
    closeTimer = null
  }

  if (refCount === 1 && !es) {
    // HTTP fallback：SSE 握手前先灌入历史，首屏即可显示 Sparkline。
    getSystemMetrics()
      .then((res) => {
        history.splice(0, history.length, ...(res.history ?? []))
        if (res.current) current.value = res.current
      })
      .catch((e: Error) => { lastError.value = e.message })
    open()
  }

  onBeforeUnmount(() => {
    refCount = Math.max(0, refCount - 1)
    if (refCount === 0) {
      closeTimer = window.setTimeout(() => {
        closeTimer = null
        if (refCount === 0) close()
      }, 800)
    }
  })

  return { current, history, connected, lastError }
}
