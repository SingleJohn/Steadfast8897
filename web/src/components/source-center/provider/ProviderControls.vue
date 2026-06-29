<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { NButton, NCheckbox, NInput, NPopconfirm, NSelect, NTooltip } from 'naive-ui'
import type { SourceProvider, SourceProviderListOptions } from '@/api/source'

const props = defineProps<{
  providers: SourceProvider[]
  selectedIds: number[]
  action: string
  healthFilters: SourceProviderListOptions
  includeHidden: boolean
}>()

const emit = defineEmits<{
  'update:selectedIds': [value: number[]]
  'update:filtered': [value: SourceProvider[]]
  updateHealthFilters: [filters: SourceProviderListOptions]
  updateIncludeHidden: [value: boolean]
  batchEnable: []
  batchDisable: []
  batchHealth: []
  batchDelete: [ids?: number[]]
  batchEnableIds: [ids: number[]]
  batchDisableIds: [ids: number[]]
  batchHealthIds: [ids: number[]]
}>()

const healthFilter = shallowRef<string | null>(null)
const enabledFilter = shallowRef<string | null>(null)
const runtimeFilter = shallowRef<string | null>(null)
const keywordFilter = shallowRef('')
const runtimeHealthFilter = shallowRef<string | null>(props.healthFilters.runtime_status || null)
const homeHealthFilter = shallowRef<string | null>(props.healthFilters.home_status || null)
const categoryHealthFilter = shallowRef<string | null>(props.healthFilters.category_status || null)
const showAdvanced = shallowRef(false)

