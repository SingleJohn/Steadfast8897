<script setup lang="ts">
import { computed, h, inject } from 'vue'
import {
  NButton,
  NCollapse,
  NCollapseItem,
  NDataTable,
  NDatePicker,
  NDivider,
  NGrid,
  NGridItem,
  NIcon,
  NInput,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  NText,
} from 'naive-ui'
import { FunnelOutline, RefreshOutline, TimeOutline, GlobeOutline, CheckmarkCircleOutline } from '@vicons/ionicons5'

import ErrorBanner from '@/components/ErrorBanner.vue'
import PageSectionCard from '@/components/PageSectionCard.vue'
import RedirectTopTables from '@/pages/observability/RedirectTopTables.vue'
import StatCard from '@/components/StatCard.vue'
import { OBS_KEY } from '@/composables/observabilityContext'

const obs = inject(OBS_KEY)
if (!obs) throw new Error('RedirectTab must be used within ObservabilityPage')

const {
  redirectIsLive,
  redirectBackend,
  redirectUserID,
  redirectUserName,
  redirectIP,
  redirectUAContains,
  redirectPathPrefix,
  redirectLimit,
  redirectRange,
  redirectSummary,
  redirectSummaryError,
  redirectLogsError,
  redirectSummaryLoading,
  redirectLogsLoading,
  redirectLogsItems,
  redirectLogsColumns,
  rowProps,
  redirectLogsOffset,
  canNextRedirectPage,
  redirectBackendOptions,
  topRedirectBackends,
  redirectWindowLabel,
  redirectTraceAggLoading,
  redirectTraceAggError,
  redirectTraceRequestStages,
  redirectTraceAttemptStages,
  redirectTraceByBackend,
  redirectTraceAggMeta,
  refreshRedirectSummary,
  refreshRedirectLogs,
  refreshRedirectTraceAgg,
  resetRedirectFilters,
  nextRedirectPage,
} = obs

function onRefreshAll() {
  void refreshRedirectSummary()
  void refreshRedirectLogs(true)
  void refreshRedirectTraceAgg()
}
function onSearch() { onRefreshAll() }
function onReset() { resetRedirectFilters() }
function onNext() { void nextRedirectPage() }

const requestStageMeta: Record<string, { name: string; desc: string }> = {
  emby_lookup_ms: { name: 'Emby 路径查询', desc: '向 Emby Items API 查询播放项真实路径。' },
  route_decision_ms: { name: '路由匹配', desc: '根据 source + real path 计算命中路由与资源池。' },
  object_map_ms: { name: '对象映射', desc: '把 real path 映射成后端 object_key。' },
}

const attemptStageMeta: Record<string, { name: string; desc: string }> = {
  path_l1_lookup_ms: { name: 'L1 内存缓存读取', desc: '读取进程内对象路径缓存，命中可直接跳过后续解析。' },
  presign_ms: { name: 'S3 预签名', desc: '调用 SDK 生成 S3 直链签名 URL。' },
  sign_ms: { name: 'URL 签名', desc: 'CDN/鉴权链路的签名计算耗时。' },
  path_cache_lookup_ms: { name: '路径缓存读取', desc: '读取 GDrive 路径缓存（DB/L1）耗时。' },
  resolve_path_ms: { name: '路径解析', desc: 'GDrive 路径逐级解析到 FileID。' },
  path_cache_write_ms: { name: '路径缓存写入', desc: '将解析结果写回缓存。' },
  token_build_ms: { name: 'Token 构建', desc: '构建临时跳转 token（本地/Worker 等）。' },
  compose_link_ms: { name: '拼接直链', desc: 'Pan123 compose 模式拼接直链。' },
  path_resolve_ms: { name: '对象定位', desc: 'Pan123 路径解析到 file_id。' },
  direct_link_ms: { name: '直链获取', desc: 'Pan123 调 API 获取可下载 URL。' },
}

const requestStageRows = computed(() => [...redirectTraceRequestStages.value])

const attemptStageRows = computed(() => [...redirectTraceAttemptStages.value].sort((a: any, b: any) => Number(b.p95_ms || 0) - Number(a.p95_ms || 0)))

const backendRows = computed(() => [...redirectTraceByBackend.value].sort((a: any, b: any) => Number(b.p95_ms || 0) - Number(a.p95_ms || 0)))

