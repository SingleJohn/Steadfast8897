<script setup lang="ts">
import { computed, onMounted, ref, watch, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NEmpty, NIcon, NSelect, NTag } from 'naive-ui'
import { FunnelOutline, PlayOutline, ChevronUpOutline, GridOutline } from '@vicons/ionicons5'
import { getGenres, getItem, getItems, getImageUrl, getViews } from '../api/client'
import CardSkeleton from '../components/CardSkeleton.vue'
import ItemGrid from '../components/ItemGrid.vue'

type SortOption = { label: string; value: string; defaultOrder: string }
type StatusFilter = 'all' | 'unplayed' | 'played' | 'favorite'

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

const PAGE_SIZE = 50

const route = useRoute()
const router = useRouter()
const libraryId = computed(() => route.params.libraryId as string)
const aggregateMode = computed<'movies' | 'tvshows' | null>(() => {
  if (route.name === 'movies') return 'movies'
  if (route.name === 'tvshows') return 'tvshows'
  return null
})
const aggregateParentIds = ref<string[] | null>(null)
const routeGenreIds = computed<string[]>(() => {
  const value = route.query.genre
  if (typeof value === 'string' && value) return value.split(',').filter(Boolean)
  if (Array.isArray(value)) return value.flatMap((entry) => String(entry).split(',')).filter(Boolean)
  return []
})

const items = ref<any[]>([])
const totalCount = ref(0)
const initialLoading = ref(true)
const loadingMore = ref(false)
const libraryName = ref('')
const libraryItem = ref<any>(null)
const genres = ref<any[]>([])
const selectedGenres = ref<string[]>([])
const statusFilter = ref<StatusFilter>('all')
const sortBy = ref('SortName')
const sortOrder = ref('Ascending')
const filterOpen = ref(false)
const sentinelRef = ref<HTMLDivElement | null>(null)
const loadingLock = ref(false)
const showScrollTop = ref(false)

const sortOptions = SORTS.map((sort) => ({ label: sort.label, value: sort.value }))
const allLoaded = computed(() => items.value.length >= totalCount.value && !initialLoading.value)
const hasActiveFilters = computed(() => selectedGenres.value.length > 0 || statusFilter.value !== 'all')

const hasLibraryBackdrop = computed(() => {
  if (!libraryItem.value) return false
  return libraryItem.value.BackdropImageTags?.length > 0
})

function syncGenreQuery(genres: string[]) {
  const nextQuery = { ...route.query }
  if (genres.length > 0) nextQuery.genre = genres.join(',')
  else delete nextQuery.genre
  router.replace({ query: nextQuery })
}

