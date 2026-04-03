<script setup lang="ts">
import { computed } from 'vue'
import { getImageUrl } from '../api/client'

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

const linkTarget = computed(() => {
  if (props.item.CollectionType || props.item.Type === 'CollectionFolder') {
    return `/library/${props.item.Id}`
  }
  return `/item/${props.item.Id}`
})
</script>

<template>
  <div class="card-box">
    <router-link :to="linkTarget" class="card-link">
      <div :class="shapeClass" class="card-surface elevation-2">
        <div
          v-if="hasImage"
          class="card-content"
          :style="{ backgroundImage: `url(${imgSrc})` }"
        />
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
            <svg width="28" height="28" viewBox="0 0 24 24" fill="#fff"><path d="M8 5v14l11-7z"/></svg>
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
  border-radius: var(--app-radius);
}

.thumb-card {
  position: relative;
  padding-bottom: 56.25%;
  contain: strict;
  border-radius: var(--app-radius);
}

.square-card {
  position: relative;
  padding-bottom: 100%;
  contain: strict;
  border-radius: var(--app-radius);
}

.elevation-2 {
  box-shadow: none;
  transition: opacity 0.2s ease;
}

.card-link:hover .elevation-2 {
  box-shadow: none;
  transform: none;
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
  background: linear-gradient(180deg, rgba(0,0,0,0.06), rgba(0,0,0,0.55));
  opacity: 0;
  transition: opacity 0.2s;
  border-radius: inherit;
  z-index: 1;
}

.card-link:hover .card-hover-overlay { opacity: 1; }

.card-play-icon {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: rgba(255,255,255,0.18);
  backdrop-filter: blur(14px);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 12px 24px rgba(0, 0, 0, 0.24);
}

.card-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 4px;
  background: rgba(0,0,0,0.5);
  z-index: 3;
  border-radius: 0 0 var(--app-radius) var(--app-radius);
}

.card-progress-fill {
  height: 100%;
  background: var(--app-primary, #10b981);
}

.card-text {
  margin-top: 0.55em;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 0 0.15em;
}

.card-title {
  display: block;
  font-weight: 600;
  font-size: 13px;
  line-height: 1.35;
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
