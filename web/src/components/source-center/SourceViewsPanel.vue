<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NCheckbox, NDataTable, NInput, NInputNumber, NModal, NPopconfirm, NSelect, NSpace, NTag, NTooltip } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { DimensionValue, SourceProvider, SourceView, SourceViewDimensionMeta, SourceViewPreview } from '@/api/source'

const props = defineProps<{
  views: SourceView[]
  providers: SourceProvider[]
  preview: SourceViewPreview | null
  previewLoading: boolean
  matchValueError: string
  activeDimensionMeta: SourceViewDimensionMeta | null
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
  coverStylesLoaded: boolean
  showcaseIconOptions: Array<{ label: string; value: string }>
  showcaseIcon: string
  showcaseShowPosterTitles: boolean
  showcaseShowCount: boolean
  generatingCover: boolean
  solidModalMenuProps: Record<string, any>
  forceSolidModalStyle: Record<string, string>
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
  'update:showcaseIcon': [value: string]
  'update:showcaseShowPosterTitles': [value: boolean]
  'update:showcaseShowCount': [value: boolean]
  edit: [view?: SourceView]
  save: []
  preview: []
  remove: [id: number]
  discover: []
  applyDiscover: []
  move: [index: number, delta: number]
  openCover: [id: number]
  closeCover: []
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
const coverTargetView = computed(() => props.views.find((view) => view.Id === props.coverTargetId) || null)
const parserPolicyNote = 'Parser 本轮仍是全局播放解析器；在线虚拟库只限制组织视图与站点范围，不让库级解析器进入播放上下文。'
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
}

function coverPreviewUrl(url?: string, maxWidth = 180) {
  if (!url) return ''
  return `${url}${url.includes('?') ? '&' : '?'}maxWidth=${maxWidth}&format=jpg&quality=85`
}

