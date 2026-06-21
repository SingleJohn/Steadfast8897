<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NCheckbox, NDataTable, NInput, NSelect, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { DimensionValue, SourceView } from '@/api/source'

const props = defineProps<{
  views: SourceView[]
  draft: {
    id: number | null
    Name: string
    DisplayName: string
    Dimension: string
    MatchValue: string
    MatchValues: string[]
    CollectionType: string
    Enabled: boolean
    ExposeToEmby: boolean
  }
  discoverDimension: string
  discoverSearch: string
  discoverValues: DimensionValue[]
  discoverSelected: string[]
  discoverLoading: boolean
  coverTargetId: number | null
  coverStyle: string
  coverStyleOptions: Array<{ label: string; value: string }>
  generatingCover: boolean
}>()

const emit = defineEmits<{
  'update:draftName': [value: string]
  'update:draftDisplayName': [value: string]
  'update:draftDimension': [value: string]
  'update:draftMatchValue': [value: string]
  'update:draftCollectionType': [value: string]
  'update:draftEnabled': [value: boolean]
  'update:draftExpose': [value: boolean]
  'update:discoverDimension': [value: string]
  'update:discoverSearch': [value: string]
  'update:discoverSelected': [value: string[]]
  'update:coverStyle': [value: string]
  edit: [view?: SourceView]
  save: []
  remove: [id: number]
  discover: []
  applyDiscover: []
  move: [index: number, delta: number]
  openCover: [id: number]
  confirmCover: []
  restoreCover: [id: number]
}>()

const dimensionOptions = [
  { label: '内容类型 normalized_kind', value: 'normalized_kind' },
  { label: '地区 region', value: 'region' },
  { label: '类型/地区 kind_region', value: 'kind_region' },
  { label: 'Provider', value: 'provider' },
  { label: '自定义 custom', value: 'custom' },
]
const collectionOptions = [
  { label: '混合', value: 'mixed' },
  { label: '电影', value: 'movies' },
  { label: '剧集', value: 'tvshows' },
]
const discoverOptions = computed(() => props.discoverValues.map((v) => ({
  label: `${v.Value} (${v.Count})${v.AlreadyAdded ? ' 已加入' : ''}`,
  value: v.Value,
  disabled: v.AlreadyAdded,
})))

const columns: DataTableColumns<SourceView> = [
  { title: '名称', key: 'DisplayName', minWidth: 160 },
  {
    title: '维度',
    key: 'Dimension',
    width: 180,
    render(row) {
      return `${row.Dimension}: ${row.MatchValue}`
    },
  },
  {
    title: '可见',
    key: 'ExposeToEmby',
    width: 110,
    render(row) {
      return h(NTag, { size: 'small', type: row.ExposeToEmby ? 'success' : undefined }, { default: () => row.ExposeToEmby ? 'Emby' : '后台' })
    },
  },
  { title: '数量', key: 'ItemCount', width: 90 },
  {
    title: '操作',
    key: 'actions',
    width: 280,
    render(row, index) {
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', onClick: () => emit('edit', row) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', quaternary: true, onClick: () => emit('move', index, -1) }, { default: () => '上移' }),
          h(NButton, { size: 'small', quaternary: true, onClick: () => emit('move', index, 1) }, { default: () => '下移' }),
          h(NButton, { size: 'small', quaternary: true, onClick: () => emit('openCover', row.Id) }, { default: () => '封面' }),
          h(NButton, { size: 'small', quaternary: true, type: 'error', onClick: () => emit('remove', row.Id) }, { default: () => '删除' }),
        ],
      })
    },
  },
]
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">在线虚拟库</h2>
        <p class="panel-subtitle">按归一列聚合 source_items，可控制是否暴露给 Emby 客户端。</p>
      </div>
      <NButton @click="emit('edit')">新建</NButton>
    </div>

    <div class="view-editor">
      <NInput :value="draft.Name" placeholder="库名称" @update:value="emit('update:draftName', $event)" />
      <NInput :value="draft.DisplayName" placeholder="自定义显示名" @update:value="emit('update:draftDisplayName', $event)" />
      <NSelect :value="draft.Dimension" :options="dimensionOptions" @update:value="emit('update:draftDimension', $event)" />
      <NInput :value="draft.MatchValue" placeholder="主匹配值" @update:value="emit('update:draftMatchValue', $event)" />
      <NSelect :value="draft.CollectionType" :options="collectionOptions" @update:value="emit('update:draftCollectionType', $event)" />
      <NSpace align="center">
        <NCheckbox :checked="draft.Enabled" @update:checked="emit('update:draftEnabled', $event)">启用</NCheckbox>
        <NCheckbox :checked="draft.ExposeToEmby" @update:checked="emit('update:draftExpose', $event)">暴露到 Emby</NCheckbox>
      </NSpace>
      <NButton type="primary" @click="emit('save')">{{ draft.id ? '保存在线库' : '创建在线库' }}</NButton>
    </div>

    <div class="discover-row">
      <NSelect :value="discoverDimension" :options="dimensionOptions" @update:value="emit('update:discoverDimension', $event)" />
      <NInput :value="discoverSearch" placeholder="筛选值" clearable @update:value="emit('update:discoverSearch', $event)" />
      <NButton :loading="discoverLoading" @click="emit('discover')">发现值</NButton>
      <NSelect
        :value="discoverSelected"
        multiple
        :options="discoverOptions"
        placeholder="选择发现值"
        @update:value="emit('update:discoverSelected', $event)"
      />
      <NButton @click="emit('applyDiscover')">填入表单</NButton>
    </div>

    <NDataTable :columns="columns" :data="views" size="small" :bordered="false" />

    <div v-if="coverTargetId" class="cover-bar">
      <NSelect :value="coverStyle" :options="coverStyleOptions" placeholder="封面风格" @update:value="emit('update:coverStyle', $event)" />
      <NButton type="primary" :loading="generatingCover" @click="emit('confirmCover')">生成封面</NButton>
      <NButton quaternary @click="emit('restoreCover', coverTargetId)">清除封面</NButton>
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
.view-editor,
.discover-row,
.cover-bar {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}
.discover-row {
  grid-template-columns: 180px minmax(140px, 1fr) auto minmax(180px, 1fr) auto;
}
.cover-bar {
  grid-template-columns: minmax(200px, 320px) auto auto;
  margin-top: 14px;
  margin-bottom: 0;
}
@media (max-width: 900px) {
  .view-editor,
  .discover-row,
  .cover-bar {
    grid-template-columns: 1fr;
  }
  .panel-head {
    flex-direction: column;
  }
}
</style>