function buildParams(startIndex: number): Record<string, string> {
  const params: Record<string, string> = {
    SortBy: sortBy.value,
    SortOrder: sortOrder.value,
    Limit: String(PAGE_SIZE),
    StartIndex: String(startIndex),
    Recursive: 'true',
  }
  if (aggregateMode.value) {
    params.ParentIds = (aggregateParentIds.value || []).join(',')
    params.IncludeItemTypes = aggregateMode.value === 'movies' ? 'Movie' : 'Series'
  } else {
    params.ParentId = libraryId.value || ''
    params.IncludeItemTypes = 'Movie,Series'
  }
  if (selectedGenres.value.length > 0) params.GenreIds = selectedGenres.value.join(',')
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

function toggleGenre(genreId: string) {
  const nextGenres = selectedGenres.value.includes(genreId)
    ? selectedGenres.value.filter((id) => id !== genreId)
    : [...selectedGenres.value, genreId]
  selectedGenres.value = nextGenres
  syncGenreQuery(nextGenres)
}

function clearFilters() {
  selectedGenres.value = []
  statusFilter.value = 'all'
  syncGenreQuery([])
}

function loadInitial() {
  if (aggregateMode.value) {
    if (aggregateParentIds.value === null) return // 还在等 getViews
    if (aggregateParentIds.value.length === 0) {
      items.value = []
      totalCount.value = 0
      initialLoading.value = false
      return
    }
  } else if (!libraryId.value) {
    return
  }
  initialLoading.value = true
  items.value = []
  totalCount.value = 0
  getItems(buildParams(0))
    .then((data) => {
      items.value = data.Items || []
      totalCount.value = data.TotalRecordCount || 0
    })
    .catch(() => {})
    .finally(() => {
      initialLoading.value = false
    })
}

function loadMore() {
  if (loadingLock.value || items.value.length >= totalCount.value) return
  if (aggregateMode.value) {
    if (!aggregateParentIds.value || aggregateParentIds.value.length === 0) return
  } else if (!libraryId.value) {
    return
  }
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

function handleScroll() {
  showScrollTop.value = window.scrollY > 600
}

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

watch(routeGenreIds, (ids) => {
  selectedGenres.value = ids
}, { immediate: true })

watch(aggregateMode, async (mode) => {
  aggregateParentIds.value = null // 标记 pending,避免切换时旧 ids 触发脏查询
  if (!mode) return
  try {
    const res = await getViews()
    if (aggregateMode.value !== mode) return // 切得比 getViews 还快,丢弃过期结果
    const ct = mode === 'movies' ? 'movies' : 'tvshows'
    aggregateParentIds.value = (res.Items || [])
      .filter((v: any) => v.CollectionType === ct && !v.PlatformLibrary)
      .map((v: any) => v.Id)
  } catch {
    if (aggregateMode.value === mode) aggregateParentIds.value = []
  }
}, { immediate: true })

watch([libraryId, aggregateMode, aggregateParentIds, selectedGenres, statusFilter, sortBy, sortOrder], () => {
  loadInitial()
}, { deep: true, immediate: true })

watch([libraryId, aggregateMode], ([id, mode]) => {
  if (mode === 'movies') {
    libraryName.value = '电影'
    libraryItem.value = null
    return
  }
  if (mode === 'tvshows') {
    libraryName.value = '剧集'
    libraryItem.value = null
    return
  }
  if (!id) return
  getItem(id as string).then((item) => {
    libraryName.value = item.Name
    libraryItem.value = item
  }).catch(() => {})
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

onMounted(() => {
  getGenres().then((data) => {
    genres.value = data.Items || []
  }).catch(() => {})
  window.addEventListener('scroll', handleScroll, { passive: true })
})
</script>

<template>
  <div class="lib-page">
    <!-- Library Header Banner -->
    <div class="library-banner" :class="{ 'has-backdrop': hasLibraryBackdrop }">
      <div v-if="hasLibraryBackdrop" class="banner-bg">
        <img :src="getImageUrl(libraryItem.Id, 'Backdrop', 1280)" alt="" class="banner-bg-img" />
      </div>
      <div class="banner-gradient" />

      <div class="banner-content">
        <div class="banner-text">
          <h1 class="library-title">{{ libraryName || '媒体库' }}</h1>
          <div class="library-meta">
            <span v-if="!initialLoading" class="library-count">{{ totalCount }} 项</span>
          </div>
        </div>

        <div class="banner-actions">
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
    </div>

    <!-- Filter Panel -->
    <transition name="slide-down">
      <div v-if="filterOpen" class="filter-panel">
        <div class="filter-block">
          <div class="filter-label">类型</div>
          <div class="filter-chips">
            <span v-if="genres.length === 0" class="filter-empty">暂无类型数据</span>
            <button
              v-for="genre in genres"
              :key="genre.Id"
              class="filter-chip"
              :class="{ active: selectedGenres.includes(genre.Id) }"
              @click="toggleGenre(genre.Id)"
            >
              {{ genre.Name }}
            </button>
          </div>
        </div>

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

    <!-- Content -->
    <div class="library-content">
      <CardSkeleton v-if="initialLoading" :count="12" />
      <n-empty
        v-else-if="items.length === 0"
        :description="hasActiveFilters ? '没有找到匹配的内容' : '此媒体库中没有内容'"
        class="library-empty"
      />

      <template v-else>
        <ItemGrid :items="items" density="compact" />
        <div v-if="loadingMore" class="library-loading-more">
          <CardSkeleton :count="6" />
        </div>
        <div v-if="!allLoaded" ref="sentinelRef" style="height: 1px" />
      </template>
    </div>

    <!-- Scroll to top -->
    <transition name="fade-scale">
      <button v-if="showScrollTop" class="scroll-top-btn" @click="scrollToTop" aria-label="回到顶部">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
      </button>
    </transition>
  </div>
</template>

<style scoped>
.lib-page {
  position: relative;
}

/* ═══ Library Banner ═══ */
.library-banner {
  position: relative;
  margin: -56px -24px 0;
  padding: 100px 32px 28px;
  min-height: 180px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
}

.library-banner.has-backdrop {
  min-height: 260px;
}

.banner-bg {
  position: absolute;
  inset: 0;
  z-index: 0;
}

.banner-bg-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 30%;
  opacity: 0.35;
  filter: blur(2px);
}

.banner-gradient {
  position: absolute;
  inset: 0;
  z-index: 1;
  background: linear-gradient(to top, var(--app-bg) 0%, rgba(2, 6, 23, 0.5) 60%, rgba(2, 6, 23, 0.2) 100%);
}

:global(html:not(.app-dark)) .banner-gradient {
  background: linear-gradient(to top, var(--app-bg) 0%, rgba(248, 250, 252, 0.7) 60%, rgba(248, 250, 252, 0.3) 100%);
}

.banner-content {
  position: relative;
  z-index: 2;
  width: 100%;
  max-width: 1480px;
  margin: 0 auto;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
}

.banner-text { min-width: 0; }

.library-title {
  margin: 0;
  font-size: clamp(2rem, 4vw, 2.8rem);
  font-weight: 700;
  line-height: 1.1;
  color: var(--app-text);
}

.library-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
}

.library-count {
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(var(--app-primary-rgb), 0.12);
  color: var(--app-primary);
  font-size: 13px;
  font-weight: 500;
}

.banner-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.toolbar-select { width: 110px; }

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
  transition: all 0.2s;
  position: relative;
}

:global(html:not(.app-dark)) .action-chip { background: rgba(0, 0, 0, 0.04); }

.action-chip:hover { background: rgba(var(--app-primary-rgb), 0.1); }
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

/* ═══ Filter Panel ═══ */
.filter-panel {
  margin: 0 0 24px;
  padding: 20px;
  border-radius: 16px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
}

.filter-block + .filter-block { margin-top: 18px; }

.filter-label {
  margin-bottom: 10px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  font-weight: 600;
  color: var(--app-text-muted);
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
  transition: all 0.2s;
}
.filter-chip:hover { background: rgba(var(--app-primary-rgb), 0.08); }
.filter-chip.active {
  background: rgba(var(--app-primary-rgb), 0.14);
  border-color: rgba(var(--app-primary-rgb), 0.3);
  color: var(--app-text);
}

.filter-empty { color: var(--app-text-muted); font-size: 13px; }

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
  font-weight: 500;
}

