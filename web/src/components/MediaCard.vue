<script setup lang="ts">
import { computed, ref } from 'vue'
import { useIntersectionObserver } from '@vueuse/core'
import { getImageUrl } from '../api/client'
import { getPlatformIcon, platformIconMap } from '../icons/PlatformIcons'

const props = withDefaults(
  defineProps<{
    item: any
    showProgress?: boolean
    shape?: 'portrait' | 'thumb' | 'square'
  }>(),
  {
    showProgress: false,
    shape: 'portrait',
  },
)

function formatDuration(ticks?: number): string {
  if (!ticks) return ''
  const totalMin = Math.round(ticks / 10_000_000 / 60)
  if (totalMin < 60) return `${totalMin}分钟`
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  return m > 0 ? `${h}时${m}分` : `${h}时`
}

const hasImage = computed(() => {
  if (isPlatformLib.value) return false
  if (props.shape === 'thumb') {
    return props.item.BackdropImageTags?.length > 0 || props.item.ParentBackdropItemId || props.item.ImageTags?.Primary || props.item.SeriesPrimaryImageItemId
  }
  return props.item.ImageTags?.Primary || props.item.SeriesPrimaryImageItemId
})

const imgSrc = computed(() => {
  if (props.shape === 'thumb') {
    if (props.item.BackdropImageTags?.length > 0) return getImageUrl(props.item.Id, 'Backdrop', 500)
    if (props.item.ParentBackdropItemId) return getImageUrl(props.item.ParentBackdropItemId, 'Backdrop', 500)
    if (props.item.ImageTags?.Thumb) return getImageUrl(props.item.Id, 'Thumb', 500)
  }
  const id = props.item.SeriesPrimaryImageItemId || props.item.Id
  return getImageUrl(id, 'Primary', 300)
})

const cardRef = ref<HTMLElement | null>(null)
const imgVisible = ref(false)
const { stop: stopObserve } = useIntersectionObserver(
  cardRef,
  ([entry]) => {
    if (entry?.isIntersecting) {
      imgVisible.value = true
      stopObserve()
    }
  },
  { rootMargin: '200px' },
)
const lazyImgSrc = computed(() => (imgVisible.value ? imgSrc.value : ''))

const progress = computed(() => props.item.UserData?.PlayedPercentage || 0)
const rating = computed(() => props.item.CommunityRating)
const duration = computed(() => formatDuration(props.item.RunTimeTicks))
const isEpisode = computed(() => props.item.Type === 'Episode')
const subtitle = computed(() => {
  if (isEpisode.value) {
    const ep = `S${props.item.ParentIndexNumber}E${props.item.IndexNumber}`
    return props.item.SeriesName ? `${ep} · ${props.item.SeriesName}` : ep
  }
  return props.item.ProductionYear || ''
})

const shapeClass = computed(() => {
  if (props.shape === 'thumb') return 'thumb-card'
  if (props.shape === 'square') return 'square-card'
  return 'portrait-card'
})

const isPlatformLib = computed(() => {
  return !!props.item.PlatformLibrary
})

const platformName = computed(() => {
  if (!isPlatformLib.value) return ''
  return props.item.Name || ''
})

const platformIcon = computed(() => {
  if (!isPlatformLib.value) return null
  return getPlatformIcon(platformName.value)
})

const platformLogoSrc = computed(() => {
  if (!isPlatformLib.value) return ''
  const name = props.item.Name
  if (!name) return ''
  return `/Library/Platforms/Logo?name=${encodeURIComponent(name)}`
})

// Platform brand colors for background gradient
const platformGradient = computed(() => {
  const colors: Record<string, string> = {
    'Netflix': 'linear-gradient(135deg, #1a0000 0%, #3d0000 40%, #8b0000 100%)',
    'Disney+': 'linear-gradient(135deg, #020024 0%, #040e50 40%, #0d1b63 100%)',
    'HBO': 'linear-gradient(135deg, #0a0a0a 0%, #1a1a2e 40%, #2d2d3f 100%)',
    'Apple TV+': 'linear-gradient(135deg, #0a0a0a 0%, #1a1a1a 40%, #1d1d1f 100%)',
    'Amazon': 'linear-gradient(135deg, #001a2e 0%, #003050 40%, #00668a 100%)',
    'Hulu': 'linear-gradient(135deg, #001a0a 0%, #003c15 40%, #0a5c25 100%)',
    'Paramount+': 'linear-gradient(135deg, #000a2e 0%, #001b5e 40%, #0040b0 100%)',
    'Peacock': 'linear-gradient(135deg, #0a0a0a 0%, #1a1020 40%, #2a1a3a 100%)',
    'Crunchyroll': 'linear-gradient(135deg, #1a0800 0%, #3d1500 40%, #7a3000 100%)',
  }
  return colors[platformName.value] || 'linear-gradient(135deg, #1a1a2e 0%, #1e293b 40%, #334155 100%)'
})

