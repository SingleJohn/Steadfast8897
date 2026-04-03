<script setup lang="ts">
import { computed, ref, watch, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSkeleton, NIcon, NSpace } from 'naive-ui'
import {
  CheckmarkDoneOutline,
  Heart,
  HeartOutline,
  PlayOutline,
  RefreshOutline,
} from '@vicons/ionicons5'
import { getItem, getItems, getImageUrl, toggleFavorite, togglePlayed, scrapeItemMetadata } from '../api/client'
import { useAuth } from '../composables/useAuth'
import { useUiStore } from '../stores/ui'

const route = useRoute()
const router = useRouter()
const { auth } = useAuth()
const ui = useUiStore()

const setBackdrop = inject<(url: string) => void>('setBackdrop', () => {})

const itemId = computed(() => route.params.itemId as string)
const item = ref<any>(null)
const seasons = ref<any[]>([])
const selectedSeason = ref('')
const episodes = ref<any[]>([])
const loading = ref(true)
const scraping = ref(false)
const brokenPeopleImages = ref<Record<string, boolean>>({})

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
            <h1
              class="item-title"
              :style="ui.isDark
                ? undefined
                : {
                    color: '#fff',
                    textShadow: '0 2px 12px rgba(0, 0, 0, 0.45)',
                  }"
            >
              {{ item.Name }}
            </h1>
            <h2 v-if="originalTitle" class="item-original-title">{{ originalTitle }}</h2>

            <div
              class="meta-row"
              :style="ui.isDark
                ? undefined
                : {
                    color: 'rgba(255, 255, 255, 0.88)',
                    textShadow: '0 1px 8px rgba(0, 0, 0, 0.35)',
                  }"
            >
              <span v-if="item.CommunityRating" class="meta-rating">★ {{ item.CommunityRating.toFixed(1) }}</span>
              <span v-if="item.ProductionYear" class="meta-tag">{{ item.ProductionYear }}</span>
              <span v-if="item.RunTimeTicks" class="meta-tag">{{ formatRuntime(item.RunTimeTicks) }}</span>
              <span
                v-if="item.RunTimeTicks"
                class="meta-ends"
                :style="ui.isDark ? undefined : { opacity: '0.9' }"
              >
                结束于 {{ endTimeStr(item.RunTimeTicks) }}
              </span>
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
              <img :src="getImageUrl(ep.SeriesPrimaryImageItemId || ep.Id, 'Primary', 200)" alt="" class="ep-thumb" />
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
              <img :src="getImageUrl(ep.SeriesPrimaryImageItemId || ep.Id, 'Primary', 200)" alt="" class="ep-thumb" />
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

      <!-- Back link -->
      <div v-if="item.Type === 'Episode' && item.SeriesName" class="back-section">
        <router-link :to="'/item/' + item.SeriesId" class="back-link">← 返回「{{ item.SeriesName }}」</router-link>
      </div>
    </div>
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
  background: rgba(15, 15, 15, 0.8);
}

/* ═══ Hero Backdrop (seerr-inspired) ═══ */
.hero-backdrop-section {
  position: relative;
  width: calc(100% + 48px);
  margin: -56px -24px 0;
  min-height: 560px;
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
  min-height: 560px;
  padding: 0 0 104px;
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
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 24px 48px rgba(0, 0, 0, 0.4);
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
  font-weight: 500;
  text-decoration: none;
  display: inline-block;
  margin-bottom: 6px;
}

.item-title {
  font-size: clamp(2rem, 4vw, 3rem);
  font-weight: 700;
  color: var(--app-text);
  margin: 0;
  line-height: 1.15;
}

:global(html:not(.app-dark)) .item-title {
  color: #fff;
  text-shadow: 0 2px 12px rgba(0, 0, 0, 0.45);
}

.item-original-title {
  margin: 6px 0 0;
  font-size: 1rem;
  font-weight: 400;
  color: var(--app-text-muted);
}

.meta-row {
  display: flex;
  align-items: center;
  gap: 4px;
  margin: 14px 0 16px;
  flex-wrap: wrap;
  font-size: 14px;
  color: var(--app-text-muted);
}

.meta-row > span { padding: 0 6px; }
.meta-row > span:first-child { padding-left: 0; }

