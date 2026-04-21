// 单例的作业调度 SSE 连接 + 响应式快照表。
//
// 所有页面（OverviewPage 的 TaskCenterCard / 服务观测的 TasksTab）共享
// 同一个 EventSource：引用计数归零时关闭连接，避免多标签页打开多条流。
//
// 失败自动重连：指数退避，最大 30s。
import { computed, reactive, ref, onBeforeUnmount } from 'vue'

import { listTasks, taskStreamUrl, type TaskKind, type TaskSnapshot } from '@/api/tasks'

type SnapshotMap = Partial<Record<TaskKind, TaskSnapshot>>

const snapshots = reactive<SnapshotMap>({})
const connected = ref(false)
const lastError = ref<string>('')

let es: EventSource | null = null
let refCount = 0
let retryTimer: number | null = null
let retryDelay = 1000
// 页面切换（Overview → TasksTab）时 refCount 会短暂为 0 再升 1；
// 延迟关闭避免连接立即断开重连。
let closeTimer: number | null = null

function getToken(): string | null {
  return localStorage.getItem('accessToken')
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

function handleSnapshot(s: TaskSnapshot) {
  snapshots[s.kind] = s
}

function open() {
  if (es) return
  const url = taskStreamUrl(getToken())
  try {
    es = new EventSource(url)
  } catch (e) {
    lastError.value = (e as Error).message
    scheduleReconnect()
    return
  }

  es.addEventListener('snapshot', (ev: MessageEvent) => {
    try {
      const s = JSON.parse(ev.data) as TaskSnapshot
      handleSnapshot(s)
    } catch {
      /* 忽略单条解析错误，不影响后续事件 */
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
    // EventSource 会自己重连，但它不支持退避；我们主动关闭 + 退避后重开。
    try {
      es?.close()
    } catch {}
    es = null
    if (refCount > 0) scheduleReconnect()
  }
}

function close() {
  clearRetry()
  if (es) {
    try {
      es.close()
    } catch {}
    es = null
  }
  connected.value = false
}

/**
 * 组件级订阅器。会在组件挂载期维持连接。
 * 第一个订阅者打开连接并先请求一次 /Tasks 填充初值；最后一个订阅者离开后关闭。
 */
export function useTaskStream() {
  refCount++
  if (closeTimer != null) {
    window.clearTimeout(closeTimer)
    closeTimer = null
  }

  if (refCount === 1 && !es) {
    // HTTP fallback：SSE 握手前先把当前状态灌进去，首屏不等待。
    listTasks()
      .then((res) => {
        for (const s of res.items ?? []) handleSnapshot(s)
      })
      .catch((e: Error) => {
        lastError.value = e.message
      })
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

  const tasks = computed<TaskSnapshot[]>(() => {
    // 'scrape' 刻意省略:方案 C 后刮削由 scrape_queue + ScrapeWorker 持续驱动,
    // 不再作为一等任务卡片。历史表仍会展示 kind='scrape' 的旧运行记录。
    const order: TaskKind[] = ['scan', 'probe', 'backfill', 'update']
    const out: TaskSnapshot[] = []
    for (const k of order) {
      const s = snapshots[k]
      if (s) out.push(s)
    }
    return out
  })

  const runningCount = computed(() =>
    tasks.value.filter((t) =>
      t.status === 'running' || t.status === 'queued' || t.status === 'stopping',
    ).length,
  )

  return {
    tasks,
    snapshots,
    runningCount,
    connected,
    lastError,
    getByKind: (k: TaskKind) => snapshots[k] ?? null,
  }
}
