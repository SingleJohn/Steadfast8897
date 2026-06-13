<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import {
  NCard,
  NButton,
  NSpin,
  NTag,
  NTabs,
  NTabPane,
  NInputNumber,
  NIcon,
  NPagination,
  NPopconfirm,
} from 'naive-ui'
import { CloudDownloadOutline, RefreshOutline, SaveOutline } from '@vicons/ionicons5'
import { useToast } from '@/composables/useToast'
import EmptyState from '@/components/EmptyState.vue'
import { useVisibleInterval } from '@/composables/useVisibleInterval'
import {
  getScrapeQueueStats,
  getScrapeQueueRecent,
  getScrapeQueueTaskDetail,
  retryScrapeQueueTask,
  retryAllFailedScrapeQueueTasks,
  getRefreshQueueStats,
  getRefreshQueueRecent,
  getRefreshQueueTaskDetail,
  retryRefreshQueueTask,
  retryAllFailedRefreshQueueTasks,
  getMetricsSnapshot,
  invalidateScrapeCache,
  scrapeAllMetadata,
  setIngestWorkerCount,
  setScrapeWorkerCount,
  setRefreshWorkerCount,
  type ScrapeQueueStats,
  type ScrapeQueueTask,
  type ScrapeQueueTaskDetail,
  type ScrapeQueueIdentifyDetail,
  type RefreshQueueStats,
  type RefreshQueueTask,
  type RefreshQueueTaskDetail,
  type MetricsSnapshot,
} from '@/api/client'

type TaskListTab = 'failed' | 'running'
type DisplayTask = {
  item_type: string
  series_name?: string
  index_number?: number
  parent_index_number?: number
  item_name: string
  item_id: string
}

const { showToast } = useToast()

const scrapeStats = ref<ScrapeQueueStats>({ pending: 0, running: 0, done: 0, failed: 0 })
const refreshStats = ref<RefreshQueueStats>({ pending: 0, running: 0, done: 0, failed: 0 })
const metrics = ref<MetricsSnapshot>({})
const loading = ref(false)
const firstLoaded = ref(false)

const ingestWorkerInput = ref<number>(4)
const ingestWorkerSaving = ref(false)
const scrapeWorkerInput = ref<number>(4)
const scrapeWorkerSaving = ref(false)
const refreshWorkerInput = ref<number>(1)
const refreshWorkerSaving = ref(false)
const scrapeAllLoading = ref(false)

const POLL_INTERVAL = 5000
const PAGE_SIZE = 50
let refreshing = false
let scrapeListRequestId = 0
let refreshListRequestId = 0

const scrapeActiveTab = ref<TaskListTab>('failed')
const scrapeFailedPage = ref(1)
const scrapeRunningPage = ref(1)
const scrapeFailedTasks = ref<ScrapeQueueTask[]>([])
const scrapeRunningTasks = ref<ScrapeQueueTask[]>([])
const scrapeFailedTotal = ref(0)
const scrapeRunningTotal = ref(0)
const scrapeListLoading = ref(false)
const scrapeExpandedId = ref<number | null>(null)
const scrapeDetailCache = reactive<Record<number, ScrapeQueueTaskDetail>>({})
const scrapeDetailLoading = ref<number | null>(null)
const scrapeDetailError = reactive<Record<number, string>>({})

const refreshActiveTab = ref<TaskListTab>('failed')
const refreshFailedPage = ref(1)
const refreshRunningPage = ref(1)
const refreshFailedTasks = ref<RefreshQueueTask[]>([])
const refreshRunningTasks = ref<RefreshQueueTask[]>([])
const refreshFailedTotal = ref(0)
const refreshRunningTotal = ref(0)
const refreshListLoading = ref(false)
const refreshExpandedId = ref<number | null>(null)
const refreshDetailCache = reactive<Record<number, RefreshQueueTaskDetail>>({})
const refreshDetailLoading = ref<number | null>(null)
const refreshDetailError = reactive<Record<number, string>>({})

const scrapeCurrentList = computed(() =>
  scrapeActiveTab.value === 'failed' ? scrapeFailedTasks.value : scrapeRunningTasks.value,
)
const scrapeCurrentTotal = computed(() =>
  scrapeActiveTab.value === 'failed' ? scrapeFailedTotal.value : scrapeRunningTotal.value,
)
const scrapeCurrentPage = computed({
  get: () => (scrapeActiveTab.value === 'failed' ? scrapeFailedPage.value : scrapeRunningPage.value),
  set: (v: number) => {
    if (scrapeActiveTab.value === 'failed') scrapeFailedPage.value = v
    else scrapeRunningPage.value = v
  },
})

const refreshCurrentList = computed(() =>
  refreshActiveTab.value === 'failed' ? refreshFailedTasks.value : refreshRunningTasks.value,
)
const refreshCurrentTotal = computed(() =>
  refreshActiveTab.value === 'failed' ? refreshFailedTotal.value : refreshRunningTotal.value,
)
const refreshCurrentPage = computed({
  get: () => (refreshActiveTab.value === 'failed' ? refreshFailedPage.value : refreshRunningPage.value),
  set: (v: number) => {
    if (refreshActiveTab.value === 'failed') refreshFailedPage.value = v
    else refreshRunningPage.value = v
  },
})

