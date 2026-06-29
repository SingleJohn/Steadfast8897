<script setup lang="ts">
import { computed } from 'vue'
import { NDrawer, NDrawerContent, NEmpty, NSpin, NTabPane, NTabs, NTag } from 'naive-ui'
import type {
  SourceProviderDiagnoseResult,
  SourceProviderHomeProfile,
  SourceProviderHomeProfileSlice,
} from '@/api/source'
import { runtimeKindLabel } from '../sourceGlossary'

const props = defineProps<{
  show: boolean
  tab: string
  loading: boolean
  providerName: string
  diagnosis: SourceProviderDiagnoseResult | null
  homeProfile: SourceProviderHomeProfile | null
  categories: Array<{ id: string; name: string }>
  searchResult: any
}>()

function posterStyle(url?: string) {
  if (!url) return {}
  return { backgroundImage: `url("${url}")` }
}

const emit = defineEmits<{
  'update:show': [value: boolean]
  'update:tab': [value: string]
}>()

const searchPageItems = computed<any[]>(() => props.searchResult?.page?.items || [])

// 各 tab 的一句话摘要，显示在抽屉头部
const summary = computed(() => {
  if (props.tab === 'diagnose' && props.diagnosis) {
    return `${runtimeKindLabel(props.diagnosis.runtime_kind)} · ${props.diagnosis.duration_ms} ms · ${props.diagnosis.overall_status}`
  }
  if (props.tab === 'home' && props.homeProfile) {
    return `分类 ${props.homeProfile.categories.length} · 首页条目 ${props.homeProfile.home_items.length}`
  }
  if (props.tab === 'categories') {
    return `分类 ${props.categories.length} 个`
  }
  if (props.tab === 'search' && props.searchResult?.page) {
    return `结果 ${searchPageItems.value.length} · 入库 ${props.searchResult.items?.length || 0}`
  }
  return ''
})

function diagnoseStatusType(status: string) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'error'
  if (status === 'unsupported') return 'warning'
  return undefined
}

function diagnoseMethodLabel(method: string) {
  if (method === 'home') return 'homeContent'
  if (method === 'homeVideo') return 'homeVideoContent'
  if (method === 'category') return '分类'
  if (method === 'search') return '搜索'
  if (method === 'detail') return '详情'
  return method
}

function homeProfileSourceLabel(value: string) {
  if (value === 'homeVideoContent') return 'homeVideoContent'
  if (value === 'homeContent') return 'homeContent'
  return value || '-'
}

function homeProfileSliceLabel(slice: SourceProviderHomeProfileSlice) {
  if (slice.method === 'homeVideo') return 'homeVideoContent'
  if (slice.method === 'home') return 'homeContent'
  return slice.method
}

function homeProfileMessage(slice: SourceProviderHomeProfileSlice) {
  const parts = []
  if (slice.error_type) parts.push(slice.error_type)
  if (slice.error_message) parts.push(slice.error_message)
  return parts.join(': ')
}
</script>

