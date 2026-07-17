<script setup lang="ts">
import { computed, reactive, shallowRef, watch } from 'vue'
import {
  NButton,
  NCard,
  NIcon,
  NPagination,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  NTooltip,
} from 'naive-ui'
import { ChevronDownOutline, ChevronForwardOutline, RefreshOutline } from '@vicons/ionicons5'

import EmptyState from '@/components/EmptyState.vue'
import { useToast } from '@/composables/useToast'
import { useVisibleInterval } from '@/composables/useVisibleInterval'
import {
  getRefreshQueueRecent,
  getRefreshQueueTaskDetail,
  getScrapeQueueRecent,
  getScrapeQueueTaskDetail,
  retryRefreshQueueTask,
  retryScrapeQueueTask,
  type RefreshQueueStats,
  type RefreshQueueTaskDetail,
  type ScrapeQueueIdentifyDetail,
  type ScrapeQueueStats,
  type ScrapeQueueTask,
} from '@/api/client'

type QueueKind = 'scrape' | 'refresh'
type TaskListTab = 'failed' | 'running' | 'pending'
type QueueTaskView = Omit<ScrapeQueueTask, 'task_type'> & {
  task_type?: string
  scope?: string
  source?: string
}
type QueueDetailView = QueueTaskView & {
  request_url?: string
  response_status?: number
  response_sample?: string
  detail_json?: ScrapeQueueIdentifyDetail
  options?: RefreshQueueTaskDetail['options']
}

const props = defineProps<{
  kind: QueueKind
  stats: ScrapeQueueStats | RefreshQueueStats
}>()

const emit = defineEmits<{ changed: [] }>()
const { showToast } = useToast()

const PAGE_SIZE = 50
const POLL_INTERVAL = 5000
const activeTab = shallowRef<TaskListTab>('failed')
const loading = shallowRef(false)
const firstLoaded = shallowRef(false)
const expandedId = shallowRef<number | null>(null)
const detailLoading = shallowRef<number | null>(null)
const tasks = shallowRef<QueueTaskView[]>([])
const pageByStatus = reactive<Record<TaskListTab, number>>({ failed: 1, running: 1, pending: 1 })
const totalByStatus = reactive<Record<TaskListTab, number>>({ failed: 0, running: 0, pending: 0 })
const detailCache = reactive<Record<number, QueueDetailView>>({})
const detailError = reactive<Record<number, string>>({})
let requestId = 0

const title = computed(() => (props.kind === 'scrape' ? 'Scrape 队列' : 'Refresh 队列'))
const subtitle = computed(() => (
  props.kind === 'scrape'
    ? '远程识别、剧集补全与演员图任务'
    : '本地元数据、图片与子树刷新任务'
))
const activePage = computed({
  get: () => pageByStatus[activeTab.value],
  set: (value: number) => { pageByStatus[activeTab.value] = value },
})
const activeTotal = computed(() => totalByStatus[activeTab.value])

function statusCount(status: TaskListTab): number {
  return props.stats[status]
}

async function loadList() {
  const currentRequest = ++requestId
  loading.value = true
  try {
    const options = {
      status: activeTab.value,
      limit: PAGE_SIZE,
      offset: (activePage.value - 1) * PAGE_SIZE,
    }
    const response = props.kind === 'scrape'
      ? await getScrapeQueueRecent(options)
      : await getRefreshQueueRecent(options)
    if (currentRequest !== requestId) return
    tasks.value = (response.tasks ?? []) as QueueTaskView[]
    totalByStatus[activeTab.value] = response.total ?? 0
    firstLoaded.value = true
  } catch (error: any) {
    if (!firstLoaded.value) showToast(error?.message || `加载 ${title.value} 失败`, 'error')
  } finally {
    if (currentRequest === requestId) loading.value = false
  }
}

watch([activeTab, activePage], () => {
  expandedId.value = null
  tasks.value = []
  void loadList()
})

async function retryTask(id: number) {
  try {
    if (props.kind === 'scrape') await retryScrapeQueueTask(id)
    else await retryRefreshQueueTask(id)
    delete detailCache[id]
    delete detailError[id]
    expandedId.value = null
    showToast('任务已重置为待处理', 'success')
    emit('changed')
    await loadList()
  } catch (error: any) {
    showToast(error?.message || '重试失败', 'error')
  }
}

