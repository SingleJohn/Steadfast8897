<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NIcon, NModal } from 'naive-ui'
import { CloseOutline, FilmOutline, OpenOutline, PlayCircleOutline } from '@vicons/ionicons5'
import type { TrailerInfo } from '../types'
import { canPlayTrailerInline, normalizeTrailerIndex, trailerTabLabel } from '../utils/trailer'

const props = defineProps<{
  show: boolean
  selectedIndex: number
  trailers: TrailerInfo[]
  itemName: string
  poster?: string
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  'update:selectedIndex': [value: number]
}>()

const trailerVideoRef = ref<HTMLVideoElement | null>(null)
const selectedTrailer = computed<TrailerInfo | null>(() => props.trailers[props.selectedIndex] || props.trailers[0] || null)
const selectedTrailerCanPlay = computed(() => selectedTrailer.value ? canPlayTrailerInline(selectedTrailer.value.Url) : false)

watch(() => props.show, (show) => {
  if (show || !trailerVideoRef.value) return
  trailerVideoRef.value.pause()
  trailerVideoRef.value.currentTime = 0
})

function closeTrailer() {
  emit('update:show', false)
  if (!trailerVideoRef.value) return
  trailerVideoRef.value.pause()
  trailerVideoRef.value.currentTime = 0
}

function selectTrailer(index: number | string) {
  const normalizedIndex = normalizeTrailerIndex(index)
  if (!props.trailers[normalizedIndex]) return
  if (trailerVideoRef.value) {
    trailerVideoRef.value.pause()
    trailerVideoRef.value.currentTime = 0
  }
  emit('update:selectedIndex', normalizedIndex)
}

function isSelectedTrailer(index: number | string): boolean {
  return props.selectedIndex === normalizeTrailerIndex(index)
}
</script>

<template>
  <n-modal :show="show" class="trailer-modal" :mask-closable="true" @update:show="emit('update:show', $event)">
    <div class="trailer-panel">
      <header class="trailer-header">
        <div class="trailer-title-group">
          <span class="trailer-kicker">
            <n-icon :size="16"><FilmOutline /></n-icon>
            预告片
          </span>
          <h3>{{ selectedTrailer?.Name || itemName }}</h3>
        </div>
        <button type="button" class="trailer-close" aria-label="关闭预告片" @click="closeTrailer">
          <n-icon :size="24"><CloseOutline /></n-icon>
        </button>
      </header>

      <div v-if="selectedTrailer" class="trailer-stage">
        <video
          v-if="selectedTrailerCanPlay"
          :key="selectedTrailer.Url"
          ref="trailerVideoRef"
          class="trailer-video"
          controls
          autoplay
          playsinline
          preload="metadata"
          :poster="poster || ''"
        >
          <source :src="selectedTrailer.Url" />
        </video>
        <div v-else class="trailer-external">
          <n-icon :size="42"><PlayCircleOutline /></n-icon>
          <p>这个预告片地址不是浏览器可直接播放的视频源。</p>
          <a :href="selectedTrailer.Url" target="_blank" rel="noopener noreferrer" class="trailer-open-link">
            <n-icon :size="16"><OpenOutline /></n-icon>
            打开预告片链接
          </a>
        </div>
      </div>

      <div v-if="trailers.length > 1" class="trailer-tabs" role="tablist" aria-label="预告片列表">
        <button
          v-for="(trailer, idx) in trailers"
          :key="trailer.Url"
          type="button"
          class="trailer-tab"
          :class="{ active: isSelectedTrailer(idx) }"
          role="tab"
          :aria-selected="isSelectedTrailer(idx)"
          @click="selectTrailer(idx)"
        >
          {{ trailerTabLabel(trailer, idx) }}
        </button>
      </div>
    </div>
  </n-modal>
</template>