const columns: DataTableColumns<SourceView> = [
  {
    title: '封面',
    key: 'CoverUrl',
    width: 92,
    render(row) {
      return h('div', {
        class: ['source-cover-thumb', row.CoverUrl ? '' : 'is-empty'],
        title: row.CoverUrl ? '已生成封面' : '尚未生成封面',
      }, row.CoverUrl
        ? [h('img', { src: coverPreviewUrl(row.CoverUrl, 180), alt: `${row.DisplayName || row.Name} 封面`, loading: 'lazy' })]
        : [h('span', {}, '未生成')])
    },
  },
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
          h(NPopconfirm, {
            positiveText: '删除',
            negativeText: '取消',
            onPositiveClick: () => emit('remove', row.Id),
          }, {
            trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => '删除' }),
            default: () => `删除在线虚拟库“${row.DisplayName || row.Name}”？只删除组织视图，不删除 source_items。`,
          }),
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
        <h2 class="panel-title">在线虚拟库构建器</h2>
        <p class="panel-subtitle">在线虚拟库是 Emby 可见的组织视图，不是配置包，也不是单个站点。</p>
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
            <span class="field-label">
              维度
              <NTooltip>
                <template #trigger><span class="lbl-info" aria-label="维度说明">?</span></template>
                选择聚合方式：内容类型 / 地区 / 类型+地区 / 站点 / 自定义。维度决定“主匹配值”该怎么填。
              </NTooltip>
            </span>
            <NSelect :value="draft.Dimension" :options="dimensionOptions" @update:value="emit('update:draftDimension', $event)" />
          </label>
          <label class="field">
            <span class="field-label">
              主匹配值
              <NTooltip v-if="activeDimensionMeta">
                <template #trigger><span class="lbl-info" aria-label="主匹配值说明">?</span></template>
                {{ activeDimensionMeta.desc }}。{{ activeDimensionMeta.hint }}
              </NTooltip>
            </span>
            <NInput
              :value="draft.MatchValue"
              :placeholder="activeDimensionMeta?.placeholder || 'movie/CN、anime、CN 或 Provider ID'"
              :status="matchValueError ? 'error' : undefined"
              @update:value="emit('update:draftMatchValue', $event)"
            />
            <span v-if="matchValueError" class="field-error">{{ matchValueError }}</span>
            <div v-if="activeDimensionMeta?.examples?.length" class="example-chips">
              <span class="example-label">示例（点击填入）</span>
              <NTag
                v-for="ex in activeDimensionMeta.examples"
                :key="ex"
                size="small"
                class="example-chip"
                @click="emit('update:draftMatchValue', ex)"
              >{{ ex }}</NTag>
            </div>
          </label>
          <label class="field">
            <span class="field-label">
              库类型
              <NTooltip>
                <template #trigger><span class="lbl-info" aria-label="库类型说明">?</span></template>
                决定 Emby 客户端把该库识别为电影库、剧集库还是混合库，影响展示与刮削规则。
              </NTooltip>
            </span>
            <NSelect :value="draft.CollectionType" :options="collectionOptions" @update:value="emit('update:draftCollectionType', $event)" />
          </label>
          <label class="field">
            <span class="field-label">
              排序
              <NTooltip>
                <template #trigger><span class="lbl-info" aria-label="排序说明">?</span></template>
                数字越小越靠前，用于多个库在 Emby 首页/侧栏的展示顺序。也可在列表里用“上移/下移”调整。
              </NTooltip>
            </span>
            <NInputNumber :value="draft.SortOrder" :min="0" :step="1" @update:value="emit('update:draftSortOrder', Number($event || 0))" />
          </label>
        </div>

        <label class="field full-field">
          <span class="field-label">站点范围</span>
          <NSelect
            :value="draft.ProviderIds"
            multiple
            filterable
            clearable
            :options="providerOptions"
            placeholder="不选择则包含全部站点"
            @update:value="emit('update:draftProviderIds', $event as number[])"
          />
        </label>
        <div class="provider-health">
          <NTag v-if="selectedProviders.length === 0" size="small">全部站点</NTag>
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
          <NTooltip>
            <template #trigger>
              <NCheckbox :checked="draft.Enabled" @update:checked="emit('update:draftEnabled', $event)">启用</NCheckbox>
            </template>
            启用后该在线虚拟库在后台生效并参与聚合；停用则保留配置但不收录。
          </NTooltip>
          <NTooltip>
            <template #trigger>
              <NCheckbox :checked="draft.ExposeToEmby" @update:checked="emit('update:draftExpose', $event)">暴露到 Emby</NCheckbox>
            </template>
            开启后 Emby/Infuse 等客户端可见此库；关闭则仅后台可见，不下发给客户端。
          </NTooltip>
        </div>
        <p class="helper-text">站点选择会限制这个在线虚拟库收录哪些来源数据。{{ parserPolicyNote }}</p>

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
          <NButton type="primary" @click="emit('save')">{{ draft.id ? '保存在线虚拟库' : '创建在线虚拟库' }}</NButton>
        </div>
      </div>

      <aside class="preview-pane" aria-label="在线虚拟库预览">
        <div class="preview-head">
          <div>
            <h3 class="section-title">命中预览</h3>
            <p class="helper-text">保存前查看命中数量、站点分布和样例条目。</p>
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
            <div v-if="preview.providers.length === 0" class="empty-state compact">暂无站点命中</div>
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

    <NDataTable v-if="views.length > 0" :columns="columns" :data="views" :pagination="tablePagination" size="small" :bordered="false" />
    <div v-else class="empty-state">暂无在线虚拟库；可用维度发现后创建虚拟库，是否暴露给 Emby 由开关控制。</div>

    <NModal
      :show="!!coverTargetId"
      preset="card"
      title="生成虚拟库封面"
      :style="[forceSolidModalStyle, { width: '480px', maxWidth: '92vw' }]"
      class="solid-modal-card force-solid-modal"
      @update:show="!$event && emit('closeCover')"
    >
      <div class="cover-preview-block">
        <div v-if="coverTargetView?.CoverUrl" class="cover-preview-frame">
          <img :src="coverPreviewUrl(coverTargetView.CoverUrl, 520)" :alt="`${coverTargetView.DisplayName || coverTargetView.Name} 封面预览`" />
        </div>
        <div v-else class="cover-preview-empty">当前在线虚拟库尚未生成封面</div>
      </div>

      <div class="form-group">
        <label class="form-label">封面风格</label>
        <NSelect
          :value="coverStyle"
          :options="coverStyleOptions"
          :loading="!coverStylesLoaded"
          :menu-props="solidModalMenuProps"
          placeholder="选择风格"
          @update:value="emit('update:coverStyle', $event)"
        />
      </div>

      <div v-if="coverStyle === 'showcase'" class="batch-cover-options">
        <div class="form-group">
          <label class="form-label">预制图标</label>
          <NSelect
            :value="showcaseIcon"
            :options="showcaseIconOptions"
            :menu-props="solidModalMenuProps"
            @update:value="emit('update:showcaseIcon', $event)"
          />
        </div>
        <div class="batch-cover-checks">
          <NCheckbox :checked="showcaseShowPosterTitles" @update:checked="emit('update:showcaseShowPosterTitles', $event)">显示海报标题</NCheckbox>
          <NCheckbox :checked="showcaseShowCount" @update:checked="emit('update:showcaseShowCount', $event)">显示媒体数量</NCheckbox>
        </div>
      </div>

      <div class="setting-desc">
        只替换当前在线虚拟库的展示封面，不会修改 source_items，也不会影响站点配置。
      </div>

      <template #footer>
        <NSpace justify="space-between">
          <NPopconfirm
            v-if="coverTargetId"
            positive-text="清除"
            negative-text="取消"
            @positive-click="emit('restoreCover', coverTargetId)"
          >
            <template #trigger>
              <NButton quaternary>清除封面</NButton>
            </template>
            清除后会恢复在线虚拟库默认封面。
          </NPopconfirm>
          <span v-else></span>

          <NSpace>
            <NButton @click="emit('closeCover')">取消</NButton>
            <NButton type="primary" :loading="generatingCover" :disabled="!coverStyle || generatingCover" @click="emit('confirmCover')">
              生成
            </NButton>
          </NSpace>
        </NSpace>
      </template>
    </NModal>
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

