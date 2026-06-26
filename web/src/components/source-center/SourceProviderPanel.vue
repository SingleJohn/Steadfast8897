<script setup lang="ts">
import { computed, h, shallowRef } from 'vue'
import { NButton, NDataTable, NInput, NPopconfirm, NSelect, NSpace, NTag, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type {
  SourceProvider,
  SourceProviderDiagnoseResult,
  SourceProviderHealthSummary,
  SourceProviderHomeProfile,
  SourceProviderHomeProfileSlice,
  SourceProviderListOptions,
} from '@/api/source'
import { copyText } from '@/utils/externalPlayers'

const props = defineProps<{
  providers: SourceProvider[]
  activeProviderId: number | null
  keyword: string
  searchResult: any
  categories: Array<{ id: string; name: string }>
  diagnosis: SourceProviderDiagnoseResult | null
  homeProfile: SourceProviderHomeProfile | null
  action: string
  selectedIds: number[]
  healthFilters: SourceProviderListOptions
}>()
const message = useMessage()

const emit = defineEmits<{
  'update:activeProviderId': [value: number | null]
  'update:keyword': [value: string]
  'update:selectedIds': [value: number[]]
  toggle: [id: number, enabled: boolean]
  batchEnable: []
  batchDisable: []
  batchHealth: []
  batchDelete: [ids?: number[]]
  batchEnableIds: [ids: number[]]
  batchDisableIds: [ids: number[]]
  batchHealthIds: [ids: number[]]
  updateHealthFilters: [filters: SourceProviderListOptions]
  health: [id: number]
  diagnose: [id: number]
  homeProfile: [id: number]
  categories: [id: number]
  search: []
}>()

const providerOptions = computed(() => props.providers.map((p) => ({ label: `${p.Name} (${p.SourceKey})`, value: p.ID })))
const selectedProviders = computed(() => props.providers.filter((provider) => props.selectedIds.includes(provider.ID)))
const selectedEnabledCount = computed(() => selectedProviders.value.filter((provider) => provider.Enabled).length)
const selectedRuntimeCount = computed(() => selectedProviders.value.filter((provider) => provider.RuntimeKind !== 'native_cms').length)
const healthFilter = shallowRef<string | null>(null)
const runtimeHealthFilter = shallowRef<string | null>(props.healthFilters.runtime_status || null)
const homeHealthFilter = shallowRef<string | null>(props.healthFilters.home_status || null)
const categoryHealthFilter = shallowRef<string | null>(props.healthFilters.category_status || null)
const enabledFilter = shallowRef<string | null>(null)
const runtimeFilter = shallowRef<string | null>(null)
const keywordFilter = shallowRef('')
const filteredProviders = computed(() => {
  const keyword = keywordFilter.value.trim().toLowerCase()
  return props.providers.filter((provider) => {
    if (healthFilter.value && normalizeHealth(provider.HealthStatus) !== healthFilter.value) return false
    if (enabledFilter.value === 'enabled' && !provider.Enabled) return false
    if (enabledFilter.value === 'disabled' && provider.Enabled) return false
    if (runtimeFilter.value && provider.RuntimeKind !== runtimeFilter.value) return false
    if (keyword) {
      const haystack = `${provider.Name} ${provider.SourceKey} ${provider.API || ''}`.toLowerCase()
      if (!haystack.includes(keyword)) return false
    }
    return true
  })
})
const filteredProviderIds = computed(() => filteredProviders.value.map((provider) => provider.ID))
const filteredSelectedCount = computed(() => filteredProviderIds.value.filter((id) => props.selectedIds.includes(id)).length)
const filteredChangeCounts = computed(() => {
  const disabled = filteredProviders.value.filter((provider) => !provider.Enabled).length
  const enabled = filteredProviders.value.length - disabled
  return { enabled, disabled }
})
const homeUsableProviders = computed(() => props.providers.filter((provider) => isProviderHomeUsable(provider)))
const failedHealthProviders = computed(() => props.providers.filter((provider) => hasProviderBlockingHealthFailure(provider)))
const failedHealthEnabledCount = computed(() => failedHealthProviders.value.filter((provider) => provider.Enabled).length)
const healthFilterOptions = [
  { label: '全部探活状态', value: '' },
  { label: '探活正常', value: 'ok' },
  { label: '部分可用', value: 'partial' },
  { label: '探活失败', value: 'error' },
  { label: '未探活', value: 'unknown' },
]
const methodStatusOptions = [
  { label: '全部', value: '' },
  { label: 'ok', value: 'ok' },
  { label: 'partial', value: 'partial' },
  { label: 'error', value: 'error' },
  { label: 'unhealthy', value: 'unhealthy' },
  { label: 'unknown', value: 'unknown' },
  { label: 'skipped', value: 'skipped' },
]
const enabledFilterOptions = [
  { label: '全部启用状态', value: '' },
  { label: '已启用', value: 'enabled' },
  { label: '已停用', value: 'disabled' },
]
const runtimeFilterOptions = [
  { label: '全部运行态', value: '' },
  { label: 'JSON CMS', value: 'native_cms' },
  { label: 'DRPY JS', value: 'js_node_drpy' },
  { label: 'CSP JAR', value: 'csp_dex' },
]
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}

