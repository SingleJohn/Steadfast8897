<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDataTable, NInput, NSelect, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SourceProvider } from '@/api/source'

const props = defineProps<{
  providers: SourceProvider[]
  activeProviderId: number | null
  keyword: string
  searchResult: any
  categories: Array<{ id: string; name: string }>
  action: string
}>()

const emit = defineEmits<{
  'update:activeProviderId': [value: number | null]
  'update:keyword': [value: string]
  toggle: [id: number, enabled: boolean]
  health: [id: number]
  categories: [id: number]
  search: []
}>()

const providerOptions = computed(() => props.providers.map((p) => ({ label: `${p.Name} (${p.SourceKey})`, value: p.ID })))

const columns: DataTableColumns<SourceProvider> = [
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
    width: 120,
    render(row) {
      const type = row.HealthStatus === 'ok' ? 'success' : row.HealthStatus === 'error' ? 'error' : 'default'
      return hTag(row.HealthStatus || 'unknown', type)
    },
  },
  {
    title: '启用',
    key: 'Enabled',
    width: 110,
    render(row) {
      return hButton(row.Enabled ? '停用' : '启用', () => emit('toggle', row.ID, !row.Enabled))
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 180,
    render(row) {
      return hActions(row)
    },
  },
]

function hTag(label: string, type: 'success' | 'error' | 'default') {
  return h(NTag, { size: 'small', type: type === 'default' ? undefined : type }, { default: () => label })
}

function hButton(label: string, onClick: () => void) {
  return h(NButton, { size: 'small', quaternary: true, onClick }, { default: () => label })
}

function hActions(row: SourceProvider) {
  return h(NSpace, { size: 4 }, {
    default: () => [
      h(NButton, { size: 'small', loading: props.action === `health:${row.ID}`, onClick: () => emit('health', row.ID) }, { default: () => '探活' }),
      h(NButton, { size: 'small', quaternary: true, loading: props.action === `categories:${row.ID}`, onClick: () => emit('categories', row.ID) }, { default: () => '分类' }),
    ],
  })
}

function runtimeLabel(value: string) {
  if (value === 'native_cms') return 'JSON CMS'
  if (value === 'js_node_drpy') return 'DRPY JS'
  if (value === 'csp_dex') return 'CSP JAR'
  return value
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

    <NDataTable v-if="providers.length > 0" :columns="columns" :data="providers" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无 Provider，先在配置页导入来源配置。</div>

    <div class="test-grid">
      <NSelect
        :value="activeProviderId"
        :options="providerOptions"
        placeholder="选择 Provider"
        clearable
        @update:value="emit('update:activeProviderId', $event)"
      />
      <NInput :value="keyword" placeholder="搜索关键词" clearable @update:value="emit('update:keyword', $event)" />
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
  gap: 10px;
  margin-top: 14px;
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
.empty-state {
  border: 1px dashed var(--app-border);
  border-radius: 8px;
  padding: 18px;
  color: var(--app-text-muted);
  font-size: 13px;
}
@media (max-width: 760px) {
  .test-grid {
    grid-template-columns: 1fr;
  }
}
</style>
