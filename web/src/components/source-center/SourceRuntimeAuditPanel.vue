<script setup lang="ts">
import { computed, h, shallowRef } from 'vue'
import { NButton, NDataTable, NInput, NSelect, NTag, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceProvider, SourceRuntimeArtifact, SourceRuntimeInvocation, SourceRuntimeInvocationListOptions } from '@/api/source'
import { copyText } from '@/utils/externalPlayers'

const props = defineProps<{
  invocations: SourceRuntimeInvocation[]
  artifacts: SourceRuntimeArtifact[]
  providers: SourceProvider[]
  filters: SourceRuntimeInvocationListOptions
  loading: boolean
  action: string
}>()

const emit = defineEmits<{
  refresh: []
  updateFilters: [filters: SourceRuntimeInvocationListOptions]
  trust: [id: number]
}>()

const message = useMessage()
const errorCount = computed(() => props.invocations.filter((item) => item.Status === 'error').length)
const keywordFilter = shallowRef('')
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}
const statusOptions = computed(() => buildOptions(['ok', 'error', ...props.invocations.map((item) => item.Status)], '全部状态'))
const runtimeOptions = computed(() => buildOptions(['native_cms', 'js_node_drpy', 'csp_dex', ...props.invocations.map((item) => item.RuntimeKind)], '全部运行时'))
const methodOptions = computed(() => buildOptions(['home', 'homeVideo', 'category', 'search', 'detail', 'play', ...props.invocations.map((item) => item.Method)], '全部方法'))
const errorTypeOptions = computed(() => buildOptions(props.invocations.map((item) => item.ErrorType || ''), '全部错误类型'))
const providerOptions = computed(() => [
  { label: '全部站点', value: 0 },
  ...props.providers.map((provider) => ({ label: `${provider.Name} (${provider.SourceKey})`, value: provider.ID })),
])
const filteredInvocations = computed(() => {
  const keyword = keywordFilter.value.trim().toLowerCase()
  return props.invocations.filter((item) => {
    if (!keyword) return true
    const haystack = [
      item.ProviderName,
      item.ProviderID ? `provider ${item.ProviderID}` : '',
      item.Method,
      item.RuntimeKind,
      item.Status,
      item.ErrorType,
      item.ErrorMessage,
    ].join(' ').toLowerCase()
    return haystack.includes(keyword)
  })
})
const filteredErrorCount = computed(() => filteredInvocations.value.filter((item) => item.Status === 'error').length)
const activeProviderFilter = computed(() => props.filters.provider_id || 0)
const activeStatusFilter = computed(() => props.filters.status || '')
const activeRuntimeFilter = computed(() => props.filters.runtime_kind || '')
const activeMethodFilter = computed(() => props.filters.method || '')
const activeErrorTypeFilter = computed(() => props.filters.error_type || '')
const activeLimit = computed(() => props.filters.limit || 100)

const invocationColumns: DataTableColumns<SourceRuntimeInvocation> = [
  { title: '时间', key: 'InvokedAt', width: 170, render: (row) => formatTime(row.InvokedAt) },
  { title: '运行时', key: 'RuntimeKind', width: 110, render: (row) => runtimeLabel(row.RuntimeKind) },
  { title: '站点', key: 'ProviderID', minWidth: 150, render: (row) => row.ProviderName || (row.ProviderID ? `Provider ${row.ProviderID}` : '-') },
  { title: '方法', key: 'Method', width: 90 },
  {
    title: '状态',
    key: 'Status',
    width: 90,
    render(row) {
      return h(NTag, { size: 'small', type: row.Status === 'ok' ? 'success' : 'error' }, { default: () => row.Status })
    },
  },
  { title: '耗时', key: 'DurationMS', width: 90, render: (row) => `${row.DurationMS} ms` },
  {
    title: '错误',
    key: 'ErrorType',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render(row) {
      const text = row.ErrorType || row.ErrorMessage || ''
      if (!text) return '-'
      return h('div', { class: 'copy-cell' }, [
        h('span', text),
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          onClick: () => copyInvocation(row),
        }, { default: () => '复制' }),
      ])
    },
  },
]

