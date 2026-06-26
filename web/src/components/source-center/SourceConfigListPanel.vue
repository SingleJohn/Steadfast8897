<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceConfig, SourceConfigImpact } from '@/api/source'

const props = defineProps<{
  configs: SourceConfig[]
  action: string
  deleteTarget: SourceConfig | null
  deleteImpact: SourceConfigImpact | null
  deleteLoading: boolean
}>()

const emit = defineEmits<{
  toggle: [id: number, enabled: boolean]
  inspectDelete: [config: SourceConfig]
  cancelDelete: []
  confirmDelete: []
  refresh: []
}>()

const enabledCount = computed(() => props.configs.filter((config) => config.Enabled).length)
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}
const impactMetrics = computed(() => {
  const impact = props.deleteImpact
  if (!impact) return []
  return [
    { label: 'Provider', value: impact.ProviderCount },
    { label: 'Parser', value: impact.ParserCount },
    { label: 'source_items', value: impact.SourceItemCount },
    { label: 'play_sources', value: impact.PlaySourceCount },
    { label: 'Artifacts', value: impact.RuntimeArtifactCount },
    { label: '审计保留', value: impact.RuntimeInvocationCount },
    { label: '受影响在线虚拟库', value: impact.AffectedLibraryViewCount },
  ]
})

const columns: DataTableColumns<SourceConfig> = [
  { title: '名称', key: 'Name', minWidth: 180 },
  {
    title: '状态',
    key: 'Enabled',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: row.Enabled ? 'success' : undefined }, { default: () => row.Enabled ? '启用' : '停用' })
    },
  },
  {
    title: '导入状态',
    key: 'ImportStatus',
    width: 120,
    render(row) {
      return row.ImportStatus || '-'
    },
  },
  { title: '来源', key: 'SourceURL', minWidth: 220, ellipsis: { tooltip: true }, render: (row) => row.SourceURL || '-' },
  { title: '更新时间', key: 'UpdatedAt', width: 170, render: (row) => formatTime(row.UpdatedAt || row.ImportedAt) },
  {
    title: '操作',
    key: 'actions',
    width: 170,
    render(row) {
      return h('div', { class: 'row-actions' }, [
        h(NButton, {
          size: 'small',
          quaternary: true,
          loading: props.action === `toggle:${row.ID}`,
          onClick: () => emit('toggle', row.ID, !row.Enabled),
        }, { default: () => row.Enabled ? '停用' : '启用' }),
        h(NButton, {
          size: 'small',
          quaternary: true,
          type: 'error',
          loading: props.deleteLoading && props.deleteTarget?.ID === row.ID,
          onClick: () => emit('inspectDelete', row),
        }, { default: () => '删除' }),
      ])
    },
  },
]

function formatTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">配置包</h2>
        <p class="panel-subtitle">{{ enabledCount }}/{{ configs.length }} 个启用；删除前会先展示站点、Parser、在线虚拟库与审计影响。</p>
      </div>
      <NButton quaternary size="small" @click="emit('refresh')">刷新</NButton>
    </div>

    <NDataTable v-if="configs.length > 0" :columns="columns" :data="configs" :pagination="tablePagination" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无来源配置，先导入 TVBox 或 CMS 源清单。</div>

    <section v-if="deleteTarget" class="impact-panel" aria-live="polite">
      <div class="impact-head">
        <div>
          <h3 class="impact-title">删除影响确认</h3>
          <p class="panel-subtitle">将删除配置“{{ deleteTarget.Name }}”，并清理其 Provider/Parser；运行时调用审计只保留脱敏记录。</p>
        </div>
        <NTag type="error" size="small">需二次确认</NTag>
      </div>

      <div v-if="deleteImpact" class="impact-grid">
        <div v-for="metric in impactMetrics" :key="metric.label" class="impact-metric">
          <span>{{ metric.label }}</span>
          <strong>{{ metric.value }}</strong>
        </div>
      </div>
      <div v-else class="empty-state compact">正在加载影响摘要...</div>

      <div v-if="deleteImpact?.AffectedLibraryViews?.length" class="view-impact-list">
        <div v-for="view in deleteImpact.AffectedLibraryViews" :key="view.ID" class="view-impact-row">
          <span>{{ view.DisplayName || view.Name }}</span>
          <NTag size="small" type="warning">移除 Provider {{ view.RemovedProviderIDs.length }}</NTag>
        </div>
      </div>

      <div class="danger-copy">
        删除后不会写入本地 `items`，但该配置下在线数据、播放来源、Provider 选择引用会被清理。请确认影响摘要后再继续。
      </div>
      <div class="confirm-actions">
        <NButton @click="emit('cancelDelete')">取消</NButton>
        <NButton type="error" :loading="deleteLoading" :disabled="!deleteImpact" @click="emit('confirmDelete')">确认删除配置</NButton>
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
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.panel-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}

.panel-subtitle,
.empty-state {
  color: var(--app-text-muted);
  font-size: 13px;
}

.panel-subtitle {
  margin: 4px 0 0;
}

.empty-state {
  border: 1px dashed var(--app-border);
  border-radius: 8px;
  padding: 18px;
}

.empty-state.compact {
  padding: 12px;
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.impact-panel {
  display: grid;
  gap: 12px;
  border: 1px solid color-mix(in srgb, #d03050 42%, var(--app-border));
  border-radius: 8px;
  background: color-mix(in srgb, #d03050 5%, var(--app-surface-1));
  padding: 14px;
}

.impact-head,
.confirm-actions {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.impact-title {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
}

.impact-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

.impact-metric {
  display: grid;
  gap: 4px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 10px;
  font-size: 12px;
}

.impact-metric span,
.danger-copy {
  color: var(--app-text-muted);
}

.impact-metric strong {
  font-size: 18px;
}

.view-impact-list {
  display: grid;
  gap: 6px;
}

.view-impact-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border-bottom: 1px solid var(--app-border);
  padding-bottom: 6px;
  font-size: 13px;
}

.danger-copy {
  font-size: 13px;
}

@media (max-width: 760px) {
  .panel-head {
    flex-direction: column;
  }

  .impact-head,
  .confirm-actions {
    flex-direction: column;
  }

  .impact-grid {
    grid-template-columns: 1fr;
  }
}
</style>
