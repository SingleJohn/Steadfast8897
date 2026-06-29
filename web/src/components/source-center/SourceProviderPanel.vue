<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { NButton, NInput, NSelect } from 'naive-ui'
import type {
  SourceProvider,
  SourceProviderDiagnoseResult,
  SourceProviderHomeProfile,
  SourceProviderListOptions,
} from '@/api/source'
import ProviderControls from './provider/ProviderControls.vue'
import ProviderTable from './provider/ProviderTable.vue'
import ProviderInspectDrawer from './provider/ProviderInspectDrawer.vue'

const props = defineProps<{
  providers: SourceProvider[]
  activeProviderId: number | null
  keyword: string
  searchResult: any
  categories: Array<{ id: string; name: string }>
  diagnosis: SourceProviderDiagnoseResult | null
  homeProfile: SourceProviderHomeProfile | null
  action: string
  selectedIds: number[]
  healthFilters: SourceProviderListOptions
  includeHidden: boolean
}>()

const emit = defineEmits<{
  'update:activeProviderId': [value: number | null]
  'update:keyword': [value: string]
  'update:selectedIds': [value: number[]]
  toggle: [id: number, enabled: boolean]
  batchEnable: []
  batchDisable: []
  batchHealth: []
  batchDelete: [ids?: number[]]
  batchEnableIds: [ids: number[]]
  batchDisableIds: [ids: number[]]
  batchHealthIds: [ids: number[]]
  batchCatalog: []
  fetchCatalog: [id: number]
  updateHealthFilters: [filters: SourceProviderListOptions]
  updateIncludeHidden: [value: boolean]
  health: [id: number]
  diagnose: [id: number]
  homeProfile: [id: number]
  categories: [id: number]
  search: []
}>()

// 受控筛选结果由 ProviderControls 计算并上抛，交给表格展示
const filtered = shallowRef<SourceProvider[]>([])

// 排障结果抽屉
const drawerOpen = shallowRef(false)
const drawerTab = shallowRef<'diagnose' | 'home' | 'categories' | 'search'>('diagnose')

const providerOptions = computed(() => props.providers.map((p) => ({ label: `${p.Name} (${p.SourceKey})`, value: p.ID })))
const activeProviderName = computed(() => {
  const p = props.providers.find((item) => item.ID === props.activeProviderId)
  return p ? `${p.Name} (${p.SourceKey})` : ''
})

function onToggle(id: number, enabled: boolean) {
  emit('toggle', id, enabled)
}

// 点击行内"诊断/首页/分类"时立即打开抽屉到对应页签并触发加载，数据到达后响应式填充。
// 不再依赖 watch 结果变化才弹出（那会出现“点了没反应、要再点一次”的问题）。
function onInspect(tab: 'diagnose' | 'home' | 'categories', id: number) {
  emit('update:activeProviderId', id)
  drawerTab.value = tab
  drawerOpen.value = true
  if (tab === 'diagnose') emit('diagnose', id)
  else if (tab === 'home') emit('homeProfile', id)
  else emit('categories', id)
}

function onSearchTest() {
  drawerTab.value = 'search'
  drawerOpen.value = true
  emit('search')
}

// 抽屉当前页签是否正在加载（用于显示加载态而非空状态）。
const inspectLoading = computed(() => /^(diagnose|home-profile|categories|search):/.test(props.action))
</script>

<template>
  <section class="source-panel">
    <div class="panel-head">
      <div>
        <h2 class="panel-title">站点管理</h2>
        <p class="panel-subtitle">站点是 TVBox/CMS 配置拆出的 Provider 运行单元，可单独启停、探活和诊断。</p>
      </div>
    </div>

    <ProviderControls
      v-if="providers.length > 0"
      :providers="providers"
      :selected-ids="selectedIds"
      :action="action"
      :health-filters="healthFilters"
      :include-hidden="includeHidden"
      @update:selected-ids="emit('update:selectedIds', $event)"
      @update:filtered="filtered = $event"
      @update-health-filters="emit('updateHealthFilters', $event)"
      @update-include-hidden="emit('updateIncludeHidden', $event)"
      @batch-enable="emit('batchEnable')"
      @batch-disable="emit('batchDisable')"
      @batch-health="emit('batchHealth')"
      @batch-delete="emit('batchDelete', $event)"
      @batch-enable-ids="emit('batchEnableIds', $event)"
      @batch-disable-ids="emit('batchDisableIds', $event)"
      @batch-health-ids="emit('batchHealthIds', $event)"
      @batch-catalog="emit('batchCatalog')"
    />

    <ProviderTable
      v-if="providers.length > 0"
      :providers="filtered"
      :selected-ids="selectedIds"
      :action="action"
      @update:selected-ids="emit('update:selectedIds', $event)"
      @toggle="onToggle"
      @health="emit('health', $event)"
      @diagnose="onInspect('diagnose', $event)"
      @home-profile="onInspect('home', $event)"
      @categories="onInspect('categories', $event)"
      @fetch-catalog="emit('fetchCatalog', $event)"
      @delete-one="emit('batchDelete', [$event])"
    />
    <div v-else class="empty-state">暂无站点，先在配置包页导入 TVBox 或 CMS 来源配置。</div>

    <div class="inspect-panel">
      <div class="inspect-head">
        <div>
          <h3 class="inspect-title">站点排障</h3>
          <p class="panel-subtitle">点击站点行的“诊断 / 首页 / 分类”，或在此选择站点做搜索测试；结果会在右侧抽屉中展示。</p>
        </div>
        <NButton
          v-if="diagnosis || homeProfile || categories.length || searchResult"
          quaternary
          @click="drawerOpen = true"
        >
          查看排障结果
        </NButton>
      </div>
      <div class="test-grid">
        <label class="field">
          <span class="field-label">目标站点</span>
          <NSelect
            :value="activeProviderId"
            :options="providerOptions"
            placeholder="选择站点"
            clearable
            @update:value="emit('update:activeProviderId', $event)"
          />
        </label>
        <label class="field">
          <span class="field-label">搜索关键词</span>
          <NInput :value="keyword" placeholder="搜索关键词" clearable @update:value="emit('update:keyword', $event)" />
        </label>
        <NButton type="primary" :loading="!!activeProviderId && action === `search:${activeProviderId}`" @click="onSearchTest">搜索测试</NButton>
      </div>
    </div>

    <ProviderInspectDrawer
      :show="drawerOpen"
      :tab="drawerTab"
      :loading="inspectLoading"
      :provider-name="activeProviderName"
      :diagnosis="diagnosis"
      :home-profile="homeProfile"
      :categories="categories"
      :search-result="searchResult"
      @update:show="drawerOpen = $event"
      @update:tab="drawerTab = $event as any"
    />
  </section>
</template>

<style scoped>
.source-panel {
  display: grid;
  gap: 14px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-1);
  padding: 16px;
}
.panel-head {
  margin-bottom: 0;
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
.inspect-panel {
  display: grid;
  gap: 10px;
  border: 1px solid var(--app-border);
  border-radius: 8px;
  background: var(--app-surface-2);
  padding: 12px;
}
.inspect-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.inspect-title {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
}
.test-grid {
  display: grid;
  grid-template-columns: minmax(220px, 0.8fr) minmax(180px, 1fr) auto;
  align-items: end;
  gap: 10px;
}
.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.field-label {
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
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
  .inspect-head {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
