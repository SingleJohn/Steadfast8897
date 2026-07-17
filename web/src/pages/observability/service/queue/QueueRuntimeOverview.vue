<script setup lang="ts">
import { computed } from 'vue'
import {
  NButton,
  NCard,
  NIcon,
  NInputNumber,
  NPopconfirm,
  NProgress,
  NTag,
  NTooltip,
} from 'naive-ui'
import {
  CloudDownloadOutline,
  CloudOfflineOutline,
  DatabaseOutline,
  PulseOutline,
  RefreshOutline,
  SaveOutline,
  ShieldCheckmarkOutline,
  WarningOutline,
} from '@vicons/ionicons5'

import type { MetricsSnapshot, RefreshQueueStats, ScrapeQueueStats } from '@/api/client'

type BusyState = {
  refresh: boolean
  scrapeAll: boolean
  reloadTmdb: boolean
  ingestWorker: boolean
  scrapeWorker: boolean
  refreshWorker: boolean
}

const props = defineProps<{
  metrics: MetricsSnapshot
  scrapeStats: ScrapeQueueStats
  refreshStats: RefreshQueueStats
  busy: BusyState
  updatedAt?: string
  snapshotError?: string
}>()

const emit = defineEmits<{
  refresh: []
  scrapeAll: []
  reloadTmdb: []
  retryScrape: []
  retryRefresh: []
  saveIngestWorker: []
  saveScrapeWorker: []
  saveRefreshWorker: []
}>()

const ingestWorker = defineModel<number>('ingestWorker', { required: true })
const scrapeWorker = defineModel<number>('scrapeWorker', { required: true })
const refreshWorker = defineModel<number>('refreshWorker', { required: true })

const poolPercent = computed(() => {
  const pool = props.metrics.db_pool
  if (!pool || pool.max_conns <= 0) return 0
  return Math.min(100, Math.round((pool.acquired_conns / pool.max_conns) * 100))
})

const health = computed(() => {
  const runtime = props.metrics.scrape_worker
  if (props.snapshotError) {
    return { level: 'warning', title: '运行快照读取失败', detail: '正在保留上一份数据并等待下一轮恢复。' }
  }
  if (!runtime || !props.metrics.db_pool) {
    return { level: 'notice', title: '正在读取运行状态', detail: '连接池与 worker 快照即将就绪。' }
  }
  if (runtime?.state_write_healthy === false) {
    return { level: 'critical', title: '队列状态写回异常', detail: 'worker 已主动减速，请先检查数据库连接。' }
  }
  if (poolPercent.value >= 90) {
    return { level: 'critical', title: '数据库连接池接近耗尽', detail: '新请求可能开始等待连接。' }
  }
  if (runtime?.tmdb_state === 'not_configured') {
    return { level: 'warning', title: '远程刮削已暂停', detail: 'TMDB 未配置，本地画质任务仍可继续。' }
  }
  if (runtime?.circuit_open) {
    return { level: 'warning', title: 'TMDB 熔断保护中', detail: '远程任务保持排队，冷却结束后自动探测。' }
  }
  if (runtime?.tmdb_state === 'config_error' || poolPercent.value >= 70) {
    return { level: 'warning', title: '运行链路有压力', detail: '连接池或 TMDB 配置读取需要关注。' }
  }
  if ((props.metrics.ingest_overflow_total ?? 0) > 0 || props.scrapeStats.failed + props.refreshStats.failed > 0) {
    return { level: 'notice', title: '服务运行中，有待处理异常', detail: '失败任务已保留，可在下方展开诊断。' }
  }
  return { level: 'healthy', title: '运行链路正常', detail: '入库、刷新和刮削 worker 均处于可用状态。' }
})

const healthIcon = computed(() => {
  if (health.value.level === 'healthy') return ShieldCheckmarkOutline
  if (health.value.level === 'critical') return CloudOfflineOutline
  return WarningOutline
})

const tmdbLabel = computed(() => {
  const labels: Record<string, string> = {
    unknown: '初始化中',
    ready: '可用',
    degraded: '波动',
    cooldown: '熔断冷却',
    probing: '恢复探测',
    not_configured: '未配置',
    config_error: '配置读取失败',
  }
  return labels[props.metrics.scrape_worker?.tmdb_state ?? 'unknown'] ?? '未知'
})

