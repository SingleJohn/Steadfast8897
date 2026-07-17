<script setup lang="ts">
import { reactive, shallowRef } from 'vue'

import {
  getMetricsSnapshot,
  invalidateScrapeCache,
  retryAllFailedRefreshQueueTasks,
  retryAllFailedScrapeQueueTasks,
  scrapeAllMetadata,
  setIngestWorkerCount,
  setRefreshWorkerCount,
  setScrapeWorkerCount,
  type MetricsSnapshot,
  type RefreshQueueStats,
  type ScrapeQueueStats,
} from '@/api/client'
import { useToast } from '@/composables/useToast'
import { useVisibleInterval } from '@/composables/useVisibleInterval'

import QueueRuntimeOverview from './queue/QueueRuntimeOverview.vue'
import QueueTaskList from './queue/QueueTaskList.vue'

const POLL_INTERVAL = 5000
const emptyStats = (): ScrapeQueueStats => ({ pending: 0, running: 0, done: 0, failed: 0 })

const { showToast } = useToast()
const scrapeStats = shallowRef<ScrapeQueueStats>(emptyStats())
const refreshStats = shallowRef<RefreshQueueStats>(emptyStats())
const metrics = shallowRef<MetricsSnapshot>({})
const updatedAt = shallowRef<string>()
const snapshotError = shallowRef('')
const ingestWorkerInput = shallowRef(4)
const scrapeWorkerInput = shallowRef(4)
const refreshWorkerInput = shallowRef(1)
const busy = reactive({
  refresh: false,
  scrapeAll: false,
  reloadTmdb: false,
  ingestWorker: false,
  scrapeWorker: false,
  refreshWorker: false,
})
let refreshing = false

async function refresh() {
  if (refreshing) return
  refreshing = true
  busy.refresh = true
  try {
    const snapshot = await getMetricsSnapshot()
    snapshotError.value = ''
    metrics.value = snapshot
    scrapeStats.value = {
      pending: snapshot.scrape_pending ?? scrapeStats.value.pending,
      running: snapshot.scrape_running ?? scrapeStats.value.running,
      failed: snapshot.scrape_failed ?? scrapeStats.value.failed,
      done: snapshot.scrape_done ?? scrapeStats.value.done,
    }
    refreshStats.value = {
      pending: snapshot.refresh_pending ?? refreshStats.value.pending,
      running: snapshot.refresh_running ?? refreshStats.value.running,
      failed: snapshot.refresh_failed ?? refreshStats.value.failed,
      done: snapshot.refresh_done ?? refreshStats.value.done,
    }
    if (!busy.ingestWorker && typeof snapshot.ingest_worker_count === 'number') {
      ingestWorkerInput.value = snapshot.ingest_worker_count
    }
    if (!busy.scrapeWorker && typeof snapshot.scrape_worker_count === 'number') {
      scrapeWorkerInput.value = snapshot.scrape_worker_count
    }
    if (!busy.refreshWorker && typeof snapshot.refresh_worker_count === 'number') {
      refreshWorkerInput.value = snapshot.refresh_worker_count
    }
    updatedAt.value = new Date().toISOString()
  } catch (error: any) {
    snapshotError.value = error?.message || '运行快照读取失败'
  } finally {
    busy.refresh = false
    refreshing = false
  }
}

async function handleScrapeAll() {
  busy.scrapeAll = true
  try {
    const result = await scrapeAllMetadata() as { enqueued?: number }
    const count = Number(result?.enqueued ?? 0)
    showToast(count > 0 ? `已加入 ${count} 条识别任务` : '当前没有需要补齐的元数据', count > 0 ? 'success' : 'info')
    await refresh()
  } catch (error: any) {
    showToast(error?.message || '任务入队失败', 'error')
  } finally {
    busy.scrapeAll = false
  }
}

async function handleReloadTmdb() {
  busy.reloadTmdb = true
  try {
    const result = await invalidateScrapeCache()
    const label = result.runtime.tmdb_state === 'ready' ? 'TMDB 客户端已重载' : 'TMDB 配置仍不可用，远程任务保持暂停'
    showToast(label, result.runtime.tmdb_state === 'ready' ? 'success' : 'warning')
    await refresh()
  } catch (error: any) {
    showToast(error?.message || '重载 TMDB 失败', 'error')
    await refresh()
  } finally {
    busy.reloadTmdb = false
  }
}

async function handleRetryScrape() {
  try {
    const result = await retryAllFailedScrapeQueueTasks()
    showToast(`已重置 ${result.reset} 条 Scrape 任务`, 'success')
    await refresh()
  } catch (error: any) {
    showToast(error?.message || '批量重试失败', 'error')
  }
}

async function handleRetryRefresh() {
  try {
    const result = await retryAllFailedRefreshQueueTasks()
    showToast(`已重置 ${result.reset} 条 Refresh 任务`, 'success')
    await refresh()
  } catch (error: any) {
    showToast(error?.message || '批量重试失败', 'error')
  }
}

async function saveWorker(kind: 'ingest' | 'scrape' | 'refresh') {
  const definitions = {
    ingest: { input: ingestWorkerInput, request: setIngestWorkerCount, label: '入库' },
    scrape: { input: scrapeWorkerInput, request: setScrapeWorkerCount, label: '刮削' },
    refresh: { input: refreshWorkerInput, request: setRefreshWorkerCount, label: '刷新' },
  } as const
  const definition = definitions[kind]
  busy[`${kind}Worker`] = true
  try {
    const result = await definition.request(definition.input.value)
    showToast(`${definition.label} worker 已调整为 ${result.count}`, 'success')
    await refresh()
  } catch (error: any) {
    showToast(error?.message || '保存 worker 配置失败', 'error')
  } finally {
    busy[`${kind}Worker`] = false
  }
}

useVisibleInterval(refresh, POLL_INTERVAL, { immediate: true })
</script>

<template>
  <div class="queue-tab">
    <QueueRuntimeOverview
      v-model:ingest-worker="ingestWorkerInput"
      v-model:scrape-worker="scrapeWorkerInput"
      v-model:refresh-worker="refreshWorkerInput"
      :metrics="metrics"
      :scrape-stats="scrapeStats"
      :refresh-stats="refreshStats"
      :busy="busy"
      :updated-at="updatedAt"
      :snapshot-error="snapshotError"
      @refresh="refresh"
      @scrape-all="handleScrapeAll"
      @reload-tmdb="handleReloadTmdb"
      @retry-scrape="handleRetryScrape"
      @retry-refresh="handleRetryRefresh"
      @save-ingest-worker="saveWorker('ingest')"
      @save-scrape-worker="saveWorker('scrape')"
      @save-refresh-worker="saveWorker('refresh')"
    />

    <div class="queue-lists">
      <QueueTaskList kind="scrape" :stats="scrapeStats" @changed="refresh" />
      <QueueTaskList kind="refresh" :stats="refreshStats" @changed="refresh" />
    </div>
  </div>
</template>

<style scoped>
.queue-tab {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.queue-lists {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 14px;
}

@media (max-width: 1120px) {
  .queue-lists { grid-template-columns: 1fr; }
}
</style>