const columns: DataTableColumns<SourceProvider> = [
  { type: 'selection', width: 42 },
  { title: '名称', key: 'Name', minWidth: 150 },
  { title: 'Key', key: 'SourceKey', width: 120 },
  {
    title: '运行态',
    key: 'RuntimeKind',
    width: 150,
    render(row) {
      return runtimeLabel(row.RuntimeKind)
    },
  },
  {
    title: '状态',
    key: 'HealthStatus',
    width: 190,
    render(row) {
      return h(NSpace, { size: 4, vertical: true }, {
        default: () => [
          hTag(row.HealthStatus || 'unknown', healthTagType(row.HealthStatus)),
          hHealthTags(row.Health),
        ],
      })
    },
  },
  {
    title: '最近错误',
    key: 'LastError',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render(row) {
      if (!row.LastError) return '-'
      return h('div', { class: 'error-cell' }, [
        h('span', row.LastError),
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          onClick: () => copyProviderError(row),
        }, { default: () => '复制' }),
      ])
    },
  },
  {
    title: '启用',
    key: 'Enabled',
    width: 110,
    render(row) {
      return h(NPopconfirm, {
        positiveText: row.Enabled ? '停用' : '启用',
        negativeText: '取消',
        onPositiveClick: () => emit('toggle', row.ID, !row.Enabled),
      }, {
        trigger: () => hButton(row.Enabled ? '停用' : '启用'),
        default: () => `${row.Enabled ? '停用' : '启用'} Provider “${row.Name}”？在线库命中范围会随之变化。`,
      })
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 230,
    render(row) {
      return hActions(row)
    },
  },
]

function hTag(label: string, type?: 'success' | 'error' | 'default' | 'warning' | 'info') {
  return h(NTag, { size: 'small', type: type === 'default' ? undefined : type }, { default: () => label })
}

function hHealthTags(health?: SourceProviderHealthSummary) {
  const tags = [
    ['R', health?.runtime_status],
    ['H', health?.home_status],
    ['C', health?.category_status],
    ['S', health?.search_status],
    ['P', health?.play_ready_status],
  ].filter((item): item is [string, string] => !!item[1])
  if (tags.length === 0) {
    return h('span', { class: 'health-empty' }, '未分项')
  }
  return h(NSpace, { size: 3 }, {
    default: () => tags.map(([label, status]) => hTag(`${label}:${status}`, healthTagType(status))),
  })
}

function hButton(label: string) {
  return h(NButton, { size: 'small', quaternary: true }, { default: () => label })
}