.field-error {
  color: #d03050;
  font-size: 12px;
}

.field-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.lbl-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  border-radius: 8px;
  background: var(--app-surface-2);
  color: var(--app-text-muted);
  font-size: 11px;
  font-weight: 600;
  cursor: help;
}

.example-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 2px;
}

.example-label {
  color: var(--app-text-muted);
  font-size: 12px;
}

.example-chip {
  cursor: pointer;
}

.example-chip:hover {
  filter: brightness(1.08);
}

.provider-health {
  flex-wrap: wrap;
}

.toggle-row {
  flex-wrap: wrap;
  align-items: center;
}

.discover-row {
  display: grid;
  gap: 10px;
}

.discover-row {
  grid-template-columns: 180px minmax(140px, 1fr) auto minmax(180px, 1fr) auto;
}

.builder-actions {
  justify-content: flex-end;
}

.source-cover-thumb,
.cover-preview-frame {
  overflow: hidden;
  border: 1px solid var(--app-border);
  background: var(--app-surface-2, rgba(255,255,255,0.04));
}

.source-cover-thumb {
  display: grid;
  place-items: center;
  width: 72px;
  height: 40px;
  border-radius: 6px;
  color: var(--app-text-muted);
  font-size: 11px;
}

.source-cover-thumb img,
.cover-preview-frame img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.source-cover-thumb.is-empty {
  border-style: dashed;
}

.cover-preview-block {
  margin-bottom: 18px;
}

.cover-preview-frame {
  aspect-ratio: 16 / 9;
  border-radius: 8px;
}

.cover-preview-empty {
  display: grid;
  min-height: 120px;
  place-items: center;
  border: 1px dashed var(--app-border);
  border-radius: 8px;
  color: var(--app-text-muted);
  font-size: 13px;
}

.form-group {
  margin-bottom: 20px;
}

.form-label {
  display: block;
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
  text-transform: uppercase;
}

.setting-desc {
  color: var(--app-text-muted);
  font-size: 12px;
  margin-top: 2px;
}

.batch-cover-options {
  padding-top: 4px;
}

.batch-cover-checks {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-bottom: 12px;
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
  .discover-row {
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