<template>
  <NDrawer
    :show="show"
    :width="640"
    placement="right"
    @update:show="emit('update:show', $event)"
  >
    <NDrawerContent closable>
      <template #header>
        <div class="drawer-head">
          <span class="drawer-title">站点排障结果</span>
          <span v-if="providerName" class="drawer-provider">{{ providerName }}</span>
          <span v-if="summary" class="drawer-summary">{{ summary }}</span>
        </div>
      </template>

      <NTabs :value="tab" type="line" animated @update:value="emit('update:tab', $event)">
        <!-- FongMi 兼容诊断 -->
        <NTabPane name="diagnose" tab="兼容诊断">
          <div v-if="loading && !diagnosis" class="loading-state"><NSpin size="small" /><span>正在诊断…</span></div>
          <section v-else-if="diagnosis" class="block" aria-live="polite">
            <div class="block-head">
              <p class="muted">
                {{ diagnosis.provider_name }} · {{ runtimeKindLabel(diagnosis.runtime_kind) }} · {{ diagnosis.duration_ms }} ms
              </p>
              <NTag size="small" :type="diagnoseStatusType(diagnosis.overall_status)">{{ diagnosis.overall_status }}</NTag>
            </div>
            <div class="note">
              FongMi 首页海报墙可能来自 homeVideoContent；homeContent 为空不一定代表源坏。分类、首页与搜索应分开判断，本诊断不会改变探活状态或写入在线缓存。
            </div>
            <div class="card-grid">
              <article v-for="item in diagnosis.results" :key="item.method" class="card">
                <div class="card-head">
                  <strong>{{ diagnoseMethodLabel(item.method) }}</strong>
                  <NTag size="small" :type="diagnoseStatusType(item.status)">{{ item.status }}</NTag>
                </div>
                <div class="metrics">
                  <span>{{ item.latency_ms }} ms</span>
                  <span>class {{ item.categories_count }}</span>
                  <span>filters {{ item.filters_count }}</span>
                  <span>list {{ item.items_count }}</span>
                </div>
                <p v-if="item.message" class="msg">{{ item.error_type ? `${item.error_type}: ` : '' }}{{ item.message }}</p>
                <div v-if="item.sample_items?.length" class="sample-list">
                  <div v-for="sample in item.sample_items" :key="`${item.method}:${sample.source_item_id || sample.title}`" class="sample-row">
                    <span class="sample-title">{{ sample.title || sample.source_item_id || '-' }}</span>
                    <span class="muted">{{ sample.item_type || '-' }}<template v-if="sample.year"> · {{ sample.year }}</template></span>
                  </div>
                </div>
              </article>
            </div>
          </section>
          <NEmpty v-else description="点击站点行的“诊断”后在此查看 FongMi 兼容诊断结果。" />
        </NTabPane>

        <!-- 首页画像 -->
        <NTabPane name="home" tab="首页画像">
          <div v-if="loading && !homeProfile" class="loading-state"><NSpin size="small" /><span>正在拉取首页内容…</span></div>
          <section v-else-if="homeProfile" class="block" aria-live="polite">
            <div class="block-head">
              <p class="muted">
                {{ runtimeKindLabel(homeProfile.runtime_kind) }} · 列表来源 {{ homeProfileSourceLabel(homeProfile.home_item_source) }}
                · 分类 {{ homeProfile.categories.length }} · 首页 {{ homeProfile.home_items.length }}
              </p>
              <NTag size="small" type="info">read-only</NTag>
            </div>
            <div v-if="homeProfile.categories.length" class="chips">
              <NTag v-for="cat in homeProfile.categories" :key="cat.id" size="small" round>{{ cat.name }}</NTag>
            </div>
            <div v-if="homeProfile.home_items.length" class="poster-wall">
              <article
                v-for="item in homeProfile.home_items"
                :key="item.source_item_id || item.title"
                class="poster-card"
              >
                <div class="poster" :class="{ empty: !item.poster_url }" :style="posterStyle(item.poster_url)">
                  <span v-if="!item.poster_url">{{ item.item_type || '—' }}</span>
                </div>
                <div class="poster-title" :title="item.title || item.source_item_id">{{ item.title || item.source_item_id || '-' }}</div>
                <div class="poster-meta">{{ item.item_type || '' }}<template v-if="item.year"> · {{ item.year }}</template></div>
              </article>
            </div>
            <NEmpty v-else size="small" description="该站点首页未返回内容条目；可看上面的分类，或用「抓取入库」按分类填充。" />
            <details class="diag-fold">
              <summary>首页来源诊断</summary>
              <div class="card-grid">
                <article
                  v-for="slice in [homeProfile.sources.home_content, homeProfile.sources.home_video_content]"
                  :key="slice.method"
                  class="card"
                >
                  <div class="card-head">
                    <strong>{{ homeProfileSliceLabel(slice) }}</strong>
                    <NTag size="small" :type="diagnoseStatusType(slice.status)">{{ slice.status }}</NTag>
                  </div>
                  <div class="metrics">
                    <span>{{ slice.duration_ms }} ms</span>
                    <span>class {{ slice.categories_count }}</span>
                    <span>list {{ slice.items_count }}</span>
                  </div>
                  <p v-if="homeProfileMessage(slice)" class="msg">{{ homeProfileMessage(slice) }}</p>
                </article>
              </div>
            </details>
          </section>
          <NEmpty v-else description="点击站点行的「首页」图标，在此查看首页内容墙（只读，不写入在线缓存）。" />
        </NTabPane>

        <!-- 分类 -->
        <NTabPane name="categories" tab="分类">
          <div v-if="loading && !categories.length" class="loading-state"><NSpin size="small" /><span>正在拉取分类…</span></div>
          <section v-else-if="categories.length" class="block">
            <p class="muted">共 {{ categories.length }} 个分类。点站点行的「抓取入库」可把这些分类的内容批量填进在线虚拟库。</p>
            <div class="chips">
              <NTag v-for="cat in categories" :key="cat.id" size="small" round>{{ cat.name }}</NTag>
            </div>
          </section>
          <NEmpty v-else description="点击站点行的「分类」图标，在此查看该站点的分类列表。" />
        </NTabPane>

        <!-- 搜索测试 -->
        <NTabPane name="search" tab="搜索测试">
          <div v-if="loading && !searchResult" class="loading-state"><NSpin size="small" /><span>正在搜索…</span></div>
          <section v-else-if="searchResult?.page" class="block">
            <div class="metrics">
              <span>页码 {{ searchResult.page.page }}</span>
              <span>结果 {{ searchPageItems.length }}</span>
              <span>入库 {{ searchResult.items?.length || 0 }}</span>
            </div>
            <div v-if="searchPageItems.length" class="poster-wall">
              <article v-for="(item, idx) in searchPageItems" :key="item.source_item_id || item.title || idx" class="poster-card">
                <div class="poster" :class="{ empty: !item.poster_url }" :style="posterStyle(item.poster_url)">
                  <span v-if="!item.poster_url">{{ item.item_type || '—' }}</span>
                </div>
                <div class="poster-title" :title="item.title || item.source_item_id">{{ item.title || item.source_item_id || '-' }}</div>
                <div class="poster-meta">{{ item.item_type || '' }}<template v-if="item.year"> · {{ item.year }}</template></div>
              </article>
            </div>
            <NEmpty v-else size="small" description="无结果" />
          </section>
          <NEmpty v-else description="在下方「站点排障」选择站点、填关键词并点「搜索测试」，在此查看返回结果。" />
        </NTabPane>
      </NTabs>
    </NDrawerContent>
  </NDrawer>
