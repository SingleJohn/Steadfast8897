<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceRuntimeArtifact, SourceRuntimeInvocation } from '@/api/source'

const props = defineProps<{
  invocations: SourceRuntimeInvocation[]
  artifacts: SourceRuntimeArtifact[]
  loading: boolean
}>()

const emit = defineEmits<{
  refresh: []
}>()

const errorCount = computed(() => props.invocations.filter((item) => item.Status === 'error').length)

const invocationColumns: DataTableColumns<SourceRuntimeInvocation> = [
  { title: '时间', key: 'InvokedAt', width: 170, render: (row) => formatTime(row.InvokedAt) },
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
  { title: '错误', key: 'ErrorType', minWidth: 150, ellipsis: { tooltip: true }, render: (row) => row.ErrorType || row.ErrorMessage || '-' },
  { title: 'URL Hash', key: 'URLHash', width: 120, render: (row) => row.URLHash || '-' },
]

const artifactColumns: DataTableColumns<SourceRuntimeArtifact> = [
  { title: '名称', key: 'Name', minWidth: 150 },
  { title: '类型', key: 'ArtifactKind', width: 120 },
  {
    title: '信任',
    key: 'TrustStatus',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: row.TrustStatus === 'trusted' ? 'success' : undefined }, { default: () => row.TrustStatus || 'unverified' })
    },
  },
  { title: '大小', key: 'ByteSize', width: 100, render: (row) => formatBytes(row.ByteSize) },
  { title: 'SHA256', key: 'SHA256', minWidth: 180, ellipsis: { tooltip: true } },
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
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">运行时审计</h2>
        <p class="panel-subtitle">最近 {{ invocations.length }} 次调用，{{ errorCount }} 次失败；敏感 URL 仅保留 hash。</p>
      </div>
      <NButton quaternary size="small" :loading="loading" @click="emit('refresh')">刷新</NButton>
    </div>

    <div class="audit-grid">
      <div class="audit-section">
        <div class="section-title">调用记录</div>
        <NDataTable :columns="invocationColumns" :data="invocations" size="small" :bordered="false" />
      </div>
      <div class="audit-section">
        <div class="section-title">Artifacts</div>
        <NDataTable :columns="artifactColumns" :data="artifacts" size="small" :bordered="false" />
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
@media (max-width: 760px) {
  .panel-head {
    flex-direction: column;
  }
}
</style>
