<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NSkeleton, useMessage } from 'naive-ui'
import {
  getItem,
  getItems,
  scrapeItemByTmdbId,
  scrapeItemMetadata,
  searchTmdbForItem,
  toggleFavorite,
  togglePlayed,
} from '../api/client'
import { useAuth } from '../composables/useAuth'
import BackdropGallery from './item-detail/components/BackdropGallery.vue'
import BackdropLightbox from './item-detail/components/BackdropLightbox.vue'
import CastCarousel from './item-detail/components/CastCarousel.vue'
import CustomScrapeModal from './item-detail/components/CustomScrapeModal.vue'
import DetailHero from './item-detail/components/DetailHero.vue'
import DetailOverview from './item-detail/components/DetailOverview.vue'
import EpisodeSection from './item-detail/components/EpisodeSection.vue'
import MediaInfoSection from './item-detail/components/MediaInfoSection.vue'
import TrailerModal from './item-detail/components/TrailerModal.vue'
import type { CrewGroup, TrailerInfo } from './item-detail/types'
import { backdropSourceIdFor, backdropTagsFor, backdropUrl } from './item-detail/utils/images'

const route = useRoute()
const router = useRouter()
const { auth } = useAuth()
const message = useMessage()
const setBackdrop = inject<(url: string) => void>('setBackdrop', () => {})

const itemId = computed(() => route.params.itemId as string)
const item = ref<any>(null)
const seasons = ref<any[]>([])
const selectedSeason = ref('')
const episodes = ref<any[]>([])
const loading = ref(true)
const scraping = ref(false)

const activeBackdropIndex = ref(0)
const showBackdropPreview = ref(false)
const showTrailerModal = ref(false)
const selectedTrailerIndex = ref(0)

const showCustomScrape = ref(false)
const customTmdbId = ref<number | null>(null)
const customQuery = ref('')
const customYear = ref<number | null>(null)
const tmdbResults = ref<any[]>([])
const tmdbSearching = ref(false)
const tmdbApplying = ref<number | null>(null)
const hasSearchedTmdb = ref(false)

const forceSolidModalStyle: Record<string, string> = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

async function loadItem() {
  const id = itemId.value
  if (!id) return
  loading.value = true
  seasons.value = []
  episodes.value = []
  selectedSeason.value = ''
  activeBackdropIndex.value = 0
  showBackdropPreview.value = false
  showTrailerModal.value = false
  selectedTrailerIndex.value = 0

  try {
    const data = await getItem(id)
    item.value = data
    const tags = backdropTagsFor(data)
    if (tags.length > 0) {
      setBackdrop(backdropUrl(backdropSourceIdFor(data), 0, tags[0], 1920))
    }

    if (data.Type === 'Series') {
      const seasonData = await getItems({ ParentId: id, SortBy: 'IndexNumber', SortOrder: 'Ascending' })
      const s = seasonData.Items || []
      seasons.value = s
      if (s.length > 0) selectedSeason.value = s[0].Id
    } else if (data.Type === 'Season') {
      const epData = await getItems({ ParentId: id, SortBy: 'IndexNumber', SortOrder: 'Ascending' })
      episodes.value = epData.Items || []
    }
  } catch {
    // 保持原页面行为:详情加载失败时由 skeleton/空态承接,不额外打扰用户。
  } finally {
    loading.value = false
  }
}

watch(itemId, () => { void loadItem() }, { immediate: true })

watch(selectedSeason, (id) => {
  if (!id) return
  getItems({ ParentId: id, SortBy: 'IndexNumber', SortOrder: 'Ascending' })
    .then((d) => { episodes.value = d.Items || [] })
    .catch(() => {})
})

const hasBackdrop = computed(() => backdropTagsFor(item.value).length > 0)
const hasPoster = computed(() => !!item.value && !!(item.value.ImageTags?.Primary || item.value.SeriesPrimaryImageItemId))
const isFavorite = computed(() => !!item.value?.UserData?.IsFavorite)
const isPlayed = computed(() => !!item.value?.UserData?.Played)
const canPlay = computed(() => !!item.value && (item.value.Type === 'Movie' || item.value.Type === 'Episode'))
const backdropSourceId = computed(() => backdropSourceIdFor(item.value))
const posterId = computed(() => item.value ? item.value.SeriesPrimaryImageItemId || item.value.Id : '')
const originalTitle = computed(() => {
  const title = item.value?.OriginalTitle?.trim()
  if (!title || title === item.value?.Name) return ''
  return title
})
const titleDensity = computed(() => {
  const len = item.value?.Name?.length || 0
  if (len > 72) return 'title-compact'
  if (len > 42) return 'title-long'
  return ''
})
const backdropImages = computed(() => {
  const sourceId = backdropSourceId.value
  if (!sourceId) return []
  return backdropTagsFor(item.value).map((tag, index) => ({
    index,
    tag,
    src: backdropUrl(sourceId, index, tag, 1920),
    thumb: backdropUrl(sourceId, index, tag, 420),
  }))
})
const activeBackdropImage = computed(() => backdropImages.value[activeBackdropIndex.value] || backdropImages.value[0] || null)
const trailers = computed<TrailerInfo[]>(() => (
  (item.value?.RemoteTrailers || []) as TrailerInfo[]
).filter((t) => typeof t?.Url === 'string' && t.Url.length > 0))
const actors = computed(() => (item.value?.People || []).filter((p: any) => p.Type === 'Actor'))
const writers = computed(() => (item.value?.People || []).filter((p: any) => p.Type === 'Writer'))
const crew = computed<CrewGroup[]>(() => {
  const result: CrewGroup[] = []
  if (writers.value.length) result.push({ label: '编剧', people: writers.value })
  return result
})

