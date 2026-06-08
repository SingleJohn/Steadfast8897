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
    <div v-if="episodes.length > 0" class="ep-list">
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
          <img :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" width="200" height="113" loading="lazy" />
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

  <div v-if="item.Type === 'Season' && episodes.length > 0" class="episodes-section">
    <h3 class="section-heading">剧集</h3>
    <div class="ep-list">
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
          <img :src="getImageUrl(ep.Id, 'Primary', 200)" alt="" class="ep-thumb" width="200" height="113" loading="lazy" />
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