const artifactColumns: DataTableColumns<SourceRuntimeArtifact> = [
  { title: '名称', key: 'Name', minWidth: 150 },
  { title: '类型', key: 'ArtifactKind', width: 130, render: (row) => artifactLabel(row.ArtifactKind) },
  {
    title: '信任',
    key: 'TrustStatus',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: artifactTrustType(row.TrustStatus) }, { default: () => row.TrustStatus || 'unverified' })
    },
  },
  { title: '大小', key: 'ByteSize', width: 100, render: (row) => formatBytes(row.ByteSize) },
  {
    title: 'SHA256',
    key: 'SHA256',
    minWidth: 210,
    ellipsis: { tooltip: true },
    render(row) {
      return h('div', { class: 'copy-cell' }, [
        h('span', row.SHA256 || '-'),
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          disabled: !row.SHA256,
          onClick: () => copyValue('SHA256 已复制', row.SHA256),
        }, { default: () => '复制' }),
      ])
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 110,
    render(row) {
      if (isArtifactTrusted(row.TrustStatus)) return '-'
      return h(NButton, {
        size: 'small',
        quaternary: true,
        loading: props.action === `trust-artifact:${row.ID}`,
        onClick: () => emit('trust', row.ID),
      }, { default: () => '确认信任' })
    },
  },
]

function formatTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

function runtimeLabel(value: string) {
  if (value === 'csp_dex') return 'CSP JAR'
  if (value === 'js_node_drpy') return 'DRPY JS'
  return value || '-'
}

function artifactLabel(value: string) {
  if (value === 'csp_dex_jar') return 'CSP JAR'
  if (value === 'drpy_rule') return 'DRPY 规则'
  if (value === 'drpy_engine') return 'DRPY 引擎'
  return value || '-'
}

function isArtifactTrusted(value: string) {
  const trust = (value || '').toLowerCase()
  return trust === 'verified' || trust === 'trusted'
}

function artifactTrustType(value: string) {
  return isArtifactTrusted(value) ? 'success' : undefined
}

function buildOptions(values: string[], allLabel: string) {
  const unique = Array.from(new Set(values.filter(Boolean))).sort()
  return [
    { label: allLabel, value: '' },
    ...unique.map((value) => ({ label: value, value })),
  ]
}

function updateFilter(patch: SourceRuntimeInvocationListOptions) {
  const next = { ...props.filters, ...patch }
  emit('updateFilters', {
    limit: next.limit || 100,
    provider_id: next.provider_id || undefined,
    method: next.method || undefined,
    status: next.status || undefined,
    error_type: next.error_type || undefined,
    runtime_kind: next.runtime_kind || undefined,
    start_time: next.start_time || undefined,
    end_time: next.end_time || undefined,
  })
}

function clearFilters() {
  keywordFilter.value = ''
  emit('updateFilters', { limit: activeLimit.value })
}

async function copyValue(successMessage: string, value: string) {
  const ok = await copyText(value)
  if (ok) message.success(successMessage)
  else message.error('复制失败，请手动选中')
}

