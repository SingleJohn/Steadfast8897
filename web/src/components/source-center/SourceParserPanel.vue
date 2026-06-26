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
const parserSupportRows = [
  { type: '0', label: 'WebView/嗅探', status: '不支持', reason: '依赖客户端 WebView、DOM 和媒体嗅探。' },
  { type: '1', label: 'JSON 模板', status: '支持', reason: '服务端请求模板并校验解析结果 URL。' },
  { type: '2', label: '直连/免解析', status: '不走解析器', reason: '由播放源 direct/provider runtime 处理。' },
  { type: '3', label: 'Mix/Sniffer', status: '不支持', reason: '依赖 WebView 嗅探。' },
  { type: '4', label: 'Super Parse', status: '不支持', reason: '依赖壳私有能力。' },
]
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
      return parserTypeLabel(row.ParserType)
    },
  },
  {
    title: '能力',
    key: 'support',
    minWidth: 160,
    render(row) {
      const supported = row.ParserType === 1
      const reason = parserUnsupportedReason(row)
      return h('div', { class: 'support-cell' }, [
        h(NTag, { size: 'small', type: supported ? 'success' : 'warning' }, { default: () => (supported ? '服务端支持' : 'unsupported') }),
        reason ? h('span', { class: 'support-reason' }, reason) : null,
      ])
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

function parserTypeLabel(type: number) {
  if (type === 0) return 'WebView'
  if (type === 1) return 'JSON 模板'
  if (type === 2) return '直连'
  if (type === 3) return '嗅探'
  if (type === 4) return 'Super'
  return `类型 ${type}`
}

function parserUnsupportedReason(row: SourceParser) {
  if (row.ParserType === 1) return ''
  if (row.LastError) return row.LastError
  const rawReason = row.Raw?.fyms_unsupported_reason
  if (typeof rawReason === 'string') return rawReason
  if (row.ParserType === 0) return '依赖客户端 WebView/嗅探。'
  if (row.ParserType === 2) return '按直连/免解析口径处理。'
  if (row.ParserType === 3) return '依赖 WebView 嗅探。'
  if (row.ParserType === 4) return '依赖壳私有能力。'
  return '当前类型暂不支持。'
}
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

    <div class="support-matrix">
      <div v-for="row in parserSupportRows" :key="row.type" class="support-item">
        <span class="support-type">type={{ row.type }}</span>
        <span class="support-label">{{ row.label }}</span>
        <NTag size="small" :type="row.status === '支持' ? 'success' : row.status === '不支持' ? 'warning' : undefined">
          {{ row.status }}
        </NTag>
        <span class="support-reason">{{ row.reason }}</span>
      </div>
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
.support-matrix {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 8px;
  margin-bottom: 12px;
}
.support-item {
  min-height: 72px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 10px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.support-type,
.support-label {
  font-size: 12px;
  font-weight: 700;
}
.support-reason {
  color: var(--app-text-muted);
  font-size: 12px;
  line-height: 1.4;
}
.support-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
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
