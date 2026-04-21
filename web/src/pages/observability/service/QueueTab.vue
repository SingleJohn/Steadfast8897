<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
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
import {
  getScrapeQueueStats,
  getScrapeQueueRecent,
  getScrapeQueueTaskDetail,
  retryScrapeQueueTask,
  retryAllFailedScrapeQueueTasks,
  getMetricsSnapshot,
  invalidateScrapeCache,
  scrapeAllMetadata,
  setIngestWorkerCount,
  setScrapeWorkerCount,
  type ScrapeQueueStats,
  type ScrapeQueueTask,
  type ScrapeQueueTaskDetail,
  type MetricsSnapshot,
} from '@/api/client'

const { showToast } = useToast()

const stats = ref<ScrapeQueueStats>({ pending: 0, running: 0, done: 0, failed: 0 })
const metrics = ref<MetricsSnapshot>({})
const loading = ref(false)
const firstLoaded = ref(false)

const ingestWorkerInput = ref<number>(4)
const ingestWorkerSaving = ref(false)
const scrapeWorkerInput = ref<number>(4)
const scrapeWorkerSaving = ref(false)
const scrapeAllLoading = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null
const POLL_INTERVAL = 5000

// 任务列表按 status 分 Tab,每个 Tab 独立分页。
const activeTab = ref<'failed' | 'running'>('failed')
const PAGE_SIZE = 50
const failedPage = ref(1)
const runningPage = ref(1)
const failedTasks = ref<ScrapeQueueTask[]>([])
const runningTasks = ref<ScrapeQueueTask[]>([])
const failedTotal = ref(0)
const runningTotal = ref(0)
const listLoading = ref(false)

const currentList = computed(() =>
  activeTab.value === 'failed' ? failedTasks.value : runningTasks.value,
)
const currentTotal = computed(() =>
  activeTab.value === 'failed' ? failedTotal.value : runningTotal.value,
)
const currentPage = computed({
  get: () => (activeTab.value === 'failed' ? failedPage.value : runningPage.value),
  set: (v) => {
    if (activeTab.value === 'failed') failedPage.value = v
    else runningPage.value = v
  },
})

const expandedId = ref<number | null>(null)
const detailCache = reactive<Record<number, ScrapeQueueTaskDetail>>({})
const detailLoading = ref<number | null>(null)
const detailError = reactive<Record<number, string>>({})

async function loadTaskList() {
  listLoading.value = true
  try {
    const status = activeTab.value
    const page = status === 'failed' ? failedPage.value : runningPage.value
    const r = await getScrapeQueueRecent({
      status,
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    })
    if (status === 'failed') {
      failedTasks.value = r.tasks || []
      failedTotal.value = r.total ?? 0
    } else {
      runningTasks.value = r.tasks || []
      runningTotal.value = r.total ?? 0
    }
  } catch {
    // 静默;下次轮询自动重试
  } finally {
    listLoading.value = false
  }
}

async function refresh() {
  loading.value = true
  try {
    const [s, m] = await Promise.allSettled([
      getScrapeQueueStats(),
      getMetricsSnapshot(),
    ])
    if (s.status === 'fulfilled') stats.value = s.value
    if (m.status === 'fulfilled') {
      metrics.value = m.value
      // 用户未在编辑时同步输入框,避免正在改的值被轮询覆盖
      if (!ingestWorkerSaving.value && typeof m.value.ingest_worker_count === 'number') {
        ingestWorkerInput.value = m.value.ingest_worker_count
      }
      if (!scrapeWorkerSaving.value && typeof m.value.scrape_worker_count === 'number') {
        scrapeWorkerInput.value = m.value.scrape_worker_count
      }
    }
    await loadTaskList()
    firstLoaded.value = true
  } finally {
    loading.value = false
  }
}

// 切 Tab 或翻页时重新拉列表(但只拉当前 Tab,不动 stats/metrics)
watch([activeTab, failedPage, runningPage], () => {
  expandedId.value = null
  loadTaskList()
})

async function handleRetry(id: number) {
  try {
    await retryScrapeQueueTask(id)
    showToast('已重置为 pending', 'success')
    delete detailCache[id]
    delete detailError[id]
    if (expandedId.value === id) expandedId.value = null
    await refresh()
  } catch (e: any) {
    showToast(e.message || '重试失败', 'error')
  }
}

