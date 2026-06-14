<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue'
import {
  NButton,
  NCard,
  NDataTable,
  NIcon,
  NInput,
  NInputNumber,
  NSelect,
  NTag,
  type DataTableColumns,
  type DataTableSortState,
  type PaginationProps,
} from 'naive-ui'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { useToast } from '@/composables/useToast'
import { useUiStore } from '@/stores/ui'
import EmptyState from '@/components/EmptyState.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import {
  getBreakdownReport,
  getPlayActivity,
  getUserUsageRanking,
  type UserUsageBucket,
  type UserUsageRankingRow,
  type UserUsageRankingSortBy,
} from '@/api/client'

import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
} from 'echarts/components'

use([CanvasRenderer, BarChart, GridComponent, TooltipComponent])

const ui = useUiStore()
const { showToast } = useToast()

type SortOrder = 'asc' | 'desc'

const RANGE_OPTIONS = [
  { label: '近 7 天', value: 7 },
  { label: '近 30 天', value: 30 },
  { label: '近 90 天', value: 90 },
  { label: '近 180 天', value: 180 },
  { label: '全部历史', value: 0 },
]

const ranking = ref<UserUsageRankingRow[]>([])
const statsPlayActivity = ref<any[]>([])
const clientBreakdown = ref<any[]>([])
const playerBreakdown = ref<any[]>([])
const loading = ref(false)
const loaded = ref(false)
const error = ref<string | null>(null)
const total = ref(0)
let statsRequestSeq = 0
const summary = reactive({
  active_users: 0,
  total_plays: 0,
  total_duration: 0,
  client_count: 0,
  player_count: 0,
  ip_count: 0,
})

const filters = reactive({
  days: 30,
  user: '',
  client_name: '',
  device_name: '',
  client_ip: '',
  min_client_count: null as number | null,
  min_player_count: null as number | null,
  min_ip_count: null as number | null,
})

const pager = reactive({
  page: 1,
  pageSize: 20,
  sortBy: 'total_plays' as UserUsageRankingSortBy,
  sortOrder: 'desc' as SortOrder,
})

const rangeLabel = computed(() => {
  if (filters.days === 0) return '全部历史'
  return `近 ${filters.days} 天`
})

const hasFilters = computed(() =>
  Boolean(
    filters.user.trim() ||
    filters.client_name ||
    filters.device_name ||
    filters.client_ip.trim() ||
    filters.min_client_count != null ||
    filters.min_player_count != null ||
    filters.min_ip_count != null,
  ),
)

const clientOptions = computed(() =>
  breakdownOptions(clientBreakdown.value, filters.device_name),
)

const playerOptions = computed(() =>
  breakdownOptions(playerBreakdown.value, filters.client_name),
)

function breakdownOptions(items: any[], selected: string) {
  const seen = new Map<string, number>()
  for (const item of items) {
    const label = String(item.label || '').trim()
    if (!label) continue
    seen.set(label, Number(item.count || 0))
  }
  if (selected && !seen.has(selected)) seen.set(selected, 0)
  return [...seen.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, 50)
    .map(([label, count]) => ({ label: count > 0 ? `${label} (${count})` : label, value: label }))
}

function requestParams() {
  return {
    days: filters.days,
    page: pager.page,
    page_size: pager.pageSize,
    sort_by: pager.sortBy,
    sort_order: pager.sortOrder,
    user: filters.user.trim() || undefined,
    client_name: filters.client_name || undefined,
    device_name: filters.device_name || undefined,
    client_ip: filters.client_ip.trim() || undefined,
    min_client_count: filters.min_client_count ?? undefined,
    min_player_count: filters.min_player_count ?? undefined,
    min_ip_count: filters.min_ip_count ?? undefined,
  }
}