async function copyInvocation(row: SourceRuntimeInvocation) {
  const text = [
    `Time: ${formatTime(row.InvokedAt)}`,
    `Runtime: ${runtimeLabel(row.RuntimeKind)}`,
    `Provider: ${row.ProviderName || row.ProviderID || '-'}`,
    `Method: ${row.Method}`,
    `Status: ${row.Status}`,
    `Duration: ${row.DurationMS} ms`,
    `ErrorType: ${row.ErrorType || '-'}`,
    `ErrorMessage: ${row.ErrorMessage || '-'}`,
    row.URLHash ? `URLHash: ${row.URLHash}` : '',
  ].filter(Boolean).join('\n')
  const ok = await copyText(text)
  if (ok) message.success('排障摘要已复制')
  else message.error('复制失败，请手动选中')
}
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">运行时审计</h2>
        <p class="panel-subtitle">最近 {{ invocations.length }} 次调用，{{ errorCount }} 次失败；敏感 URL 不明文展示，必要时在复制摘要中携带脱敏关联 hash。</p>
      </div>
      <NButton quaternary size="small" :loading="loading" @click="emit('refresh')">刷新</NButton>
    </div>

    <div class="audit-grid">
      <div class="audit-section">
        <div class="section-head">
          <div>
            <div class="section-title">调用记录</div>
            <p class="panel-subtitle">当前筛选 {{ filteredInvocations.length }} 条，失败 {{ filteredErrorCount }} 条。</p>
          </div>
        </div>
        <div class="audit-filters">
          <label class="field">
            <span class="field-label">站点</span>
            <NSelect
              :value="activeProviderFilter"
              :options="providerOptions"
              filterable
              @update:value="updateFilter({ provider_id: Number($event || 0) })"
            />
          </label>
          <label class="field">
            <span class="field-label">状态</span>
            <NSelect :value="activeStatusFilter" :options="statusOptions" @update:value="updateFilter({ status: $event || undefined })" />
          </label>
          <label class="field">
            <span class="field-label">运行时</span>
            <NSelect :value="activeRuntimeFilter" :options="runtimeOptions" @update:value="updateFilter({ runtime_kind: $event || undefined })" />
          </label>
          <label class="field">
            <span class="field-label">方法</span>
            <NSelect :value="activeMethodFilter" :options="methodOptions" @update:value="updateFilter({ method: $event || undefined })" />
          </label>
          <label class="field">
            <span class="field-label">错误类型</span>
            <NSelect :value="activeErrorTypeFilter" :options="errorTypeOptions" @update:value="updateFilter({ error_type: $event || undefined })" />
          </label>
          <label class="field keyword-field">
            <span class="field-label">关键词</span>
            <NInput :value="keywordFilter" placeholder="站点 / 错误类型 / 错误摘要" clearable @update:value="keywordFilter = $event" />
          </label>
          <label class="field limit-field">
            <span class="field-label">返回条数</span>
            <NSelect
              :value="activeLimit"
              :options="[
                { label: '100 条', value: 100 },
                { label: '200 条', value: 200 },
                { label: '500 条', value: 500 },
              ]"
              @update:value="updateFilter({ limit: Number($event || 100) })"
            />
          </label>
          <div class="filter-actions">
            <NButton size="small" quaternary @click="clearFilters">清空筛选</NButton>
          </div>
        </div>
        <NDataTable v-if="filteredInvocations.length > 0" :columns="invocationColumns" :data="filteredInvocations" :pagination="tablePagination" size="small" :bordered="false" />
        <div v-else class="empty-state">暂无运行时调用记录。</div>
      </div>
      <div class="audit-section">
        <div class="section-title">Artifacts</div>
        <NDataTable v-if="artifacts.length > 0" :columns="artifactColumns" :data="artifacts" :pagination="tablePagination" size="small" :bordered="false" />
        <div v-else class="empty-state">暂无 runtime artifact。</div>
      </div>
    </div>
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
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}
.panel-title,
.section-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}
.section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.audit-filters {
  display: grid;
  grid-template-columns: minmax(180px, 1.2fr) minmax(110px, 0.6fr) minmax(130px, 0.7fr) minmax(110px, 0.6fr) minmax(150px, 0.8fr) minmax(220px, 1.2fr) minmax(110px, 0.6fr) auto;
  gap: 10px;
  align-items: end;
  margin-bottom: 12px;
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
.filter-actions {
  display: flex;
  align-items: end;
  min-height: 34px;
}
.panel-subtitle {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
}
.audit-grid {
  display: grid;
  gap: 14px;
}
.audit-section {
  min-width: 0;
}
.section-title {
  margin-bottom: 8px;
  font-size: 13px;
}
.copy-cell {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px;
  align-items: center;
}
.copy-cell span {
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
  .panel-head {
    flex-direction: column;
  }

  .audit-filters {
    grid-template-columns: 1fr;
  }
}
</style>
