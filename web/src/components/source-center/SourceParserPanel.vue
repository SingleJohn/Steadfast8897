<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceParser } from '@/api/source'

const props = defineProps<{
  parsers: SourceParser[]
  action: string
}>()

const emit = defineEmits<{
  toggle: [id: number, enabled: boolean]
  refresh: []
}>()

const enabledCount = computed(() => props.parsers.filter((parser) => parser.Enabled).length)
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}

const columns: DataTableColumns<SourceParser> = [
  { title: '名称', key: 'Name', minWidth: 150 },
  {
    title: '类型',
    key: 'ParserType',
    width: 90,
    render(row) {
      return row.ParserType === 3 ? '嗅探' : `模板 ${row.ParserType}`
    },
  },
  {
    title: '信任',
    key: 'TrustStatus',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: row.TrustStatus === 'trusted' ? 'success' : undefined }, { default: () => row.TrustStatus || 'unverified' })
    },
  },
  {
    title: '状态',
    key: 'Status',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: row.Enabled ? 'success' : undefined }, { default: () => (row.Enabled ? '启用' : '停用') })
    },
  },
  {
    title: '最近错误',
    key: 'LastError',
    minWidth: 180,
    ellipsis: { tooltip: true },
  },
  {
    title: '操作',
    key: 'actions',
    width: 110,
    render(row) {
      return h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          loading: props.action === `toggle:${row.ID}`,
          onClick: () => emit('toggle', row.ID, !row.Enabled),
        },
        { default: () => (row.Enabled ? '停用' : '启用') },
      )
    },
  },
]
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">解析器</h2>
        <p class="panel-subtitle">{{ enabledCount }}/{{ parsers.length }} 条启用，parse=1 线路会按启用顺序尝试。</p>
      </div>
      <NButton quaternary size="small" @click="emit('refresh')">刷新</NButton>
    </div>

    <NDataTable v-if="parsers.length > 0" :columns="columns" :data="parsers" :pagination="tablePagination" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无解析器；当前播放解析仍按全局启用解析器策略执行。</div>
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
.panel-subtitle {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 13px;
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
