<script setup lang="ts">
import { computed, ref, watch, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSkeleton, NIcon, NSpace, NModal, NInput, NInputNumber, NSpin } from 'naive-ui'
import {
  CheckmarkDoneOutline,
  Heart,
  HeartOutline,
  PlayOutline,
  RefreshOutline,
  SearchOutline,
  FolderOpenOutline,
} from '@vicons/ionicons5'
import { getItem, getItems, getImageUrl, toggleFavorite, togglePlayed, scrapeItemMetadata, searchTmdbForItem, scrapeItemByTmdbId } from '../api/client'
import { useAuth } from '../composables/useAuth'
import QualityBadge from '@/components/QualityBadge.vue'

const route = useRoute()
const router = useRouter()
const { auth } = useAuth()

const setBackdrop = inject<(url: string) => void>('setBackdrop', () => {})

const itemId = computed(() => route.params.itemId as string)
const item = ref<any>(null)
const seasons = ref<any[]>([])
const selectedSeason = ref('')
const episodes = ref<any[]>([])
const loading = ref(true)
const scraping = ref(false)
const brokenPeopleImages = ref<Record<string, boolean>>({})

// 自定义刮削
const showCustomScrape = ref(false)
const customQuery = ref('')
const customYear = ref<number | null>(null)
const tmdbResults = ref<any[]>([])
const tmdbSearching = ref(false)
const tmdbApplying = ref<number | null>(null)
const forceSolidModalStyle = {
  '--n-color': 'var(--app-modal-solid-card)',
  '--n-color-modal': 'var(--app-modal-solid-card)',
  '--n-border-color': 'var(--app-modal-solid-border)',
  '--n-box-shadow': 'var(--app-shadow-card)',
  '--n-action-color': 'var(--app-modal-solid-soft)',
}

async function openCustomScrape() {
  if (!item.value) return
  customQuery.value = item.value.Name || ''
  customYear.value = item.value.ProductionYear || null
  tmdbResults.value = []
  tmdbApplying.value = null
  showCustomScrape.value = true
}

async function handleTmdbSearch() {
  if (!item.value || !customQuery.value.trim()) return
  tmdbSearching.value = true
  tmdbResults.value = []
  try {
    const res = await searchTmdbForItem(item.value.Id, customQuery.value.trim(), customYear.value || undefined)
    tmdbResults.value = res.results || []
  } catch (e: any) {
    tmdbResults.value = []
  } finally {
    tmdbSearching.value = false
  }
}

async function handleApplyTmdb(tmdbId: number) {
  if (!item.value) return
  tmdbApplying.value = tmdbId
  try {
    await scrapeItemByTmdbId(item.value.Id, tmdbId)
    showCustomScrape.value = false
    item.value = await getItem(item.value.Id)
  } catch { /* ignore */ } finally {
    tmdbApplying.value = null
  }
}

function splitPath(fullPath: string) {
  const idx = fullPath.lastIndexOf('/')
  if (idx < 0) return { dir: '', file: fullPath }
  return { dir: fullPath.substring(0, idx), file: fullPath.substring(idx + 1) }
}

function formatFileSize(bytes: number | null | undefined) {
  if (!bytes) return ''
  const gb = bytes / 1024 / 1024 / 1024
  return gb >= 1 ? `${gb.toFixed(2)} GB` : `${(bytes / 1024 / 1024).toFixed(0)} MB`
}

function formatBitrate(bps: number | null | undefined): string {
  if (!bps || bps <= 0) return ''
  const mbps = bps / 1_000_000
  if (mbps >= 1) return `${mbps.toFixed(mbps >= 10 ? 0 : 1)} Mbps`
  const kbps = bps / 1000
  return `${Math.round(kbps)} Kbps`
}

function streamTypeLabel(type: string): string {
  if (type === 'Video') return '视频'
  if (type === 'Audio') return '音频'
  if (type === 'Subtitle') return '字幕'
  return type
}

function formatStream(s: any): string {
  if (s?.DisplayTitle) return s.DisplayTitle
  const parts: string[] = []
  if (s?.Codec) parts.push(String(s.Codec).toUpperCase())
  if (s?.Type === 'Video') {
    if (s.Width && s.Height) parts.push(`${s.Width}×${s.Height}`)
    if (s.BitDepth) parts.push(`${s.BitDepth}-bit`)
    if (s.PixelFormat) parts.push(s.PixelFormat)
    if (s.BitRate) parts.push(formatBitrate(s.BitRate))
  } else if (s?.Type === 'Audio') {
    if (s.Channels) parts.push(`${s.Channels} 声道`)
    if (s.SampleRate) parts.push(`${Math.round(s.SampleRate / 1000)} kHz`)
    if (s.Language) parts.push(s.Language)
    if (s.BitRate) parts.push(formatBitrate(s.BitRate))
  } else if (s?.Type === 'Subtitle') {
    if (s.Language) parts.push(s.Language)
    if (s.Title) parts.push(s.Title)
  }
  return parts.join(' · ')
}

function groupedStreams(src: any): { type: string; streams: any[] }[] {
  const streams = (src?.MediaStreams || []) as any[]
  const order = ['Video', 'Audio', 'Subtitle']
  return order
    .map((type) => ({ type, streams: streams.filter((s) => s.Type === type) }))
    .filter((g) => g.streams.length > 0)
}