// Regular library (CollectionFolder) styling
const isLibrary = computed(() => {
  return !isPlatformLib.value && (props.item.CollectionType || props.item.Type === 'CollectionFolder')
})

const libraryGradient = computed(() => {
  const ct = props.item.CollectionType
  const gradients: Record<string, string> = {
    'movies': 'linear-gradient(135deg, #0a0a1e 0%, #16213e 40%, #1a3a5c 100%)',
    'tvshows': 'linear-gradient(135deg, #0a0a1e 0%, #1b2838 40%, #1a4a4a 100%)',
    'music': 'linear-gradient(135deg, #1a0a1e 0%, #2d1b3d 40%, #4a1942 100%)',
  }
  return gradients[ct] || 'linear-gradient(135deg, #0f172a 0%, #1e293b 40%, #334155 100%)'
})

const librarySvgIcon = computed(() => {
  const ct = props.item.CollectionType
  if (ct === 'movies') return 'movie'
  if (ct === 'tvshows') return 'tv'
  if (ct === 'music') return 'music'
  return 'folder'
})

const linkTarget = computed(() => {
  if (props.item.CollectionType || props.item.Type === 'CollectionFolder') {
    return `/library/${props.item.Id}`
  }
  return `/item/${props.item.Id}`
})
</script>

<template>
  <div ref="cardRef" class="card-box">
    <router-link :to="linkTarget" class="card-link">
      <div :class="shapeClass" class="card-surface elevation-2">
        <div
          v-if="hasImage"
          class="card-content"
          :class="{ 'card-content-loading': !imgVisible }"
          :style="lazyImgSrc ? { backgroundImage: `url(${lazyImgSrc})` } : {}"
        />
        <div v-else-if="isPlatformLib" class="card-content card-platform" :style="{ background: platformGradient }">
          <img v-if="platformLogoSrc" :src="platformLogoSrc" class="platform-logo-img" alt="" />
          <component v-else :is="platformIcon" class="platform-icon-large" />
        </div>
        <div v-else-if="isLibrary" class="card-content card-library" :style="{ background: libraryGradient }">
          <svg v-if="librarySvgIcon === 'movie'" class="library-icon" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
            <rect x="6" y="8" width="36" height="32" rx="3" stroke="currentColor" stroke-width="2"/>
            <path d="M6 16h36M6 32h36M14 8v32M34 8v32" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
            <circle cx="24" cy="24" r="5" stroke="currentColor" stroke-width="1.5"/>
            <circle cx="24" cy="24" r="1.5" fill="currentColor"/>
          </svg>
          <svg v-else-if="librarySvgIcon === 'tv'" class="library-icon" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
            <rect x="4" y="12" width="40" height="28" rx="3" stroke="currentColor" stroke-width="2"/>
            <polyline points="30,6 24,12 18,6" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round"/>
            <rect x="10" y="17" width="28" height="18" rx="1" stroke="currentColor" stroke-width="1" opacity="0.4"/>
            <polygon points="20,21 20,30 28,25.5" fill="currentColor" opacity="0.6"/>
          </svg>
          <svg v-else-if="librarySvgIcon === 'music'" class="library-icon" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M18 38V14l20-6v24" stroke="currentColor" stroke-width="2" fill="none"/>
            <circle cx="14" cy="38" r="5" stroke="currentColor" stroke-width="2"/>
            <circle cx="34" cy="32" r="5" stroke="currentColor" stroke-width="2"/>
          </svg>
          <svg v-else class="library-icon" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M6 18a3 3 0 013-3h10l4 5h16a3 3 0 013 3v16a3 3 0 01-3 3H9a3 3 0 01-3-3V18z" stroke="currentColor" stroke-width="2"/>
          </svg>
          <span class="library-name-overlay">{{ item.Name }}</span>
        </div>
        <div v-else class="card-content card-noimg">
          <span>{{ item.Name }}</span>
        </div>

        <div v-if="rating > 0 || duration || item.UserData?.Played || item.UserData?.UnplayedItemCount" class="card-upper">
          <span v-if="rating > 0" class="card-badge badge-rating">&#9733; {{ rating.toFixed(1) }}</span>
          <span v-if="item.UserData?.UnplayedItemCount" class="card-badge badge-unplayed">{{ item.UserData.UnplayedItemCount }}</span>
          <span v-if="item.UserData?.Played" class="card-badge badge-played">&#10003;</span>
        </div>

        <div class="card-hover-overlay">
          <span class="card-play-icon">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="#fff"><path d="M8 5v14l11-7z"/></svg>
          </span>
        </div>

        <div v-if="showProgress && progress > 0" class="card-progress">
          <div class="card-progress-fill" :style="{ width: `${Math.min(progress, 100)}%` }" />
        </div>
      </div>
    </router-link>
    <div class="card-text">
      <span class="card-title">{{ item.Name }}</span>
      <span v-if="subtitle" class="card-subtitle">{{ subtitle }}</span>
    </div>
  </div>