const tmdbTagType = computed<'success' | 'warning' | 'error' | 'default'>(() => {
  const state = props.metrics.scrape_worker?.tmdb_state
  if (state === 'ready') return 'success'
  if (state === 'not_configured' || state === 'config_error') return 'error'
  if (state === 'degraded' || state === 'cooldown' || state === 'probing') return 'warning'
  return 'default'
})

const updatedLabel = computed(() => {
  if (!props.updatedAt) return '等待首次刷新'
  return `更新于 ${new Date(props.updatedAt).toLocaleTimeString()}`
})

const cooldownLabel = computed(() => {
  const until = props.metrics.scrape_worker?.cooldown_until
  return until ? `冷却至 ${new Date(until).toLocaleTimeString()}` : '累计请求'
})

const runtimeError = computed(() => {
  if (props.snapshotError) return props.snapshotError
  const runtime = props.metrics.scrape_worker
  if (!runtime) return ''
  if (runtime.state_write_healthy === false) return runtime.last_state_write_error ?? '队列状态写回失败'
  return runtime.tmdb_state !== 'ready' ? runtime.last_error ?? '' : ''
})
</script>

<template>
  <div class="runtime-overview">
    <section class="health-band" :class="`health-${health.level}`">
      <div class="health-main">
        <span class="health-icon">
          <NIcon :component="healthIcon" />
        </span>
        <div class="health-copy">
          <div class="health-title">{{ health.title }}</div>
          <div class="health-detail">{{ health.detail }}</div>
        </div>
      </div>
      <div class="health-actions">
        <span class="updated-at">{{ updatedLabel }}</span>
        <NTooltip trigger="hover">
          <template #trigger>
            <NButton quaternary circle :loading="busy.refresh" aria-label="刷新运行状态" @click="emit('refresh')">
              <template #icon><NIcon><RefreshOutline /></NIcon></template>
            </NButton>
          </template>
          刷新运行状态
        </NTooltip>
      </div>
    </section>

    <div class="signal-grid">
      <div class="signal-cell">
        <div class="signal-heading">
          <span class="signal-label"><NIcon><DatabaseOutline /></NIcon> 数据库连接</span>
          <strong>{{ metrics.db_pool?.acquired_conns ?? '-' }} / {{ metrics.db_pool?.max_conns ?? '-' }}</strong>
        </div>
        <NProgress
          type="line"
          :percentage="poolPercent"
          :height="6"
          :show-indicator="false"
          :status="poolPercent >= 90 ? 'error' : poolPercent >= 70 ? 'warning' : 'success'"
        />
        <div class="signal-meta">
          <span>空闲 {{ metrics.db_pool?.idle_conns ?? '-' }}</span>
          <span>等待累计 {{ metrics.db_pool?.empty_acquire_count ?? '-' }}</span>
        </div>
      </div>

      <div class="signal-cell">
        <div class="signal-heading">
          <span class="signal-label"><NIcon><PulseOutline /></NIcon> TMDB 依赖</span>
          <NTag size="small" :type="tmdbTagType" :bordered="false">{{ tmdbLabel }}</NTag>
        </div>
        <div class="signal-value">{{ metrics.tmdb_requests_total ?? '-' }}</div>
        <div class="signal-meta">
          <span>{{ cooldownLabel }}</span>
          <span>熔断 {{ metrics.scrape_worker?.circuit_openings_total ?? 0 }} 次</span>
        </div>
      </div>

      <div class="signal-cell">
        <div class="signal-heading">
          <span class="signal-label">Scrape 队列</span>
          <strong>{{ scrapeStats.pending + scrapeStats.running }}</strong>
        </div>
        <div class="queue-dots" aria-label="Scrape 队列状态">
          <span class="queue-dot pending">待处理 {{ scrapeStats.pending }}</span>
          <span class="queue-dot running">运行 {{ scrapeStats.running }}</span>
          <span class="queue-dot failed">失败 {{ scrapeStats.failed }}</span>
        </div>
        <div class="signal-meta">
          <span>Claim 失败 {{ metrics.scrape_worker?.claim_failures_total ?? 0 }}</span>
          <span>写回失败 {{ metrics.scrape_worker?.state_write_failures_total ?? 0 }}</span>
        </div>
      </div>

      <div class="signal-cell">
        <div class="signal-heading">
          <span class="signal-label">Ingest / Refresh</span>
          <strong>{{ metrics.ingest_channel_depth ?? '-' }}</strong>
        </div>
        <div class="queue-dots">
          <span class="queue-dot pending">刷新待处理 {{ refreshStats.pending }}</span>
          <span class="queue-dot running">运行 {{ refreshStats.running }}</span>
          <span class="queue-dot failed">失败 {{ refreshStats.failed }}</span>
        </div>
        <div class="signal-meta"><span>入库溢出 {{ metrics.ingest_overflow_total ?? '-' }}</span></div>
      </div>
    </div>

    <div v-if="runtimeError" class="runtime-error">
      <NIcon><WarningOutline /></NIcon>
      <span class="runtime-error-label">最近错误</span>
      <code :title="runtimeError">{{ runtimeError }}</code>
    </div>

    <NCard class="control-panel" size="small">
      <div class="control-header">
        <div>
          <div class="control-title">Worker 与恢复操作</div>
          <div class="control-subtitle">配置保存后立即生效，远程刮削仍受共享限流与熔断保护。</div>
        </div>
        <div class="command-row">
          <NPopconfirm @positive-click="emit('scrapeAll')">
            <template #trigger>
              <NButton size="small" type="primary" :loading="busy.scrapeAll">
                <template #icon><NIcon><CloudDownloadOutline /></NIcon></template>
                补齐缺失元数据
              </NButton>
            </template>
            将缺失元数据的 Movie/Series 加入持久化队列。
          </NPopconfirm>
          <NButton size="small" :loading="busy.reloadTmdb" @click="emit('reloadTmdb')">
            <template #icon><NIcon><RefreshOutline /></NIcon></template>
            重载 TMDB
          </NButton>
        </div>
      </div>

      <div class="worker-grid">
        <div class="worker-row">
          <div class="worker-name">入库</div>
          <div class="worker-caption">文件事件消费</div>
          <NInputNumber v-model:value="ingestWorker" :min="1" :max="64" size="small" />
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton circle secondary size="small" :loading="busy.ingestWorker" aria-label="保存入库 worker" @click="emit('saveIngestWorker')">
                <template #icon><NIcon><SaveOutline /></NIcon></template>
              </NButton>
            </template>
            保存入库 worker 数
          </NTooltip>
        </div>

        <div class="worker-row">
          <div class="worker-name">刮削</div>
          <div class="worker-caption">远程元数据任务</div>
          <NInputNumber v-model:value="scrapeWorker" :min="1" :max="16" size="small" />
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton circle secondary size="small" :loading="busy.scrapeWorker" aria-label="保存刮削 worker" @click="emit('saveScrapeWorker')">
                <template #icon><NIcon><SaveOutline /></NIcon></template>
              </NButton>
            </template>
            保存刮削 worker 数
          </NTooltip>
          <NPopconfirm v-if="scrapeStats.failed > 0" @positive-click="emit('retryScrape')">
            <template #trigger><NButton text type="warning" size="tiny">重试 {{ scrapeStats.failed }} 条</NButton></template>
            重置全部 Scrape 失败任务。
          </NPopconfirm>
        </div>

        <div class="worker-row">
          <div class="worker-name">刷新</div>
          <div class="worker-caption">本地 metadata / artwork</div>
          <NInputNumber v-model:value="refreshWorker" :min="1" :max="8" size="small" />
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton circle secondary size="small" :loading="busy.refreshWorker" aria-label="保存刷新 worker" @click="emit('saveRefreshWorker')">
                <template #icon><NIcon><SaveOutline /></NIcon></template>
              </NButton>
            </template>
            保存刷新 worker 数
          </NTooltip>
          <NPopconfirm v-if="refreshStats.failed > 0" @positive-click="emit('retryRefresh')">
            <template #trigger><NButton text type="warning" size="tiny">重试 {{ refreshStats.failed }} 条</NButton></template>
            重置全部 Refresh 失败任务。
          </NPopconfirm>
        </div>
      </div>
    </NCard>
  </div>