function formatRuntime(ticks: number): string {
  const min = Math.round(ticks / 10_000_000 / 60)
  const hr = Math.floor(min / 60)
  const m = min % 60
  if (hr > 0 && m > 0) return `${hr}小时${m}分钟`
  if (hr > 0) return `${hr}小时`
  return `${m}分钟`
}

function endTimeStr(ticks: number): string {
  const mins = Math.round(ticks / 10_000_000 / 60)
  const end = new Date(Date.now() + mins * 60000)
  return `${end.getHours().toString().padStart(2, '0')}:${end.getMinutes().toString().padStart(2, '0')}`
}

async function loadItem() {
  const id = itemId.value
  if (!id) return
  loading.value = true; seasons.value = []; episodes.value = []; selectedSeason.value = ''
  brokenPeopleImages.value = {}
  try {
    const data = await getItem(id)
    item.value = data
    const bdId = data.ParentBackdropItemId || data.Id
    if (data.BackdropImageTags?.length > 0 || data.ParentBackdropItemId) {
      setBackdrop(getImageUrl(bdId, 'Backdrop', 1920))
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
  } catch { /* ignore */ } finally { loading.value = false }
}

watch(itemId, () => loadItem(), { immediate: true })
watch(selectedSeason, (id) => {
  if (!id) return
  getItems({ ParentId: id, SortBy: 'IndexNumber', SortOrder: 'Ascending' })
    .then((d) => { episodes.value = d.Items || [] }).catch(() => {})
})

const hasBackdrop = computed(() => item.value && (item.value.BackdropImageTags?.length > 0 || item.value.ParentBackdropItemId))
const hasPoster = computed(() => item.value && (item.value.ImageTags?.Primary || item.value.SeriesPrimaryImageItemId))
const isFavorite = computed(() => item.value?.UserData?.IsFavorite)
const isPlayed = computed(() => item.value?.UserData?.Played)
const canPlay = computed(() => item.value && (item.value.Type === 'Movie' || item.value.Type === 'Episode'))
const backdropId = computed(() => item.value ? item.value.ParentBackdropItemId || item.value.Id : '')
const posterId = computed(() => item.value ? item.value.SeriesPrimaryImageItemId || item.value.Id : '')
const originalTitle = computed(() => {
  const title = item.value?.OriginalTitle?.trim()
  if (!title || title === item.value?.Name) return ''
  return title
})

const writers = computed(() => (item.value?.People || []).filter((p: any) => p.Type === 'Writer'))
const actors = computed(() => (item.value?.People || []).filter((p: any) => p.Type === 'Actor'))
const crew = computed(() => {
  const result: { label: string; people: any[] }[] = []
  if (writers.value.length) result.push({ label: '编剧', people: writers.value })
  return result
})


function personImgSrc(person: any): string {
  const imageId = person.PrimaryImageItemId || person.Id
  const imageKey = String(imageId || person.Name || '')
  if (brokenPeopleImages.value[imageKey]) return ''
  if (person.ImageUrl) return person.ImageUrl
  if (person.PrimaryImageTag || person.ImageTags?.Primary) return getImageUrl(imageId, 'Primary', 200)
  if (imageId) return getImageUrl(imageId, 'Primary', 200)
  return ''
}

function handlePersonImageError(person: any) {
  const imageKey = String(person.PrimaryImageItemId || person.Id || person.Name || '')
  if (!imageKey) return
  brokenPeopleImages.value = {
    ...brokenPeopleImages.value,
    [imageKey]: true,
  }
}

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
  try { await scrapeItemMetadata(item.value.Id); item.value = await getItem(item.value.Id) }
  catch { /* ignore */ } finally { scraping.value = false }
}