</template>

<style scoped>
.card-box {
  text-decoration: none;
  color: unset;
}

.card-link {
  text-decoration: none;
  color: unset;
  display: block;
}

.card-surface {
  overflow: hidden;
  background: var(--app-surface-2);
}

.portrait-card {
  position: relative;
  padding-bottom: 150%;
  contain: strict;
  border-radius: var(--app-radius-xl, 24px);
}

.thumb-card {
  position: relative;
  padding-bottom: 56.25%;
  contain: strict;
  border-radius: var(--app-radius-xl, 24px);
}

.square-card {
  position: relative;
  padding-bottom: 100%;
  contain: strict;
  border-radius: var(--app-radius-xl, 24px);
}

.elevation-2 {
  box-shadow: none;
  transition: box-shadow 400ms ease-out;
}

.card-link:hover .elevation-2 {
  box-shadow: var(--app-shadow-ambient, 0 24px 48px rgba(0, 0, 0, 0.55));
}

.card-content {
  position: absolute;
  inset: 0;
  overflow: hidden;
  background-size: cover;
  background-repeat: no-repeat;
  background-clip: content-box;
  background-position: center center;
  -webkit-tap-highlight-color: transparent;
  border-radius: inherit;
  transition: transform 400ms ease-out;
}

.card-link:hover .card-content {
  transform: scale(1.05);
}

.card-content-loading {
  background: linear-gradient(
    135deg,
    rgba(255, 255, 255, 0.04) 0%,
    rgba(255, 255, 255, 0.08) 50%,
    rgba(255, 255, 255, 0.04) 100%
  );
  background-size: 200% 200%;
  animation: card-content-shimmer 1.6s ease-in-out infinite;
}

@keyframes card-content-shimmer {
  0% { background-position: 0% 0%; }
  100% { background-position: 200% 200%; }
}

.card-noimg {
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(180deg, rgba(255,255,255,0.08), rgba(255,255,255,0.02));
  color: var(--app-text-muted);
  font-size: 13px;
  text-align: center;
  padding: 12px;
}

.card-platform {
  display: flex;
  align-items: center;
  justify-content: center;
}

.platform-icon-large {
  width: 96px;
  height: 96px;
  opacity: 0.95;
  filter: drop-shadow(0 6px 16px rgba(0, 0, 0, 0.5));
}

.platform-logo-img {
  width: 55%;
  max-height: 55%;
  object-fit: contain;
  border-radius: 16px;
  filter: drop-shadow(0 6px 20px rgba(0, 0, 0, 0.6));
}

.card-library {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.library-icon {
  width: 72px;
  height: 72px;
  color: rgba(255, 255, 255, 0.25);
  filter: drop-shadow(0 4px 12px rgba(0, 0, 0, 0.3));
}

.library-name-overlay {
  font-size: 16px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.7);
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.5);
  letter-spacing: 1px;
  max-width: 80%;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-upper {
  position: absolute;
  right: 0.5em;
  top: 0.5em;
  gap: 0.3em;
  display: flex;
  align-items: center;
  z-index: 2;
}

.card-badge {
  padding: 2px 6px;
  background: rgba(0,0,0,0.7);
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.4;
  color: rgba(255,255,255,0.9);
}

.badge-rating { color: #f5c518; }

.badge-unplayed {
  min-width: 20px;
  text-align: center;
  background: var(--app-primary);
  color: #fff;
  border-radius: 10px;
  padding: 2px 6px;
}

.badge-played {
  background: var(--app-primary, #10b981);
  color: #fff;
  border-radius: 50%;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  font-size: 12px;
}

.card-hover-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(180deg, rgba(0,0,0,0.08) 0%, rgba(0,0,0,0.55) 100%);
  opacity: 0;
  transition: opacity 0.25s ease;
  border-radius: inherit;
  z-index: 2;
  pointer-events: none;
}

.card-link:hover .card-hover-overlay { opacity: 1; }

.card-play-icon {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.22);
  backdrop-filter: blur(20px) saturate(1.3);
  -webkit-backdrop-filter: blur(20px) saturate(1.3);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}

.card-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 3px;
  background: rgba(0, 0, 0, 0.55);
  z-index: 3;
  border-radius: 0 0 var(--app-radius-xl, 24px) var(--app-radius-xl, 24px);
}

.card-progress-fill {
  height: 100%;
  background: var(--app-primary, #10b981);
  border-radius: inherit;
}

.card-text {
  margin-top: 0.7em;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 0 0.2em;
}

.card-title {
  display: block;
  font-weight: 600;
  font-size: 13px;
  line-height: 1.35;
  letter-spacing: -0.005em;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-subtitle {
  display: block;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 2px;
}
</style>