</template>

<style scoped>
.runtime-overview {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.health-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 68px;
  padding: 12px 16px;
  border: 1px solid var(--n-border-color);
  border-left-width: 4px;
  border-radius: 6px;
  background: var(--n-card-color);
}

.health-healthy { border-left-color: #18a058; }
.health-notice { border-left-color: #2080f0; }
.health-warning { border-left-color: #f0a020; }
.health-critical { border-left-color: #d03050; }

.health-main,
.health-actions,
.signal-heading,
.signal-label,
.control-header,
.command-row,
.worker-row {
  display: flex;
  align-items: center;
}

.health-main { gap: 12px; min-width: 0; }
.health-icon { display: inline-flex; font-size: 24px; }
.health-copy { min-width: 0; }
.health-title { font-size: 16px; font-weight: 650; color: var(--n-text-color-1); }
.health-detail { margin-top: 2px; font-size: 12px; color: var(--n-text-color-3); }
.health-actions { gap: 8px; flex-shrink: 0; }
.updated-at { font-size: 11px; color: var(--n-text-color-3); }

.signal-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--n-border-color);
  border-radius: 6px;
  overflow: hidden;
  background: var(--n-card-color);
}

.signal-cell {
  min-width: 0;
  min-height: 112px;
  padding: 14px;
  border-right: 1px solid var(--n-border-color);
}

.signal-cell:last-child { border-right: 0; }
.signal-heading { justify-content: space-between; gap: 12px; font-variant-numeric: tabular-nums; }
.signal-label { gap: 6px; font-size: 12px; color: var(--n-text-color-2); }
.signal-value { margin-top: 13px; font-size: 24px; font-weight: 650; font-variant-numeric: tabular-nums; }
.signal-meta { display: flex; justify-content: space-between; gap: 8px; margin-top: 9px; font-size: 11px; color: var(--n-text-color-3); }
.queue-dots { display: flex; flex-wrap: wrap; gap: 7px 12px; margin-top: 15px; }
.queue-dot { position: relative; padding-left: 10px; font-size: 11px; color: var(--n-text-color-2); white-space: nowrap; }
.queue-dot::before { position: absolute; left: 0; top: 5px; width: 5px; height: 5px; border-radius: 50%; content: ''; }
.queue-dot.pending::before { background: #8a8f98; }
.queue-dot.running::before { background: #f0a020; }
.queue-dot.failed::before { background: #d03050; }
.runtime-error { display: grid; grid-template-columns: 18px auto minmax(0, 1fr); align-items: center; gap: 8px; padding: 9px 12px; border: 1px solid rgba(208, 48, 80, 0.28); border-radius: 5px; background: rgba(208, 48, 80, 0.06); color: #d03050; }
.runtime-error-label { font-size: 11px; font-weight: 650; white-space: nowrap; }
.runtime-error code { min-width: 0; overflow: hidden; color: var(--n-text-color-2); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }

.control-panel { border-radius: 6px; }
.control-header { justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.control-title { font-size: 14px; font-weight: 650; }
.control-subtitle { margin-top: 2px; font-size: 11px; color: var(--n-text-color-3); }
.command-row { gap: 8px; flex-wrap: wrap; }
.worker-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-top: 1px solid var(--n-border-color); }
.worker-row { display: grid; grid-template-columns: 44px minmax(110px, 1fr) 84px 30px; gap: 8px; padding: 13px 14px 0; }
.worker-row + .worker-row { border-left: 1px solid var(--n-border-color); }
.worker-name { font-size: 13px; font-weight: 650; }
.worker-caption { min-width: 0; font-size: 11px; color: var(--n-text-color-3); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.worker-row :deep(.n-button--text-type) { grid-column: 2 / -1; justify-self: start; }

@media (max-width: 980px) {
  .signal-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .signal-cell:nth-child(2) { border-right: 0; }
  .signal-cell:nth-child(-n + 2) { border-bottom: 1px solid var(--n-border-color); }
  .worker-grid { grid-template-columns: 1fr; }
  .worker-row + .worker-row { border-left: 0; border-top: 1px solid var(--n-border-color); }
}

@media (max-width: 640px) {
  .health-band,
  .control-header { align-items: flex-start; flex-direction: column; }
  .health-actions { width: 100%; justify-content: space-between; }
  .signal-grid { grid-template-columns: 1fr; }
  .signal-cell { border-right: 0; border-bottom: 1px solid var(--n-border-color); }
  .signal-cell:last-child { border-bottom: 0; }
  .worker-row { grid-template-columns: 40px minmax(80px, 1fr) 80px 30px; padding-left: 0; padding-right: 0; }
}
</style>
