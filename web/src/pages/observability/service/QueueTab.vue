<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import {
  NCard,
  NButton,
  NSpin,
  NTag,
  NInputNumber,
  NIcon,
  NPopconfirm,
} from 'naive-ui'
import { RefreshOutline, TrashOutline, SaveOutline } from '@vicons/ionicons5'
import { useToast } from '@/composables/useToast'
import EmptyState from '@/components/EmptyState.vue'
import {
  getScrapeQueueStats,
  getScrapeQueueRecent,
  retryScrapeQueueTask,
  retryAllFailedScrapeQueueTasks,
  getMetricsSnapshot,
  invalidateScrapeCache,
  setIngestWorkerCount,
  type ScrapeQueueStats,
  type ScrapeQueueTask,
  type MetricsSnapshot,
} from '@/api/client'

const { showToast } = useToast()

const stats = ref<ScrapeQueueStats>({ pending: 0, running: 0, done: 0, failed: 0 })
const metrics = ref<MetricsSnapshot>({})
const recent = ref<ScrapeQueueTask[]>([])
const loading = ref(false)
const firstLoaded = ref(false)

const workerCountInput = ref<number>(4)
const workerCountSaving = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null
const POLL_INTERVAL = 5000

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
      // 同步 worker 输入框(用户没编辑时)
      if (!workerCountSaving.value && typeof m.value.ingest_worker_count === 'number') {
        workerCountInput.value = m.value.ingest_worker_count
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
    await refresh()
  } catch (e: any) {
    showToast(e.message || '重试失败', 'error')
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

async function handleSaveWorkerCount() {
  workerCountSaving.value = true
  try {
    const r = await setIngestWorkerCount(workerCountInput.value)
    showToast(`Ingest worker 数已调整为 ${r.count}`, 'success')
    await refresh()
  } catch (e: any) {
    showToast(e.message || '保存失败', 'error')
  } finally {
    workerCountSaving.value = false
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
    <!-- KPI 卡片 -->
    <div class="kpi-grid">
      <NCard class="kpi-card">
        <div class="kpi-label">Pending</div>
        <div class="kpi-value pending">{{ stats.pending }}</div>
      </NCard>
      <NCard class="kpi-card">
        <div class="kpi-label">Running</div>
        <div class="kpi-value running">{{ stats.running }}</div>
      </NCard>
      <NCard class="kpi-card">
        <div class="kpi-label">Failed</div>
        <div class="kpi-value failed">{{ stats.failed }}</div>
      </NCard>
      <NCard class="kpi-card">
        <div class="kpi-label">Done</div>
        <div class="kpi-value done">{{ stats.done }}</div>
      </NCard>
    </div>

    <!-- Metrics + 管理操作 -->
    <NCard title="Metrics 快照 / 管理" class="metrics-card">
      <div class="metrics-grid">
        <div class="metric-item">
          <span class="metric-label">Ingest 通道深度</span>
          <span class="metric-value">{{ metrics.ingest_channel_depth ?? '-' }}</span>
        </div>
        <div class="metric-item">
          <span class="metric-label">Ingest 溢出累计</span>
          <span class="metric-value">{{ metrics.ingest_overflow_total ?? '-' }}</span>
        </div>
        <div class="metric-item">
          <span class="metric-label">TMDB 请求累计</span>
          <span class="metric-value">{{ metrics.tmdb_requests_total ?? '-' }}</span>
        </div>
        <div class="metric-item worker-item">
          <span class="metric-label">Ingest Worker 数</span>
          <div class="worker-control">
            <NInputNumber
              v-model:value="workerCountInput"
              :min="1"
              :max="64"
              size="small"
              style="width: 90px"
            />
            <NButton size="small" type="primary" :loading="workerCountSaving" @click="handleSaveWorkerCount">
              <template #icon><NIcon><SaveOutline /></NIcon></template>
              保存
            </NButton>
          </div>
        </div>
      </div>
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

    <!-- 最近任务表 -->
    <NCard title="最近任务(failed + running)" class="tasks-card">
      <NSpin :show="loading && !firstLoaded">
        <EmptyState v-if="firstLoaded && recent.length === 0" description="暂无 failed / running 任务" />
        <div v-else class="task-list">
          <div
            v-for="t in recent"
            :key="t.id"
            class="task-row"
            :class="`status-${t.status}`"
          >
            <div class="task-main">
              <NTag :type="statusColor(t.status)" size="small">{{ t.status }}</NTag>
              <NTag size="small" class="type-tag">{{ taskTypeLabel(t.task_type) }}</NTag>
              <span class="task-name">{{ t.item_name || t.item_id }}</span>
              <span class="task-type-hint">{{ t.item_type }}</span>
            </div>
            <div class="task-meta">
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
                @click="handleRetry(t.id)"
              >
                重试
              </NButton>
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

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
@media (max-width: 768px) {
  .kpi-grid { grid-template-columns: repeat(2, 1fr); }
}

.kpi-card {
  text-align: center;
}
.kpi-label {
  font-size: 13px;
  color: var(--n-text-color-3, #888);
  margin-bottom: 6px;
}
.kpi-value {
  font-size: 28px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.kpi-value.pending { color: #909399; }
.kpi-value.running { color: #e6a23c; }
.kpi-value.failed { color: #f56c6c; }
.kpi-value.done { color: #67c23a; }

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
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

.task-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
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
</style>