function hActions(row: SourceProvider) {
  return h(NSpace, { size: 4 }, {
    default: () => [
      h(NButton, { size: 'small', loading: props.action === `health:${row.ID}`, onClick: () => emit('health', row.ID) }, { default: () => '探活' }),
      h(NButton, { size: 'small', quaternary: true, loading: props.action === `diagnose:${row.ID}`, onClick: () => emit('diagnose', row.ID) }, { default: () => '诊断' }),
      h(NButton, { size: 'small', quaternary: true, loading: props.action === `home-profile:${row.ID}`, onClick: () => emit('homeProfile', row.ID) }, { default: () => '首页' }),
      h(NButton, { size: 'small', quaternary: true, loading: props.action === `categories:${row.ID}`, onClick: () => emit('categories', row.ID) }, { default: () => '分类' }),
      h(NPopconfirm, {
        positiveText: '删除',
        negativeText: '取消',
        onPositiveClick: () => emit('batchDelete', [row.ID]),
      }, {
        trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true, loading: props.action === 'batch-delete' }, { default: () => '删除' }),
        default: () => `删除 Provider “${row.Name}”？在线缓存条目会级联删除，运行时审计保留脱敏记录。`,
      }),
    ],
  })
}

async function copyProviderError(row: SourceProvider) {
  const text = [
    `Provider: ${row.Name}`,
    `SourceKey: ${row.SourceKey}`,
    `HealthStatus: ${row.HealthStatus || 'unknown'}`,
    `Error: ${row.LastError || '-'}`,
  ].join('\n')
  const ok = await copyText(text)
  if (ok) message.success('Provider 错误已复制')
  else message.error('复制失败，请手动选中')
}

function runtimeLabel(value: string) {
  if (value === 'native_cms') return 'JSON CMS'
  if (value === 'js_node_drpy') return 'DRPY JS'
  if (value === 'csp_dex') return 'CSP JAR'
  return value
}

function normalizeHealth(value: string) {
  return value || 'unknown'
}

function healthTagType(status?: string) {
  if (status === 'ok') return 'success'
  if (status === 'error' || status === 'unhealthy') return 'error'
  if (status === 'partial') return 'warning'
  if (status === 'skipped' || status === 'unknown') return 'default'
  return 'info'
}

function isProviderHomeUsable(provider: SourceProvider) {
  return provider.Health?.home_status === 'ok' || provider.Health?.home_status === 'partial'
}

function hasProviderBlockingHealthFailure(provider: SourceProvider) {
  const failed = new Set(['error', 'unhealthy'])
  return failed.has(provider.Health?.runtime_status || '')
    || failed.has(provider.Health?.home_status || '')
    || failed.has(provider.Health?.category_status || '')
}

function diagnoseStatusType(status: string) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'error'
  if (status === 'unsupported') return 'warning'
  return undefined
}

function diagnoseMethodLabel(method: string) {
  if (method === 'home') return 'homeContent'
  if (method === 'homeVideo') return 'homeVideoContent'
  if (method === 'category') return '分类'
  if (method === 'search') return '搜索'
  if (method === 'detail') return '详情'
  return method
}

function homeProfileSourceLabel(value: string) {
  if (value === 'homeVideoContent') return 'homeVideoContent'
  if (value === 'homeContent') return 'homeContent'
  return value || '-'
}

function homeProfileSliceLabel(slice: SourceProviderHomeProfileSlice) {
  if (slice.method === 'homeVideo') return 'homeVideoContent'
  if (slice.method === 'home') return 'homeContent'
  return slice.method
}

function homeProfileMessage(slice: SourceProviderHomeProfileSlice) {
  const parts = []
  if (slice.error_type) parts.push(slice.error_type)
  if (slice.error_message) parts.push(slice.error_message)
  return parts.join(': ')
}

function selectFilteredProviders() {
  const ids = new Set([...props.selectedIds, ...filteredProviderIds.value])
  emit('update:selectedIds', Array.from(ids))
}

function clearSelectedProviders() {
  emit('update:selectedIds', [])
}