async function fetchDetail(id: number) {
  detailLoading.value = id
  delete detailError[id]
  try {
    const detail = props.kind === 'scrape'
      ? await getScrapeQueueTaskDetail(id)
      : await getRefreshQueueTaskDetail(id)
    detailCache[id] = detail as QueueDetailView
  } catch (error: any) {
    detailError[id] = error?.message || '加载失败'
  } finally {
    if (detailLoading.value === id) detailLoading.value = null
  }
}

async function toggleDetail(id: number) {
  if (expandedId.value === id) {
    expandedId.value = null
    return
  }
  expandedId.value = id
  if (!detailCache[id]) await fetchDetail(id)
}

function taskTypeLabel(value?: string): string {
  const labels: Record<string, string> = {
    identify: '识别',
    refresh: '重刮',
    backfill_quality: '画质',
    backfill_episode_name: '剧集名',
    backfill_episode_image: '剧集图',
    backfill_actor_images: '演员图',
  }
  return labels[value ?? ''] ?? value ?? '任务'
}

function refreshScopeLabel(value?: string): string {
  const labels: Record<string, string> = { metadata: '元数据', images: '图片', subtree: '子树' }
  return labels[value ?? ''] ?? value ?? '刷新'
}

function refreshSourceLabel(value?: string): string {
  const labels: Record<string, string> = { manual: '手动', scan: '扫库', fsnotify: '文件监控', sidecar: '边车文件' }
  return labels[value ?? ''] ?? value ?? '-'
}

function episodeTag(task: QueueTaskView): string {
  const season = task.parent_index_number
  const episode = task.index_number
  if (task.item_type === 'Episode' && season != null && episode != null) {
    return `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`
  }
  if (task.item_type === 'Season' && episode != null) return `S${String(episode).padStart(2, '0')}`
  return ''
}

function displayTitle(task: QueueTaskView): string {
  const episode = episodeTag(task)
  if (task.series_name && episode) return `${task.series_name} · ${episode}`
  return task.series_name || task.item_name || task.item_id
}

function formatDate(value?: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function scheduleLabel(task: QueueTaskView): string {
  if (task.status === 'pending') return `计划 ${formatDate(task.next_run_at)}`
  if (task.status === 'running') return `领取于 ${formatDate(task.updated_at)}`
  return `失败于 ${formatDate(task.updated_at)}`
}

function statusTagType(status?: number): 'success' | 'warning' | 'error' | 'default' {
  if (!status) return 'default'
  if (status >= 200 && status < 300) return 'success'
  if (status >= 400 && status < 500) return 'warning'
  if (status >= 500) return 'error'
  return 'default'
}

function formatJSON(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') {
    try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
  }
  return JSON.stringify(value, null, 2)
}

function identifySummary(detail?: ScrapeQueueIdentifyDetail): string {
  if (!detail) return ''
  const lines: string[] = []
  if (detail.stage) lines.push(`阶段: ${detail.stage}`)
  if (detail.reason) lines.push(`原因: ${detail.reason}`)
  if (detail.best_score != null) lines.push(`最高分: ${detail.best_score}`)
  if (detail.threshold != null) lines.push(`阈值: ${detail.threshold}`)
  if (detail.providers?.length) lines.push(`来源: ${detail.providers.join(' -> ')}`)
  return lines.join('\n')
}

useVisibleInterval(loadList, POLL_INTERVAL, { immediate: true })
</script>