function handleGenreClick(genreId: string) {
  const libraryId = item.value?.ParentId
  if (!libraryId) return
  router.push({ name: 'library', params: { libraryId }, query: { genre: genreId } })
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
      <img :src="getImageUrl(backdropId, 'Backdrop', 1920)" :alt="item.Name" class="detail-page-bg-img" />
      <div class="detail-page-bg-overlay" />
    </div>

    <!-- ═══ Hero Backdrop Section (seerr-inspired) ═══ -->
    <div class="hero-backdrop-section">
      <div class="hero-gradient" />

      <div class="hero-content">
        <div class="hero-inner">
          <div v-if="hasPoster" class="hero-poster">
            <div class="poster-card">
              <img :src="getImageUrl(posterId, 'Primary', 400)" :alt="item.Name" class="poster-img" />
            </div>
          </div>

          <div class="hero-info">
            <router-link v-if="item.Type === 'Episode' && item.SeriesName" :to="'/item/' + item.SeriesId" class="series-link">{{ item.SeriesName }}</router-link>
            <h1 class="item-title">{{ item.Name }}</h1>
            <h2 v-if="originalTitle" class="item-original-title">{{ originalTitle }}</h2>

            <div class="meta-row">
              <span v-if="item.CommunityRating" class="meta-rating">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="#f5c518" aria-hidden="true">
                  <path d="M12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"/>
                </svg>
                <strong>{{ item.CommunityRating.toFixed(1) }}</strong>
                <span class="meta-rating-label">IMDb</span>
              </span>
              <span v-if="item.ProductionYear" class="meta-dot">•</span>
              <span v-if="item.ProductionYear" class="meta-tag">{{ item.ProductionYear }}</span>
              <span v-if="item.RunTimeTicks" class="meta-dot">•</span>
              <span v-if="item.RunTimeTicks" class="meta-tag">{{ formatRuntime(item.RunTimeTicks) }}</span>
              <span v-if="item.RunTimeTicks" class="meta-ends">结束于 {{ endTimeStr(item.RunTimeTicks) }}</span>
              <span v-if="item.OfficialRating" class="meta-cert">{{ item.OfficialRating }}</span>
              <span v-if="item.Type === 'Episode'" class="meta-tag">S{{ String(item.ParentIndexNumber || 0).padStart(2, '0') }}E{{ String(item.IndexNumber || 0).padStart(2, '0') }}</span>
            </div>

            <div class="action-row">
              <button v-if="canPlay" type="button" class="btn-play" @click="router.push('/play/' + item.Id)">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>
                {{ item.UserData?.PlaybackPositionTicks > 0 ? '继续播放' : '播放' }}
              </button>
              <button type="button" class="btn-action" :class="{ active: isFavorite }" @click="handleFavorite" title="收藏">
                <n-icon :size="16">
                  <component :is="isFavorite ? Heart : HeartOutline" />
                </n-icon>
                <span>{{ isFavorite ? '已收藏' : '收藏' }}</span>
              </button>
              <button type="button" class="btn-action" :class="{ active: isPlayed }" @click="handlePlayed" title="已播放">
                <n-icon :size="16"><CheckmarkDoneOutline /></n-icon>
                <span>{{ isPlayed ? '已播放' : '标记已播' }}</span>
              </button>
              <button v-if="auth.isAdmin" type="button" class="btn-action" :disabled="scraping" @click="handleScrape" title="刮削元数据">
                <n-icon :size="16"><RefreshOutline /></n-icon>
                <span>{{ scraping ? '刮削中' : '刮削' }}</span>
              </button>
              <button v-if="auth.isAdmin" type="button" class="btn-action" @click="openCustomScrape" title="自定义刮削">
                <n-icon :size="16"><SearchOutline /></n-icon>
                <span>自定义刮削</span>
              </button>
            </div>

            <div v-if="item.GenreItems?.length" class="genre-row">
              <button
                v-for="g in item.GenreItems"
                :key="g.Id"
                type="button"
                class="genre-chip"
                :disabled="!item.ParentId"
                @click="handleGenreClick(g.Id)"
              >
                {{ g.Name }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══ Main Content ═══ -->
    <div class="detail-body">
      <!-- Overview & Info -->
      <div class="content-grid">
        <div class="content-main">
          <p v-if="item.Tagline" class="item-tagline">{{ item.Tagline }}</p>
          <template v-if="item.Overview">
            <h3 class="section-heading section-heading-light">简介</h3>
            <p class="item-overview">{{ item.Overview }}</p>
          </template>

          <!-- Crew inline -->
          <div v-if="crew.length" class="crew-inline">
            <div v-for="group in crew" :key="group.label" class="crew-group">
              <span class="crew-label">{{ group.label }}</span>
              <span class="crew-names">{{ group.people.map(p => p.Name).join(', ') }}</span>
            </div>
          </div>

        </div>

        <!-- Facts sidebar -->
        <div class="content-facts">
          <div v-if="item.ProviderIds?.Tmdb || item.ProviderIds?.Imdb" class="facts-block">
            <h3 class="section-heading">外部链接</h3>
            <div class="ext-links">
              <a v-if="item.ProviderIds?.Tmdb" :href="`https://www.themoviedb.org/${item.Type === 'Movie' ? 'movie' : 'tv'}/${item.ProviderIds.Tmdb}`" target="_blank" rel="noopener noreferrer" class="ext-link ext-tmdb">TMDB ↗</a>
              <a v-if="item.ProviderIds?.Imdb" :href="`https://www.imdb.com/title/${item.ProviderIds.Imdb}`" target="_blank" rel="noopener noreferrer" class="ext-link ext-imdb">IMDb ↗</a>
            </div>
          </div>
        </div>
      </div>

      <!-- ═══ Cast Carousel (seerr-style horizontal scroll) ═══ -->
      <div v-if="actors.length" class="cast-section">
        <h3 class="section-heading section-heading-light">演员</h3>
        <div class="cast-scroll">
          <div v-for="p in actors.slice(0, 20)" :key="p.Name + (p.Role || '')" class="cast-card">
            <div class="cast-img">
              <img v-if="personImgSrc(p)" :src="personImgSrc(p)" :alt="p.Name" @error="handlePersonImageError(p)" />
              <svg v-else width="24" height="24" viewBox="0 0 24 24" fill="currentColor" opacity="0.3"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>
            </div>
            <div class="cast-info">
              <span class="cast-name">{{ p.Name }}</span>
              <span v-if="p.Role" class="cast-role">{{ p.Role }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- ═══ Episodes (Series) ═══ -->
      <div v-if="item.Type === 'Series'" class="episodes-section">
        <h3 class="section-heading">剧集</h3>
        <div v-if="seasons.length > 0" class="season-tabs">
          <button v-for="s in seasons" :key="s.Id" class="season-tab" :class="{ active: selectedSeason === s.Id }" @click="selectedSeason = s.Id">{{ s.Name }}</button>
        </div>
        <div v-if="episodes.length > 0" class="ep-list">
          <div v-for="ep in episodes" :key="ep.Id" class="ep-card" @click="router.push('/item/' + ep.Id)">
            <div v-if="(ep.UserData?.PlayedPercentage || 0) > 0 && (ep.UserData?.PlayedPercentage || 0) < 100" class="ep-progress" :style="{ width: (ep.UserData?.PlayedPercentage || 0) + '%' }" />
            <div class="ep-thumb-wrap">
              <img :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" />
            </div>
            <div class="ep-body">
              <div class="ep-header">
                <span class="ep-num">E{{ ep.IndexNumber || '-' }}</span>
                <span class="ep-title-text">{{ ep.Name }}</span>
                <span v-if="ep.UserData?.Played" class="ep-played-badge">✓</span>
              </div>
              <div class="ep-sub">
                <span v-if="ep.RunTimeTicks" class="ep-dur">{{ formatRuntime(ep.RunTimeTicks) }}</span>
              </div>
              <p v-if="ep.Overview" class="ep-desc">{{ ep.Overview }}</p>
            </div>
            <button class="ep-play-btn" @click.stop="router.push('/play/' + ep.Id)">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>
            </button>
          </div>
        </div>
        <p v-else class="empty-hint">暂无剧集信息</p>
      </div>

      <!-- ═══ Episodes (Season) ═══ -->
      <div v-if="item.Type === 'Season' && episodes.length > 0" class="episodes-section">
        <h3 class="section-heading">剧集</h3>
        <div class="ep-list">
          <div v-for="ep in episodes" :key="ep.Id" class="ep-card" @click="router.push('/item/' + ep.Id)">
            <div v-if="(ep.UserData?.PlayedPercentage || 0) > 0 && (ep.UserData?.PlayedPercentage || 0) < 100" class="ep-progress" :style="{ width: (ep.UserData?.PlayedPercentage || 0) + '%' }" />
            <div class="ep-thumb-wrap">
              <img :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" />
            </div>
            <div class="ep-body">
              <div class="ep-header">
                <span class="ep-num">E{{ ep.IndexNumber || '-' }}</span>
                <span class="ep-title-text">{{ ep.Name }}</span>
                <span v-if="ep.UserData?.Played" class="ep-played-badge">✓</span>
              </div>
              <div class="ep-sub">
                <span v-if="ep.RunTimeTicks" class="ep-dur">{{ formatRuntime(ep.RunTimeTicks) }}</span>
              </div>
              <p v-if="ep.Overview" class="ep-desc">{{ ep.Overview }}</p>
            </div>
            <button class="ep-play-btn" @click.stop="router.push('/play/' + ep.Id)">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>
            </button>
          </div>
        </div>
      </div>

      <!-- ═══ 媒体信息(所有用户可见:画质/编码/码率/流;路径仅管理员) ═══ -->
      <div v-if="item.MediaSources?.length" class="media-info-section">
        <h3 class="section-heading">媒体信息</h3>
        <div class="ms-list">
          <article
            v-for="(src, idx) in item.MediaSources"
            :key="src.Id || idx"
            class="ms-card"
          >
            <header class="ms-header">
              <div class="ms-title">
                <strong v-if="(item.MediaSources?.length || 0) > 1">
                  {{ src.Name || `版本 ${idx + 1}` }}
                </strong>
                <span v-if="src.Container" class="ms-container">{{ src.Container.toUpperCase() }}</span>
              </div>
              <quality-badge
                :resolution="src.FymsResolution"
                :hdr="src.FymsHdrFormat"
                :source="src.FymsSource"
                :video-codec="src.FymsVideoCodec"
                :audio-codec="src.FymsAudioCodec"
              />
            </header>

            <div class="ms-facts">
              <div v-if="src.Bitrate" class="ms-fact">
                <span class="ms-fact-label">总码率</span>
                <span class="ms-fact-value">{{ formatBitrate(src.Bitrate) }}</span>
              </div>
              <div v-if="src.Size" class="ms-fact">
                <span class="ms-fact-label">大小</span>
                <span class="ms-fact-value">{{ formatFileSize(src.Size) }}</span>
              </div>
              <div v-if="auth.isAdmin && src.Path" class="ms-fact ms-fact-path">
                <span class="ms-fact-label">
                  <n-icon :size="13" style="vertical-align: -2px"><FolderOpenOutline /></n-icon>
                  路径
                </span>
                <code class="ms-path">{{ src.Path }}</code>
              </div>
            </div>

            <div
              v-for="group in groupedStreams(src)"
              :key="group.type"
              class="ms-stream-group"
            >
              <h4 class="ms-stream-type">{{ streamTypeLabel(group.type) }}</h4>
              <ul class="ms-stream-list">
                <li
                  v-for="(s, si) in group.streams"
                  :key="`${group.type}-${si}`"
                  class="ms-stream"
                >
                  <span class="ms-stream-text">{{ formatStream(s) }}</span>
                  <span v-if="s.IsDefault" class="ms-stream-flag">默认</span>
                  <span v-if="s.IsForced" class="ms-stream-flag">强制</span>
                </li>
              </ul>
            </div>
          </article>
        </div>

        <!-- 兜底:无 MediaSources 但有 item.Path(旧数据,仅 admin) -->
        <div v-if="auth.isAdmin && item.Path && !item.MediaSources?.length" class="ms-card ms-fallback">
          <div class="ms-fact ms-fact-path">
            <span class="ms-fact-label">
              <n-icon :size="13" style="vertical-align: -2px"><FolderOpenOutline /></n-icon>
              路径
            </span>
            <code class="ms-path">{{ item.Path }}</code>
          </div>
        </div>
      </div>

      <!-- Back link -->
      <div v-if="item.Type === 'Episode' && item.SeriesName" class="back-section">
        <router-link :to="'/item/' + item.SeriesId" class="back-link">← 返回「{{ item.SeriesName }}」</router-link>
      </div>
    </div>

    <!-- ═══ 自定义刮削弹窗 ═══ -->
    <n-modal
      v-model:show="showCustomScrape"
      preset="card"
      title="自定义刮削 - 搜索 TMDB"
      :style="[forceSolidModalStyle, { maxWidth: '680px', maxHeight: '85vh' }]"
      class="solid-modal-card force-solid-modal custom-scrape-modal"
      :bordered="false"
    >
      <div class="tmdb-search-bar">
        <n-input v-model:value="customQuery" placeholder="输入名称搜索 TMDB" clearable @keyup.enter="handleTmdbSearch" style="flex: 1" />
        <n-input-number v-model:value="customYear" :min="1900" :max="2030" placeholder="年份" clearable style="width: 110px" />
        <n-button type="primary" :loading="tmdbSearching" @click="handleTmdbSearch" :disabled="!customQuery.trim()">搜索</n-button>
      </div>
      <div v-if="tmdbSearching" class="tmdb-loading"><n-spin /></div>
      <div v-else-if="tmdbResults.length" class="tmdb-results">
        <div v-for="r in tmdbResults" :key="r.id" class="tmdb-result-card" @click="handleApplyTmdb(r.id)">
          <img v-if="r.poster_path" :src="'https://image.tmdb.org/t/p/w92' + r.poster_path" class="tmdb-poster" />
          <div v-else class="tmdb-poster tmdb-poster-empty">?</div>
          <div class="tmdb-info">
            <div class="tmdb-title">
              {{ r.title || r.name || '未知' }}
              <span v-if="(r.release_date || r.first_air_date)" class="tmdb-year">({{ (r.release_date || r.first_air_date || '').substring(0, 4) }})</span>
            </div>
            <div class="tmdb-meta">
              <span v-if="r.vote_average" class="tmdb-rating">TMDB {{ r.vote_average?.toFixed?.(1) || r.vote_average }}</span>
              <span class="tmdb-id">ID: {{ r.id }}</span>
            </div>
            <div v-if="r.overview" class="tmdb-overview">{{ r.overview.length > 120 ? r.overview.substring(0, 120) + '...' : r.overview }}</div>
          </div>
          <div v-if="tmdbApplying === r.id" class="tmdb-applying"><n-spin size="small" /></div>
        </div>
      </div>
      <div v-else-if="!tmdbSearching && customQuery" class="tmdb-empty-state">点击搜索查找 TMDB 结果</div>
    </n-modal>
  </div>
</template>

<style scoped>
/* ═══ Skeleton ═══ */
.detail-skeleton { margin: 0; }
.skeleton-hero { height: 480px; margin: -56px -24px 0; }
.skeleton-body { padding: 32px 48px; }

/* ═══ Page Container ═══ */
.detail-page {
  position: relative;
  margin: 0;
}

.detail-page-bg {
  position: absolute;
  top: -56px;
  left: -24px;
  right: -24px;
  bottom: 0;
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
}

.detail-page-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 20%;
  transform: scale(1.12);
  transform-origin: center top;
}

.detail-page-bg-overlay {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(
      to bottom,
      rgba(14, 14, 14, 0) 0%,
      rgba(14, 14, 14, 0) 35%,
      rgba(14, 14, 14, 0.55) 60%,
      rgba(14, 14, 14, 0.92) 82%,
      #0e0e0e 100%
    ),
    linear-gradient(
      to right,
      rgba(14, 14, 14, 0.72) 0%,
      rgba(14, 14, 14, 0.3) 35%,
      rgba(14, 14, 14, 0.05) 60%,
      rgba(14, 14, 14, 0) 80%
    );
}

/* ═══ Hero Backdrop (seerr-inspired) ═══ */
.hero-backdrop-section {
  position: relative;
  width: calc(100% + 48px);
  margin: -56px -24px 0;
  min-height: clamp(520px, 68vh, 680px);
  overflow: visible;
  z-index: 1;
}

.hero-gradient {
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  background: none;
}

.hero-content {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-end;
  min-height: clamp(520px, 68vh, 680px);
  padding: 0 0 96px;
}

.hero-inner {
  width: 100%;
  max-width: 1480px;
  margin: 0 auto;
  padding: 0 32px;
  display: flex;
  gap: 32px;
  align-items: flex-end;
}

/* ═══ Poster ═══ */
.hero-poster {
  flex-shrink: 0;
  width: 220px;
}

.poster-card {
  position: relative;
  padding-bottom: 150%;
  border-radius: var(--app-radius-xl, 24px);
  overflow: hidden;
  box-shadow: var(--app-shadow-ambient, 0 24px 48px rgba(0, 0, 0, 0.55));
}

.poster-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

/* ═══ Hero Info ═══ */
.hero-info {
  flex: 1;
  min-width: 0;
  padding-bottom: 4px;
}

.series-link {
  color: var(--app-primary);
  font-size: 14px;
  font-weight: 600;
  text-decoration: none;
  display: inline-block;
  margin-bottom: 10px;
  letter-spacing: 0.01em;
}

.item-title {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: clamp(2.25rem, 5.5vw, 4.75rem);
  font-weight: 900;
  line-height: 1.02;
  letter-spacing: -0.035em;
  color: #fff;
  margin: 0;
  text-shadow: 0 4px 24px rgba(0, 0, 0, 0.55);
}

.item-original-title {
  margin: 10px 0 0;
  font-size: 1rem;
  font-weight: 400;
  color: rgba(255, 255, 255, 0.6);
}

.meta-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px 12px;
  margin: 18px 0 22px;
  font-size: 14px;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.85);
}