function clearFilteredSelection() {
  const filtered = new Set(filteredProviderIds.value)
  emit('update:selectedIds', props.selectedIds.filter((id) => !filtered.has(id)))
}

function updateHealthFilter(value: string | null) {
  healthFilter.value = value || null
}

function updateRuntimeHealthFilter(value: string | null) {
  runtimeHealthFilter.value = value || null
  emitHealthFilters()
}

function updateHomeHealthFilter(value: string | null) {
  homeHealthFilter.value = value || null
  emitHealthFilters()
}

function updateCategoryHealthFilter(value: string | null) {
  categoryHealthFilter.value = value || null
  emitHealthFilters()
}

function emitHealthFilters() {
  emit('updateHealthFilters', {
    runtime_status: runtimeHealthFilter.value || undefined,
    home_status: homeHealthFilter.value || undefined,
    category_status: categoryHealthFilter.value || undefined,
  })
}

function updateEnabledFilter(value: string | null) {
  enabledFilter.value = value || null
}

function updateRuntimeFilter(value: string | null) {
  runtimeFilter.value = value || null
}

function emitFilteredBatch(action: 'enable' | 'disable' | 'health' | 'delete') {
  const ids = filteredProviderIds.value
  if (action === 'enable') emit('batchEnableIds', ids)
  else if (action === 'disable') emit('batchDisableIds', ids)
  else if (action === 'health') emit('batchHealthIds', ids)
  else emit('batchDelete', ids)
}

function emitHomeUsableEnable() {
  emit('batchEnableIds', homeUsableProviders.value.map((provider) => provider.ID))
}

