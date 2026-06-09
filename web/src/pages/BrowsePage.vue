<script setup lang="ts">
import { computed, ref, watch, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty, NIcon, NSelect, NTag } from 'naive-ui'
import { FunnelOutline } from '@vicons/ionicons5'
import { getItems } from '../api/client'
import CardSkeleton from '../components/CardSkeleton.vue'
import ItemGrid from '../components/ItemGrid.vue'

type BrowseKind = 'genre' | 'person' | 'tag'
type SortOption = { label: string; value: string; defaultOrder: string }
type StatusFilter = 'all' | 'unplayed' | 'played' | 'favorite'

const PAGE_SIZE = 50

const SORTS: SortOption[] = [
  { label: '名称', value: 'SortName', defaultOrder: 'Ascending' },
  { label: '添加时间', value: 'DateCreated', defaultOrder: 'Descending' },
  { label: '年份', value: 'ProductionYear', defaultOrder: 'Descending' },
  { label: '评分', value: 'CommunityRating', defaultOrder: 'Descending' },
]

const STATUS_OPTIONS: { label: string; value: StatusFilter }[] = [
  { label: '全部', value: 'all' },
  { label: '未观看', value: 'unplayed' },
  { label: '已观看', value: 'played' },
  { label: '收藏', value: 'favorite' },
]

const route = useRoute()

const items = ref<any[]>([])
const totalCount = ref(0)
const initialLoading = ref(true)
const loadingMore = ref(false)
const loadingLock = ref(false)
const filterOpen = ref(false)
const sentinelRef = ref<HTMLDivElement | null>(null)

const sortBy = ref('SortName')
const sortOrder = ref('Ascending')
const statusFilter = ref<StatusFilter>('all')

const sortOptions = SORTS.map((sort) => ({ label: sort.label, value: sort.value }))
const allLoaded = computed(() => items.value.length >= totalCount.value && !initialLoading.value)
const hasActiveFilters = computed(() => statusFilter.value !== 'all')

const kind = computed<BrowseKind>(() => {
  const raw = String(route.params.kind || '').toLowerCase()
  if (raw === 'person' || raw === 'tag') return raw
  return 'genre'
})

const value = computed(() => String(route.params.value || '').trim())
const displayName = computed(() => {
  const q = route.query.name
  const name = typeof q === 'string' ? q : Array.isArray(q) ? q[0] : ''
  return (name || value.value).trim()
})

const kindLabel = computed(() => {
  if (kind.value === 'person') return '演员'
  if (kind.value === 'tag') return '标签'
  return '类型'
})

const pageTitle = computed(() => displayName.value || kindLabel.value)

function isUuidLike(s: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s)
}

function buildParams(startIndex: number): Record<string, string> {
  const params: Record<string, string> = {
    Recursive: 'true',
    IncludeItemTypes: 'Movie,Series',
    SortBy: sortBy.value,
    SortOrder: sortOrder.value,
    Limit: String(PAGE_SIZE),
    StartIndex: String(startIndex),
  }

  if (kind.value === 'genre') {
    if (isUuidLike(value.value)) params.GenreIds = value.value
    else params.Genres = displayName.value
  } else if (kind.value === 'person') {
    if (isUuidLike(value.value)) params.PersonIds = value.value
    else params.Person = displayName.value
    params.PersonTypes = 'Actor'
  } else if (kind.value === 'tag') {
    params.Tags = displayName.value
  }

  if (statusFilter.value === 'unplayed') params.Filters = 'IsUnplayed'
  else if (statusFilter.value === 'played') params.Filters = 'IsPlayed'
  else if (statusFilter.value === 'favorite') params.Filters = 'IsFavorite'

  return params
}

function handleSortChange(value: string) {
  const sort = SORTS.find((item) => item.value === value)
  if (!sort) return
  sortBy.value = sort.value
  sortOrder.value = sort.defaultOrder
}

function toggleSortOrder() {
  sortOrder.value = sortOrder.value === 'Ascending' ? 'Descending' : 'Ascending'
}

function clearFilters() {
  statusFilter.value = 'all'
}

function loadInitial() {
  if (!value.value && !displayName.value) return
  initialLoading.value = true
  items.value = []
  totalCount.value = 0
  getItems(buildParams(0))
    .then((data) => {
      items.value = data.Items || []
      totalCount.value = data.TotalRecordCount || 0
    })
    .catch(() => {
      items.value = []
      totalCount.value = 0
    })
    .finally(() => {
      initialLoading.value = false
    })
}

function loadMore() {
  if (loadingLock.value || items.value.length >= totalCount.value) return
  loadingLock.value = true
  loadingMore.value = true
  getItems(buildParams(items.value.length))
    .then((data) => {
      items.value = [...items.value, ...(data.Items || [])]
      totalCount.value = data.TotalRecordCount || 0
    })
    .catch(() => {})
    .finally(() => {
      loadingMore.value = false
      loadingLock.value = false
    })
}

watch([kind, value, displayName, statusFilter, sortBy, sortOrder], () => {
  loadInitial()
}, { immediate: true })

watchEffect((onCleanup) => {
  const sentinel = sentinelRef.value
  if (!sentinel) return
  const observer = new IntersectionObserver((entries) => {
    if (entries[0]?.isIntersecting && !initialLoading.value && !loadingLock.value && items.value.length < totalCount.value) {
      loadMore()
    }
  }, { rootMargin: '200px' })
  observer.observe(sentinel)
  onCleanup(() => observer.disconnect())
})
</script>