<template>
  <NCard class="queue-list-card" size="small">
    <div class="queue-list-header">
      <div>
        <div class="queue-list-title">{{ title }}</div>
        <div class="queue-list-subtitle">{{ subtitle }}</div>
      </div>
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary circle size="small" :loading="loading" :aria-label="`刷新 ${title}`" @click="loadList">
            <template #icon><NIcon><RefreshOutline /></NIcon></template>
          </NButton>
        </template>
        刷新任务列表
      </NTooltip>
    </div>

    <NTabs v-model:value="activeTab" type="line" size="small" animated>
      <NTabPane name="failed" :tab="`失败 ${statusCount('failed')}`" />
      <NTabPane name="running" :tab="`运行 ${statusCount('running')}`" />
      <NTabPane name="pending" :tab="`待处理 ${statusCount('pending')}`" />
    </NTabs>

    <NSpin :show="loading && !firstLoaded">
      <EmptyState
        v-if="firstLoaded && tasks.length === 0"
        :description="activeTab === 'failed' ? '当前没有失败任务' : activeTab === 'running' ? '当前没有运行中任务' : '当前没有待处理任务'"
      />
      <div v-else class="task-list">
        <article v-for="task in tasks" :key="task.id" class="task-item">
          <div
            role="button"
            tabindex="0"
            class="task-row"
            :class="[`status-${task.status}`, { expanded: expandedId === task.id }]"
            @click="toggleDetail(task.id)"
            @keydown.enter.prevent="toggleDetail(task.id)"
            @keydown.space.prevent="toggleDetail(task.id)"
          >
            <span class="task-main">
              <span class="task-badges">
                <NTag size="small" :bordered="false">
                  {{ kind === 'scrape' ? taskTypeLabel(task.task_type) : refreshScopeLabel(task.scope) }}
                </NTag>
                <NTag v-if="kind === 'refresh'" size="small" :bordered="false" type="info">
                  {{ refreshSourceLabel(task.source) }}
                </NTag>
                <span v-if="task.retry_count > 0" class="retry-count">重试 {{ task.retry_count }}</span>
              </span>
              <span class="task-title">{{ displayTitle(task) }}</span>
              <span class="task-type">{{ task.item_type }}</span>
            </span>
            <span class="task-aside">
              <span class="task-time">{{ scheduleLabel(task) }}</span>
              <NButton
                v-if="task.status === 'failed'"
                size="tiny"
                secondary
                type="primary"
                @click.stop="retryTask(task.id)"
                @keydown.stop
              >
                重试
              </NButton>
              <NIcon class="detail-toggle">
                <ChevronDownOutline v-if="expandedId === task.id" />
                <ChevronForwardOutline v-else />
              </NIcon>
            </span>
            <code v-if="task.file_path" class="task-path" :title="task.file_path">{{ task.file_path }}</code>
            <span v-if="task.last_error" class="task-error" :title="task.last_error">{{ task.last_error }}</span>
          </div>

          <div v-if="expandedId === task.id" class="task-detail">
            <NSpin :show="detailLoading === task.id && !detailCache[task.id]">
              <div v-if="detailError[task.id]" class="detail-error">{{ detailError[task.id] }}</div>
              <div v-else-if="detailCache[task.id]" class="detail-grid">
                <div v-if="detailCache[task.id].last_error" class="detail-block detail-block-wide">
                  <div class="detail-label">错误</div>
                  <pre class="detail-pre error-pre">{{ detailCache[task.id].last_error }}</pre>
                </div>
                <div v-if="detailCache[task.id].request_url" class="detail-block detail-block-wide">
                  <div class="detail-label">请求地址</div>
                  <code class="detail-code">{{ detailCache[task.id].request_url }}</code>
                </div>
                <div v-if="detailCache[task.id].response_status" class="detail-block">
                  <div class="detail-label">HTTP 状态</div>
                  <NTag size="small" :type="statusTagType(detailCache[task.id].response_status)">
                    {{ detailCache[task.id].response_status }}
                  </NTag>
                </div>
                <div class="detail-block">
                  <div class="detail-label">任务 ID</div>
                  <code class="detail-code">{{ task.id }} / {{ task.item_id }}</code>
                </div>
                <div v-if="kind === 'scrape' && identifySummary(detailCache[task.id].detail_json)" class="detail-block detail-block-wide">
                  <div class="detail-label">识别摘要</div>
                  <pre class="detail-pre">{{ identifySummary(detailCache[task.id].detail_json) }}</pre>
                </div>
                <div v-if="kind === 'scrape' && detailCache[task.id].detail_json?.parsed" class="detail-block">
                  <div class="detail-label">解析结果</div>
                  <pre class="detail-pre">{{ formatJSON(detailCache[task.id].detail_json?.parsed) }}</pre>
                </div>
                <div v-if="kind === 'scrape' && detailCache[task.id].detail_json?.search_attempts?.length" class="detail-block">
                  <div class="detail-label">搜索尝试</div>
                  <pre class="detail-pre">{{ formatJSON(detailCache[task.id].detail_json?.search_attempts) }}</pre>
                </div>
                <div v-if="kind === 'refresh'" class="detail-block detail-block-wide">
                  <div class="detail-label">刷新参数</div>
                  <pre class="detail-pre">{{ formatJSON(detailCache[task.id].options ?? {}) }}</pre>
                </div>
                <div v-if="detailCache[task.id].response_sample" class="detail-block detail-block-wide">
                  <div class="detail-label">响应内容</div>
                  <pre class="detail-pre">{{ formatJSON(detailCache[task.id].response_sample) }}</pre>
                </div>
                <div
                  v-if="!detailCache[task.id].last_error && !detailCache[task.id].request_url && !detailCache[task.id].response_sample && !detailCache[task.id].detail_json && !detailCache[task.id].options"
                  class="detail-empty"
                >
                  当前任务没有额外诊断信息。
                </div>
              </div>
            </NSpin>
          </div>
        </article>
      </div>
    </NSpin>

    <div v-if="activeTotal > PAGE_SIZE" class="pagination-row">
      <NPagination
        v-model:page="activePage"
        :page-count="Math.ceil(activeTotal / PAGE_SIZE)"
        :page-size="PAGE_SIZE"
        :page-slot="7"
        size="small"
      />
      <span>共 {{ activeTotal }} 条</span>
    </div>
  </NCard>