function emitFailedHealthDisable() {
  emit('batchDisableIds', failedHealthProviders.value.map((provider) => provider.ID))
}
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">Provider 管理</h2>
        <p class="panel-subtitle">启停、健康检查、分类查看与搜索测试。</p>
      </div>
    </div>

    <div v-if="providers.length > 0" class="bulk-bar" aria-live="polite">
      <div>
        <strong>已选择 {{ selectedIds.length }} 个 Provider</strong>
        <p class="panel-subtitle">
          其中 {{ selectedEnabledCount }} 个启用，{{ selectedRuntimeCount }} 个依赖 JS/CSP 运行时；当前筛选命中
          {{ filteredProviders.length }} 个，已选 {{ filteredSelectedCount }} 个。
        </p>
      </div>
      <div class="bulk-actions">
        <NButton size="small" :disabled="filteredProviders.length === 0" @click="selectFilteredProviders">选择筛选结果</NButton>
        <NButton size="small" quaternary :disabled="filteredSelectedCount === 0" @click="clearFilteredSelection">取消筛选选择</NButton>
        <NButton size="small" quaternary :disabled="selectedIds.length === 0" @click="clearSelectedProviders">清空选择</NButton>
        <NPopconfirm
          positive-text="批量启用"
          negative-text="取消"
          :disabled="selectedIds.length === 0"
          @positive-click="emit('batchEnable')"
        >
          <template #trigger>
            <NButton size="small" :disabled="selectedIds.length === 0" :loading="action === 'batch-enable'">批量启用</NButton>
          </template>
          将启用 {{ selectedIds.length }} 个 Provider；相关在线库可能开始收录这些站点的数据。
        </NPopconfirm>
        <NPopconfirm
          positive-text="批量停用"
          negative-text="取消"
          :disabled="selectedIds.length === 0"
          @positive-click="emit('batchDisable')"
        >
          <template #trigger>
            <NButton size="small" type="error" ghost :disabled="selectedIds.length === 0" :loading="action === 'batch-disable'">批量停用</NButton>
          </template>
          将停用 {{ selectedIds.length }} 个 Provider；依赖这些 Provider 的在线库命中会减少。
        </NPopconfirm>
        <NPopconfirm
          positive-text="开始探活"
          negative-text="取消"
          :disabled="selectedIds.length === 0"
          @positive-click="emit('batchHealth')"
        >
          <template #trigger>
            <NButton size="small" :disabled="selectedIds.length === 0" :loading="action === 'batch-health'">批量探活</NButton>
          </template>
          将并发探活 {{ selectedIds.length }} 个 Provider，单站失败不会中断整批。
        </NPopconfirm>
        <NPopconfirm
          positive-text="删除"
          negative-text="取消"
          :disabled="selectedIds.length === 0"
          @positive-click="emit('batchDelete')"
        >
          <template #trigger>
            <NButton size="small" type="error" :disabled="selectedIds.length === 0" :loading="action === 'batch-delete'">删除所选</NButton>
          </template>
          将删除 {{ selectedIds.length }} 个 Provider，并清理在线库 Provider 引用；在线缓存条目会随 Provider 级联删除，运行时审计保留脱敏记录。
        </NPopconfirm>
      </div>
    </div>

    <div v-if="providers.length > 0" class="fm3-actions">
      <div>
        <strong>分项健康批量操作</strong>
        <p class="panel-subtitle">
          首页可用按 home_status=ok/partial 判断；明确失败按 runtime/home/category 为 error 或 unhealthy 判断。
        </p>
      </div>
      <div class="bulk-actions">
        <NPopconfirm
          positive-text="启用首页可用"
          negative-text="取消"
          :disabled="homeUsableProviders.length === 0"
          @positive-click="emitHomeUsableEnable"
        >
          <template #trigger>
            <NButton size="small" :disabled="homeUsableProviders.length === 0" :loading="action === 'batch-enable'">启用首页可用</NButton>
          </template>
          将启用 home_status 为 ok/partial 的 {{ homeUsableProviders.length }} 个 Provider；筛选来自已加载的分项健康摘要。
        </NPopconfirm>
        <NPopconfirm
          positive-text="停用明确失败"
          negative-text="取消"
          :disabled="failedHealthProviders.length === 0"
          @positive-click="emitFailedHealthDisable"
        >
          <template #trigger>
            <NButton size="small" type="error" ghost :disabled="failedHealthProviders.length === 0" :loading="action === 'batch-disable'">停用明确失败</NButton>
          </template>
          将停用 runtime/home/category 明确失败的 {{ failedHealthProviders.length }} 个 Provider，其中 {{ failedHealthEnabledCount }} 个当前已启用。
        </NPopconfirm>
      </div>
    </div>

    <div v-if="providers.length > 0" class="filter-bar">
      <label class="field">
        <span class="field-label">探活状态</span>
        <NSelect :value="healthFilter || ''" :options="healthFilterOptions" @update:value="updateHealthFilter" />
      </label>
      <label class="field">
        <span class="field-label">启用状态</span>
        <NSelect :value="enabledFilter || ''" :options="enabledFilterOptions" @update:value="updateEnabledFilter" />
      </label>
      <label class="field">
        <span class="field-label">运行类型</span>
        <NSelect :value="runtimeFilter || ''" :options="runtimeFilterOptions" @update:value="updateRuntimeFilter" />
      </label>
      <label class="field keyword-field">
        <span class="field-label">关键词</span>
        <NInput :value="keywordFilter" placeholder="名称 / SourceKey / API" clearable @update:value="keywordFilter = $event" />
      </label>
    </div>

    <div v-if="providers.length > 0" class="health-filter-bar">
      <label class="field">
        <span class="field-label">Runtime 健康</span>
        <NSelect :value="runtimeHealthFilter || ''" :options="methodStatusOptions" @update:value="updateRuntimeHealthFilter" />
      </label>
      <label class="field">
        <span class="field-label">首页健康</span>
        <NSelect :value="homeHealthFilter || ''" :options="methodStatusOptions" @update:value="updateHomeHealthFilter" />
      </label>
      <label class="field">
        <span class="field-label">分类健康</span>
        <NSelect :value="categoryHealthFilter || ''" :options="methodStatusOptions" @update:value="updateCategoryHealthFilter" />
      </label>
    </div>

    <div v-if="providers.length > 0" class="filtered-actions">
      <span class="panel-subtitle">
        筛选结果 {{ filteredProviders.length }} 个；启用筛选结果会实际启用 {{ filteredChangeCounts.disabled }} 个停用项，停用筛选结果会实际停用 {{ filteredChangeCounts.enabled }} 个启用项。
      </span>
      <div class="bulk-actions">
        <NPopconfirm
          positive-text="启用筛选结果"
          negative-text="取消"
          :disabled="filteredProviders.length === 0"
          @positive-click="emitFilteredBatch('enable')"
        >
          <template #trigger>
            <NButton size="small" :disabled="filteredProviders.length === 0" :loading="action === 'batch-enable'">启用筛选结果</NButton>
          </template>
          将启用当前筛选命中的 {{ filteredProviders.length }} 个 Provider，其中 {{ filteredChangeCounts.disabled }} 个会从停用变为启用。
        </NPopconfirm>
        <NPopconfirm
          positive-text="停用筛选结果"
          negative-text="取消"
          :disabled="filteredProviders.length === 0"
          @positive-click="emitFilteredBatch('disable')"
        >
          <template #trigger>
            <NButton size="small" type="error" ghost :disabled="filteredProviders.length === 0" :loading="action === 'batch-disable'">停用筛选结果</NButton>
          </template>
          将停用当前筛选命中的 {{ filteredProviders.length }} 个 Provider，其中 {{ filteredChangeCounts.enabled }} 个会从启用变为停用。
        </NPopconfirm>
        <NPopconfirm
          positive-text="探活筛选结果"
          negative-text="取消"
          :disabled="filteredProviders.length === 0"
          @positive-click="emitFilteredBatch('health')"
        >
          <template #trigger>
            <NButton size="small" :disabled="filteredProviders.length === 0" :loading="action === 'batch-health'">探活筛选结果</NButton>
          </template>
          将并发探活当前筛选命中的 {{ filteredProviders.length }} 个 Provider。
        </NPopconfirm>
        <NPopconfirm
          positive-text="删除筛选结果"
          negative-text="取消"
          :disabled="filteredProviders.length === 0"
          @positive-click="emitFilteredBatch('delete')"
        >
          <template #trigger>
            <NButton size="small" type="error" :disabled="filteredProviders.length === 0" :loading="action === 'batch-delete'">删除筛选结果</NButton>
          </template>
          将删除当前筛选命中的 {{ filteredProviders.length }} 个 Provider，并清理在线库 Provider 引用；在线缓存条目会随 Provider 级联删除。
        </NPopconfirm>
      </div>
    </div>

    <NDataTable
      v-if="providers.length > 0"
      :columns="columns"
      :data="filteredProviders"
      :checked-row-keys="selectedIds"
      :pagination="tablePagination"
      :row-key="(row: SourceProvider) => row.ID"
      size="small"
      :bordered="false"
      @update:checked-row-keys="emit('update:selectedIds', $event as number[])"
    />
    <div v-else class="empty-state">暂无 Provider，先在配置页导入来源配置。</div>

    <div class="test-grid">
      <label class="field">
        <span class="field-label">测试 Provider</span>
        <NSelect
          :value="activeProviderId"
          :options="providerOptions"
          placeholder="选择 Provider"
          clearable
          @update:value="emit('update:activeProviderId', $event)"
        />
      </label>
      <label class="field">
        <span class="field-label">搜索关键词</span>
        <NInput :value="keyword" placeholder="搜索关键词" clearable @update:value="emit('update:keyword', $event)" />
      </label>
      <NButton type="primary" :loading="!!activeProviderId && action === `search:${activeProviderId}`" @click="emit('search')">搜索测试</NButton>
    </div>

    <div v-if="categories.length > 0" class="chips">
      <NTag v-for="cat in categories" :key="cat.id" size="small">{{ cat.name }}</NTag>
    </div>

    <div v-if="searchResult?.page" class="result-strip">
      <span>页码 {{ searchResult.page.page }}</span>
      <span>结果 {{ searchResult.page.items?.length || 0 }}</span>
      <span>入库 {{ searchResult.items?.length || 0 }}</span>
    </div>

    <section v-if="diagnosis" class="diagnosis-panel" aria-live="polite">
      <div class="diagnosis-head">
        <div>
          <h3 class="diagnosis-title">FongMi 兼容诊断</h3>
          <p class="panel-subtitle">
            {{ diagnosis.provider_name }} · {{ runtimeLabel(diagnosis.runtime_kind) }} · {{ diagnosis.duration_ms }} ms
          </p>
        </div>
        <NTag size="small" :type="diagnoseStatusType(diagnosis.overall_status)">
          {{ diagnosis.overall_status }}
        </NTag>
      </div>
      <div class="diagnosis-note">
        FongMi 首页海报墙可能来自 homeVideoContent；homeContent 为空不一定代表源坏。分类、首页与搜索应分开判断，本诊断不会改变探活状态或写入在线缓存。
      </div>
      <div class="diagnosis-grid">
        <article v-for="item in diagnosis.results" :key="item.method" class="diagnosis-card">
          <div class="diagnosis-card-head">
            <strong>{{ diagnoseMethodLabel(item.method) }}</strong>
            <NTag size="small" :type="diagnoseStatusType(item.status)">{{ item.status }}</NTag>
          </div>
          <div class="diagnosis-metrics">
            <span>{{ item.latency_ms }} ms</span>
            <span>class {{ item.categories_count }}</span>
            <span>filters {{ item.filters_count }}</span>
            <span>list {{ item.items_count }}</span>
          </div>
          <p v-if="item.message" class="diagnosis-message">{{ item.error_type ? `${item.error_type}: ` : '' }}{{ item.message }}</p>
          <div v-if="item.sample_items?.length" class="sample-list">
            <div v-for="sample in item.sample_items" :key="`${item.method}:${sample.source_item_id || sample.title}`" class="sample-row">
              <span class="sample-title">{{ sample.title || sample.source_item_id || '-' }}</span>
              <span class="sample-meta">{{ sample.item_type || '-' }}<template v-if="sample.year"> · {{ sample.year }}</template></span>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section v-if="homeProfile" class="home-profile-panel" aria-live="polite">
      <div class="diagnosis-head">
        <div>
          <h3 class="diagnosis-title">首页画像</h3>
          <p class="panel-subtitle">
            {{ runtimeLabel(homeProfile.runtime_kind) }} · 首页列表来源 {{ homeProfileSourceLabel(homeProfile.home_item_source) }}
          </p>
        </div>
        <NTag size="small" type="info">read-only</NTag>
      </div>
      <div class="home-metrics">
        <span>class {{ homeProfile.categories.length }}</span>
        <span>filters {{ homeProfile.filters_count }}</span>
        <span>home items {{ homeProfile.home_items.length }}</span>
      </div>
      <div class="diagnosis-grid">
        <article
          v-for="slice in [homeProfile.sources.home_content, homeProfile.sources.home_video_content]"
          :key="slice.method"
          class="diagnosis-card"
        >
          <div class="diagnosis-card-head">
            <strong>{{ homeProfileSliceLabel(slice) }}</strong>
            <NTag size="small" :type="diagnoseStatusType(slice.status)">{{ slice.status }}</NTag>
          </div>
          <div class="diagnosis-metrics">
            <span>{{ slice.duration_ms }} ms</span>
            <span>class {{ slice.categories_count }}</span>
            <span>filters {{ slice.filters_count }}</span>
            <span>list {{ slice.items_count }}</span>
          </div>
          <p v-if="homeProfileMessage(slice)" class="diagnosis-message">{{ homeProfileMessage(slice) }}</p>
        </article>
      </div>
      <div v-if="homeProfile.categories.length" class="chips">
        <NTag v-for="cat in homeProfile.categories.slice(0, 24)" :key="cat.id" size="small">{{ cat.name }}</NTag>
      </div>
      <div v-if="homeProfile.home_items.length" class="sample-list">
        <div
          v-for="item in homeProfile.home_items.slice(0, 8)"
          :key="item.source_item_id || item.title"
          class="sample-row"
        >
          <span class="sample-title">{{ item.title || item.source_item_id || '-' }}</span>
          <span class="sample-meta">{{ item.item_type || '-' }}<template v-if="item.year"> · {{ item.year }}</template></span>
        </div>
      </div>
    </section>
  </section>
