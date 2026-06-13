<script setup lang="ts">
import { computed, ref, watch, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty, NIcon, NSelect, NTag, useMessage } from 'naive-ui'
import { FunnelOutline, Heart, HeartOutline } from '@vicons/ionicons5'
import { getItems, getItem, getImageUrl, toggleFavorite } from '../api/client'
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
const message = useMessage()

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

// ── 演员页头部:拉取 person 详情(头像/简介/生日/三围 Tags/外链)。
// /Items/{personId} 现已返回完整 Emby person 详情;value 为 person UUID 时才取。
const personDetail = ref<any>(null)
const avatarBroken = ref(false)
const favoriteBusy = ref(false)
const showPersonHero = computed(() => kind.value === 'person' && !!personDetail.value)
const personIsFavorite = computed(() => !!personDetail.value?.UserData?.IsFavorite)

watch([kind, value], () => {
  personDetail.value = null
  avatarBroken.value = false
  favoriteBusy.value = false
  if (kind.value === 'person' && isUuidLike(value.value)) {
    getItem(value.value)
      .then((d) => { if (d && d.Type === 'Person') personDetail.value = d })
      .catch(() => {})
  }
}, { immediate: true })

const personAvatar = computed(() => {
  const d = personDetail.value
  if (!d || avatarBroken.value || !d.ImageTags?.Primary) return ''
  return getImageUrl(d.Id, 'Primary', { maxWidth: 400 })
})
const personBackdrop = computed(() => {
  const d = personDetail.value
  if (!d || !(d.BackdropImageTags?.length)) return ''
  return getImageUrl(d.Id, 'Backdrop', { maxWidth: 1280 })
})
// mdc-ng 把 <br> 当字面文本塞进 Overview。转成真正的换行(配合 CSS white-space: pre-line),
// 并剥掉其它残留 HTML 标签 —— 不用 v-html,避免 XSS。
const personOverview = computed(() => {
  const raw = String(personDetail.value?.Overview || '')
  if (!raw) return ''
  return raw
    .replace(/<br\s*\/?>/gi, '\n') // <br> / <br/> / <br /> → 换行
    .replace(/<[^>]+>/g, '')       // 去掉其它 HTML 标签
    // 剔除 mdc-ng 自动拼接的「===== 外部链接 =====」段及其后所有内容
    // (这些链接已用下方 chips 展示,重复且是噪声;有真实 bio 时保留 bio)。
    .replace(/={3,}\s*外部链接\s*={3,}[\s\S]*$/i, '')
    .replace(/\n{3,}/g, '\n\n')    // 合并多余空行
    .trim()
})
const personBirthday = computed(() => {
  const pd = personDetail.value?.PremiereDate
  return pd ? String(pd).slice(0, 10) : ''
})
const personLocations = computed<string[]>(() => personDetail.value?.ProductionLocations || [])
// mdc-ng 的 Tags 形如「罩杯: H」「身高: 151」「三围: 97/60/94」;过滤掉账号/纯链接项(已在外链里)。
const personTags = computed<string[]>(() =>
  (personDetail.value?.Tags || []).filter((t: string) => !/^账号\s*[:：]/.test(t) && !/^https?:\/\//i.test(t)),
)
const personLinks = computed<{ name: string; url: string }[]>(() => {
  const ids = personDetail.value?.ProviderIds || {}
  const out: { name: string; url: string }[] = []
  for (const [k, raw] of Object.entries(ids)) {
    const v = String(raw || '').trim()
    if (!v) continue
    switch (k.toLowerCase()) {
      case 'imdb': out.push({ name: 'IMDb', url: `https://www.imdb.com/name/${v}` }); break
      case 'tmdb': out.push({ name: 'TMDB', url: `https://www.themoviedb.org/person/${v}` }); break
      case 'twitter': out.push({ name: 'Twitter', url: `https://twitter.com/${v}` }); break
      case 'instagram': out.push({ name: 'Instagram', url: `https://www.instagram.com/${v}` }); break
      case 'xhamster': out.push({ name: 'xHamster', url: `https://xhamster.com/pornstars/${v}` }); break
    }
  }
  return out
})

async function handlePersonFavorite() {
  if (!personDetail.value || favoriteBusy.value) return
  const next = !personIsFavorite.value
  favoriteBusy.value = true
  try {
    const userData = await toggleFavorite(personDetail.value.Id, next) as any
    personDetail.value = {
      ...personDetail.value,
      UserData: {
        ...(personDetail.value.UserData || {}),
        ...(userData || {}),
        IsFavorite: userData?.IsFavorite ?? next,
      },
    }
  } catch (e: any) {
    message.error(e?.message || '收藏状态更新失败')
  } finally {
    favoriteBusy.value = false
  }
}

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
  <div class="browse-page" :class="{ 'has-person-hero': showPersonHero }">
    <section v-if="showPersonHero" class="person-hero">
      <div v-if="personBackdrop" class="person-hero-bg" :style="{ backgroundImage: `url(${personBackdrop})` }" />
      <div class="person-hero-inner">
        <div class="person-avatar">
          <img v-if="personAvatar" :src="personAvatar" :alt="personDetail.Name" @error="avatarBroken = true" />
          <svg v-else width="56" height="56" viewBox="0 0 24 24" fill="currentColor" opacity="0.3" aria-hidden="true"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>
        </div>
        <div class="person-info">
          <div class="browse-kicker">演员</div>
          <div class="person-title-row">
            <h1 class="person-name">{{ personDetail.Name }}</h1>
            <button
              type="button"
              class="person-favorite-btn"
              :class="{ active: personIsFavorite }"
              :disabled="favoriteBusy"
              :aria-pressed="personIsFavorite"
              :aria-label="personIsFavorite ? '取消收藏演员' : '收藏演员'"
              :title="personIsFavorite ? '取消收藏演员' : '收藏演员'"
              @click="handlePersonFavorite"
            >
              <n-icon :size="17">
                <component :is="personIsFavorite ? Heart : HeartOutline" />
              </n-icon>
              <span>{{ personIsFavorite ? '已收藏' : '收藏' }}</span>
            </button>
          </div>
          <div class="person-meta">
            <span v-if="personBirthday" class="meta-chip">🎂 {{ personBirthday }}</span>
            <span v-for="loc in personLocations" :key="loc" class="meta-chip">📍 {{ loc }}</span>
            <span v-if="!initialLoading" class="meta-chip meta-chip--count">{{ totalCount }} 部作品</span>
          </div>
          <div v-if="personTags.length" class="person-tags">
            <span v-for="t in personTags" :key="t" class="person-tag">{{ t }}</span>
          </div>
          <p v-if="personOverview" class="person-overview">{{ personOverview }}</p>
          <div v-if="personLinks.length" class="person-links">
            <a v-for="l in personLinks" :key="l.url" :href="l.url" target="_blank" rel="noopener" class="person-link">{{ l.name }}</a>
          </div>
        </div>
      </div>
    </section>

    <header class="browse-header" :class="{ 'browse-header--compact': showPersonHero }">
      <div v-if="!showPersonHero" class="browse-kicker">{{ kindLabel }}</div>
      <div class="browse-title-row">
        <div v-if="!showPersonHero" class="browse-title-wrap">
          <h1 class="browse-title">{{ pageTitle }}</h1>
          <div class="browse-meta">
            <span v-if="!initialLoading" class="browse-count">{{ totalCount }} 项</span>
          </div>
        </div>
        <div v-else class="browse-title-wrap" />

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

/* ── 演员页头部 ───────────────────────────── */
.has-person-hero {
  padding-top: 0;
}

.person-hero {
  position: relative;
  margin: 0 8px 8px;
  border-radius: 18px;
  overflow: hidden;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
}

.person-hero-bg {
  position: absolute;
  inset: 0;
  background-size: cover;
  background-position: center 20%;
  opacity: 0.22;
  filter: saturate(1.1);
}

.person-hero-bg::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, rgba(0, 0, 0, 0.1), var(--app-surface-2));
}

