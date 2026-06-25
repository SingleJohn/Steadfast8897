<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NTag, useMessage } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceRuntimeArtifact, SourceRuntimeInvocation } from '@/api/source'
import { copyText } from '@/utils/externalPlayers'

const props = defineProps<{
  invocations: SourceRuntimeInvocation[]
  artifacts: SourceRuntimeArtifact[]
  loading: boolean
  action: string
}>()

const emit = defineEmits<{
  refresh: []
  trust: [id: number]
}>()

const message = useMessage()
const errorCount = computed(() => props.invocations.filter((item) => item.Status === 'error').length)
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}

const invocationColumns: DataTableColumns<SourceRuntimeInvocation> = [
  { title: '时间', key: 'InvokedAt', width: 170, render: (row) => formatTime(row.InvokedAt) },
  { title: '运行时', key: 'RuntimeKind', width: 110, render: (row) => runtimeLabel(row.RuntimeKind) },
  { title: 'Provider', key: 'ProviderID', width: 90, render: (row) => row.ProviderID || '-' },
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
    minWidth: 190,
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
  {
    title: 'URL Hash',
    key: 'URLHash',
    width: 130,
    render(row) {
      if (!row.URLHash) return '-'
      return h('div', { class: 'copy-cell' }, [
        h('span', row.URLHash),
        h(NButton, {
          size: 'tiny',
          quaternary: true,
          onClick: () => copyValue('URL Hash 已复制', row.URLHash || ''),
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

async function copyValue(successMessage: string, value: string) {
  const ok = await copyText(value)
  if (ok) message.success(successMessage)
  else message.error('复制失败，请手动选中')
}

async function copyInvocation(row: SourceRuntimeInvocation) {
  const text = [
    `Time: ${formatTime(row.InvokedAt)}`,
    `Runtime: ${runtimeLabel(row.RuntimeKind)}`,
    `ProviderID: ${row.ProviderID || '-'}`,
    `Method: ${row.Method}`,
    `Status: ${row.Status}`,
    `ErrorType: ${row.ErrorType || '-'}`,
    `ErrorMessage: ${row.ErrorMessage || '-'}`,
    `URLHash: ${row.URLHash || '-'}`,
  ].join('\n')
  const ok = await copyText(text)
  if (ok) message.success('调用错误已复制')
  else message.error('复制失败，请手动选中')
}
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">运行时审计</h2>
        <p class="panel-subtitle">最近 {{ invocations.length }} 次调用，{{ errorCount }} 次失败；CSP JAR 与 JS 调用均只保留敏感 URL hash。</p>
      </div>
      <NButton quaternary size="small" :loading="loading" @click="emit('refresh')">刷新</NButton>
    </div>

    <div class="audit-grid">
      <div class="audit-section">
        <div class="section-title">调用记录</div>
        <NDataTable v-if="invocations.length > 0" :columns="invocationColumns" :data="invocations" :pagination="tablePagination" size="small" :bordered="false" />
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
}
</style>