async function loadScrapeTaskList() {
  const requestId = ++scrapeListRequestId
  scrapeListLoading.value = true
  try {
    const status = scrapeActiveTab.value
    const page = status === 'failed' ? scrapeFailedPage.value : scrapeRunningPage.value
    const r = await getScrapeQueueRecent({
      status,
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    })
    if (requestId !== scrapeListRequestId) return
    if (status === 'failed') {
      scrapeFailedTasks.value = r.tasks || []
      scrapeFailedTotal.value = r.total ?? 0
    } else {
      scrapeRunningTasks.value = r.tasks || []
      scrapeRunningTotal.value = r.total ?? 0
    }
  } catch {
    // 静默;下次轮询自动重试
  } finally {
    if (requestId === scrapeListRequestId) scrapeListLoading.value = false
  }
}

async function loadRefreshTaskList() {
  const requestId = ++refreshListRequestId
  refreshListLoading.value = true
  try {
    const status = refreshActiveTab.value
    const page = status === 'failed' ? refreshFailedPage.value : refreshRunningPage.value
    const r = await getRefreshQueueRecent({
      status,
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    })
    if (requestId !== refreshListRequestId) return
    if (status === 'failed') {
      refreshFailedTasks.value = r.tasks || []
      refreshFailedTotal.value = r.total ?? 0
    } else {
      refreshRunningTasks.value = r.tasks || []
      refreshRunningTotal.value = r.total ?? 0
    }
  } catch {
    // 静默;下次轮询自动重试
  } finally {
    if (requestId === refreshListRequestId) refreshListLoading.value = false
  }
}

async function refresh() {
  if (refreshing) return
  refreshing = true
  loading.value = true
  try {
    const [scrapeStatsRes, refreshStatsRes, metricsRes] = await Promise.allSettled([
      getScrapeQueueStats(),
      getRefreshQueueStats(),
      getMetricsSnapshot(),
    ])
    if (scrapeStatsRes.status === 'fulfilled') scrapeStats.value = scrapeStatsRes.value
    if (refreshStatsRes.status === 'fulfilled') refreshStats.value = refreshStatsRes.value
    if (metricsRes.status === 'fulfilled') {
      metrics.value = metricsRes.value
      if (!ingestWorkerSaving.value && typeof metricsRes.value.ingest_worker_count === 'number') {
        ingestWorkerInput.value = metricsRes.value.ingest_worker_count
      }
      if (!scrapeWorkerSaving.value && typeof metricsRes.value.scrape_worker_count === 'number') {
        scrapeWorkerInput.value = metricsRes.value.scrape_worker_count
      }
      if (!refreshWorkerSaving.value && typeof metricsRes.value.refresh_worker_count === 'number') {
        refreshWorkerInput.value = metricsRes.value.refresh_worker_count
      }
    }
    await Promise.allSettled([loadScrapeTaskList(), loadRefreshTaskList()])
    firstLoaded.value = true
  } finally {
    loading.value = false
    refreshing = false
  }
}

watch([scrapeActiveTab, scrapeFailedPage, scrapeRunningPage], () => {
  scrapeExpandedId.value = null
  void loadScrapeTaskList()
})

watch([refreshActiveTab, refreshFailedPage, refreshRunningPage], () => {
  refreshExpandedId.value = null
  void loadRefreshTaskList()
})

async function handleScrapeRetry(id: number) {
  try {
    await retryScrapeQueueTask(id)
    showToast('已重置为 pending', 'success')
    delete scrapeDetailCache[id]
    delete scrapeDetailError[id]
    if (scrapeExpandedId.value === id) scrapeExpandedId.value = null
    await refresh()
  } catch (e: any) {
    showToast(e.message || '重试失败', 'error')
  }
}

async function fetchScrapeDetail(id: number) {
  scrapeDetailLoading.value = id
  delete scrapeDetailError[id]
  try {
    scrapeDetailCache[id] = await getScrapeQueueTaskDetail(id)
  } catch (e: any) {
    scrapeDetailError[id] = e?.message || '加载失败'
  } finally {
    if (scrapeDetailLoading.value === id) scrapeDetailLoading.value = null
  }
}

async function toggleScrapeExpand(id: number) {
  if (scrapeExpandedId.value === id) {
    scrapeExpandedId.value = null
    return
  }
  scrapeExpandedId.value = id
  if (!scrapeDetailCache[id]) await fetchScrapeDetail(id)
}

async function handleRefreshRetry(id: number) {
  try {
    await retryRefreshQueueTask(id)
    showToast('已重置为 pending', 'success')
    delete refreshDetailCache[id]
    delete refreshDetailError[id]
    if (refreshExpandedId.value === id) refreshExpandedId.value = null
    await refresh()
  } catch (e: any) {
    showToast(e.message || '重试失败', 'error')
  }
}

async function fetchRefreshDetail(id: number) {
  refreshDetailLoading.value = id
  delete refreshDetailError[id]
  try {
    refreshDetailCache[id] = await getRefreshQueueTaskDetail(id)
  } catch (e: any) {
    refreshDetailError[id] = e?.message || '加载失败'
  } finally {
    if (refreshDetailLoading.value === id) refreshDetailLoading.value = null
  }
}

async function toggleRefreshExpand(id: number) {
  if (refreshExpandedId.value === id) {
    refreshExpandedId.value = null
    return
  }
  refreshExpandedId.value = id
  if (!refreshDetailCache[id]) await fetchRefreshDetail(id)
}