.person-hero-inner {
  position: relative;
  display: flex;
  gap: 28px;
  padding: 32px 32px 30px;
  align-items: flex-start;
}

.person-avatar {
  flex: none;
  width: 168px;
  height: 168px;
  border-radius: 50%;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.06);
  border: 3px solid rgba(255, 255, 255, 0.14);
  box-shadow: var(--app-shadow-card);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--app-text-muted);
}

.person-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.person-info {
  min-width: 0;
  flex: 1;
  padding-top: 6px;
}

.person-title-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.person-name {
  margin: 4px 0 0;
  color: var(--app-text);
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 2.6rem;
  font-weight: 900;
  line-height: 1.05;
}

.person-favorite-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 36px;
  padding: 0 15px;
  margin-top: 8px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background: rgba(255, 255, 255, 0.07);
  color: var(--app-text);
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s, color 0.2s, opacity 0.2s;
}

.person-favorite-btn:hover {
  background: rgba(var(--app-primary-rgb), 0.12);
  border-color: rgba(var(--app-primary-rgb), 0.35);
}

.person-favorite-btn.active {
  color: var(--app-primary);
  background: rgba(var(--app-primary-rgb), 0.16);
  border-color: rgba(var(--app-primary-rgb), 0.42);
}

.person-favorite-btn:disabled {
  cursor: wait;
  opacity: 0.65;
}

.person-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
}

.meta-chip {
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.07);
  border: 1px solid var(--app-border);
  color: var(--app-text);
  font-size: 13px;
  font-weight: 600;
}

.meta-chip--count {
  background: rgba(var(--app-primary-rgb), 0.14);
  border-color: rgba(var(--app-primary-rgb), 0.3);
  color: var(--app-primary);
}

.person-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  margin-top: 14px;
}

.person-tag {
  padding: 3px 10px;
  border-radius: 8px;
  background: rgba(var(--app-primary-rgb), 0.1);
  color: var(--app-text);
  font-size: 12.5px;
}

.person-overview {
  margin: 16px 0 0;
  max-width: 880px;
  color: var(--app-text-muted);
  font-size: 14px;
  line-height: 1.7;
  white-space: pre-line;
  word-break: break-word;
}

.person-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.person-link {
  padding: 5px 14px;
  border-radius: 999px;
  border: 1px solid rgba(var(--app-primary-rgb), 0.35);
  color: var(--app-primary);
  font-size: 13px;
  font-weight: 600;
  text-decoration: none;
  transition: background 0.2s;
}

.person-link:hover {
  background: rgba(var(--app-primary-rgb), 0.12);
}

.browse-header--compact {
  padding: 8px 8px 16px;
}

@media (max-width: 719px) {
  .person-hero {
    margin: 0 0 8px;
  }

  .person-hero-inner {
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 18px;
    padding: 24px 18px;
  }

  .person-avatar {
    width: 128px;
    height: 128px;
  }

  .person-name {
    font-size: 2rem;
  }

  .person-title-row {
    justify-content: center;
  }

  .person-meta,
  .person-tags,
  .person-links {
    justify-content: center;
  }

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