</template>

<style scoped>
.queue-list-card { border-radius: 6px; }
.queue-list-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 2px; }
.queue-list-title { font-size: 14px; font-weight: 650; color: var(--n-text-color-1); }
.queue-list-subtitle { margin-top: 2px; font-size: 11px; color: var(--n-text-color-3); }
.task-list { display: flex; flex-direction: column; gap: 6px; margin-top: 5px; }
.task-item { min-width: 0; }
.task-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 7px 16px;
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--n-border-color);
  border-left-width: 3px;
  border-radius: 5px;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}
.task-row:hover { background: var(--n-action-color); }
.task-row.expanded { border-bottom-left-radius: 0; border-bottom-right-radius: 0; }
.task-row.status-failed { border-left-color: #d03050; }
.task-row.status-running { border-left-color: #f0a020; }
.task-row.status-pending { border-left-color: #8a8f98; }
.task-main,
.task-aside,
.task-badges { display: flex; align-items: center; min-width: 0; }
.task-main { gap: 8px; }
.task-aside { justify-content: flex-end; gap: 9px; }
.task-badges { gap: 5px; flex-shrink: 0; }
.task-title { min-width: 0; overflow: hidden; color: var(--n-text-color-1); font-size: 13px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.task-type,
.task-time { flex-shrink: 0; color: var(--n-text-color-3); font-size: 11px; white-space: nowrap; }
.retry-count { color: #d68a00; font-size: 11px; white-space: nowrap; }
.detail-toggle { flex-shrink: 0; color: var(--n-text-color-3); }
.task-path { grid-column: 1; min-width: 0; overflow: hidden; color: var(--n-text-color-2); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.task-error { grid-column: 1 / -1; max-height: 3em; overflow: hidden; color: #d03050; font-size: 11px; line-height: 1.5; overflow-wrap: anywhere; }
.task-detail { padding: 12px; border: 1px solid var(--n-border-color); border-top: 0; border-bottom-left-radius: 5px; border-bottom-right-radius: 5px; background: var(--n-action-color); }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.detail-block { min-width: 0; }
.detail-block-wide { grid-column: 1 / -1; }
.detail-label { margin-bottom: 4px; color: var(--n-text-color-3); font-size: 10px; font-weight: 650; text-transform: uppercase; }
.detail-code,
.detail-pre { display: block; max-height: 260px; margin: 0; padding: 7px 9px; overflow: auto; border-radius: 4px; background: var(--n-card-color); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 11px; line-height: 1.5; overflow-wrap: anywhere; white-space: pre-wrap; }
.error-pre { color: #d03050; background: rgba(208, 48, 80, 0.08); }
.detail-error,
.detail-empty { color: var(--n-text-color-3); font-size: 12px; }
.pagination-row { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 12px; color: var(--n-text-color-3); font-size: 11px; }

@media (max-width: 720px) {
  .task-row { grid-template-columns: 1fr; }
  .task-main { align-items: flex-start; flex-wrap: wrap; }
  .task-aside { justify-content: space-between; }
  .task-path { grid-column: 1; }
  .detail-grid { grid-template-columns: 1fr; }
  .detail-block-wide { grid-column: 1; }
}
</style>