const healthFilterOptions = [
  { label: '全部探活状态', value: '' },
  { label: '探活正常', value: 'ok' },
  { label: '部分可用', value: 'partial' },
  { label: '探活失败', value: 'error' },
  { label: '未探活', value: 'unknown' },
]
const methodStatusOptions = [
  { label: '全部', value: '' },
  { label: 'ok 正常', value: 'ok' },
  { label: 'partial 部分', value: 'partial' },
  { label: 'error 失败', value: 'error' },
  { label: 'unhealthy 不健康', value: 'unhealthy' },
  { label: 'unknown 未探活', value: 'unknown' },
  { label: 'skipped 已跳过', value: 'skipped' },
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

function normalizeHealth(value: string) {
  return value || 'unknown'
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

const selectedProviders = computed(() => props.providers.filter((provider) => props.selectedIds.includes(provider.ID)))
const selectedEnabledCount = computed(() => selectedProviders.value.filter((provider) => provider.Enabled).length)
const selectedRuntimeCount = computed(() => selectedProviders.value.filter((provider) => provider.RuntimeKind !== 'native_cms').length)
const hiddenProviderCount = computed(() => props.providers.filter((provider) => !provider.Visible).length)
const homeUsableProviders = computed(() => props.providers.filter((provider) => isProviderHomeUsable(provider)))
const failedHealthProviders = computed(() => props.providers.filter((provider) => hasProviderBlockingHealthFailure(provider)))
const failedHealthEnabledCount = computed(() => failedHealthProviders.value.filter((provider) => provider.Enabled).length)

// KPI 概览
const totalCount = computed(() => props.providers.length)
const enabledCount = computed(() => props.providers.filter((provider) => provider.Enabled).length)
const okCount = computed(() => props.providers.filter((provider) => provider.HealthStatus === 'ok').length)
const failedCount = computed(() => failedHealthProviders.value.length)

const advancedActive = computed(() =>
  !!(runtimeHealthFilter.value || homeHealthFilter.value || categoryHealthFilter.value || props.includeHidden))

watch(filteredProviders, (value) => emit('update:filtered', value), { immediate: true })

function emitHealthFilters() {
  emit('updateHealthFilters', {
    runtime_status: runtimeHealthFilter.value || undefined,
    home_status: homeHealthFilter.value || undefined,
    category_status: categoryHealthFilter.value || undefined,
  })
}

function setRuntimeHealth(value: string | null) {
  runtimeHealthFilter.value = value || null
  emitHealthFilters()
}
function setHomeHealth(value: string | null) {
  homeHealthFilter.value = value || null
  emitHealthFilters()
}
function setCategoryHealth(value: string | null) {
  categoryHealthFilter.value = value || null
  emitHealthFilters()
}

function selectAllProviders() {
  emit('update:selectedIds', props.providers.map((provider) => provider.ID))
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
  <div class="controls">
    <!-- KPI 概览 -->
    <div class="kpi-row">
      <div class="kpi">
        <span class="kpi-num">{{ totalCount }}</span>
        <span class="kpi-label">站点总数</span>
      </div>
      <div class="kpi">
        <span class="kpi-num is-ok">{{ enabledCount }}</span>
        <span class="kpi-label">已启用</span>
      </div>
      <div class="kpi">
        <span class="kpi-num is-ok">{{ okCount }}</span>
        <span class="kpi-label">探活正常</span>
      </div>
      <div class="kpi">
        <span class="kpi-num" :class="{ 'is-error': failedCount > 0 }">{{ failedCount }}</span>
        <span class="kpi-label">明确失败</span>
      </div>
    </div>

    <!-- 筛选工具条 -->
    <div class="filter-toolbar">
      <NInput
        class="keyword"
        :value="keywordFilter"
        placeholder="搜索名称 / SourceKey / API"
        clearable
        @update:value="keywordFilter = $event"
      />
      <NSelect class="sel" :value="healthFilter || ''" :options="healthFilterOptions" @update:value="healthFilter = $event || null" />
      <NSelect class="sel" :value="enabledFilter || ''" :options="enabledFilterOptions" @update:value="enabledFilter = $event || null" />
      <NSelect class="sel" :value="runtimeFilter || ''" :options="runtimeFilterOptions" @update:value="runtimeFilter = $event || null" />
      <NButton
        :type="advancedActive ? 'primary' : 'default'"
        :secondary="advancedActive"
        @click="showAdvanced = !showAdvanced"
      >
        高级筛选{{ advancedActive ? '（已启用）' : '' }}
      </NButton>
    </div>

    <!-- 高级筛选（分项健康 + 隐藏站点） -->
    <div v-if="showAdvanced" class="filter-advanced">
      <label class="field">
        <span class="field-label">Runtime 健康</span>
        <NSelect :value="runtimeHealthFilter || ''" :options="methodStatusOptions" @update:value="setRuntimeHealth" />
      </label>
      <label class="field">
        <span class="field-label">首页健康</span>
        <NSelect :value="homeHealthFilter || ''" :options="methodStatusOptions" @update:value="setHomeHealth" />
      </label>
      <label class="field">
        <span class="field-label">分类健康</span>
        <NSelect :value="categoryHealthFilter || ''" :options="methodStatusOptions" @update:value="setCategoryHealth" />
      </label>
      <label class="field visibility-field">
        <span class="field-label">隐藏站点</span>
        <NCheckbox :checked="includeHidden" @update:checked="emit('updateIncludeHidden', $event === true)">
          显示隐藏（{{ hiddenProviderCount }}）
        </NCheckbox>
      </label>
    </div>

    <!-- 跨页选择条：明确批量作用于全部分页，而非当前表格页 -->
    <div class="select-bar">
      <span class="select-summary">
        已选 <strong>{{ selectedIds.length }}</strong> / 共 {{ totalCount }} 个站点
        <span class="select-note">（批量操作对所有分页生效，不限当前表格页）</span>
      </span>
      <div class="select-actions">
        <NButton
          size="small"
          type="primary"
          secondary
          :disabled="totalCount === 0 || selectedIds.length === totalCount"
          @click="selectAllProviders"
        >
          全选全部（{{ totalCount }}）
        </NButton>
        <NButton
          size="small"
          :disabled="filteredProviders.length === 0 || filteredSelectedCount === filteredProviders.length"
          @click="selectFilteredProviders"
        >
          全选当前筛选（{{ filteredProviders.length }}）
        </NButton>
        <NButton size="small" quaternary :disabled="selectedIds.length === 0" @click="clearSelectedProviders">
          清空选择
        </NButton>
      </div>
    </div>

    <!-- 批量操作（按作用域分组） -->
    <div class="bulk">
      <div class="bulk-group">
        <div class="bulk-head">
          <span class="bulk-title">对选中</span>
          <span class="bulk-meta">{{ selectedIds.length }} 个（{{ selectedEnabledCount }} 启用 · {{ selectedRuntimeCount }} JS/CSP）</span>
        </div>
        <div class="bulk-actions">
          <NPopconfirm positive-text="批量启用" negative-text="取消" :disabled="selectedIds.length === 0" @positive-click="emit('batchEnable')">
            <template #trigger>
              <NButton size="small" :disabled="selectedIds.length === 0" :loading="action === 'batch-enable'">启用</NButton>
            </template>
            将启用 {{ selectedIds.length }} 个站点；相关在线虚拟库可能开始收录这些站点的数据。
          </NPopconfirm>
          <NPopconfirm positive-text="批量停用" negative-text="取消" :disabled="selectedIds.length === 0" @positive-click="emit('batchDisable')">
            <template #trigger>
              <NButton size="small" type="error" ghost :disabled="selectedIds.length === 0" :loading="action === 'batch-disable'">停用</NButton>
            </template>
            将停用 {{ selectedIds.length }} 个站点；依赖这些站点的在线虚拟库命中会减少。
          </NPopconfirm>
          <NPopconfirm positive-text="开始探活" negative-text="取消" :disabled="selectedIds.length === 0" @positive-click="emit('batchHealth')">
            <template #trigger>
              <NButton size="small" :disabled="selectedIds.length === 0" :loading="action === 'batch-health'">探活</NButton>
            </template>
            将并发探活 {{ selectedIds.length }} 个站点，单站失败不会中断整批。
          </NPopconfirm>
          <NPopconfirm positive-text="删除" negative-text="取消" :disabled="selectedIds.length === 0" @positive-click="emit('batchDelete')">
            <template #trigger>
              <NButton size="small" type="error" :disabled="selectedIds.length === 0" :loading="action === 'batch-delete'">删除</NButton>
            </template>
            将删除 {{ selectedIds.length }} 个站点，并清理在线虚拟库引用；在线缓存条目会随站点级联删除，运行时审计保留脱敏记录。
          </NPopconfirm>
          <NButton size="small" quaternary :disabled="selectedIds.length === 0" @click="clearSelectedProviders">清空</NButton>
        </div>
      </div>

      <div class="bulk-group">
        <div class="bulk-head">
          <span class="bulk-title">按健康</span>
          <NTooltip>
            <template #trigger>
              <span class="bulk-info" aria-label="按健康说明">?</span>
            </template>
            首页可用按 home_status=ok/partial 判断；明确失败按 runtime/home/category 为 error 或 unhealthy 判断。
          </NTooltip>
        </div>
        <div class="bulk-actions">
          <NPopconfirm positive-text="启用首页可用" negative-text="取消" :disabled="homeUsableProviders.length === 0" @positive-click="emitHomeUsableEnable">
            <template #trigger>
              <NButton size="small" :disabled="homeUsableProviders.length === 0" :loading="action === 'batch-enable'">启用首页可用（{{ homeUsableProviders.length }}）</NButton>
            </template>
            将启用 home_status 为 ok/partial 的 {{ homeUsableProviders.length }} 个 Provider；筛选来自已加载的分项健康摘要。
          </NPopconfirm>
          <NPopconfirm positive-text="停用明确失败" negative-text="取消" :disabled="failedHealthProviders.length === 0" @positive-click="emitFailedHealthDisable">
            <template #trigger>
              <NButton size="small" type="error" ghost :disabled="failedHealthProviders.length === 0" :loading="action === 'batch-disable'">停用明确失败（{{ failedHealthProviders.length }}）</NButton>
            </template>
            将停用 runtime/home/category 明确失败的 {{ failedHealthProviders.length }} 个 Provider，其中 {{ failedHealthEnabledCount }} 个当前已启用。
          </NPopconfirm>
        </div>
      </div>

      <div class="bulk-group">
        <div class="bulk-head">
          <span class="bulk-title">按筛选</span>
          <span class="bulk-meta">命中 {{ filteredProviders.length }} · 已选 {{ filteredSelectedCount }}</span>
        </div>
        <div class="bulk-actions">
          <NButton size="small" :disabled="filteredProviders.length === 0" @click="selectFilteredProviders">加入选中（{{ filteredProviders.length }}）</NButton>
          <NButton size="small" quaternary :disabled="filteredSelectedCount === 0" @click="clearFilteredSelection">移出选中</NButton>
          <NPopconfirm positive-text="启用筛选结果" negative-text="取消" :disabled="filteredProviders.length === 0" @positive-click="emitFilteredBatch('enable')">
            <template #trigger>
              <NButton size="small" :disabled="filteredProviders.length === 0" :loading="action === 'batch-enable'">启用</NButton>
            </template>
            将启用当前筛选命中的 {{ filteredProviders.length }} 个站点，其中 {{ filteredChangeCounts.disabled }} 个会从停用变为启用。
          </NPopconfirm>
          <NPopconfirm positive-text="停用筛选结果" negative-text="取消" :disabled="filteredProviders.length === 0" @positive-click="emitFilteredBatch('disable')">
            <template #trigger>
              <NButton size="small" type="error" ghost :disabled="filteredProviders.length === 0" :loading="action === 'batch-disable'">停用</NButton>
            </template>
            将停用当前筛选命中的 {{ filteredProviders.length }} 个站点，其中 {{ filteredChangeCounts.enabled }} 个会从启用变为停用。
          </NPopconfirm>
          <NPopconfirm positive-text="探活筛选结果" negative-text="取消" :disabled="filteredProviders.length === 0" @positive-click="emitFilteredBatch('health')">
            <template #trigger>
              <NButton size="small" :disabled="filteredProviders.length === 0" :loading="action === 'batch-health'">探活</NButton>
            </template>
            将并发探活当前筛选命中的 {{ filteredProviders.length }} 个站点。
          </NPopconfirm>
          <NPopconfirm positive-text="删除筛选结果" negative-text="取消" :disabled="filteredProviders.length === 0" @positive-click="emitFilteredBatch('delete')">
            <template #trigger>
              <NButton size="small" type="error" :disabled="filteredProviders.length === 0" :loading="action === 'batch-delete'">删除</NButton>
            </template>
            将删除当前筛选命中的 {{ filteredProviders.length }} 个站点，并清理在线虚拟库引用；在线缓存条目会随站点级联删除。
          </NPopconfirm>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.controls {
  display: grid;
  gap: 12px;
}
.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
.kpi {
  display: grid;
  gap: 2px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 10px 12px;
}
.kpi-num {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}
.kpi-num.is-ok {
  color: #18a058;
}
.kpi-num.is-error {
  color: #d03050;
}
.kpi-label {
  color: var(--app-text-muted);
  font-size: 12px;
}
.filter-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.filter-toolbar .keyword {
  flex: 1 1 240px;
  min-width: 200px;
}
.filter-toolbar .sel {
  width: 150px;
}
.filter-advanced {
  display: grid;
  grid-template-columns: repeat(4, minmax(150px, 1fr));
  gap: 10px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 12px;
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
.visibility-field {
  align-content: start;
}
.select-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 8px 12px;
}
.select-summary {
  font-size: 13px;
}
.select-summary strong {
  font-variant-numeric: tabular-nums;
}
.select-note {
  color: var(--app-text-muted);
  font-size: 12px;
}
.select-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.bulk {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.bulk-group {
  display: grid;
  gap: 8px;
  flex: 1 1 260px;
  min-width: 240px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 10px 12px;
}
.bulk-head {
  display: flex;
  align-items: center;
  gap: 6px;
}
.bulk-title {
  font-size: 13px;
  font-weight: 700;
}
.bulk-meta {
  color: var(--app-text-muted);
  font-size: 12px;
}
.bulk-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  border-radius: 8px;
  background: var(--app-surface-1);
  color: var(--app-text-muted);
  font-size: 11px;
  font-weight: 600;
  cursor: help;
}
.bulk-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
@media (max-width: 760px) {
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .filter-toolbar .sel {
    width: 100%;
    flex: 1 1 140px;
  }
  .filter-advanced {
    grid-template-columns: 1fr;
  }
}
</style>
