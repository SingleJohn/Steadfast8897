<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import {
  NCard,
  NButton,
  NSpin,
  NTag,
  NInputNumber,
  NIcon,
  NPopconfirm,
} from 'naive-ui'
import { RefreshOutline, SaveOutline } from '@vicons/ionicons5'
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
const recent = ref<ScrapeQueueTask[]>([])
const loading = ref(false)
const firstLoaded = ref(false)

const ingestWorkerInput = ref<number>(4)
const ingestWorkerSaving = ref(false)
const scrapeWorkerInput = ref<number>(4)
const scrapeWorkerSaving = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null
const POLL_INTERVAL = 5000

const expandedId = ref<number | null>(null)
const detailCache = reactive<Record<number, ScrapeQueueTaskDetail>>({})
const detailLoading = ref<number | null>(null)
const detailError = reactive<Record<number, string>>({})

async function refresh() {
  loading.value = true
  try {
    const [s, r, m] = await Promise.allSettled([
      getScrapeQueueStats(),
      getScrapeQueueRecent(20),
      getMetricsSnapshot(),
    ])
    if (s.status === 'fulfilled') stats.value = s.value
    if (r.status === 'fulfilled') recent.value = r.value.tasks || []
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
    firstLoaded.value = true
  } finally {
    loading.value = false
  }
}

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

    <!-- ============ 最近任务列表 ============ -->
    <NCard title="最近任务(failed + running)" class="tasks-card">
      <NSpin :show="loading && !firstLoaded">
        <EmptyState v-if="firstLoaded && recent.length === 0" description="暂无 failed / running 任务" />
        <div v-else class="task-list">
          <div v-for="t in recent" :key="t.id" class="task-item">
            <div
              class="task-row"
              :class="[`status-${t.status}`, { expanded: expandedId === t.id }]"
              @click="toggleExpand(t.id)"
            >
              <div class="task-main">
                <NTag :type="statusColor(t.status)" size="small">{{ t.status }}</NTag>
                <NTag size="small" class="type-tag">{{ taskTypeLabel(t.task_type) }}</NTag>
                <span class="task-name">{{ t.item_name || t.item_id }}</span>
                <span class="task-type-hint">{{ t.item_type }}</span>
                <span class="expand-caret">{{ expandedId === t.id ? '▾' : '▸' }}</span>
              </div>
              <div class="task-meta" @click.stop>
                <span v-if="t.retry_count > 0" class="meta-pill">重试 {{ t.retry_count }}</span>
                <span class="meta-pill">下次 {{ formatDate(t.next_run_at) }}</span>
                <span v-if="t.last_error" class="meta-pill err" :title="t.last_error">
                  {{ t.last_error.length > 80 ? t.last_error.slice(0, 80) + '…' : t.last_error }}
                </span>
                <NButton
                  v-if="t.status === 'failed'"
                  size="tiny"
                  type="primary"
                  ghost
                  @click.stop="handleRetry(t.id)"
                >
                  重试
                </NButton>
              </div>
            </div>
            <div v-if="expandedId === t.id" class="task-detail">
              <NSpin :show="detailLoading === t.id && !detailCache[t.id]">
                <div v-if="detailError[t.id]" class="detail-error">
                  加载详情失败:{{ detailError[t.id] }}
                </div>
                <div v-else-if="detailCache[t.id]" class="detail-body">
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

/* ============ 任务列表(沿用旧样式)============ */
.task-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.task-item {
  display: flex;
  flex-direction: column;
}
.task-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--n-card-color, rgba(0, 0, 0, 0.02));
  border: 1px solid var(--n-border-color, rgba(0, 0, 0, 0.06));
  gap: 12px;
  flex-wrap: wrap;
  cursor: pointer;
  transition: background 0.15s;
}
.task-row:hover {
  background: var(--n-color, rgba(0, 0, 0, 0.04));
}
.task-row.expanded {
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
}
.expand-caret {
  font-size: 11px;
  color: var(--n-text-color-3, #999);
  margin-left: 4px;
  user-select: none;
}
.task-row.status-failed {
  border-left: 3px solid #f56c6c;
}
.task-row.status-running {
  border-left: 3px solid #e6a23c;
}
.task-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 200px;
}
.task-name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 280px;
}
.task-type-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #999);
}
.type-tag {
  background: var(--n-color, rgba(0, 0, 0, 0.05));
}
.task-meta {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.meta-pill {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--n-color, rgba(0, 0, 0, 0.06));
  color: var(--n-text-color-2, #666);
}
.meta-pill.err {
  background: rgba(245, 108, 108, 0.12);
  color: #f56c6c;
  max-width: 400px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.task-detail {
  padding: 12px 14px;
  border: 1px solid var(--n-border-color, rgba(0, 0, 0, 0.06));
  border-top: none;
  border-bottom-left-radius: 6px;
  border-bottom-right-radius: 6px;
  background: var(--n-color, rgba(0, 0, 0, 0.015));
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