</template>

<style scoped>
.source-panel {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
  padding: 16px;
}
.panel-head {
  margin-bottom: 12px;
}
.panel-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}
.panel-subtitle {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
}
.test-grid {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(180px, 1fr) auto;
  align-items: end;
  gap: 10px;
  margin-top: 14px;
}
.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.field-label {
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
}
.bulk-bar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 12px;
  margin-bottom: 12px;
}
.fm3-actions {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 12px;
}
.bulk-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
}
.filter-bar {
  display: grid;
  grid-template-columns: minmax(150px, 0.7fr) minmax(150px, 0.7fr) minmax(150px, 0.7fr) minmax(220px, 1.4fr);
  gap: 10px;
  margin-bottom: 12px;
}
.health-filter-bar {
  display: grid;
  grid-template-columns: repeat(3, minmax(150px, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}
.health-empty {
  color: var(--app-text-muted);
  font-size: 12px;
}
.filtered-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--app-border);
  padding-bottom: 12px;
  margin-bottom: 12px;
}
.keyword-field {
  min-width: 220px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}
.result-strip {
  display: flex;
  gap: 16px;
  margin-top: 12px;
  color: var(--app-text-muted);
  font-size: 13px;
}
.diagnosis-panel {
  display: grid;
  gap: 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 14px;
  margin-top: 14px;
}
.home-profile-panel {
  display: grid;
  gap: 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 14px;
  margin-top: 14px;
}
.home-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  color: var(--app-text-muted);
  font-size: 13px;
}
.diagnosis-head,
.diagnosis-card-head,
.sample-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.diagnosis-title {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
}
.diagnosis-note,
.diagnosis-message,
.sample-meta {
  color: var(--app-text-muted);
  font-size: 12px;
}
.diagnosis-note {
  border-left: 3px solid rgba(59, 130, 246, 0.45);
  padding-left: 10px;
}
.diagnosis-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.diagnosis-card {
  display: grid;
  gap: 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-1);
  padding: 10px;
}
.diagnosis-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--app-text-muted);
  font-size: 12px;
}
.diagnosis-message {
  margin: 0;
  overflow-wrap: anywhere;
}
.sample-list {
  display: grid;
  gap: 5px;
}
.sample-row {
  min-width: 0;
  border-top: 1px solid var(--app-border);
  padding-top: 5px;
  font-size: 12px;
}
.sample-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.error-cell {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px;
  align-items: center;
}

.error-cell span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty-state {
  border: 1px dashed var(--app-border);
  border-radius: 8px;
  padding: 18px;
  color: var(--app-text-muted);
  font-size: 13px;
}
@media (max-width: 760px) {
  .bulk-bar {
    flex-direction: column;
  }
  .fm3-actions {
    flex-direction: column;
  }
  .bulk-actions {
    justify-content: flex-start;
  }
  .filter-bar {
    grid-template-columns: 1fr;
  }
  .health-filter-bar {
    grid-template-columns: 1fr;
  }
  .filtered-actions {
    align-items: flex-start;
    flex-direction: column;
  }
  .test-grid {
    grid-template-columns: 1fr;
  }
  .diagnosis-head {
    align-items: flex-start;
    flex-direction: column;
  }
  .diagnosis-grid {
    grid-template-columns: 1fr;
  }
}
</style>