async function fetchDetail(id: number) {
  detailLoading.value = id
  delete detailError[id]
  try {
    detailCache[id] = await getScrapeQueueTaskDetail(id)
  } catch (e: any) {
    detailError[id] = e?.message || '加载失败'
  } finally {
    if (detailLoading.value === id) detailLoading.value = null
  }
}

async function toggleExpand(id: number) {
  if (expandedId.value === id) {
    expandedId.value = null
    return
  }
  expandedId.value = id
  if (!detailCache[id]) await fetchDetail(id)
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

async function handleRetryAll() {
  try {
    const r = await retryAllFailedScrapeQueueTasks()
    showToast(`已重置 ${r.reset} 个失败任务`, 'success')
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

function statusColor(status: string): 'success' | 'warning' | 'error' | 'default' {
  switch (status) {
    case 'done': return 'success'
    case 'running': return 'warning'
    case 'failed': return 'error'
    default: return 'default'
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

function formatDate(s?: string): string {
  if (!s) return ''
  try {
    return new Date(s).toLocaleString()
  } catch {
    return s
  }
}

// 给 Episode / Season 拼一个 "S01E05" 风格的角标;Movie / Series 顶层返回空字符串。
function episodeTag(t: ScrapeQueueTask): string {
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

// 主展示名:Episode 显示 "剧集名 S01E05",其他直接 item_name
function displayTitle(t: ScrapeQueueTask): string {
  const ep = episodeTag(t)
  if (t.series_name && ep) return `${t.series_name} · ${ep}`
  if (t.series_name) return t.series_name
  return t.item_name || t.item_id
}

onMounted(() => {
  refresh()
  pollTimer = setInterval(refresh, POLL_INTERVAL)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div class="queue-tab">
    <!-- ============ 入库流水线(Ingest)============ -->
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
          <span class="metric-label">溢出累计</span>
          <span
            class="metric-value"
            :class="{ warn: (metrics.ingest_overflow_total ?? 0) > 0 }"
          >{{ metrics.ingest_overflow_total ?? '-' }}</span>
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
          <span class="metric-sub">[1, 64] · 扫库/监控并发(根据数据库性能合理控制数量,会导致数据库CPU占用增加)</span>
        </div>
      </div>
    </NCard>

    <!-- ============ 刮削流水线(Scrape)============ -->
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

      <!-- KPI -->
      <div class="kpi-grid">
        <div class="kpi-card">
          <div class="kpi-label">Pending</div>
          <div class="kpi-value pending">{{ stats.pending }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Running</div>
          <div class="kpi-value running">{{ stats.running }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Failed</div>
          <div class="kpi-value failed">{{ stats.failed }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Done</div>
          <div class="kpi-value done">{{ stats.done }}</div>
        </div>
      </div>

      <!-- Metrics + Worker 控件 -->
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

      <!-- 管控按钮 -->
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
        <NPopconfirm @positive-click="handleRetryAll">
          <template #trigger>
            <NButton size="small" type="warning" :disabled="stats.failed === 0">
              <template #icon><NIcon><RefreshOutline /></NIcon></template>
              重试全部失败 ({{ stats.failed }})
            </NButton>
          </template>
          把所有 failed 任务重置为 pending,立即重试。
        </NPopconfirm>
      </div>
    </NCard>

    <!-- ============ 任务列表(按 status 分 Tab + 分页)============ -->
    <NCard class="tasks-card">
      <NTabs v-model:value="activeTab" type="line" size="small" animated>
        <NTabPane name="failed" :tab="`失败 (${stats.failed})`" />
        <NTabPane name="running" :tab="`运行中 (${stats.running})`" />
      </NTabs>

      <NSpin :show="listLoading && !firstLoaded">
        <EmptyState
          v-if="firstLoaded && currentList.length === 0"
          :description="activeTab === 'failed' ? '暂无失败任务' : '暂无运行中任务'"
        />
        <div v-else class="task-list">
          <div v-for="t in currentList" :key="t.id" class="task-item">
            <div
              class="task-row"
              :class="[`status-${t.status}`, { expanded: expandedId === t.id }]"
              @click="toggleExpand(t.id)"
            >
              <!-- 顶行:标题 + 类型标签 + 操作按钮 -->
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
                    @click.stop="handleRetry(t.id)"
                  >
                    重试
                  </NButton>
                  <span class="expand-caret">{{ expandedId === t.id ? '▾' : '▸' }}</span>
                </div>
              </div>

              <!-- 次行:物理路径 + 错误摘要 -->
              <div class="task-sub" @click.stop>
                <code v-if="t.file_path" class="file-path" :title="t.file_path">{{ t.file_path }}</code>
                <span v-else class="file-path empty">(无物理路径)</span>
                <span class="meta-dim">下次 {{ formatDate(t.next_run_at) }}</span>
              </div>
              <div v-if="t.last_error" class="task-error" :title="t.last_error">
                {{ t.last_error }}
              </div>
            </div>
            <div v-if="expandedId === t.id" class="task-detail">
              <NSpin :show="detailLoading === t.id && !detailCache[t.id]">
                <div v-if="detailError[t.id]" class="detail-error">
                  加载详情失败:{{ detailError[t.id] }}
                </div>
                <div v-else-if="detailCache[t.id]" class="detail-body">
                  <div class="detail-row" v-if="detailCache[t.id].file_path">
                    <div class="detail-label">File Path</div>
                    <code class="detail-url">{{ detailCache[t.id].file_path }}</code>
                  </div>
                  <div class="detail-row" v-if="detailCache[t.id].request_url">
                    <div class="detail-label">Request URL</div>
                    <code class="detail-url">{{ detailCache[t.id].request_url }}</code>
                  </div>
                  <div class="detail-row" v-if="detailCache[t.id].response_status">
                    <div class="detail-label">HTTP Status</div>
                    <NTag :type="statusTagType(detailCache[t.id].response_status)" size="small">
                      {{ detailCache[t.id].response_status }}
                    </NTag>
                  </div>
                  <div class="detail-row" v-if="detailCache[t.id].last_error">
                    <div class="detail-label">Error</div>
                    <pre class="detail-pre err-pre">{{ detailCache[t.id].last_error }}</pre>
                  </div>
                  <div class="detail-row" v-if="detailCache[t.id].response_sample">
                    <div class="detail-label">Response Body</div>
                    <pre class="detail-pre">{{ formatResponseBody(detailCache[t.id].response_sample) }}</pre>
                  </div>
                  <div
                    v-if="!detailCache[t.id].request_url && !detailCache[t.id].response_sample && !detailCache[t.id].last_error"
                    class="detail-empty"
                  >
                    无诊断信息(本地任务或尚未失败)
                  </div>
                </div>
              </NSpin>
            </div>
          </div>
        </div>

        <div v-if="currentTotal > PAGE_SIZE" class="pagination-row">
          <NPagination
            v-model:page="currentPage"
            :page-count="Math.ceil(currentTotal / PAGE_SIZE)"
            :page-size="PAGE_SIZE"
            :page-slot="7"
            size="small"
          />
          <span class="pagination-info">共 {{ currentTotal }} 条</span>
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

/* ============ Pipeline section ============ */
.pipeline-card {
  /* 让每个 pipeline section 顶部留出一个 header 区块 */
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

/* ============ KPI grid ============ */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
@media (max-width: 768px) {
  .kpi-grid { grid-template-columns: repeat(2, 1fr); }
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
.kpi-value.pending { color: #909399; }
.kpi-value.running { color: #e6a23c; }
.kpi-value.failed { color: #f56c6c; }
.kpi-value.done { color: #67c23a; }

/* ============ Metrics grid ============ */
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 12px;
}
@media (max-width: 768px) {
  .metrics-grid { grid-template-columns: repeat(2, 1fr); }
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
.worker-item {
  grid-column: span 1;
}

.manage-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* ============ 任务列表(两行布局,主题色友好)============ */
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
  /* 透明底 + 明显边框:暗色/亮色主题下都能看清文字 */
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

/* 顶行:标题区 + 操作区 */
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
.type-tag {
  flex-shrink: 0;
}
.expand-caret {
  font-size: 11px;
  color: var(--n-text-color-3);
  user-select: none;
}

/* 次行:物理路径 + 次要元数据 */
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

/* 错误摘要:完整展示,不截断 */
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

/* 分页器 */
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