async function handleFavorite() {
  if (!item.value) return
  await toggleFavorite(item.value.Id, !isFavorite.value)
  item.value = { ...item.value, UserData: { ...item.value.UserData, IsFavorite: !isFavorite.value } }
}

async function handlePlayed() {
  if (!item.value) return
  await togglePlayed(item.value.Id, !isPlayed.value)
  item.value = { ...item.value, UserData: { ...item.value.UserData, Played: !isPlayed.value } }
}

async function handleScrape() {
  if (!item.value) return
  scraping.value = true
  try {
    const res = await scrapeItemMetadata(item.value.Id)
    message.success(res.message || '已加入刷新队列')
  } catch (e: any) {
    message.error(e?.message || '刷新入队失败')
  } finally {
    scraping.value = false
  }
}

function selectBackdrop(index: number) {
  if (!backdropImages.value[index]) return
  activeBackdropIndex.value = index
  const img = backdropImages.value[index]
  if (img) setBackdrop(img.src)
}

function openBackdropPreview(index: number) {
  selectBackdrop(index)
  showBackdropPreview.value = true
}

function prevBackdrop() {
  const count = backdropImages.value.length
  if (count <= 1) return
  selectBackdrop((activeBackdropIndex.value - 1 + count) % count)
}

function nextBackdrop() {
  const count = backdropImages.value.length
  if (count <= 1) return
  selectBackdrop((activeBackdropIndex.value + 1) % count)
}

function openTrailer(index = 0) {
  if (!trailers.value.length) return
  selectedTrailerIndex.value = Math.min(Math.max(index, 0), trailers.value.length - 1)
  showTrailerModal.value = true
}

function openCustomScrape() {
  if (!item.value) return
  customQuery.value = item.value.Name || ''
  customYear.value = item.value.ProductionYear || null
  customTmdbId.value = null
  tmdbResults.value = []
  tmdbApplying.value = null
  hasSearchedTmdb.value = false
  showCustomScrape.value = true
}

async function handleTmdbIdSearch() {
  if (!item.value || !customTmdbId.value || customTmdbId.value <= 0) return
  tmdbSearching.value = true
  hasSearchedTmdb.value = true
  tmdbResults.value = []
  try {
    const res = await searchTmdbForItem(item.value.Id, { tmdbId: customTmdbId.value })
    tmdbResults.value = res.results || []
    if (!tmdbResults.value.length) message.warning('未找到对应的 TMDB 条目')
  } catch (e: any) {
    tmdbResults.value = []
    message.error(e?.message || 'TMDB ID 搜索失败')
  } finally {
    tmdbSearching.value = false
  }
}

async function handleTmdbSearch() {
  if (!item.value || !customQuery.value.trim()) return
  tmdbSearching.value = true
  hasSearchedTmdb.value = true
  tmdbResults.value = []
  try {
    const res = await searchTmdbForItem(item.value.Id, { query: customQuery.value.trim(), year: customYear.value || undefined })
    tmdbResults.value = res.results || []
    if (!tmdbResults.value.length) message.warning('未找到匹配的 TMDB 结果')
  } catch (e: any) {
    tmdbResults.value = []
    message.error(e?.message || 'TMDB 搜索失败')
  } finally {
    tmdbSearching.value = false
  }
}

async function handleApplyTmdb(tmdbId: number) {
  if (!item.value || tmdbApplying.value !== null) return
  tmdbApplying.value = tmdbId
  try {
    await scrapeItemByTmdbId(item.value.Id, tmdbId)
    showCustomScrape.value = false
    item.value = await getItem(item.value.Id)
    message.success('已应用 TMDB 元数据')
  } catch (e: any) {
    message.error(e?.message || '应用 TMDB 元数据失败')
  } finally {
    tmdbApplying.value = null
  }
}

