<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSpin } from 'naive-ui'
import { getItem, getPlaybackInfo, getStreamUrl } from '../api/client'
import VideoPlayer from '../components/VideoPlayer.vue'

export interface TrackInfo {
  index: number
  language?: string
  title?: string
  isDefault: boolean
}

function formatTitle(item: Record<string, unknown>): string {
  if (item.Type === 'Episode') {
    const series = (item.SeriesName as string) || ''
    const season = item.ParentIndexNumber != null ? `S${String(item.ParentIndexNumber).padStart(2, '0')}` : ''
    const episode = item.IndexNumber != null ? `E${String(item.IndexNumber).padStart(2, '0')}` : ''
    const tag = [season, episode].filter(Boolean).join('')
    const parts = [series, tag, item.Name as string | undefined].filter(Boolean)
    return parts.join(' - ')
  }
  return (item.Name as string) || '未知标题'
}

const route = useRoute()
const router = useRouter()

const streamUrl = ref('')
const startPosition = ref(0)
const title = ref('')
const audioTracks = ref<TrackInfo[]>([])
const subtitleTracks = ref<TrackInfo[]>([])
const error = ref('')
const loading = ref(true)

const resolvedItemId = computed(() => {
  const p = route.params.itemId
  return typeof p === 'string' ? p : Array.isArray(p) ? p[0] ?? '' : ''
})

const shouldResume = computed(() => route.query.from !== 'start')

async function load() {
  const id = resolvedItemId.value
  if (!id) { loading.value = false; return }
  loading.value = true
  error.value = ''
  streamUrl.value = ''
  try {
    const [item, playbackInfo] = await Promise.all([getItem(id), getPlaybackInfo(id)])
    title.value = formatTitle(item as Record<string, unknown>)
    startPosition.value = shouldResume.value ? item.UserData?.PlaybackPositionTicks || 0 : 0
    const streams = item.MediaStreams || []
    audioTracks.value = streams
      .filter((s: any) => s.Type === 'Audio')
      .map((s: any) => ({ index: s.Index, language: s.Language, title: s.Title, isDefault: !!s.IsDefault }))
    subtitleTracks.value = streams
      .filter((s: any) => s.Type === 'Subtitle')
      .map((s: any) => ({ index: s.Index, language: s.Language, title: s.Title, isDefault: !!s.IsDefault }))
    const source = playbackInfo.MediaSources?.[0]
    if (source) streamUrl.value = getStreamUrl(id, source.Id)
    else error.value = '没有可用的媒体源'
  } catch {
    error.value = '加载播放信息失败'
  } finally {
    loading.value = false
  }
}

watch(
  () => [resolvedItemId.value, route.query.from] as const,
  () => { load() },
  { immediate: true }
)

function goBack() { router.back() }
function onEnded() { router.back() }
</script>

<template>
  <div v-if="loading" class="player-fullscreen">
    <div class="player-center">
      <n-spin size="large" />
      <span style="color: rgba(255,255,255,0.7); font-size: 15px; font-weight: 500; letter-spacing: 0.5px">加载播放器...</span>
    </div>
  </div>

  <div v-else-if="error" class="player-fullscreen">
    <div class="player-center">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" />
      </svg>
      <p style="color: rgba(255,255,255,0.8); font-size: 15px; margin: 8px 0 0; text-align: center">{{ error }}</p>
      <n-button secondary style="margin-top: 12px; min-width: 100px" @click="goBack">返回</n-button>
    </div>
  </div>

  <div v-else-if="streamUrl" class="player-fullscreen">
    <div class="player-top-overlay">
      <button type="button" class="player-back-btn" title="返回" @click="goBack">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="15 18 9 12 15 6" />
        </svg>
      </button>
      <span style="color: #fff; font-size: 16px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-shadow: 0 1px 4px rgba(0,0,0,0.6)">{{ title }}</span>
    </div>
    <div style="flex: 1; position: relative; width: 100%; height: 100%">
      <VideoPlayer
        :src="streamUrl"
        :item-id="resolvedItemId"
        :start-position-ticks="startPosition"
        :audio-tracks="audioTracks"
        :subtitle-tracks="subtitleTracks"
        @ended="onEnded"
      />
    </div>
  </div>
</template>

<style>
@keyframes player-spin { to { transform: rotate(360deg); } }

.player-fullscreen {
  position: fixed; inset: 0; z-index: 1000;
  background: #000; display: flex; flex-direction: column; overflow: hidden;
}
.player-center {
  flex: 1; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 16px;
}
.player-top-overlay {
  position: absolute; top: 0; left: 0; right: 0; z-index: 10;
  display: flex; align-items: center; gap: 8px;
  padding: 16px 20px; height: 80px;
  background: linear-gradient(to bottom, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.4) 60%, transparent 100%);
  transition: opacity 0.3s ease;
}
.player-top-overlay:hover { opacity: 1 !important; }
.player-back-btn {
  color: #fff; flex-shrink: 0; width: 36px; height: 36px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%; background: rgba(255,255,255,0.1);
  border: none; cursor: pointer; transition: background 0.2s ease;
}
.player-back-btn:hover { background: rgba(255,255,255,0.2); }
</style>