/* ═══ Content ═══ */
.library-content {
  max-width: 1480px;
  margin: 0 auto;
  padding: 24px 0 0;
}

.library-empty { padding: 80px 0; }

.library-loading-more { margin-top: 24px; }

/* ═══ Scroll Top ═══ */
.scroll-top-btn {
  position: fixed;
  right: 28px;
  bottom: 28px;
  width: 44px;
  height: 44px;
  border: 1px solid var(--app-border);
  border-radius: 50%;
  background: var(--app-surface-1);
  color: var(--app-text);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(12px);
  z-index: 50;
  transition: transform 0.2s;
}
.scroll-top-btn:hover { transform: scale(1.1); }

/* ═══ Transitions ═══ */
.slide-down-enter-active, .slide-down-leave-active {
  transition: all 0.25s ease;
}
.slide-down-enter-from, .slide-down-leave-to {
  opacity: 0;
  transform: translateY(-12px);
}

.fade-scale-enter-active, .fade-scale-leave-active {
  transition: all 0.2s ease;
}
.fade-scale-enter-from, .fade-scale-leave-to {
  opacity: 0;
  transform: scale(0.8);
}

/* ═══ Responsive ═══ */
@media (max-width: 959px) {
  .library-banner {
    margin: -56px -16px 0;
    padding: 80px 16px 24px;
  }

  .banner-content {
    flex-direction: column;
    align-items: flex-start;
  }

  .banner-actions { justify-content: flex-start; }
}
</style>