function statusTagType(status?: number): 'success' | 'warning' | 'error' | 'default' {
  if (!status) return 'default'
  if (status >= 200 && status < 300) return 'success'
  if (status >= 400 && status < 500) return 'warning'
  if (status >= 500) return 'error'
  return 'default'
}

function formatResponseBody(s?: string): string {
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

function formatRefreshOptions(options?: RefreshQueueTaskDetail['options']): string {
  if (!options) return '默认'
  const lines = Object.entries(options)
    .filter(([, value]) => Boolean(value))
    .map(([key, value]) => `${key}: ${String(value)}`)
  return lines.length > 0 ? lines.join('\n') : '默认'
}

function scrapeIdentifyDetail(detail?: ScrapeQueueTaskDetail): ScrapeQueueIdentifyDetail | null {
  return detail?.detail_json || null
}

function formatIdentifyParsed(detail?: ScrapeQueueIdentifyDetail | null): string {
  const parsed = detail?.parsed
  if (!parsed) return ''
  const lines: string[] = []
  if (parsed.title) lines.push(`title: ${parsed.title}`)
  if (parsed.original_title) lines.push(`original_title: ${parsed.original_title}`)
  if (typeof parsed.year === 'number') lines.push(`year: ${parsed.year}`)
  if (parsed.media_hint) lines.push(`media_hint: ${parsed.media_hint}`)
  if (parsed.ids && Object.keys(parsed.ids).length > 0) {
    lines.push(`ids: ${Object.entries(parsed.ids).map(([k, v]) => `${k}=${v}`).join(', ')}`)
  }
  if (parsed.junk && parsed.junk.length > 0) {
    lines.push(`junk: ${parsed.junk.join(', ')}`)
  }
  return lines.join('\n')
}

function formatIdentifyAttempt(attempt: NonNullable<ScrapeQueueIdentifyDetail['search_attempts']>[number]): string {
  const year = typeof attempt.year === 'number' ? ` · year=${attempt.year}` : ''
  return `${attempt.source} · ${attempt.query}${year}`
}

function formatIdentifyCandidate(candidate: NonNullable<ScrapeQueueIdentifyDetail['candidates']>[number]): string {
  const lines = [
    `${candidate.provider}/${candidate.provider_id}`,
    `title: ${candidate.title || '-'}`,
    `score: ${candidate.score}`,
  ]
  if (candidate.original_title) lines.push(`original_title: ${candidate.original_title}`)
  if (typeof candidate.year === 'number') lines.push(`year: ${candidate.year}`)
  if (typeof candidate.popularity === 'number') lines.push(`popularity: ${candidate.popularity}`)
  if (candidate.source) lines.push(`source: ${candidate.source}`)
  if (candidate.external_ids && Object.keys(candidate.external_ids).length > 0) {
    lines.push(`external_ids: ${Object.entries(candidate.external_ids).map(([k, v]) => `${k}=${v}`).join(', ')}`)
  }
  return lines.join('\n')
}

async function handleRetryAllScrape() {
  try {
    const r = await retryAllFailedScrapeQueueTasks()
    showToast(`已重置 ${r.reset} 个失败任务`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '批量重置失败', 'error')
  }
}

async function handleRetryAllRefresh() {
  try {
    const r = await retryAllFailedRefreshQueueTasks()
    showToast(`已重置 ${r.reset} 个刷新失败任务`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '批量重置失败', 'error')
  }
}

async function handleInvalidateCache() {
  try {
    await invalidateScrapeCache()
    showToast('Aggregator / TmdbClient 缓存已失效,下次任务重建', 'success')
  } catch (e: any) {
    showToast(e.message || '失败', 'error')
  }
}

async function handleScrapeAllMissing() {
  scrapeAllLoading.value = true
  try {
    const r: any = await scrapeAllMetadata()
    const n = Number(r?.enqueued ?? 0)
    if (n === 0) {
      showToast('没有需要入队的 item(都已有元数据或已入队)', 'info')
    } else {
      showToast(`已入队 ${n} 条 identify 任务,worker 将自动消费`, 'success')
    }
    await refresh()
  } catch (e: any) {
    showToast(e.message || '入队失败', 'error')
  } finally {
    scrapeAllLoading.value = false
  }
}

async function handleSaveIngestWorker() {
  ingestWorkerSaving.value = true
  try {
    const r = await setIngestWorkerCount(ingestWorkerInput.value)
    showToast(`入库 worker 数已调整为 ${r.count}`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '保存失败', 'error')
  } finally {
    ingestWorkerSaving.value = false
  }
}

async function handleSaveScrapeWorker() {
  scrapeWorkerSaving.value = true
  try {
    const r = await setScrapeWorkerCount(scrapeWorkerInput.value)
    showToast(`刮削 worker 数已调整为 ${r.count}`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '保存失败', 'error')
  } finally {
    scrapeWorkerSaving.value = false
  }
}

async function handleSaveRefreshWorker() {
  refreshWorkerSaving.value = true
  try {
    const r = await setRefreshWorkerCount(refreshWorkerInput.value)
    showToast(`刷新 worker 数已调整为 ${r.count}`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '保存失败', 'error')
  } finally {
    refreshWorkerSaving.value = false
  }
}

function taskTypeLabel(t: string): string {
  const m: Record<string, string> = {
    identify: '识别',
    backfill_quality: '画质',
    backfill_episode_name: '剧集名',
    backfill_episode_image: '剧集图',
    refresh: '重刮',
  }
  return m[t] || t
}

function refreshScopeLabel(scope: string): string {
  const m: Record<string, string> = {
    metadata: '元数据',
    images: '图片',
    subtree: '子树',
  }
  return m[scope] || scope
}

function refreshSourceLabel(source: string): string {
  const m: Record<string, string> = {
    manual: '手动',
    scan: '扫库',
    fsnotify: '文件监控',
    sidecar: '边车文件',
  }
  return m[source] || source
}

function formatDate(s?: string): string {
  if (!s) return ''
  try {
    return new Date(s).toLocaleString()
  } catch {
    return s
  }
}

function episodeTag(t: DisplayTask): string {
  const s = t.parent_index_number
  const e = t.index_number
  if (t.item_type === 'Episode' && s != null && e != null) {
    return `S${String(s).padStart(2, '0')}E${String(e).padStart(2, '0')}`
  }
  if (t.item_type === 'Season' && e != null) {
    return `S${String(e).padStart(2, '0')}`
  }
  return ''
}

function displayTitle(t: DisplayTask): string {
  const ep = episodeTag(t)
  if (t.series_name && ep) return `${t.series_name} · ${ep}`
  if (t.series_name) return t.series_name
  return t.item_name || t.item_id
}

useVisibleInterval(refresh, POLL_INTERVAL, { immediate: true })
</script>

<template>
  <div class="queue-tab">
    <NCard class="pipeline-card">
      <div class="pipeline-header">
        <div class="pipeline-title">
          <span class="pipeline-badge ingest">入库</span>
          <span class="pipeline-name">Ingest · 文件事件 → items 表</span>
        </div>
        <div class="pipeline-hint">
          in-memory channel · 溢出会让扫库漏文件,加 worker 或再扫一次补齐。
        </div>
      </div>
      <div class="metrics-grid">
        <div class="metric-item">
          <span class="metric-label">通道深度</span>
          <span class="metric-value">{{ metrics.ingest_channel_depth ?? '-' }}</span>
          <span class="metric-sub">当前 buffer 中未消费</span>
        </div>
        <div class="metric-item">
          <span
            class="metric-value"
            :class="{ warn: (metrics.ingest_overflow_total ?? 0) > 0 }"
          >{{ metrics.ingest_overflow_total ?? '-' }}</span>
          <span class="metric-label">溢出累计</span>
          <span class="metric-sub">自启动以来丢弃的事件</span>
        </div>
        <div class="metric-item worker-item">
          <span class="metric-label">Worker 数</span>
          <div class="worker-control">
            <NInputNumber
              v-model:value="ingestWorkerInput"
              :min="1"
              :max="64"
              size="small"
              style="width: 90px"
            />
            <NButton size="small" type="primary" :loading="ingestWorkerSaving" @click="handleSaveIngestWorker">
              <template #icon><NIcon><SaveOutline /></NIcon></template>
              保存
            </NButton>
          </div>
          <span class="metric-sub">[1, 64] · 扫库/监控并发</span>
        </div>
      </div>
    </NCard>

    <NCard class="pipeline-card">
      <div class="pipeline-header">
        <div class="pipeline-title">
          <span class="pipeline-badge scrape">刮削</span>
          <span class="pipeline-name">Scrape · scrape_queue 表 → TMDB</span>
        </div>
        <div class="pipeline-hint">
          PG 持久化队列 · 失败任务不丢,按退避重试;Pending 堆积说明刮不过来,可加 worker 或检查限流。
        </div>
      </div>

      <div class="kpi-grid">
        <div class="kpi-card">
          <div class="kpi-label">Pending</div>
          <div class="kpi-value pending">{{ scrapeStats.pending }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Running</div>
          <div class="kpi-value running">{{ scrapeStats.running }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Failed</div>
          <div class="kpi-value failed">{{ scrapeStats.failed }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Done</div>
          <div class="kpi-value done">{{ scrapeStats.done }}</div>
        </div>
      </div>

      <div class="metrics-grid">
        <div class="metric-item">
          <span class="metric-label">TMDB 请求累计</span>
          <span class="metric-value">{{ metrics.tmdb_requests_total ?? '-' }}</span>
          <span class="metric-sub">自启动以来的 TMDB 调用次数</span>
        </div>
        <div class="metric-item worker-item">
          <span class="metric-label">Worker 数</span>
          <div class="worker-control">
            <NInputNumber
              v-model:value="scrapeWorkerInput"
              :min="1"
              :max="16"
              size="small"
              style="width: 90px"
            />
            <NButton size="small" type="primary" :loading="scrapeWorkerSaving" @click="handleSaveScrapeWorker">
              <template #icon><NIcon><SaveOutline /></NIcon></template>
              保存
            </NButton>
          </div>
          <span class="metric-sub">[1, 16] · TMDB 共享限流 3rps</span>
        </div>
      </div>

      <div class="manage-row">
        <NButton size="small" @click="refresh" :loading="loading">
          <template #icon><NIcon><RefreshOutline /></NIcon></template>
          手动刷新
        </NButton>
        <NPopconfirm @positive-click="handleScrapeAllMissing">
          <template #trigger>
            <NButton size="small" type="primary" :loading="scrapeAllLoading">
              <template #icon><NIcon><CloudDownloadOutline /></NIcon></template>
              刮削全部缺失元数据
            </NButton>
          </template>
          把所有缺 overview 且未识别的 Movie/Series 以最高优先级入队 identify,worker 自动消费。
        </NPopconfirm>
        <NPopconfirm @positive-click="handleInvalidateCache">
          <template #trigger>
            <NButton size="small">失效刮削缓存</NButton>
          </template>
          改了 tmdb_api_key / providers 配置后点一次,Aggregator/TmdbClient 缓存重建,免重启。
        </NPopconfirm>
        <NPopconfirm @positive-click="handleRetryAllScrape">
          <template #trigger>
            <NButton size="small" type="warning" :disabled="scrapeStats.failed === 0">
              <template #icon><NIcon><RefreshOutline /></NIcon></template>
              重试全部失败 ({{ scrapeStats.failed }})
            </NButton>
          </template>
          把所有 failed 任务重置为 pending,立即重试。
        </NPopconfirm>
      </div>
    </NCard>

    <NCard class="pipeline-card">
      <div class="pipeline-header">
        <div class="pipeline-title">
          <span class="pipeline-badge refreshq">刷新</span>
          <span class="pipeline-name">Refresh · refresh_queue 表 → 本地 metadata / artwork</span>
        </div>
        <div class="pipeline-hint">
          本地优先的 item-level refresh 队列。sidecar 变更、手动刷新都会在这里汇聚，只有 metadata 且允许远程时才桥接到 scrape。
        </div>
      </div>

      <div class="kpi-grid">
        <div class="kpi-card">
          <div class="kpi-label">Pending</div>
          <div class="kpi-value pending">{{ refreshStats.pending }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Running</div>
          <div class="kpi-value running">{{ refreshStats.running }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Failed</div>
          <div class="kpi-value failed">{{ refreshStats.failed }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Done</div>
          <div class="kpi-value done">{{ refreshStats.done }}</div>
        </div>
      </div>

      <div class="metrics-grid">
        <div class="metric-item">
          <span class="metric-label">Worker 数</span>
          <div class="worker-control">
            <NInputNumber
              v-model:value="refreshWorkerInput"
              :min="1"
              :max="8"
              size="small"
              style="width: 90px"
            />
            <NButton size="small" type="primary" :loading="refreshWorkerSaving" @click="handleSaveRefreshWorker">
              <template #icon><NIcon><SaveOutline /></NIcon></template>
              保存
            </NButton>
          </div>
          <span class="metric-sub">[1, 8] · 本地 refresh 并发</span>
        </div>
        <div class="metric-item">
          <span class="metric-label">远程桥接规则</span>
          <span class="metric-value small">metadata only</span>
          <span class="metric-sub">仅 `allow_remote=true` 且 scope=metadata 时进入 scrape_queue</span>
        </div>
      </div>

      <div class="manage-row">
        <NButton size="small" @click="refresh" :loading="loading">
          <template #icon><NIcon><RefreshOutline /></NIcon></template>
          手动刷新
        </NButton>
        <NPopconfirm @positive-click="handleRetryAllRefresh">
          <template #trigger>
            <NButton size="small" type="warning" :disabled="refreshStats.failed === 0">
              <template #icon><NIcon><RefreshOutline /></NIcon></template>
              重试全部失败 ({{ refreshStats.failed }})
            </NButton>
          </template>
          把所有 failed refresh 任务重置为 pending,立即重试。
        </NPopconfirm>
      </div>
    </NCard>

    <NCard class="tasks-card">
      <div class="card-heading">Scrape 队列任务</div>
      <NTabs v-model:value="scrapeActiveTab" type="line" size="small" animated>
        <NTabPane name="failed" :tab="`失败 (${scrapeStats.failed})`" />
        <NTabPane name="running" :tab="`运行中 (${scrapeStats.running})`" />
      </NTabs>

      <NSpin :show="scrapeListLoading && !firstLoaded">
        <EmptyState
          v-if="firstLoaded && scrapeCurrentList.length === 0"
          :description="scrapeActiveTab === 'failed' ? '暂无失败任务' : '暂无运行中任务'"
        />
        <div v-else class="task-list">
          <div v-for="t in scrapeCurrentList" :key="t.id" class="task-item">
            <div
              class="task-row"
              :class="[`status-${t.status}`, { expanded: scrapeExpandedId === t.id }]"
              @click="toggleScrapeExpand(t.id)"
            >
              <div class="task-head">
                <div class="task-head-left">
                  <NTag size="small" class="type-tag">{{ taskTypeLabel(t.task_type) }}</NTag>
                  <span class="task-title">{{ displayTitle(t) }}</span>
                  <span v-if="t.item_type" class="task-type-hint">{{ t.item_type }}</span>
                  <span v-if="t.retry_count > 0" class="meta-pill warn">重试 {{ t.retry_count }}</span>
                </div>
                <div class="task-head-right" @click.stop>
                  <NButton
                    v-if="t.status === 'failed'"
                    size="tiny"
                    type="primary"
                    ghost
                    @click.stop="handleScrapeRetry(t.id)"
                  >
                    重试
                  </NButton>
                  <span class="expand-caret">{{ scrapeExpandedId === t.id ? '▾' : '▸' }}</span>
                </div>
              </div>

              <div class="task-sub" @click.stop>
                <code v-if="t.file_path" class="file-path" :title="t.file_path">{{ t.file_path }}</code>
                <span v-else class="file-path empty">(无物理路径)</span>
                <span class="meta-dim">下次 {{ formatDate(t.next_run_at) }}</span>
              </div>
              <div v-if="t.last_error" class="task-error" :title="t.last_error">
                {{ t.last_error }}
              </div>
            </div>

            <div v-if="scrapeExpandedId === t.id" class="task-detail">
              <NSpin :show="scrapeDetailLoading === t.id && !scrapeDetailCache[t.id]">
                <div v-if="scrapeDetailError[t.id]" class="detail-error">
                  加载详情失败:{{ scrapeDetailError[t.id] }}
                </div>
                <div v-else-if="scrapeDetailCache[t.id]" class="detail-body">
                  <div class="detail-row" v-if="scrapeDetailCache[t.id].file_path">
                    <div class="detail-label">File Path</div>
                    <code class="detail-url">{{ scrapeDetailCache[t.id].file_path }}</code>
                  </div>
                  <div v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])" class="detail-structured">
                    <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.reason">
                      <div class="detail-label">Identify Reason</div>
                      <pre class="detail-pre err-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.reason }}</pre>
                    </div>
                    <div class="detail-meta-grid">
                      <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.stage">
                        <div class="detail-label">Stage</div>
                        <pre class="detail-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.stage }}</pre>
                      </div>
                      <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.threshold != null">
                        <div class="detail-label">Threshold</div>
                        <pre class="detail-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.threshold }}</pre>
                      </div>
                      <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.best_score != null">
                        <div class="detail-label">Best Score</div>
                        <pre class="detail-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.best_score }}</pre>
                      </div>
                      <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.auto_apply != null">
                        <div class="detail-label">Auto Apply</div>
                        <pre class="detail-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.auto_apply ? 'true' : 'false' }}</pre>
                      </div>
                      <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.providers?.length">
                        <div class="detail-label">Providers</div>
                        <pre class="detail-pre">{{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.providers?.join(' → ') }}</pre>
                      </div>
                    </div>
                    <div class="detail-row" v-if="formatIdentifyParsed(scrapeIdentifyDetail(scrapeDetailCache[t.id]))">
                      <div class="detail-label">Parsed</div>
                      <pre class="detail-pre">{{ formatIdentifyParsed(scrapeIdentifyDetail(scrapeDetailCache[t.id])) }}</pre>
                    </div>
                    <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.search_attempts?.length">
                      <div class="detail-label">Search Attempts</div>
                      <div class="detail-chip-list">
                        <code
                          v-for="(attempt, idx) in scrapeIdentifyDetail(scrapeDetailCache[t.id])?.search_attempts"
                          :key="`${t.id}-attempt-${idx}`"
                          class="detail-chip"
                        >
                          {{ formatIdentifyAttempt(attempt) }}
                        </code>
                      </div>
                    </div>
                    <div class="detail-row" v-if="scrapeIdentifyDetail(scrapeDetailCache[t.id])?.candidates?.length">
                      <div class="detail-label">
                        Candidates
                        <span class="detail-count">({{ scrapeIdentifyDetail(scrapeDetailCache[t.id])?.candidates_total ?? scrapeIdentifyDetail(scrapeDetailCache[t.id])?.candidates?.length }})</span>
                      </div>
                      <div class="detail-candidate-list">
                        <pre
                          v-for="candidate in scrapeIdentifyDetail(scrapeDetailCache[t.id])?.candidates"
                          :key="`${t.id}-${candidate.provider}-${candidate.provider_id}`"
                          class="detail-pre detail-candidate-pre"
                        >{{ formatIdentifyCandidate(candidate) }}</pre>
                      </div>
                    </div>
                  </div>
                  <div class="detail-row" v-if="scrapeDetailCache[t.id].request_url">
                    <div class="detail-label">Request URL</div>
                    <code class="detail-url">{{ scrapeDetailCache[t.id].request_url }}</code>
                  </div>
                  <div class="detail-row" v-if="scrapeDetailCache[t.id].response_status">
                    <div class="detail-label">HTTP Status</div>
                    <NTag :type="statusTagType(scrapeDetailCache[t.id].response_status)" size="small">
                      {{ scrapeDetailCache[t.id].response_status }}
                    </NTag>
                  </div>
                  <div class="detail-row" v-if="scrapeDetailCache[t.id].last_error">
                    <div class="detail-label">Error</div>
                    <pre class="detail-pre err-pre">{{ scrapeDetailCache[t.id].last_error }}</pre>
                  </div>
                  <div class="detail-row" v-if="scrapeDetailCache[t.id].response_sample">
                    <div class="detail-label">Response Body</div>
                    <pre class="detail-pre">{{ formatResponseBody(scrapeDetailCache[t.id].response_sample) }}</pre>
                  </div>
                  <div
                    v-if="!scrapeIdentifyDetail(scrapeDetailCache[t.id]) && !scrapeDetailCache[t.id].request_url && !scrapeDetailCache[t.id].response_sample && !scrapeDetailCache[t.id].last_error"
                    class="detail-empty"
                  >
                    无诊断信息(本地任务或尚未失败)
                  </div>
                </div>
              </NSpin>
            </div>
          </div>
        </div>

        <div v-if="scrapeCurrentTotal > PAGE_SIZE" class="pagination-row">
          <NPagination
            v-model:page="scrapeCurrentPage"
            :page-count="Math.ceil(scrapeCurrentTotal / PAGE_SIZE)"
            :page-size="PAGE_SIZE"
            :page-slot="7"
            size="small"
          />
          <span class="pagination-info">共 {{ scrapeCurrentTotal }} 条</span>
        </div>
      </NSpin>
    </NCard>

    <NCard class="tasks-card">
      <div class="card-heading">Refresh 队列任务</div>
      <NTabs v-model:value="refreshActiveTab" type="line" size="small" animated>
        <NTabPane name="failed" :tab="`失败 (${refreshStats.failed})`" />
        <NTabPane name="running" :tab="`运行中 (${refreshStats.running})`" />
      </NTabs>

      <NSpin :show="refreshListLoading && !firstLoaded">
        <EmptyState
          v-if="firstLoaded && refreshCurrentList.length === 0"
          :description="refreshActiveTab === 'failed' ? '暂无失败任务' : '暂无运行中任务'"
        />
        <div v-else class="task-list">
          <div v-for="t in refreshCurrentList" :key="t.id" class="task-item">
            <div
              class="task-row"
              :class="[`status-${t.status}`, { expanded: refreshExpandedId === t.id }]"
              @click="toggleRefreshExpand(t.id)"
            >
              <div class="task-head">
                <div class="task-head-left">
                  <NTag size="small" class="type-tag">{{ refreshScopeLabel(t.scope) }}</NTag>
                  <NTag size="small" class="source-tag">{{ refreshSourceLabel(t.source) }}</NTag>
                  <span class="task-title">{{ displayTitle(t) }}</span>
                  <span v-if="t.item_type" class="task-type-hint">{{ t.item_type }}</span>
                  <span v-if="t.retry_count > 0" class="meta-pill warn">重试 {{ t.retry_count }}</span>
                </div>
                <div class="task-head-right" @click.stop>
                  <NButton
                    v-if="t.status === 'failed'"
                    size="tiny"
                    type="primary"
                    ghost
                    @click.stop="handleRefreshRetry(t.id)"
                  >
                    重试
                  </NButton>
                  <span class="expand-caret">{{ refreshExpandedId === t.id ? '▾' : '▸' }}</span>
                </div>
              </div>

              <div class="task-sub" @click.stop>
                <code v-if="t.file_path" class="file-path" :title="t.file_path">{{ t.file_path }}</code>
                <span v-else class="file-path empty">(无物理路径)</span>
                <span class="meta-dim">下次 {{ formatDate(t.next_run_at) }}</span>
              </div>
              <div v-if="t.last_error" class="task-error" :title="t.last_error">
                {{ t.last_error }}
              </div>
            </div>

            <div v-if="refreshExpandedId === t.id" class="task-detail">
              <NSpin :show="refreshDetailLoading === t.id && !refreshDetailCache[t.id]">
                <div v-if="refreshDetailError[t.id]" class="detail-error">
                  加载详情失败:{{ refreshDetailError[t.id] }}
                </div>
                <div v-else-if="refreshDetailCache[t.id]" class="detail-body">
                  <div class="detail-row" v-if="refreshDetailCache[t.id].file_path">
                    <div class="detail-label">File Path</div>
                    <code class="detail-url">{{ refreshDetailCache[t.id].file_path }}</code>
                  </div>
                  <div class="detail-row">
                    <div class="detail-label">Scope / Source</div>
                    <pre class="detail-pre">{{ refreshScopeLabel(refreshDetailCache[t.id].scope) }} / {{ refreshSourceLabel(refreshDetailCache[t.id].source) }}</pre>
                  </div>
                  <div class="detail-row">
                    <div class="detail-label">Options</div>
                    <pre class="detail-pre">{{ formatRefreshOptions(refreshDetailCache[t.id].options) }}</pre>
                  </div>
                  <div class="detail-row" v-if="refreshDetailCache[t.id].last_error">
                    <div class="detail-label">Error</div>
                    <pre class="detail-pre err-pre">{{ refreshDetailCache[t.id].last_error }}</pre>
                  </div>
                  <div v-if="!refreshDetailCache[t.id].last_error" class="detail-empty">
                    当前无错误诊断,主要看 options/source/scope 即可。
                  </div>
                </div>
              </NSpin>
            </div>
          </div>
        </div>

        <div v-if="refreshCurrentTotal > PAGE_SIZE" class="pagination-row">
          <NPagination
            v-model:page="refreshCurrentPage"
            :page-count="Math.ceil(refreshCurrentTotal / PAGE_SIZE)"
            :page-size="PAGE_SIZE"
            :page-slot="7"
            size="small"
          />
          <span class="pagination-info">共 {{ refreshCurrentTotal }} 条</span>
        </div>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped>
.queue-tab {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.pipeline-header {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
  padding-bottom: 12px;
  border-bottom: 1px dashed var(--n-border-color, rgba(0, 0, 0, 0.08));
}

.pipeline-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pipeline-badge {
  font-size: 12px;
  font-weight: 600;
  padding: 2px 10px;
  border-radius: 4px;
  color: #fff;
  letter-spacing: 0.03em;
}

.pipeline-badge.ingest {
  background: #409eff;
}

.pipeline-badge.scrape {
  background: #9b59b6;
}

.pipeline-badge.refreshq {
  background: #18a058;
}

.pipeline-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--n-text-color-2, #555);
}

.pipeline-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
  line-height: 1.5;
}

.card-heading {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--n-text-color-1);
}

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}

@media (max-width: 768px) {
  .kpi-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

.kpi-card {
  text-align: center;
  padding: 14px 8px;
  background: var(--n-card-color, rgba(0, 0, 0, 0.02));
  border: 1px solid var(--n-border-color, rgba(0, 0, 0, 0.06));
  border-radius: 6px;
}

.kpi-label {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
  margin-bottom: 6px;
}

.kpi-value {
  font-size: 26px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.kpi-value.pending {
  color: #909399;
}

.kpi-value.running {
  color: #e6a23c;
}

.kpi-value.failed {
  color: #f56c6c;
}

.kpi-value.done {
  color: #67c23a;
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 12px;
}

@media (max-width: 768px) {
  .metrics-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

.metric-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.metric-label {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
}

.metric-value {
  font-size: 20px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}

.metric-value.small {
  font-size: 16px;
}

.metric-value.warn {
  color: #e6a23c;
}

.metric-sub {
  font-size: 11px;
  color: var(--n-text-color-3, #999);
}

.worker-control {
  display: flex;
  gap: 8px;
  align-items: center;
}

.manage-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.task-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.task-item {
  display: flex;
  flex-direction: column;
}

.task-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border-radius: 6px;
  background: transparent;
  border: 1px solid var(--n-border-color);
  border-left-width: 3px;
  border-left-color: var(--n-border-color);
  cursor: pointer;
  transition: background-color 0.15s, border-color 0.15s;
}

.task-row:hover {
  background: var(--n-action-color);
}

.task-row.expanded {
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
}

.task-row.status-failed {
  border-left-color: #f56c6c;
}

.task-row.status-running {
  border-left-color: #e6a23c;
}

.task-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.task-head-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  flex-wrap: wrap;
}

.task-head-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.task-title {
  font-weight: 500;
  color: var(--n-text-color-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex-shrink: 1;
}

.task-type-hint {
  font-size: 12px;
  color: var(--n-text-color-3);
  flex-shrink: 0;
}

.type-tag,
.source-tag {
  flex-shrink: 0;
}

.expand-caret {
  font-size: 11px;
  color: var(--n-text-color-3);
  user-select: none;
}

.task-sub {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  min-width: 0;
}

.file-path {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: var(--n-text-color-2);
  background: var(--n-action-color);
  padding: 2px 6px;
  border-radius: 3px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex: 1;
  user-select: all;
  cursor: text;
}

.file-path.empty {
  color: var(--n-text-color-3);
  font-style: italic;
  background: transparent;
  cursor: default;
}

.meta-dim {
  color: var(--n-text-color-3);
  flex-shrink: 0;
  white-space: nowrap;
}

.task-error {
  font-size: 12px;
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.08);
  padding: 4px 8px;
  border-radius: 3px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 4.5em;
  overflow: hidden;
  line-height: 1.5;
}

.meta-pill {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--n-action-color);
  color: var(--n-text-color-2);
  flex-shrink: 0;
}

.meta-pill.warn {
  background: rgba(230, 162, 60, 0.15);
  color: #e6a23c;
}

.task-detail {
  padding: 12px 14px;
  border: 1px solid var(--n-border-color);
  border-top: none;
  border-bottom-left-radius: 6px;
  border-bottom-right-radius: 6px;
  background: var(--n-action-color);
}

.pagination-row {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
  padding-top: 8px;
}

.pagination-info {
  font-size: 12px;
  color: var(--n-text-color-3);
}

.detail-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.detail-structured {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.detail-meta-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

@media (max-width: 768px) {
  .detail-meta-grid {
    grid-template-columns: 1fr;
  }
}

.detail-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.detail-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--n-text-color-3, #888);
  letter-spacing: 0.04em;
}

.detail-url {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  padding: 6px 8px;
  background: var(--n-card-color, rgba(0, 0, 0, 0.04));
  border-radius: 4px;
  word-break: break-all;
  user-select: all;
}

.detail-pre {
  margin: 0;
  padding: 8px 10px;
  background: var(--n-card-color, rgba(0, 0, 0, 0.04));
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
  max-height: 300px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.detail-pre.err-pre {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.08);
}

.detail-chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.detail-chip {
  display: inline-flex;
  align-items: center;
  padding: 6px 8px;
  border-radius: 4px;
  background: var(--n-card-color, rgba(0, 0, 0, 0.04));
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-all;
}

.detail-candidate-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 10px;
}

.detail-candidate-pre {
  margin: 0;
  max-height: none;
}

.detail-count {
  font-size: 11px;
  color: var(--n-text-color-3, #888);
  font-weight: 400;
  margin-left: 4px;
}

.detail-empty {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
  font-style: italic;
}

.detail-error {
  font-size: 12px;
  color: #f56c6c;
}
</style>