</template>

<style scoped>
.drawer-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 10px;
}
.drawer-title {
  font-size: 15px;
  font-weight: 700;
}
.drawer-provider {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
}
.drawer-summary {
  color: var(--app-text-muted);
  font-size: 12px;
}
.block {
  display: grid;
  gap: 12px;
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.muted {
  margin: 0;
  color: var(--app-text-muted);
  font-size: 12px;
}
.note {
  border-left: 3px solid rgba(59, 130, 246, 0.45);
  padding-left: 10px;
  color: var(--app-text-muted);
  font-size: 12px;
}
.card-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.card {
  display: grid;
  gap: 8px;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  background: var(--app-surface-2);
  padding: 10px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--app-text-muted);
  font-size: 12px;
}
.msg {
  margin: 0;
  color: var(--app-text-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.loading-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 48px 0;
  color: var(--app-text-muted);
  font-size: 13px;
}
/* FongMi 风格首页/搜索海报墙 */
.poster-wall {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
  gap: 12px;
}
.poster-card {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.poster-card .poster {
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: 8px;
  background-color: var(--app-surface-2);
  background-position: center;
  background-size: cover;
  display: grid;
  place-items: center;
  color: var(--app-text-muted);
  font-size: 12px;
}
.poster-card .poster.empty {
  border: 1px dashed var(--app-border);
}
.poster-title {
  min-width: 0;
  overflow: hidden;
  font-size: 12px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.poster-meta {
  color: var(--app-text-muted);
  font-size: 11px;
}
.diag-fold {
  margin-top: 4px;
}
.diag-fold summary {
  cursor: pointer;
  color: var(--app-text-muted);
  font-size: 12px;
  margin-bottom: 8px;
}
.sample-list {
  display: grid;
  gap: 5px;
}
.sample-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  border-top: 1px solid var(--app-border);
  padding-top: 5px;
  font-size: 12px;
}
.sample-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 700px) {
  .card-grid {
    grid-template-columns: 1fr;
  }
}
</style>