const parseRateText = computed(() => {
  const meta = redirectTraceAggMeta.value
  const sampled = Number(meta?.sampled || 0)
  const parsed = Number(meta?.parsed || 0)
  if (sampled <= 0) return '0%'
  return `${((parsed * 100) / sampled).toFixed(1)}%`
})

const requestStageColumns = [
  {
    title: '阶段',
    key: 'stage',
    width: 160,
    render: (row: any) => h(NTag, { size: 'small', round: true, bordered: false, type: 'info' }, { default: () => requestStageMeta[row.stage]?.name || row.stage }),
  },
  { title: '说明', key: 'desc', ellipsis: { tooltip: true }, render: (row: any) => requestStageMeta[row.stage]?.desc || '该阶段尚未定义说明。' },
  { title: 'Count', key: 'count', width: 78, align: 'right' as const },
  { title: 'Avg', key: 'avg_ms', width: 88, align: 'right' as const, render: (row: any) => `${Number(row.avg_ms || 0).toFixed(1)}ms` },
  { title: 'P50', key: 'p50_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.p50_ms || 0)}ms` },
  {
    title: 'P95',
    key: 'p95_ms',
    width: 92,
    align: 'right' as const,
    render: (row: any) => h('span', { class: Number(row.p95_ms || 0) >= 1000 ? 'metric-hot' : 'metric-warn' }, `${Number(row.p95_ms || 0)}ms`),
  },
  { title: 'Max', key: 'max_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.max_ms || 0)}ms` },
]

const attemptStageColumns = [
  {
    title: '阶段',
    key: 'stage',
    width: 160,
    render: (row: any) => h(NTag, { size: 'small', round: true, bordered: false, type: 'warning' }, { default: () => attemptStageMeta[row.stage]?.name || row.stage }),
  },
  { title: '说明', key: 'desc', ellipsis: { tooltip: true }, render: (row: any) => attemptStageMeta[row.stage]?.desc || '该阶段尚未定义说明。' },
  { title: 'Count', key: 'count', width: 78, align: 'right' as const },
  { title: 'Avg', key: 'avg_ms', width: 88, align: 'right' as const, render: (row: any) => `${Number(row.avg_ms || 0).toFixed(1)}ms` },
  { title: 'P50', key: 'p50_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.p50_ms || 0)}ms` },
  {
    title: 'P95',
    key: 'p95_ms',
    width: 92,
    align: 'right' as const,
    render: (row: any) => h('span', { class: Number(row.p95_ms || 0) >= 1000 ? 'metric-hot' : 'metric-warn' }, `${Number(row.p95_ms || 0)}ms`),
  },
  { title: 'Max', key: 'max_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.max_ms || 0)}ms` },
]

const backendColumns = [
  { title: 'Backend', key: 'backend', width: 150, ellipsis: { tooltip: true } },
  { title: 'Count', key: 'count', width: 82, align: 'right' as const },
  {
    title: '成功率',
    key: 'success_rate',
    width: 96,
    align: 'right' as const,
    render: (row: any) => {
      const rate = Number(row.success_rate || 0)
      const type = rate >= 0.99 ? 'success' : rate >= 0.95 ? 'warning' : 'error'
      return h(NTag, { size: 'small', round: true, bordered: false, type }, { default: () => `${(rate * 100).toFixed(1)}%` })
    },
  },
  { title: 'Avg', key: 'avg_ms', width: 88, align: 'right' as const, render: (row: any) => `${Number(row.avg_ms || 0).toFixed(1)}ms` },
  { title: 'P50', key: 'p50_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.p50_ms || 0)}ms` },
  {
    title: 'P95',
    key: 'p95_ms',
    width: 92,
    align: 'right' as const,
    render: (row: any) => h('span', { class: Number(row.p95_ms || 0) >= 1000 ? 'metric-hot' : 'metric-warn' }, `${Number(row.p95_ms || 0)}ms`),
  },
  { title: 'Max', key: 'max_ms', width: 82, align: 'right' as const, render: (row: any) => `${Number(row.max_ms || 0)}ms` },
]
</script>

<template>
  <n-space vertical :size="24">
    <n-grid cols="2 s:4" x-gap="16" y-gap="16" responsive="screen">
      <n-grid-item>
        <stat-card title="302 总数" :value="redirectSummary?.total_302 || 0" :icon="TimeOutline" type="info" />
      </n-grid-item>
      <n-grid-item>
        <stat-card :title="topRedirectBackends[0]?.backend || 'Top1'" :value="topRedirectBackends[0]?.count || 0" :icon="CheckmarkCircleOutline" type="success" />
      </n-grid-item>
      <n-grid-item>
        <stat-card :title="topRedirectBackends[1]?.backend || 'Top2'" :value="topRedirectBackends[1]?.count || 0" :icon="GlobeOutline" type="primary" />
      </n-grid-item>
      <n-grid-item>
        <stat-card title="时间窗" class="time-window-card" :value="redirectWindowLabel" :icon="TimeOutline" type="info" value-size="sm" />
      </n-grid-item>
    </n-grid>

    <error-banner v-if="redirectSummaryError" :message="redirectSummaryError" />
    <error-banner v-if="redirectLogsError" :message="redirectLogsError" />

    <page-section-card class="log-card" content-style="padding: 0;">
      <div class="filter-bar">
        <n-collapse arrow-placement="right" :default-expanded-names="['filter']">
          <template #header-extra>
            <n-space align="center">
              <n-text depth="3" size="small">实时追踪</n-text>
              <n-switch v-model:value="redirectIsLive" size="small" />
              <n-divider vertical />
              <n-button quaternary circle size="small" @click="onRefreshAll">
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
              </n-button>
            </n-space>
          </template>
          <n-collapse-item title="筛选条件" name="filter">
            <template #header>
              <n-space align="center" :size="8">
                <n-icon><FunnelOutline /></n-icon>
                <span>302 筛选</span>
              </n-space>
            </template>
            <div class="filter-content">
              <n-grid cols="1 s:2 m:4" x-gap="16" y-gap="12" responsive="screen">
                <n-grid-item>
                  <n-select v-model:value="redirectBackend" clearable placeholder="Backend" :options="redirectBackendOptions" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="redirectUserID" placeholder="Emby UserId" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="redirectUserName" placeholder="Emby 用户名(模糊)" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="redirectIP" placeholder="IP 地址" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="redirectUAContains" placeholder="UA 包含(模糊)" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="redirectPathPrefix" placeholder="路径前缀" :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-date-picker v-model:value="redirectRange" type="datetimerange" clearable :disabled="redirectIsLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-select v-model:value="redirectLimit" :disabled="redirectIsLive" :options="[{label:'50',value:50},{label:'100',value:100},{label:'200',value:200}]" />
                </n-grid-item>
              </n-grid>
              <n-space justify="end" style="margin-top: 16px">
                <n-button size="small" @click="onReset" :disabled="redirectIsLive">重置</n-button>
                <n-button type="primary" size="small" @click="onSearch" :disabled="redirectIsLive">查询</n-button>
              </n-space>
            </div>
          </n-collapse-item>
        </n-collapse>
      </div>

      <n-divider style="margin: 0" />

      <n-data-table
        :columns="redirectLogsColumns"
        :data="redirectLogsItems"
        :loading="redirectLogsLoading && !redirectIsLive"
        :row-props="rowProps"
        :max-height="600"
        :bordered="false"
        size="small"
        class="log-table"
      />

      <div class="pagination-bar">
        <n-text depth="3" size="small">
          {{ redirectIsLive ? 'Live Mode' : `Offset: ${redirectLogsOffset}` }}
        </n-text>
        <n-button size="small" :disabled="!canNextRedirectPage" @click="onNext">加载更多</n-button>
      </div>
    </page-section-card>

    <redirect-top-tables :summary="redirectSummary" :loading="redirectSummaryLoading" />

    <page-section-card title="Trace 聚合" class="trace-card">
      <error-banner v-if="redirectTraceAggError" :message="redirectTraceAggError" />
      <div class="trace-meta">
        <div class="meta-chip">
          <span class="label">采样上限</span>
          <span class="value">{{ redirectTraceAggMeta.sample_limit }}</span>
        </div>
        <div class="meta-chip">
          <span class="label">样本数</span>
          <span class="value">{{ redirectTraceAggMeta.sampled }}</span>
        </div>
        <div class="meta-chip">
          <span class="label">解析成功</span>
          <span class="value">{{ redirectTraceAggMeta.parsed }}</span>
        </div>
        <div class="meta-chip">
          <span class="label">解析成功率</span>
          <span class="value">{{ parseRateText }}</span>
        </div>
      </div>

      <n-grid cols="1" y-gap="14" responsive="screen">
        <n-grid-item>
          <div class="trace-table-title">请求级阶段（网关主链路）</div>
          <n-data-table
            :columns="requestStageColumns"
            :data="requestStageRows"
            :loading="redirectTraceAggLoading"
            :bordered="false"
            size="small"
            :max-height="280"
            class="trace-table"
          />
        </n-grid-item>
        <n-grid-item>
          <div class="trace-table-title">尝试级阶段（后端内部链路）</div>
          <n-data-table
            :columns="attemptStageColumns"
            :data="attemptStageRows"
            :loading="redirectTraceAggLoading"
            :bordered="false"
            size="small"
            :max-height="320"
            class="trace-table"
          />
        </n-grid-item>
        <n-grid-item>
          <div class="trace-table-title">Backend 汇总（按 P95 排序）</div>
          <n-data-table
            :columns="backendColumns"
            :data="backendRows"
            :loading="redirectTraceAggLoading"
            :bordered="false"
            size="small"
            :max-height="280"
            class="trace-table"
          />
        </n-grid-item>
      </n-grid>

      <div class="trace-note">
        <div class="trace-note-title">阶段说明</div>
        <div class="trace-note-grid">
          <div class="trace-note-item"><strong>请求级阶段</strong>：发生在网关主链路，定位是路由匹配慢、Emby 查询慢还是路径映射慢。</div>
          <div class="trace-note-item"><strong>尝试级阶段</strong>：发生在具体后端内部，定位是签名慢、缓存慢、路径解析慢还是上游 API 慢。</div>
          <div class="trace-note-item"><strong>P95</strong>：95% 请求低于该值，最能反映尾延迟。</div>
          <div class="trace-note-item"><strong>成功率</strong>：该 backend 的 attempt 成功比例，用于判断故障与回退情况。</div>
        </div>
      </div>
    </page-section-card>
  </n-space>
</template>

<style scoped>
.filter-bar {
  padding: 0 16px;
}

.filter-content {
  padding-bottom: 16px;
}

.log-table :deep(.n-data-table-th) {
  background-color: var(--c-slate-100);
  font-weight: 600;
  color: var(--app-text-muted);
}
.app-dark .log-table :deep(.n-data-table-th) {
  background-color: var(--c-slate-900);
}

.log-table :deep(.n-data-table-td) {
  padding-top: 12px;
  padding-bottom: 12px;
}

:deep(.path-cell) {
  font-family: monospace;
  font-size: 13px;
  color: var(--app-text);
}

:deep(.backend-tag-cell) {
  display: inline-flex;
  max-width: 140px;
}

:deep(.backend-tag-cell .n-tag__content) {
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.tabular-nums) {
  font-variant-numeric: tabular-nums;
}

.pagination-bar {
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-top: 1px solid var(--app-border);
}

.time-window-card :deep(.value) {
  white-space: nowrap;
  word-break: normal;
  overflow: hidden;
  text-overflow: ellipsis;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}

.trace-card {
  border: 1px solid var(--app-border);
}

.trace-meta {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.meta-chip {
  border: 1px solid var(--app-border);
  border-radius: 10px;
  padding: 10px 12px;
  background: color-mix(in oklab, var(--app-bg) 88%, var(--app-primary) 12%);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.meta-chip .label {
  font-size: 12px;
  color: var(--app-text-muted);
}

.meta-chip .value {
  font-size: 16px;
  font-weight: 700;
  color: var(--app-text);
}

.trace-table-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--app-text);
  margin: 2px 0 8px;
}

.trace-table :deep(.n-data-table-th) {
  background-color: color-mix(in oklab, var(--c-slate-100) 82%, var(--app-primary) 18%);
}

.app-dark .trace-table :deep(.n-data-table-th) {
  background-color: color-mix(in oklab, var(--c-slate-900) 85%, var(--app-primary) 15%);
}

.metric-hot {
  color: var(--app-error);
  font-weight: 700;
}

.metric-warn {
  color: var(--app-warning);
  font-weight: 600;
}

.trace-note {
  margin-top: 14px;
  border-top: 1px dashed var(--app-border);
  padding-top: 12px;
}

.trace-note-title {
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 8px;
}

.trace-note-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
}

.trace-note-item {
  font-size: 12px;
  line-height: 1.6;
  color: var(--app-text-muted);
}

@media (max-width: 900px) {
  .trace-meta {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .trace-note-grid {
    grid-template-columns: 1fr;
  }
}
</style>
