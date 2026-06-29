<script setup lang="ts">
import { computed } from 'vue'
import { NIcon, NSelect } from 'naive-ui'
import {
  CheckmarkDoneOutline,
  FilmOutline,
  Heart,
  HeartOutline,
  PlayOutline,
  PlaySkipBackOutline,
  RefreshOutline,
  SearchOutline,
} from '@vicons/ionicons5'
import { getImageUrl } from '@/api/client'
import { endTimeStr, formatRuntime } from '../utils/format'
import ExternalPlayMenu from './ExternalPlayMenu.vue'

const props = withDefaults(
  defineProps<{
    item: any
    hasPoster: boolean
    posterId: string
    primaryAspectRatio?: number
    primaryIsLandscape?: boolean
    originalTitle: string
    titleDensity: string
    canPlay: boolean
    isFavorite: boolean
    isPlayed: boolean
    trailersCount: number
    scraping: boolean
    isAdmin: boolean
    versionOptions?: { label: string; value: string }[]
    selectedSourceId?: string
    selectedSource?: { Id: string; Container?: string; MediaStreams?: any[] } | null
    browserUnsupported?: boolean
  }>(),
  {
    primaryAspectRatio: 0,
    primaryIsLandscape: false,
    versionOptions: () => [],
    selectedSourceId: '',
    selectedSource: null,
    browserUnsupported: false,
  },
)

const heroPosterStyle = computed(() => {
  if (!props.primaryIsLandscape) return undefined
  const ratio = props.primaryAspectRatio > 0 ? props.primaryAspectRatio : 16 / 9
  return { '--detail-poster-ratio': String(ratio) }
})

const emit = defineEmits<{
  play: []
  playFromStart: []
  trailer: []
  favorite: []
  played: []
  scrape: []
  customScrape: []
  genreClick: [genreId: string, genreName: string]
  'update:selectedSourceId': [value: string]
}>()
</script>

<template>
  <div class="hero-backdrop-section">
    <div class="hero-gradient" />

    <div class="hero-content">
      <div class="hero-inner">
        <div
          v-if="hasPoster"
          class="hero-poster"
          :class="{ 'hero-poster-landscape': primaryIsLandscape }"
          :style="heroPosterStyle"
        >
          <div class="poster-card">
            <img
              :src="getImageUrl(posterId, 'Primary', primaryIsLandscape ? 720 : 400)"
              :alt="item.Name"
              class="poster-img"
              :width="primaryIsLandscape ? 640 : 400"
              :height="primaryIsLandscape ? 360 : 600"
              fetchpriority="high"
            />
          </div>
        </div>

        <div class="hero-info">
          <router-link v-if="item.Type === 'Episode' && item.SeriesName" :to="'/item/' + item.SeriesId" class="series-link">{{ item.SeriesName }}</router-link>
          <h1 class="item-title" :class="titleDensity" :title="item.Name">{{ item.Name }}</h1>
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
            <template v-if="canPlay">
              <button
                v-if="item.UserData?.PlaybackPositionTicks > 0"
                type="button"
                class="btn-play"
                aria-label="继续播放"
                @click="emit('play')"
              >
                <n-icon :size="18"><PlayOutline /></n-icon>
                <span>继续播放</span>
              </button>
              <button
                v-if="item.UserData?.PlaybackPositionTicks > 0"
                type="button"
                class="btn-play btn-play-secondary"
                aria-label="从头播放"
                @click="emit('playFromStart')"
              >
                <n-icon :size="18"><PlaySkipBackOutline /></n-icon>
                <span>从头播放</span>
              </button>
              <button
                v-else
                type="button"
                class="btn-play"
                aria-label="播放"
                @click="emit('play')"
              >
                <n-icon :size="18"><PlayOutline /></n-icon>
                <span>播放</span>
              </button>
              <n-select
                v-if="versionOptions.length > 1"
                class="version-select"
                size="medium"
                :value="selectedSourceId"
                :options="versionOptions"
                :consistent-menu-width="false"
                :menu-props="{ class: 'version-select-menu' }"
                @update:value="(v: string) => emit('update:selectedSourceId', v)"
              />
              <ExternalPlayMenu
                :item-id="item.Id"
                :source="selectedSource"
                :title="item.Name"
                :position-ticks="item.UserData?.PlaybackPositionTicks || 0"
                :highlight="browserUnsupported"
              />
            </template>
            <button
              v-if="trailersCount > 0"
              type="button"
              class="btn-action btn-trailer"
              aria-label="播放预告片"
              title="播放预告片"
              @click="emit('trailer')"
            >
              <n-icon :size="16"><FilmOutline /></n-icon>
              <span>预告片</span>
            </button>
            <button type="button" class="btn-action" :class="{ active: isFavorite }" :aria-label="isFavorite ? '取消收藏' : '收藏'" @click="emit('favorite')" title="收藏">
              <n-icon :size="16">
                <component :is="isFavorite ? Heart : HeartOutline" />
              </n-icon>
              <span>{{ isFavorite ? '已收藏' : '收藏' }}</span>
            </button>
            <button type="button" class="btn-action" :class="{ active: isPlayed }" :aria-label="isPlayed ? '标记未播放' : '标记已播放'" @click="emit('played')" title="已播放">
              <n-icon :size="16"><CheckmarkDoneOutline /></n-icon>
              <span>{{ isPlayed ? '已播放' : '标记已播' }}</span>
            </button>
            <button v-if="isAdmin" type="button" class="btn-action" :disabled="scraping" aria-label="刮削元数据" @click="emit('scrape')" title="刮削元数据">
              <n-icon :size="16"><RefreshOutline /></n-icon>
              <span>{{ scraping ? '刮削中' : '刮削' }}</span>
            </button>
            <button v-if="isAdmin" type="button" class="btn-action" aria-label="自定义刮削" @click="emit('customScrape')" title="自定义刮削">
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
              :aria-label="`按类型筛选:${g.Name}`"
              @click="emit('genreClick', g.Id, g.Name)"
            >
              {{ g.Name }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.version-select {
  width: min(260px, 60vw);
  align-self: center;
}
</style>