function goPlay(id: string) {
  router.push({ name: 'player', params: { itemId: id }, query: { from: 'resume' } })
}

function goPlayFromStart(id: string) {
  router.push({ name: 'player', params: { itemId: id }, query: { from: 'start' } })
}

function goDetail(id: string) {
  router.push('/item/' + id)
}

function handleGenreClick(genreId: string, genreName: string) {
  router.push({ name: 'browse', params: { kind: 'genre', value: genreId }, query: { name: genreName } })
}

function handlePersonClick(person: any) {
  const id = String(person?.Id || '').trim()
  const name = String(person?.Name || '').trim()
  const value = id || name
  if (!value) return
  router.push(name
    ? { name: 'browse', params: { kind: 'person', value }, query: { name } }
    : { name: 'browse', params: { kind: 'person', value } })
}

function handleTagClick(tag: string) {
  const name = tag.trim()
  if (!name) return
  router.push({ name: 'browse', params: { kind: 'tag', value: name }, query: { name } })
}
</script>

<template>
  <div v-if="loading || !item" class="detail-skeleton">
    <div class="skeleton-hero">
      <n-skeleton height="100%" :style="{ borderRadius: 0 }" />
    </div>
    <div class="skeleton-body">
      <n-skeleton text style="width: 40%; margin-bottom: 12px" />
      <n-skeleton text style="width: 60%; margin-bottom: 24px" />
      <n-skeleton height="80px" style="width: 70%" />
    </div>
  </div>

  <div v-else class="detail-page">
    <div v-if="hasBackdrop" class="detail-page-bg">
      <img
        v-if="activeBackdropImage"
        :src="activeBackdropImage.src"
        alt=""
        class="detail-page-bg-img"
        width="1920"
        height="1080"
        fetchpriority="high"
        aria-hidden="true"
      />
      <div class="detail-page-bg-overlay" />
    </div>

    <DetailHero
      :item="item"
      :has-poster="hasPoster"
      :poster-id="posterId"
      :original-title="originalTitle"
      :title-density="titleDensity"
      :can-play="canPlay"
      :is-favorite="isFavorite"
      :is-played="isPlayed"
      :trailers-count="trailers.length"
      :scraping="scraping"
      :is-admin="auth.isAdmin"
      @play="goPlay(item.Id)"
      @play-from-start="goPlayFromStart(item.Id)"
      @trailer="openTrailer(0)"
      @favorite="handleFavorite"
      @played="handlePlayed"
      @scrape="handleScrape"
      @custom-scrape="openCustomScrape"
      @genre-click="handleGenreClick"
    />

    <div class="detail-body">
      <DetailOverview :item="item" :crew="crew" @tag-click="handleTagClick" />
      <BackdropGallery
        :item-name="item.Name"
        :images="backdropImages"
        :active-index="activeBackdropIndex"
        @preview="openBackdropPreview"
      />
      <CastCarousel :actors="actors" @person-click="handlePersonClick" />
      <EpisodeSection
        v-model:selected-season="selectedSeason"
        :item="item"
        :seasons="seasons"
        :episodes="episodes"
        @detail="goDetail"
        @play="goPlay"
      />
      <MediaInfoSection :item="item" :is-admin="auth.isAdmin" />

      <div v-if="item.Type === 'Episode' && item.SeriesName" class="back-section">
        <router-link :to="'/item/' + item.SeriesId" class="back-link">← 返回「{{ item.SeriesName }}」</router-link>
      </div>
    </div>

    <BackdropLightbox
      v-model:show="showBackdropPreview"
      :image="activeBackdropImage"
      :item-name="item.Name"
      :active-index="activeBackdropIndex"
      :total="backdropImages.length"
      @prev="prevBackdrop"
      @next="nextBackdrop"
    />

    <TrailerModal
      v-model:show="showTrailerModal"
      v-model:selected-index="selectedTrailerIndex"
      :trailers="trailers"
      :item-name="item.Name"
      :poster="activeBackdropImage?.src || ''"
    />

    <CustomScrapeModal
      v-model:show="showCustomScrape"
      v-model:custom-tmdb-id="customTmdbId"
      v-model:custom-query="customQuery"
      v-model:custom-year="customYear"
      :tmdb-results="tmdbResults"
      :tmdb-searching="tmdbSearching"
      :tmdb-applying="tmdbApplying"
      :has-searched-tmdb="hasSearchedTmdb"
      :modal-style="forceSolidModalStyle"
      @search-id="handleTmdbIdSearch"
      @search="handleTmdbSearch"
      @apply="handleApplyTmdb"
    />
  </div>
</template>

<style src="./item-detail/styles.css"></style>
