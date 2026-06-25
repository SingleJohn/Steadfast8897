<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceConfig } from '@/api/source'

const props = defineProps<{
  configs: SourceConfig[]
  action: string
}>()

const emit = defineEmits<{
  toggle: [id: number, enabled: boolean]
  refresh: []
}>()

const enabledCount = computed(() => props.configs.filter((config) => config.Enabled).length)

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
    width: 110,
    render(row) {
      return h(NButton, {
        size: 'small',
        quaternary: true,
        loading: props.action === `toggle:${row.ID}`,
        onClick: () => emit('toggle', row.ID, !row.Enabled),
      }, { default: () => row.Enabled ? '停用' : '启用' })
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
        <p class="panel-subtitle">{{ enabledCount }}/{{ configs.length }} 个启用；配置详情、影响预览与级联删除由后续管理入口继续补齐。</p>
      </div>
      <NButton quaternary size="small" @click="emit('refresh')">刷新</NButton>
    </div>

    <NDataTable v-if="configs.length > 0" :columns="columns" :data="configs" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无来源配置，先导入 TVBox 或 CMS 源清单。</div>
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

@media (max-width: 760px) {
  .panel-head {
    flex-direction: column;
  }
}
</style>
