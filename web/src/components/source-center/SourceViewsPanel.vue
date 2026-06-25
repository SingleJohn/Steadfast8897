<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NCheckbox, NDataTable, NInput, NInputNumber, NSelect, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { DimensionValue, SourceProvider, SourceView, SourceViewPreview } from '@/api/source'

const props = defineProps<{
  views: SourceView[]
  providers: SourceProvider[]
  preview: SourceViewPreview | null
  previewLoading: boolean
  draft: {
    id: number | null
    Name: string
    DisplayName: string
    Dimension: string
    MatchValue: string
    MatchValues: string[]
    CollectionType: string
    ProviderIds: number[]
    SortOrder: number
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
  'update:draftProviderIds': [value: number[]]
  'update:draftCollectionType': [value: string]
  'update:draftSortOrder': [value: number]
  'update:draftEnabled': [value: boolean]
  'update:draftExpose': [value: boolean]
  'update:discoverDimension': [value: string]
  'update:discoverSearch': [value: string]
  'update:discoverSelected': [value: string[]]
  'update:coverStyle': [value: string]
  edit: [view?: SourceView]
  save: []
  preview: []
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

const providerOptions = computed(() => props.providers.map((provider) => ({
  label: `${provider.Name} · ${provider.HealthStatus || 'unknown'}`,
  value: provider.ID,
})))
const discoverOptions = computed(() => props.discoverValues.map((v) => ({
  label: `${v.Value} (${v.Count})${v.AlreadyAdded ? ' 已加入' : ''}`,
  value: v.Value,
  disabled: v.AlreadyAdded,
})))
const selectedProviders = computed(() => props.providers.filter((provider) => props.draft.ProviderIds.includes(provider.ID)))
const parserPolicyNote = 'Parser 本轮仍是全局播放解析器；在线库只限制组织视图与 Provider 范围，不让库级解析器进入播放上下文。'

const columns: DataTableColumns<SourceView> = [
  { title: '名称', key: 'DisplayName', minWidth: 160 },
  {
    title: '组织规则',
    key: 'Dimension',
    minWidth: 210,
    render(row) {
      return `${row.Dimension}: ${row.MatchValue}`
    },
  },
  {
    title: 'Provider',
    key: 'ProviderIds',
    width: 110,
    render(row) {
      return row.ProviderIds?.length ? `${row.ProviderIds.length} 个` : '全部'
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
  { title: '排序', key: 'SortOrder', width: 80 },
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

function healthType(status: string) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'error'
  return undefined
}
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">在线库构建器</h2>
        <p class="panel-subtitle">在线库是 Emby 可见的组织视图，不是配置包，也不是单个 Provider。</p>
      </div>
      <NButton @click="emit('edit')">新建</NButton>
    </div>

    <div class="builder-layout">
      <div class="builder-form">
        <div class="form-grid">
          <label class="field">
            <span class="field-label">库名称</span>
            <NInput :value="draft.Name" placeholder="例如 国产电影" @update:value="emit('update:draftName', $event)" />
          </label>
          <label class="field">
            <span class="field-label">显示名</span>
            <NInput :value="draft.DisplayName" placeholder="可选，自定义 Emby 展示名" @update:value="emit('update:draftDisplayName', $event)" />
          </label>
          <label class="field">
            <span class="field-label">维度</span>
            <NSelect :value="draft.Dimension" :options="dimensionOptions" @update:value="emit('update:draftDimension', $event)" />
          </label>
          <label class="field">
            <span class="field-label">主匹配值</span>
            <NInput :value="draft.MatchValue" placeholder="movie/CN、anime、CN 或 Provider ID" @update:value="emit('update:draftMatchValue', $event)" />
          </label>
          <label class="field">
            <span class="field-label">库类型</span>
            <NSelect :value="draft.CollectionType" :options="collectionOptions" @update:value="emit('update:draftCollectionType', $event)" />
          </label>
          <label class="field">
            <span class="field-label">排序</span>
            <NInputNumber :value="draft.SortOrder" :min="0" :step="1" @update:value="emit('update:draftSortOrder', Number($event || 0))" />
          </label>
        </div>

        <label class="field full-field">
          <span class="field-label">Provider 范围</span>
          <NSelect
            :value="draft.ProviderIds"
            multiple
            filterable
            clearable
            :options="providerOptions"
            placeholder="不选择则包含全部 Provider"
            @update:value="emit('update:draftProviderIds', $event as number[])"
          />
        </label>
        <div class="provider-health">
          <NTag v-if="selectedProviders.length === 0" size="small">全部 Provider</NTag>
          <NTag
            v-for="provider in selectedProviders"
            :key="provider.ID"
            size="small"
            :type="healthType(provider.HealthStatus)"
          >
            {{ provider.Name }} · {{ provider.HealthStatus || 'unknown' }}
          </NTag>
        </div>

        <div class="toggle-row">
          <NCheckbox :checked="draft.Enabled" @update:checked="emit('update:draftEnabled', $event)">启用</NCheckbox>
          <NCheckbox :checked="draft.ExposeToEmby" @update:checked="emit('update:draftExpose', $event)">暴露到 Emby</NCheckbox>
        </div>
        <p class="helper-text">Provider 选择会限制这个在线库收录哪些站点的数据。{{ parserPolicyNote }}</p>

        <div class="discover-row">
          <NSelect :value="discoverDimension" :options="dimensionOptions" @update:value="emit('update:discoverDimension', $event)" />
          <NInput :value="discoverSearch" placeholder="筛选维度值" clearable @update:value="emit('update:discoverSearch', $event)" />
          <NButton :loading="discoverLoading" @click="emit('discover')">发现值</NButton>
          <NSelect
            :value="discoverSelected"
            multiple
            filterable
            :options="discoverOptions"
            placeholder="选择发现值"
            @update:value="emit('update:discoverSelected', $event)"
          />
          <NButton @click="emit('applyDiscover')">填入</NButton>
        </div>

        <div class="builder-actions">
          <NButton :loading="previewLoading" @click="emit('preview')">预览命中</NButton>
          <NButton type="primary" @click="emit('save')">{{ draft.id ? '保存在线库' : '创建在线库' }}</NButton>
        </div>
      </div>

      <aside class="preview-pane" aria-label="在线库预览">
        <div class="preview-head">
          <div>
            <h3 class="section-title">命中预览</h3>
            <p class="helper-text">保存前查看命中数量、Provider 分布和样例条目。</p>
          </div>
          <strong class="preview-count">{{ preview?.item_count ?? '-' }}</strong>
        </div>

        <div v-if="preview" class="preview-content">
          <div class="provider-breakdown">
            <div v-for="provider in preview.providers" :key="provider.provider_id" class="provider-row">
              <span>{{ provider.provider_name || `Provider ${provider.provider_id}` }}</span>
              <NTag size="small" :type="healthType(provider.health_status)">{{ provider.health_status }}</NTag>
              <strong>{{ provider.item_count }}</strong>
            </div>
            <div v-if="preview.providers.length === 0" class="empty-state compact">暂无 Provider 命中</div>
          </div>

          <div class="sample-list">
            <article v-for="item in preview.items" :key="item.public_uuid" class="sample-item">
              <div class="sample-title">{{ item.title }}</div>
              <div class="sample-meta">
                <span>{{ item.item_type }}</span>
                <span v-if="item.year">{{ item.year }}</span>
                <span>{{ item.normalized_kind || '-' }}</span>
                <span>{{ item.region || '-' }}</span>
                <span>{{ item.provider_name || `Provider ${item.provider_id}` }}</span>
              </div>
            </article>
            <div v-if="preview.items.length === 0" class="empty-state compact">暂无样例条目</div>
          </div>
        </div>
        <div v-else class="empty-state">填写维度与匹配值后点击“预览命中”。</div>
      </aside>
    </div>

    <NDataTable v-if="views.length > 0" :columns="columns" :data="views" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无在线虚拟库；可用维度发现后创建后台库，是否暴露给 Emby 由开关控制。</div>

    <div v-if="coverTargetId" class="cover-bar">
      <NSelect :value="coverStyle" :options="coverStyleOptions" placeholder="封面风格" @update:value="emit('update:coverStyle', $event)" />
      <NButton type="primary" :loading="generatingCover" @click="emit('confirmCover')">生成封面</NButton>
      <NButton quaternary @click="emit('restoreCover', coverTargetId)">清除封面</NButton>
    </div>
  </section>
</template>

<style scoped>
.source-panel,
.preview-pane {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
}

.source-panel {
  display: grid;
  gap: 16px;
  padding: 16px;
}

.panel-head,
.preview-head,
.builder-actions,
.toggle-row,
.provider-health {
  display: flex;
  gap: 12px;
}

.panel-head,
.preview-head {
  justify-content: space-between;
}

.panel-head {
  align-items: flex-start;
}

.panel-title,
.section-title {
  margin: 0;
  font-weight: 700;
}

.panel-title {
  font-size: 16px;
}

.section-title {
  font-size: 14px;
}

.panel-subtitle,
.helper-text,
.field-label,
.sample-meta,
.empty-state {
  color: var(--app-text-muted);
  font-size: 13px;
}

.panel-subtitle,
.helper-text {
  margin: 4px 0 0;
}

.builder-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(320px, 0.75fr);
  gap: 16px;
}

.builder-form {
  display: grid;
  gap: 14px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.full-field {
  max-width: none;
}

.field-label {
  font-weight: 700;
}

.provider-health {
  flex-wrap: wrap;
}

.toggle-row {
  flex-wrap: wrap;
  align-items: center;
}

.discover-row,
.cover-bar {
  display: grid;
  gap: 10px;
}

.discover-row {
  grid-template-columns: 180px minmax(140px, 1fr) auto minmax(180px, 1fr) auto;
}

.cover-bar {
  grid-template-columns: minmax(200px, 320px) auto auto;
}

.builder-actions {
  justify-content: flex-end;
}

.preview-pane {
  display: grid;
  align-content: start;
  gap: 12px;
  padding: 14px;
}

.preview-count {
  font-size: 28px;
  line-height: 1;
}

.preview-content,
.provider-breakdown,
.sample-list {
  display: grid;
  gap: 10px;
}

.provider-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  border-bottom: 1px solid var(--app-border);
  padding-bottom: 8px;
  font-size: 13px;
}

.provider-row:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}

.sample-item {
  min-width: 0;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  padding: 10px;
}

.sample-title {
  overflow: hidden;
  font-size: 13px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sample-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 5px;
}

.empty-state {
  border: 1px dashed var(--app-border);
  border-radius: 8px;
  padding: 18px;
}

.empty-state.compact {
  padding: 12px;
}

@media (max-width: 1100px) {
  .builder-layout,
  .form-grid,
  .discover-row,
  .cover-bar {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 700px) {
  .panel-head,
  .preview-head,
  .builder-actions {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
