<script setup lang="ts">
import { ref, watch, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty } from 'naive-ui'
import { getItems } from '../api/client'
import ItemGrid from '../components/ItemGrid.vue'
import CardSkeleton from '../components/CardSkeleton.vue'

type FilterType = 'all' | 'Movie' | 'Series' | 'Episode'

const tabs: { key: FilterType; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'Movie', label: '电影' },
  { key: 'Series', label: '剧集' },
  { key: 'Episode', label: '单集' },
]

const route = useRoute()

function qFromRoute(): string {
  const q = route.query.q
  if (typeof q === 'string') return q
  if (Array.isArray(q)) return q[0] || ''
  return ''
}

const query = ref(qFromRoute())
const results = ref<any[]>([])
const loading = ref(false)
const searched = ref(false)
const activeTab = ref<FilterType>('all')

async function doSearch(term: string) {
  if (!term.trim()) { results.value = []; searched.value = false; return }
  loading.value = true; searched.value = true
  try {
    const data = await getItems({
      SearchTerm: term, Recursive: 'true',
      IncludeItemTypes: 'Movie,Series,Episode',
      Limit: '60', SortBy: 'SortName', SortOrder: 'Ascending',
    })
    results.value = data.Items || []
  } catch { results.value = [] } finally { loading.value = false }
}

watch(() => route.query.q, (q) => {
  const s = typeof q === 'string' ? q : Array.isArray(q) ? q[0] || '' : ''
  query.value = s
  if (s) doSearch(s)
  else { results.value = []; searched.value = false }
})

onMounted(() => { if (query.value) doSearch(query.value) })

const filtered = computed(() =>
  activeTab.value === 'all' ? results.value : results.value.filter(r => r.Type === activeTab.value)
)

const tabCounts = computed(() => {
  const counts: Record<string, number> = { all: results.value.length }
  for (const t of tabs) {
    if (t.key !== 'all') counts[t.key] = results.value.filter(r => r.Type === t.key).length
  }
  return counts
})
</script>

<template>
  <div class="search-page">
    <n-empty
      v-if="!searched && !loading"
      description="请在顶部搜索框输入关键词"
      style="padding: 80px 20px"
    />

    <div v-if="searched" class="search-toolbar">
      <div class="search-tabs">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="search-tab"
          :class="{ active: activeTab === tab.key }"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
          <span v-if="tabCounts[tab.key] > 0" class="search-tab-count">{{ tabCounts[tab.key] }}</span>
        </button>
      </div>
    </div>

    <CardSkeleton v-if="loading" :count="12" />

    <n-empty v-if="searched && !loading && filtered.length === 0" description="未找到相关结果" style="padding: 60px 20px" />

    <template v-if="!loading && filtered.length > 0">
      <div class="search-result-count">找到 {{ filtered.length }} 个结果</div>
      <ItemGrid :items="filtered" />
    </template>
  </div>
</template>

<style scoped>
.search-page {
  max-width: 1480px;
  margin: 0 auto;
  padding-top: 18px;
}

.search-toolbar {
  position: sticky;
  top: 68px;
  z-index: 8;
  padding: 12px 8px;
  margin: 0 -8px 20px;
  background: var(--app-surface-1);
  backdrop-filter: blur(24px);
  border-bottom: 1px solid var(--app-border);
}

.search-tabs {
  display: flex;
  gap: 8px;
  overflow-x: auto;
}

.search-tab {
  padding: 8px 14px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: 999px;
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
  cursor: pointer;
  transition: color 0.2s, background 0.2s, border-color 0.2s;
  display: flex;
  align-items: center;
  gap: 6px;
}
.search-tab:hover { background: rgba(var(--app-primary-rgb), 0.1); }
.search-tab.active {
  color: var(--app-text);
  background: rgba(var(--app-primary-rgb), 0.14);
  border-color: rgba(var(--app-primary-rgb), 0.28);
}

.search-tab-count {
  font-size: 11px;
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  padding: 1px 6px;
  border-radius: 10px;
  color: var(--app-text-muted);
}

.search-result-count::before {
  content: '';
  display: inline-block;
  width: 1em;
  height: 0.15em;
  border-radius: 0.1em;
  background: var(--app-primary, #10b981);
  margin-right: 0.5em;
  vertical-align: middle;
}

.search-result-count {
  color: var(--app-text-muted);
  font-size: 13px;
  margin-bottom: 16px;
}

@media (max-width: 959px) {
  .search-toolbar {
    margin-left: -4px;
    margin-right: -4px;
  }
}

@media (max-width: 599px) {
  .search-toolbar {
    top: 60px;
  }
}
</style>