async function loadStats() {
  const requestSeq = ++statsRequestSeq
  loading.value = true
  error.value = null
  try {
    const daysForTrend = filters.days === 0 ? 30 : filters.days
    const [rankingResp, trendResp, clientResp, playerResp] = await Promise.allSettled([
      getUserUsageRanking(requestParams()),
      getPlayActivity(daysForTrend),
      getBreakdownReport('DeviceName', daysForTrend),
      getBreakdownReport('ClientName', daysForTrend),
    ])

    if (rankingResp.status === 'rejected') throw rankingResp.reason
    if (requestSeq !== statsRequestSeq) return
    ranking.value = Array.isArray(rankingResp.value.items) ? rankingResp.value.items : []
    total.value = Number(rankingResp.value.total || 0)
    Object.assign(summary, rankingResp.value.summary || {})

    statsPlayActivity.value = trendResp.status === 'fulfilled' && Array.isArray(trendResp.value) ? trendResp.value : []
    clientBreakdown.value = clientResp.status === 'fulfilled' && Array.isArray(clientResp.value) ? clientResp.value : []
    playerBreakdown.value = playerResp.status === 'fulfilled' && Array.isArray(playerResp.value) ? playerResp.value : []
    loaded.value = true
  } catch (e) {
    if (requestSeq !== statsRequestSeq) return
    const message = e instanceof Error ? e.message : '加载统计数据失败'
    error.value = message
    showToast(message, 'error')
  } finally {
    if (requestSeq === statsRequestSeq) {
      loading.value = false
    }
  }
}

function applyFilters() {
  pager.page = 1
  void loadStats()
}

function resetFilters() {
  filters.user = ''
  filters.client_name = ''
  filters.device_name = ''
  filters.client_ip = ''
  filters.min_client_count = null
  filters.min_player_count = null
  filters.min_ip_count = null
  pager.page = 1
  void loadStats()
}

function onSorterChange(sorter: DataTableSortState | DataTableSortState[] | null) {
  const next = Array.isArray(sorter) ? sorter[0] : sorter
  if (!next || !next.columnKey || !next.order) {
    pager.sortBy = 'total_plays'
    pager.sortOrder = 'desc'
  } else {
    pager.sortBy = String(next.columnKey) as UserUsageRankingSortBy
    pager.sortOrder = next.order === 'ascend' ? 'asc' : 'desc'
  }
  pager.page = 1
  void loadStats()
}

const pagination = computed<PaginationProps>(() => ({
  page: pager.page,
  pageSize: pager.pageSize,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  prefix: ({ itemCount }) => `共 ${itemCount} 位用户`,
  onUpdatePage: (page: number) => {
    pager.page = page
    void loadStats()
  },
  onUpdatePageSize: (pageSize: number) => {
    pager.pageSize = pageSize
    pager.page = 1
    void loadStats()
  },
}))

const columns = computed<DataTableColumns<UserUsageRankingRow>>(() => [
  {
    type: 'expand',
    width: 42,
    expandable: () => true,
    renderExpand,
  },
  {
    title: '#',
    key: 'rank',
    width: 54,
    render: (_row, index) => h('span', { class: rankClass(index) }, String((pager.page - 1) * pager.pageSize + index + 1)),
  },
  {
    title: '用户',
    key: 'user_name',
    sorter: true,
    sortOrder: pager.sortBy === 'user_name' ? toNaiveSort(pager.sortOrder) : false,
    width: 150,
    ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'user-cell__name' }, row.user_name || 'Unknown'),
  },
  numericColumn('播放次数', 'total_plays', 112),
  {
    title: '播放时长',
    key: 'total_duration',
    sorter: true,
    sortOrder: pager.sortBy === 'total_duration' ? toNaiveSort(pager.sortOrder) : false,
    width: 116,
    align: 'right',
    render: (row) => durationStr(row.total_duration),
  },
  numericColumn('客户端数', 'client_count', 112),
  numericColumn('播放器数', 'player_count', 112),
  numericColumn('IP 数', 'ip_count', 96),
  {
    title: '最后活跃',
    key: 'last_seen',
    sorter: true,
    sortOrder: pager.sortBy === 'last_seen' ? toNaiveSort(pager.sortOrder) : false,
    width: 168,
    render: (row) => row.last_seen ? new Date(row.last_seen).toLocaleString() : '-',
  },
  {
    title: '最近播放',
    key: 'last_item_name',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: (row) => row.last_item_name || '-',
  },
])