<template>
  <div class="browse-page">
    <header class="browse-header">
      <div class="browse-kicker">{{ kindLabel }}</div>
      <div class="browse-title-row">
        <div class="browse-title-wrap">
          <h1 class="browse-title">{{ pageTitle }}</h1>
          <div class="browse-meta">
            <span v-if="!initialLoading" class="browse-count">{{ totalCount }} 项</span>
          </div>
        </div>

        <div class="browse-actions">
          <n-select
            :value="sortBy"
            :options="sortOptions"
            size="small"
            class="toolbar-select"
            @update:value="handleSortChange"
          />
          <button class="action-chip" @click="toggleSortOrder" :title="sortOrder === 'Ascending' ? '升序' : '降序'">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" :style="{ transform: sortOrder === 'Descending' ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }">
              <path d="M12 5v14M5 12l7-7 7 7" />
            </svg>
            {{ sortOrder === 'Ascending' ? '升序' : '降序' }}
          </button>
          <button class="action-chip" :class="{ active: filterOpen || hasActiveFilters }" @click="filterOpen = !filterOpen">
            <n-icon :size="14"><FunnelOutline /></n-icon>
            筛选
            <span v-if="hasActiveFilters" class="filter-dot" />
          </button>
        </div>
      </div>
    </header>

    <transition name="slide-down">
      <div v-if="filterOpen" class="filter-panel">
        <div class="filter-block">
          <div class="filter-label">状态</div>
          <div class="filter-chips">
            <button
              v-for="option in STATUS_OPTIONS"
              :key="option.value"
              class="filter-chip"
              :class="{ active: statusFilter === option.value }"
              @click="statusFilter = option.value"
            >
              {{ option.label }}
            </button>
          </div>
        </div>

        <div v-if="hasActiveFilters" class="filter-footer">
          <n-tag size="small" type="info" round>筛选中</n-tag>
          <button class="clear-btn" @click="clearFilters">清除筛选</button>
        </div>
      </div>
    </transition>

    <main class="browse-content">
      <CardSkeleton v-if="initialLoading" :count="12" />
      <n-empty
        v-else-if="items.length === 0"
        :description="hasActiveFilters ? '没有找到匹配的内容' : '暂无相关内容'"
        class="browse-empty"
      />

      <template v-else>
        <ItemGrid :items="items" density="compact" />
        <div v-if="loadingMore" class="browse-loading-more">
          <CardSkeleton :count="6" />
        </div>
        <div v-if="!allLoaded" ref="sentinelRef" style="height: 1px" />
      </template>
    </main>
  </div>
</template>

<style scoped>
.browse-page {
  max-width: 1480px;
  margin: 0 auto;
  padding-top: 32px;
}

.browse-header {
  padding: 36px 8px 22px;
}

.browse-kicker {
  margin-bottom: 10px;
  color: var(--app-primary);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
}

.browse-title-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
}

.browse-title-wrap {
  min-width: 0;
}

.browse-title {
  margin: 0;
  color: var(--app-text);
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 3.25rem;
  font-weight: 900;
  line-height: 1;
  letter-spacing: 0;
}

.browse-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
}

.browse-count {
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(var(--app-primary-rgb), 0.12);
  color: var(--app-primary);
  font-size: 13px;
  font-weight: 600;
}

.browse-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.toolbar-select {
  width: 110px;
}

.action-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background: rgba(255, 255, 255, 0.06);
  color: var(--app-text);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s;
  position: relative;
}

.action-chip:hover {
  background: rgba(var(--app-primary-rgb), 0.1);
}

.action-chip.active {
  background: rgba(var(--app-primary-rgb), 0.14);
  border-color: rgba(var(--app-primary-rgb), 0.3);
}

.filter-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-primary);
  position: absolute;
  top: 4px;
  right: 6px;
}

.filter-panel {
  margin: 0 8px 24px;
  padding: 18px;
  border-radius: 12px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
}

.filter-label {
  margin-bottom: 10px;
  color: var(--app-text-muted);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.filter-chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.filter-chip {
  padding: 6px 14px;
  border: 1px solid var(--app-border);
  border-radius: 999px;
  background: transparent;
  color: var(--app-text);
  cursor: pointer;
  font-size: 13px;
  transition: background 0.2s, border-color 0.2s;
}

.filter-chip:hover {
  background: rgba(var(--app-primary-rgb), 0.08);
}

.filter-chip.active {
  background: rgba(var(--app-primary-rgb), 0.14);
  border-color: rgba(var(--app-primary-rgb), 0.3);
}

.filter-footer {
  margin-top: 16px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.clear-btn {
  border: 0;
  background: transparent;
  color: var(--app-primary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
}

.browse-content {
  padding: 0 0 24px;
}

.browse-empty {
  padding: 80px 0;
}

.browse-loading-more {
  margin-top: 24px;
}

.slide-down-enter-active,
.slide-down-leave-active {
  transition: all 0.25s ease;
}

.slide-down-enter-from,
.slide-down-leave-to {
  opacity: 0;
  transform: translateY(-12px);
}

@media (max-width: 719px) {
  .browse-page {
    padding-top: 18px;
  }

  .browse-header {
    padding: 28px 0 18px;
  }

  .browse-title-row {
    flex-direction: column;
    align-items: flex-start;
  }

  .browse-title {
    font-size: 2.25rem;
  }

  .browse-actions {
    justify-content: flex-start;
  }

  .filter-panel {
    margin-left: 0;
    margin-right: 0;
  }
}
</style>