.meta-rating {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.meta-rating strong {
  font-weight: 700;
  color: #fff;
}
.meta-rating-label {
  color: rgba(255, 255, 255, 0.55);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.meta-dot {
  color: rgba(255, 255, 255, 0.35);
}

.meta-ends {
  color: rgba(255, 255, 255, 0.55);
  font-size: 13px;
  margin-left: 4px;
}

.meta-cert {
  border: 0;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.85);
}

/* ═══ Action Buttons ═══ */
.action-row {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.btn-play,
.btn-action {
  height: 52px;
  padding: 0 26px;
  border-radius: 14px;
  font-family: inherit;
  font-size: 15px;
  font-weight: 700;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  cursor: pointer;
  border: 0;
  box-sizing: border-box;
  transition: background 0.2s ease, filter 0.2s ease, transform 0.15s ease;
}
.btn-play:active,
.btn-action:active {
  transform: translateY(1px);
}

.btn-play {
  background: var(--app-primary);
  color: #fff;
  min-width: 140px;
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.4);
}
.btn-play:hover {
  filter: brightness(1.1);
}

.btn-action {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
  font-weight: 600;
  padding: 0 20px;
}
.btn-action:hover {
  background: rgba(255, 255, 255, 0.18);
}
.btn-action.active {
  background: rgba(var(--app-primary-rgb), 0.22);
  color: #fff;
}
.btn-action:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* ═══ Genre Chips ═══ */
.genre-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.genre-chip {
  padding: 6px 16px;
  border-radius: 999px;
  border: 0;
  font-size: 13px;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.85);
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease;
  background: rgba(255, 255, 255, 0.08);
  appearance: none;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.genre-chip:hover {
  background: rgba(var(--app-primary-rgb), 0.22);
  color: #fff;
}
.genre-chip:disabled {
  opacity: 0.6;
  cursor: default;
}

/* ═══ Detail Body ═══ */
.detail-body {
  max-width: 1480px;
  position: relative;
  z-index: 3;
  margin: 0 auto;
  padding: 0 8px 40px;
}

/* ═══ Content Grid (overview + facts) ═══ */
.content-grid {
  display: grid;
  grid-template-columns: 1fr 280px;
  gap: 40px;
  margin-bottom: 40px;
}

@media (max-width: 960px) {
  .content-grid { grid-template-columns: 1fr; gap: 24px; }
}

.content-main { min-width: 0; }

.item-tagline {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 18px;
  font-style: italic;
  font-weight: 500;
  color: color-mix(in srgb, var(--app-primary) 60%, var(--app-text) 40%);
  margin: 0 0 14px;
  line-height: 1.5;
  letter-spacing: -0.005em;
}

.item-overview {
  font-size: 15px;
  color: rgba(255, 255, 255, 0.82);
  line-height: 1.8;
  margin: 0 0 24px;
}

/* ═══ Crew inline ═══ */
.crew-inline {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  margin-bottom: 24px;
  padding: 18px 22px;
  border-radius: var(--app-radius-card, 20px);
  background: var(--app-surface-solid, #1c1b1b);
  border: 0;
}

.crew-group { display: flex; flex-direction: column; gap: 4px; }
.crew-label {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 11px;
  font-weight: 800;
  color: var(--app-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.12em;
}
.crew-names {
  font-size: 14px;
  font-weight: 500;
  color: var(--app-text);
}

/* ═══ Section Heading ═══ */
.section-heading {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 1.375rem;
  font-weight: 800;
  letter-spacing: -0.01em;
  line-height: 1.2;
  color: var(--app-text);
  margin: 0 0 20px;
  display: flex;
  align-items: center;
  gap: 14px;
}

.section-heading::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 1.2em;
  border-radius: 2px;
  background: var(--app-primary);
  flex-shrink: 0;
}

.section-heading-light {
  color: var(--app-text);
}

/* ═══ Facts sidebar ═══ */

.facts-block { margin-bottom: 24px; }

.ext-links { display: flex; gap: 10px; flex-wrap: wrap; }

.ext-link {
  font-size: 13px;
  text-decoration: none;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 18px;
  border-radius: 10px;
  border: 1px solid;
  transition: all 0.2s;
  font-weight: 500;
}
.ext-tmdb { color: #01b4e4; background: rgba(1,180,228,0.08); border-color: rgba(1,180,228,0.2); }
.ext-tmdb:hover { background: rgba(1,180,228,0.16); }
.ext-imdb { color: #f5c518; background: rgba(245,197,24,0.08); border-color: rgba(245,197,24,0.2); }
.ext-imdb:hover { background: rgba(245,197,24,0.16); }

/* ═══ Cast Section ═══ */
.cast-section {
  margin-bottom: 48px;
}

.cast-scroll {
  display: flex;
  gap: 20px;
  overflow-x: auto;
  padding: 8px 0 16px;
  scroll-snap-type: x mandatory;
}

.cast-scroll::-webkit-scrollbar { height: 6px; }
.cast-scroll::-webkit-scrollbar-track { background: transparent; }
.cast-scroll::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.12); border-radius: 4px; }

.cast-card {
  flex-shrink: 0;
  width: 140px;
  scroll-snap-align: start;
  text-align: center;
  cursor: default;
}

.cast-img {
  width: 120px;
  height: 120px;
  border-radius: 50%;
  margin: 0 auto 14px;
  overflow: hidden;
  background: var(--app-surface-solid-2, #2a2a2a);
  border: 3px solid transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--app-text-muted);
  box-shadow: 0 8px 18px rgba(0, 0, 0, 0.4);
  transition: border-color 0.25s ease, transform 0.3s ease-out;
}
.cast-card:hover .cast-img {
  border-color: var(--app-primary);
  transform: scale(1.04);
}

.cast-img img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cast-info { min-width: 0; }
.cast-name {
  display: block;
  font-size: 14px;
  font-weight: 700;
  color: var(--app-text);
  letter-spacing: -0.005em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-role {
  display: block;
  font-size: 12px;
  color: var(--app-text-muted);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ═══ Episodes ═══ */
.episodes-section {
  margin-bottom: 48px;
}

.season-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 22px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.season-tab {
  padding: 10px 20px;
  border-radius: 999px;
  border: 0;
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.65);
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: -0.005em;
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease;
  white-space: nowrap;
}
.season-tab:hover {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}
.season-tab.active {
  background: var(--app-primary);
  color: #fff;
}

/* 水平卡片:左 16:9 缩略图 + 右纵向文本块 */
.ep-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ep-card {
  display: grid;
  grid-template-columns: 240px 1fr auto;
  gap: 20px;
  padding: 14px;
  background: var(--app-surface-solid, #1c1b1b);
  border: 0;
  border-radius: var(--app-radius-card, 20px);
  cursor: pointer;
  align-items: center;
  transition: background 0.25s ease, transform 0.25s ease, box-shadow 0.25s ease;
  position: relative;
  overflow: hidden;
}
.ep-card:hover {
  background: var(--app-surface-solid-2, #2a2a2a);
  transform: translateY(-2px);
  box-shadow: var(--app-shadow-1);
}

.ep-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 3px;
  background: var(--app-primary);
  border-radius: 0;
}

.ep-thumb-wrap {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  border-radius: var(--app-radius, 16px);
  overflow: hidden;
  background: var(--app-surface-solid-2, #2a2a2a);
}
.ep-thumb {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: inherit;
}

.ep-body {
  min-width: 0;
  padding: 4px 0;
}

.ep-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.ep-num {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 13px;
  font-weight: 800;
  letter-spacing: 0.04em;
  color: var(--app-primary);
  flex-shrink: 0;
}

.ep-title-text {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 16px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ep-played-badge {
  color: var(--app-primary);
  font-size: 13px;
  flex-shrink: 0;
}

.ep-sub {
  font-size: 12px;
  color: var(--app-text-muted);
  margin-bottom: 4px;
}

.ep-desc {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.65);
  margin: 6px 0 0;
  line-height: 1.6;
  display: -webkit-box;
  line-clamp: 3;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.ep-play-btn {
  flex-shrink: 0;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  border: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
  transition: background 0.2s ease, transform 0.2s ease;
}
.ep-play-btn:hover {
  background: var(--app-primary);
  transform: scale(1.08);
}

@media (max-width: 720px) {
  .ep-card {
    grid-template-columns: 140px 1fr auto;
    gap: 14px;
    padding: 12px;
  }
  .ep-title-text {
    font-size: 14px;
    white-space: normal;
  }
  .ep-desc {
    -webkit-line-clamp: 2;
  }
}

@media (max-width: 520px) {
  .ep-card {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .ep-play-btn {
    justify-self: flex-end;
  }
}

.empty-hint { color: var(--app-text-muted); font-size: 14px; }

.back-section { margin-top: 24px; }
.back-link { color: var(--app-primary); font-size: 14px; text-decoration: none; }

/* ═══ Responsive ═══ */
@media (max-width: 960px) {
  .detail-page-bg {
    left: -16px;
    right: -16px;
  }
  .hero-backdrop-section { width: calc(100% + 32px); margin: -56px -16px 0; }
  .hero-inner { padding: 0 20px; }
  .hero-poster { width: 160px; }
  .detail-body { padding: 24px 0 32px; }
}

@media (max-width: 640px) {
  .hero-backdrop-section { min-height: 460px; }
  .hero-content {
    min-height: 460px;
    padding-bottom: 72px;
  }
  .detail-body {
    margin-top: 0;
    padding-top: 0;
  }
  .hero-inner {
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 20px;
    padding: 0 16px;
  }
  .hero-poster { width: 160px; }
  .hero-info { text-align: center; }
  .meta-row { justify-content: center; }
  .action-row { justify-content: center; }
  .genre-row { justify-content: center; }
}

/* ═══ 媒体信息(画质/编码/流,路径仅 admin)═══ */
.media-info-section {
  margin-top: 32px;
  margin-bottom: 48px;
}

.ms-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ms-card {
  background: var(--app-surface-solid, #1c1b1b);
  border: 0;
  border-radius: var(--app-radius-card, 20px);
  padding: 22px 24px;
}

.ms-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 14px;
  margin-bottom: 18px;
}

.ms-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.ms-title strong {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.005em;
  color: var(--app-text);
}

.ms-container {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.1em;
  color: rgba(255, 255, 255, 0.72);
  background: rgba(255, 255, 255, 0.08);
  padding: 3px 9px;
  border-radius: 6px;
}

.ms-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 28px;
  margin-bottom: 8px;
}

.ms-fact {
  display: inline-flex;
  align-items: baseline;
  gap: 10px;
  font-size: 13px;
}

.ms-fact-label {
  color: var(--app-text-muted);
  font-weight: 500;
  flex-shrink: 0;
}

.ms-fact-value {
  color: var(--app-text);
  font-weight: 600;
}

.ms-fact-path {
  flex: 1 1 100%;
  align-items: flex-start;
  margin-top: 4px;
}

.ms-path {
  flex: 1;
  min-width: 0;
  font-family: 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12.5px;
  font-weight: 400;
  color: rgba(255, 255, 255, 0.72);
  word-break: break-all;
  background: rgba(255, 255, 255, 0.04);
  padding: 6px 10px;
  border-radius: 6px;
  line-height: 1.5;
}

.ms-stream-group {
  margin-top: 16px;
}

.ms-stream-type {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--app-text-muted);
  margin: 0 0 8px;
}

.ms-stream-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ms-stream {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 13.5px;
  color: rgba(255, 255, 255, 0.82);
  line-height: 1.5;
}

.ms-stream-text {
  min-width: 0;
}

.ms-stream-flag {
  font-family: 'Manrope', 'Inter', system-ui, sans-serif;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(255, 255, 255, 0.65);
  background: rgba(255, 255, 255, 0.08);
  padding: 2px 7px;
  border-radius: 4px;
}

.ms-fallback {
  margin-top: 4px;
}

/* ═══ 自定义刮削弹窗 ═══ */
.tmdb-search-bar { display: flex; gap: 8px; margin-bottom: 16px; }
.tmdb-loading { text-align: center; padding: 32px 0; }
.tmdb-results { max-height: 55vh; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
.tmdb-result-card {
  display: flex;
  gap: 12px;
  padding: 12px;
  border-radius: 10px;
  cursor: pointer;
  position: relative;
  transition: background 0.15s, border-color 0.15s, transform 0.15s;
  background: rgba(15, 23, 42, 0.58);
  border: 1px solid rgba(71, 85, 105, 0.55);
}
.tmdb-result-card:hover {
  background: rgba(30, 41, 59, 0.82);
  border-color: rgba(100, 116, 139, 0.9);
  transform: translateY(-1px);
}
.tmdb-poster { width: 46px; height: 69px; border-radius: 4px; object-fit: cover; flex-shrink: 0; }
.tmdb-poster-empty { background: rgba(30, 41, 59, 0.9); display: flex; align-items: center; justify-content: center; color: #94a3b8; font-size: 20px; }
.tmdb-info { flex: 1; min-width: 0; }
.tmdb-title { font-weight: 600; font-size: 15px; color: #eee; }
.tmdb-year { color: #888; font-weight: 400; margin-left: 4px; }
.tmdb-meta { font-size: 12px; color: #888; margin-top: 2px; display: flex; gap: 10px; }
.tmdb-rating { color: #f5c518; font-weight: 600; }
.tmdb-overview { font-size: 13px; color: #999; margin-top: 4px; line-height: 1.4; }
.tmdb-applying { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.5); border-radius: 8px; }
.tmdb-empty-state {
  text-align: center;
  padding: 24px 0;
  color: #94a3b8;
}

:deep(.custom-scrape-modal .n-input),
:deep(.custom-scrape-modal .n-input-number .n-input) {
  --n-color: rgba(15, 23, 42, 0.92) !important;
  --n-color-disabled: rgba(15, 23, 42, 0.72) !important;
  --n-color-active: rgba(15, 23, 42, 0.96) !important;
  --n-color-focus: rgba(15, 23, 42, 0.96) !important;
  --n-text-color: #f8fafc !important;
  --n-text-color-disabled: rgba(248, 250, 252, 0.55) !important;
  --n-placeholder-color: rgba(148, 163, 184, 0.92) !important;
  --n-caret-color: #f8fafc !important;
  --n-border: 1px solid rgba(71, 85, 105, 0.95) !important;
  --n-border-hover: 1px solid rgba(100, 116, 139, 1) !important;
  --n-border-focus: 1px solid rgba(56, 189, 248, 0.95) !important;
  --n-box-shadow-focus: 0 0 0 2px rgba(56, 189, 248, 0.18) !important;
}

:deep(.custom-scrape-modal .n-input .n-input__input-el),
:deep(.custom-scrape-modal .n-input-number .n-input input) {
  color: #f8fafc !important;
}

:deep(.custom-scrape-modal .n-input .n-input__placeholder),
:deep(.custom-scrape-modal .n-input-number .n-input .n-input__placeholder) {
  color: rgba(148, 163, 184, 0.92) !important;
}

:deep(.custom-scrape-modal .n-input .n-input__suffix),
:deep(.custom-scrape-modal .n-input-number .n-input .n-input__suffix),
:deep(.custom-scrape-modal .n-input-number .n-input-number-suffix) {
  color: rgba(148, 163, 184, 0.9) !important;
}
</style>