function numericColumn(title: string, key: UserUsageRankingSortBy, width: number) {
  return {
    title,
    key,
    sorter: true,
    sortOrder: pager.sortBy === key ? toNaiveSort(pager.sortOrder) : false,
    width,
    align: 'right',
    render: (row: UserUsageRankingRow) => Number(row[key] || 0).toLocaleString(),
  } as DataTableColumns<UserUsageRankingRow>[number]
}

function toNaiveSort(order: SortOrder) {
  return order === 'asc' ? 'ascend' : 'descend'
}

function rankClass(index: number) {
  const rank = (pager.page - 1) * pager.pageSize + index
  return ['rank-badge', rank < 3 ? 'rank-badge--top' : '']
}

function durationStr(seconds: number) {
  const total = Math.max(0, Math.round(seconds || 0))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (hours >= 24) return `${Math.floor(hours / 24)}d ${hours % 24}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

function compactNumber(value: number) {
  return Number(value || 0).toLocaleString()
}

function renderExpand(row: UserUsageRankingRow) {
  return h('div', { class: 'usage-detail' }, [
    detailGroup('客户端', row.top_clients),
    detailGroup('播放器', row.top_players),
    detailGroup('IP', row.top_ips),
    detailGroup('User-Agent', row.top_user_agents || []),
    h('div', { class: 'usage-detail__last' }, [
      h('span', { class: 'usage-detail__label' }, '最近环境'),
      h('span', {}, [row.last_client_name, row.last_device_name].filter(Boolean).join(' / ') || '-'),
    ]),
  ])
}

function detailGroup(title: string, items: UserUsageBucket[]) {
  const children = items.length > 0
    ? items.map((item) => h(NTag, { size: 'small', bordered: false, round: true }, { default: () => `${item.label} · ${item.count}` }))
    : [h('span', { class: 'usage-detail__empty' }, '无记录')]
  return h('div', { class: 'usage-detail__group' }, [
    h('div', { class: 'usage-detail__label' }, title),
    h('div', { class: 'usage-detail__tags' }, children),
  ])
}

const trendOption = computed(() => {
  const dark = ui.isDark
  const textColor = dark ? '#9ca3af' : '#64748b'
  const borderColor = dark ? 'rgba(148,163,184,0.12)' : 'rgba(148,163,184,0.22)'
  const data = statsPlayActivity.value

  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: dark ? 'rgba(15,23,42,0.94)' : 'rgba(255,255,255,0.96)',
      borderColor,
      textStyle: { color: dark ? '#e5e7eb' : '#111827', fontSize: 12 },
      formatter: (params: any) => {
        const p = params[0]
        return `${p.name}<br/>播放 <b>${p.value}</b> 次`
      },
    },
    grid: { left: 40, right: 10, top: 12, bottom: 28 },
    xAxis: {
      type: 'category',
      data: data.map((d: any) => d.date?.slice(5) || ''),
      axisLine: { lineStyle: { color: borderColor } },
      axisLabel: { color: textColor, fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: borderColor } },
      axisLabel: { color: textColor, fontSize: 11 },
    },
    series: [
      {
        type: 'bar',
        data: data.map((d: any) => d.count || 0),
        itemStyle: { color: '#0ea5e9', borderRadius: [3, 3, 0, 0] },
        barMaxWidth: 18,
      },
    ],
  }
})

const topClientText = computed(() => topBreakdownText(clientBreakdown.value))
const topPlayerText = computed(() => topBreakdownText(playerBreakdown.value))

function topBreakdownText(items: any[]) {
  const top = items.find((item) => item.label && item.label !== 'Unknown')
  return top ? `${top.label} · ${top.count}` : '-'
}

watch(() => filters.days, () => applyFilters())

onMounted(() => {
  void loadStats()
})
</script>

<template>
  <div class="stats-tab">
    <error-banner v-if="error" :message="error" />

    <n-card class="glass-card stats-control" :bordered="false">
      <div class="control-grid">
        <n-select
          v-model:value="filters.days"
          :options="RANGE_OPTIONS"
          size="small"
          class="control-grid__range"
        />
        <n-input
          v-model:value="filters.user"
          size="small"
          clearable
          placeholder="筛选用户"
          @keyup.enter="applyFilters"
        >
          <template #prefix><n-icon :component="SearchOutline" /></template>
        </n-input>
        <n-select
          v-model:value="filters.device_name"
          :options="clientOptions"
          size="small"
          clearable
          filterable
          tag
          placeholder="客户端"
        />
        <n-select
          v-model:value="filters.client_name"
          :options="playerOptions"
          size="small"
          clearable
          filterable
          tag
          placeholder="播放器"
        />
        <n-input
          v-model:value="filters.client_ip"
          size="small"
          clearable
          placeholder="精确 IP"
          @keyup.enter="applyFilters"
        />
        <n-input-number
          v-model:value="filters.min_client_count"
          size="small"
          clearable
          :min="0"
          placeholder="最少客户端"
        />
        <n-input-number
          v-model:value="filters.min_player_count"
          size="small"
          clearable
          :min="0"
          placeholder="最少播放器"
        />
        <n-input-number
          v-model:value="filters.min_ip_count"
          size="small"
          clearable
          :min="0"
          placeholder="最少 IP"
        />
        <div class="control-actions">
          <n-button size="small" type="primary" :loading="loading" @click="applyFilters">查询</n-button>
          <n-button size="small" quaternary :disabled="!hasFilters || loading" @click="resetFilters">重置</n-button>
          <n-button size="small" quaternary circle :loading="loading" @click="loadStats">
            <template #icon><n-icon :component="RefreshOutline" /></template>
          </n-button>
        </div>
      </div>
    </n-card>

    <div class="summary-grid">
      <div class="metric-tile metric-tile--strong">
        <span class="metric-tile__label">活跃用户</span>
        <strong>{{ compactNumber(summary.active_users) }}</strong>
        <span class="metric-tile__hint">{{ rangeLabel }}</span>
      </div>
      <div class="metric-tile">
        <span class="metric-tile__label">播放次数</span>
        <strong>{{ compactNumber(summary.total_plays) }}</strong>
        <span class="metric-tile__hint">累计播放</span>
      </div>
      <div class="metric-tile">
        <span class="metric-tile__label">播放时长</span>
        <strong>{{ durationStr(summary.total_duration) }}</strong>
        <span class="metric-tile__hint">筛选后合计</span>
      </div>
      <div class="metric-tile">
        <span class="metric-tile__label">客户端</span>
        <strong>{{ compactNumber(summary.client_count) }}</strong>
        <span class="metric-tile__hint">{{ topClientText }}</span>
      </div>
      <div class="metric-tile">
        <span class="metric-tile__label">播放器</span>
        <strong>{{ compactNumber(summary.player_count) }}</strong>
        <span class="metric-tile__hint">{{ topPlayerText }}</span>
      </div>
      <div class="metric-tile">
        <span class="metric-tile__label">IP</span>
        <strong>{{ compactNumber(summary.ip_count) }}</strong>
        <span class="metric-tile__hint">去重合计</span>
      </div>
    </div>

    <div class="stats-main">
      <n-card class="glass-card ranking-card" :bordered="false">
        <template #header>
          <div class="section-header">
            <span class="section-title">用户用量排行</span>
            <span class="section-sub">支持服务端筛选、排序与分页</span>
          </div>
        </template>
        <n-data-table
          remote
          :columns="columns"
          :data="ranking"
          :loading="loading"
          :pagination="pagination"
          :bordered="false"
          :single-line="false"
          size="small"
          :row-key="(row) => row.user_id"
          :scroll-x="1120"
          @update:sorter="onSorterChange"
          style="--n-td-color: transparent; --n-th-color: transparent; --n-td-color-hover: rgba(14,165,233,0.06)"
        />
        <empty-state v-if="loaded && !loading && ranking.length === 0" description="暂无符合条件的用户统计" />
      </n-card>

      <n-card class="glass-card trend-card" :bordered="false">
        <template #header>
          <div class="section-header">
            <span class="section-title">播放概览</span>
            <span class="section-sub">{{ filters.days === 0 ? '趋势取近 30 天' : rangeLabel }}</span>
          </div>
        </template>
        <v-chart
          v-if="statsPlayActivity.length > 0"
          :option="trendOption"
          autoresize
          class="trend-chart"
        />
        <empty-state v-else description="暂无播放趋势数据" />
        <div class="trend-notes">
          <div>
            <span>Top 客户端</span>
            <strong>{{ topClientText }}</strong>
          </div>
          <div>
            <span>Top 播放器</span>
            <strong>{{ topPlayerText }}</strong>
          </div>
        </div>
      </n-card>
    </div>
  </div>
</template>

<style scoped>
.stats-tab {
  display: flex;
  flex-direction: column;
  gap: var(--app-section-gap);
}

.stats-control {
  overflow: hidden;
}

.control-grid {
  display: grid;
  grid-template-columns: 120px minmax(160px, 1fr) minmax(150px, 1fr) minmax(150px, 1fr) minmax(130px, 0.8fr) repeat(3, 120px) auto;
  gap: 10px;
  align-items: center;
}

.control-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  white-space: nowrap;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: var(--app-section-gap);
}

.metric-tile {
  min-height: 98px;
  padding: 16px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  background:
    linear-gradient(135deg, rgba(14, 165, 233, 0.08), transparent 52%),
    var(--app-surface-1);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-width: 0;
}

.metric-tile--strong {
  background:
    linear-gradient(135deg, rgba(16, 185, 129, 0.16), transparent 58%),
    var(--app-surface-1);
}

.metric-tile__label {
  font-size: 12px;
  color: var(--app-text-muted);
}

.metric-tile strong {
  margin-top: 8px;
  font-size: 25px;
  line-height: 1.1;
  color: var(--app-text);
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

.metric-tile__hint {
  margin-top: 8px;
  min-height: 18px;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stats-main {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: var(--app-section-gap);
  align-items: start;
}

.ranking-card,
.trend-card {
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
}

.section-sub {
  font-size: 12px;
  color: var(--app-text-muted);
  white-space: nowrap;
}

.rank-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 700;
  color: var(--app-text-muted);
  background: rgba(148, 163, 184, 0.1);
  font-variant-numeric: tabular-nums;
}

.rank-badge--top {
  color: #0369a1;
  background: rgba(14, 165, 233, 0.15);
}

.user-cell__name {
  display: block;
  font-weight: 600;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.usage-detail {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) minmax(220px, 1.2fr) minmax(160px, 0.8fr);
  gap: 14px;
  padding: 12px 6px;
}

.usage-detail__group,
.usage-detail__last {
  min-width: 0;
}

.usage-detail__label {
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.usage-detail__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.usage-detail__empty {
  font-size: 12px;
  color: var(--app-text-muted);
}

.usage-detail__last span:last-child {
  display: block;
  font-size: 13px;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.trend-chart {
  height: 220px;
}

.trend-notes {
  display: grid;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid var(--app-border);
}

.trend-notes div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.trend-notes span {
  font-size: 12px;
  color: var(--app-text-muted);
}

.trend-notes strong {
  min-width: 0;
  font-size: 13px;
  color: var(--app-text);
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.n-data-table-th) {
  font-weight: 600 !important;
  font-size: 12px !important;
  color: var(--app-text-muted) !important;
}

:deep(.n-data-table-td) {
  font-size: 13px !important;
}

:deep(.n-data-table-expand-trigger) {
  color: var(--app-text-muted);
}

@media (max-width: 1380px) {
  .control-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .control-actions {
    justify-content: flex-start;
  }

  .summary-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .stats-main {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .control-grid,
  .summary-grid {
    grid-template-columns: 1fr;
  }

  .usage-detail {
    grid-template-columns: 1fr;
  }

  .metric-tile strong {
    font-size: 22px;
  }
}
</style>