.meta-rating { color: #ffd700; font-weight: 600; }
.meta-ends { opacity: 0.6; font-size: 13px; }
.meta-cert {
  border: 1px solid var(--app-border);
  border-radius: 4px;
  padding: 1px 8px !important;
  font-size: 12px;
  font-weight: 500;
}

/* ═══ Action Buttons ═══ */
.action-row {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.btn-play {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 28px;
  border-radius: 999px;
  background: var(--app-primary);
  color: #fff;
  font-size: 15px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: filter 0.2s, transform 0.15s;
}
.btn-play:hover { filter: brightness(1.12); transform: scale(1.02); }

.btn-action {
  height: 42px;
  padding: 0 14px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
  transition: all 0.2s;
  backdrop-filter: blur(8px);
}
.btn-action:hover { background: rgba(var(--app-primary-rgb), 0.15); }
.btn-action.active { color: #fff; border-color: rgba(var(--app-primary-rgb), 0.3); background: rgba(var(--app-primary-rgb), 0.2); }
.btn-action:disabled { opacity: 0.4; cursor: not-allowed; }

:global(html:not(.app-dark)) .btn-action {
  background: rgba(0, 0, 0, 0.06);
  border-color: rgba(0, 0, 0, 0.1);
}

/* ═══ Genre Chips ═══ */
.genre-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.genre-chip {
  padding: 4px 16px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  font-size: 13px;
  color: #fff;
  cursor: pointer;
  transition: all 0.2s;
  background: rgba(255, 255, 255, 0.06);
  appearance: none;
}
.genre-chip:hover { background: rgba(var(--app-primary-rgb), 0.12); border-color: rgba(var(--app-primary-rgb), 0.3); }
.genre-chip:disabled { opacity: 0.6; cursor: default; }

:global(html:not(.app-dark)) .genre-chip { background: rgba(0, 0, 0, 0.04); }

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
  font-size: 17px;
  font-style: italic;
  color: color-mix(in srgb, var(--app-primary) 60%, var(--app-text) 40%);
  margin: 0 0 12px;
  line-height: 1.5;
}

.item-overview {
  font-size: 15px;
  color: #fff;
  line-height: 1.8;
  margin: 0 0 20px;
  text-shadow: none;
}

/* ═══ Crew inline ═══ */
.crew-inline {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  margin-bottom: 24px;
  padding: 16px 20px;
  border-radius: 12px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
}

.crew-group { display: flex; flex-direction: column; gap: 2px; }
.crew-label { font-size: 12px; font-weight: 600; color: var(--app-text-muted); text-transform: uppercase; letter-spacing: 0.5px; }
.crew-names { font-size: 14px; color: var(--app-text); text-shadow: none; }

:global(html:not(.app-dark)) .crew-label,
:global(html:not(.app-dark)) .item-overview {
  text-shadow: none;
}

/* ═══ Section Heading ═══ */
.section-heading {
  font-size: 1.15rem;
  font-weight: 600;
  color: var(--app-text);
  margin: 0 0 16px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.section-heading::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 1.1em;
  border-radius: 2px;
  background: var(--app-primary);
  flex-shrink: 0;
}

.section-heading-light {
  color: #fff;
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

/* ═══ Cast Section (seerr-style horizontal scroll) ═══ */
.cast-section { margin-bottom: 40px; }

.cast-scroll {
  display: flex;
  gap: 14px;
  overflow-x: auto;
  padding-bottom: 8px;
  scroll-snap-type: x mandatory;
}

.cast-scroll::-webkit-scrollbar { height: 4px; }
.cast-scroll::-webkit-scrollbar-track { background: transparent; }
.cast-scroll::-webkit-scrollbar-thumb { background: var(--app-border); border-radius: 4px; }

.cast-card {
  flex-shrink: 0;
  width: 140px;
  scroll-snap-align: start;
  text-align: center;
}

.cast-img {
  width: 96px;
  height: 96px;
  border-radius: 50%;
  margin: 0 auto 10px;
  overflow: hidden;
  background: var(--app-surface-2);
  border: 2px solid var(--app-border);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--app-text-muted);
  transition: border-color 0.2s, transform 0.2s;
}
.cast-card:hover .cast-img { border-color: var(--app-primary); transform: scale(1.05); }

.cast-img img { width: 100%; height: 100%; object-fit: cover; }

.cast-info { min-width: 0; }
.cast-name {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: #fff;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-role {
  display: block;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ═══ Episodes ═══ */
.episodes-section { margin-bottom: 40px; }

.season-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 18px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.season-tab {
  padding: 8px 18px;
  border-radius: 999px;
  border: 1px solid var(--app-border);
  background: transparent;
  color: var(--app-text-muted);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  white-space: nowrap;
}
.season-tab:hover { background: var(--app-surface-2); color: var(--app-text); }
.season-tab.active {
  background: rgba(var(--app-primary-rgb), 0.14);
  color: var(--app-text);
  border-color: rgba(var(--app-primary-rgb), 0.3);
}

.ep-list { display: flex; flex-direction: column; gap: 8px; }

.ep-card {
  display: flex;
  gap: 16px;
  padding: 12px 16px;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  border-radius: 14px;
  cursor: pointer;
  align-items: center;
  transition: background 0.2s, border-color 0.2s;
  position: relative;
  overflow: hidden;
}
.ep-card:hover { background: rgba(var(--app-primary-rgb), 0.06); border-color: rgba(var(--app-primary-rgb), 0.2); }

.ep-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 3px;
  background: var(--app-primary);
  border-radius: 0 2px 0 0;
}

.ep-thumb-wrap { flex-shrink: 0; }
.ep-thumb {
  width: 55px;
  height: 82px;
  object-fit: cover;
  border-radius: 8px;
  background: var(--app-surface-1);
}

.ep-body { flex: 1; min-width: 0; }

.ep-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.ep-num {
  font-size: 13px;
  font-weight: 700;
  color: var(--app-primary);
  flex-shrink: 0;
}

.ep-title-text {
  font-size: 14px;
  color: var(--app-text);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ep-played-badge {
  color: var(--app-primary);
  font-size: 13px;
  flex-shrink: 0;
}

.ep-sub { font-size: 12px; color: var(--app-text-muted); margin-bottom: 2px; }

.ep-desc {
  font-size: 12px;
  color: var(--app-text-muted);
  margin: 4px 0 0;
  line-height: 1.5;
  display: -webkit-box;
  line-clamp: 2;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.ep-play-btn {
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--app-primary);
  color: #fff;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.15s, filter 0.15s;
}
.ep-play-btn:hover { transform: scale(1.1); filter: brightness(1.1); }

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
</style>
