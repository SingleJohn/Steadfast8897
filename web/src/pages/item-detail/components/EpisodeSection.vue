<script setup lang="ts">
import { NIcon } from 'naive-ui'
import { PlayOutline } from '@vicons/ionicons5'
import { getImageUrl } from '@/api/client'
import { formatRuntime } from '../utils/format'

defineProps<{
  item: any
  seasons: any[]
  selectedSeason: string
  episodes: any[]
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:selectedSeason': [value: string]
  detail: [itemId: string]
  play: [itemId: string]
}>()
</script>

<template>
  <div v-if="item.Type === 'Series'" class="episodes-section">
    <h3 class="section-heading">剧集</h3>
    <div v-if="seasons.length > 0" class="season-tabs">
      <button
        v-for="s in seasons"
        :key="s.Id"
        type="button"
        class="season-tab"
        :class="{ active: selectedSeason === s.Id }"
        @click="emit('update:selectedSeason', s.Id)"
      >
        {{ s.Name }}
      </button>
    </div>
    <div v-if="loading" class="ep-list">
      <div v-for="i in 3" :key="i" class="ep-card ep-card-skeleton" aria-hidden="true">
        <div class="ep-thumb ep-thumb-empty" />
        <div class="ep-body">
          <div class="ep-skeleton-line ep-skeleton-line-title" />
          <div class="ep-skeleton-line ep-skeleton-line-short" />
          <div class="ep-skeleton-line" />
        </div>
      </div>
    </div>
    <div v-else-if="episodes.length > 0" class="ep-list">
      <div
        v-for="ep in episodes"
        :key="ep.Id"
        class="ep-card"
        role="button"
        tabindex="0"
        @click="emit('detail', ep.Id)"
        @keyup.enter="emit('detail', ep.Id)"
        @keyup.space.prevent="emit('detail', ep.Id)"
      >
        <div v-if="(ep.UserData?.PlayedPercentage || 0) > 0 && (ep.UserData?.PlayedPercentage || 0) < 100" class="ep-progress" :style="{ width: (ep.UserData?.PlayedPercentage || 0) + '%' }" />
        <div class="ep-thumb-wrap">
          <img v-if="ep.ImageTags?.Primary" :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" width="200" height="113" loading="lazy" />
          <div v-else class="ep-thumb ep-thumb-empty" />
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
        <button type="button" class="ep-play-btn" :aria-label="`播放 ${ep.Name}`" @click.stop="emit('play', ep.Id)">
          <n-icon :size="16"><PlayOutline /></n-icon>
        </button>
      </div>
    </div>
    <p v-else class="empty-hint">暂无剧集信息</p>
  </div>

  <div v-if="item.Type === 'Season' && (loading || episodes.length > 0)" class="episodes-section">
    <h3 class="section-heading">剧集</h3>
    <div v-if="loading" class="ep-list">
      <div v-for="i in 3" :key="i" class="ep-card ep-card-skeleton" aria-hidden="true">
        <div class="ep-thumb ep-thumb-empty" />
        <div class="ep-body">
          <div class="ep-skeleton-line ep-skeleton-line-title" />
          <div class="ep-skeleton-line ep-skeleton-line-short" />
          <div class="ep-skeleton-line" />
        </div>
      </div>
    </div>
    <div v-else class="ep-list">
      <div
        v-for="ep in episodes"
        :key="ep.Id"
        class="ep-card"
        role="button"
        tabindex="0"
        @click="emit('detail', ep.Id)"
        @keyup.enter="emit('detail', ep.Id)"
        @keyup.space.prevent="emit('detail', ep.Id)"
      >
        <div v-if="(ep.UserData?.PlayedPercentage || 0) > 0 && (ep.UserData?.PlayedPercentage || 0) < 100" class="ep-progress" :style="{ width: (ep.UserData?.PlayedPercentage || 0) + '%' }" />
        <div class="ep-thumb-wrap">
          <img v-if="ep.ImageTags?.Primary" :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" width="200" height="113" loading="lazy" />
          <div v-else class="ep-thumb ep-thumb-empty" />
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
        <button type="button" class="ep-play-btn" :aria-label="`播放 ${ep.Name}`" @click.stop="emit('play', ep.Id)">
          <n-icon :size="16"><PlayOutline /></n-icon>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ep-card-skeleton {
  cursor: default;
  pointer-events: none;
}

.ep-skeleton-line {
  width: 100%;
  height: 12px;
  border-radius: 999px;
  background:
    linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.1), transparent),
    rgba(148, 163, 184, 0.14);
  background-size: 220px 100%, 100% 100%;
  animation: ep-skeleton-shimmer 1.35s ease-in-out infinite;
}

.ep-skeleton-line + .ep-skeleton-line {
  margin-top: 10px;
}

.ep-skeleton-line-title {
  width: 64%;
}

.ep-skeleton-line-short {
  width: 36%;
}

@keyframes ep-skeleton-shimmer {
  0% { background-position: -220px 0, 0 0; }
  100% { background-position: calc(100% + 220px) 0, 0 0; }
}

@media (prefers-reduced-motion: reduce) {
  .ep-skeleton-line {
    animation: none;
  }
}
</style>
