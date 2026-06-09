<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSelect, NSpin } from 'naive-ui'
import { getItem, getPlaybackInfo, getStreamUrl } from '../api/client'
import ArtVideoPlayer from '../components/ArtVideoPlayer.vue'

export interface TrackInfo {
  index: number
  language?: string
  title?: string
  isDefault: boolean
}

interface MediaStreamInfo {
  Codec?: string
  Type?: string
  Index: number
  Language?: string
  Title?: string
  IsDefault?: boolean
}

interface MediaSourceInfo {
  Id: string
  Name?: string
  Container?: string
  DirectStreamUrl?: string
  MediaStreams?: MediaStreamInfo[]
  Protocol?: string
  IsRemote?: boolean
  Size?: number
  Bitrate?: number
  FymsResolution?: string
  FymsHdrFormat?: string
  FymsVideoCodec?: string
  FymsAudioCodec?: string
  FymsSource?: string
  FymsQualityLabel?: string
  Path?: string
}

interface PlaybackInfoResponse {
  MediaSources?: MediaSourceInfo[]
  PlaySessionId?: string
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
const mediaSources = ref<MediaSourceInfo[]>([])
const selectedSource = ref<MediaSourceInfo | null>(null)
const selectedSourceId = ref('')
const playSessionId = ref('')
const browserUnsupportedReason = ref('')
const error = ref('')
const loading = ref(true)
const currentPositionTicks = ref(0)

const resolvedItemId = computed(() => {
  const p = route.params.itemId
  return typeof p === 'string' ? p : Array.isArray(p) ? p[0] ?? '' : ''
})

const shouldResume = computed(() => route.query.from !== 'start')

const sourceOptions = computed(() =>
  mediaSources.value.map((source, index) => ({
    label: formatSourceLabel(source, index),
    value: source.Id,
  })),
)

function streamDisplayTitle(stream: MediaStreamInfo): string | undefined {
  return stream.Title || stream.Language
}

function normalizeCodec(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function formatFileSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  const gb = bytes / 1024 / 1024 / 1024
  if (gb >= 1) return `${gb.toFixed(gb >= 10 ? 1 : 2)} GB`
  return `${Math.round(bytes / 1024 / 1024)} MB`
}

function formatSourceLabel(source: MediaSourceInfo, index: number): string {
  const name = source.Name && source.Name !== 'Default' ? source.Name : `版本 ${index + 1}`
  const quality = source.FymsQualityLabel || source.FymsResolution || ''
  const codec = [source.FymsVideoCodec, source.FymsAudioCodec].filter(Boolean).join('/')
  const container = source.Container ? source.Container.toUpperCase() : ''
  const size = formatFileSize(source.Size)
  return [name, quality, codec, container, size].filter(Boolean).join(' · ')
}

function resolveSourceTracks(source: MediaSourceInfo | null) {
  const streams = source?.MediaStreams || []
  audioTracks.value = streams
    .filter((s) => s.Type === 'Audio')
    .map((s) => ({
      index: s.Index,
      language: s.Language,
      title: streamDisplayTitle(s),
      isDefault: !!s.IsDefault,
    }))
  subtitleTracks.value = streams
    .filter((s) => s.Type === 'Subtitle')
    .map((s) => ({
      index: s.Index,
      language: s.Language,
      title: streamDisplayTitle(s),
      isDefault: !!s.IsDefault,
    }))
}

function resolveUnsupportedReason(source: MediaSourceInfo | null): string {
  if (!source) return ''
  const container = normalizeCodec(source.Container)
  const videoCodec = normalizeCodec(source.FymsVideoCodec)
  const audioCodec = normalizeCodec(source.FymsAudioCodec)

  if (container === 'm3u8' || container === 'm3u') {
    if (!(source.IsRemote || normalizeCodec(source.Protocol) === 'http')) {
      return '当前版本仅支持远端 HLS(m3u8) 直链播放，本地 HLS 播单暂不支持。'
    }
    return ''
  }
  if (['mkv', 'ts', 'm2ts'].includes(container)) {
    return '当前浏览器通常无法稳定直放该容器，建议使用外部播放器。'
  }
  if (['hevc', 'h265', 'x265', 'av1'].includes(videoCodec)) {
    return '当前浏览器通常无法直接播放该视频编码，建议使用外部播放器。'
  }
  if (['ac3', 'eac3', 'dts', 'truehd'].includes(audioCodec)) {
    return '当前浏览器通常无法直接播放该音频编码，建议使用外部播放器。'
  }
  return ''
}

function applyMediaSource(source: MediaSourceInfo, positionTicks: number) {
  const id = resolvedItemId.value
  selectedSource.value = source
  selectedSourceId.value = source.Id
  resolveSourceTracks(source)
  browserUnsupportedReason.value = resolveUnsupportedReason(source)
  startPosition.value = Math.max(0, positionTicks)
  streamUrl.value = id ? getStreamUrl(id, source.Id, source.DirectStreamUrl) : ''
}

function handleSourceChange(value: string | number | null) {
  if (value == null) return
  const sourceId = String(value)
  const source = mediaSources.value.find((item) => item.Id === sourceId)
  if (!source || source.Id === selectedSource.value?.Id) return
  applyMediaSource(source, currentPositionTicks.value)
}

async function load() {
  const id = resolvedItemId.value
  if (!id) { loading.value = false; return }
  loading.value = true
  error.value = ''
  streamUrl.value = ''
  mediaSources.value = []
  selectedSource.value = null
  selectedSourceId.value = ''
  playSessionId.value = ''
  browserUnsupportedReason.value = ''
  try {
    const [item, playbackInfo] = await Promise.all([
      getItem(id),
      getPlaybackInfo(id) as Promise<PlaybackInfoResponse>,
    ])
    title.value = formatTitle(item as Record<string, unknown>)
    startPosition.value = shouldResume.value ? item.UserData?.PlaybackPositionTicks || 0 : 0
    currentPositionTicks.value = startPosition.value
    const source = playbackInfo.MediaSources?.[0]
    mediaSources.value = playbackInfo.MediaSources || []
    playSessionId.value = playbackInfo.PlaySessionId || ''
    if (source) {
      applyMediaSource(source, startPosition.value)
    } else {
      error.value = '没有可用的媒体源'
    }
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
function onPositionChange(ticks: number) { currentPositionTicks.value = ticks }
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
      <span class="player-title">{{ title }}</span>
      <n-select
        v-if="sourceOptions.length > 1"
        class="player-source-select"
        size="small"
        :value="selectedSourceId"
        :options="sourceOptions"
        :consistent-menu-width="false"
        @update:value="handleSourceChange"
      />
    </div>
    <div style="flex: 1; position: relative; width: 100%; height: 100%">
      <div v-if="browserUnsupportedReason" class="player-center player-unsupported">
        <div class="player-unsupported-card">
          <h2>当前浏览器无法直接播放</h2>
          <p>{{ browserUnsupportedReason }}</p>
          <p class="player-unsupported-hint">FYMS 不做服务端转码，建议改用 Infuse 等外部播放器播放该资源。</p>
          <n-button secondary style="min-width: 100px" @click="goBack">返回</n-button>
        </div>
      </div>
      <ArtVideoPlayer
        v-else
        :src="streamUrl"
        :item-id="resolvedItemId"
        :media-source-id="selectedSource?.Id || ''"
        :play-session-id="playSessionId"
        :container="selectedSource?.Container || ''"
        :start-position-ticks="startPosition"
        :audio-tracks="audioTracks"
        :subtitle-tracks="subtitleTracks"
        @ended="onEnded"
        @position-change="onPositionChange"
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
.player-unsupported {
  padding: 24px;
}
.player-unsupported-card {
  width: min(560px, 100%);
  padding: 32px 28px;
  border-radius: 24px;
  background: linear-gradient(180deg, rgba(17, 24, 39, 0.96) 0%, rgba(10, 14, 22, 0.94) 100%);
  border: 1px solid rgba(148, 163, 184, 0.2);
  box-shadow: 0 24px 70px rgba(0,0,0,0.42);
  color: rgba(255,255,255,0.9);
}
.player-unsupported-card h2 {
  margin: 0 0 12px;
  font-size: 24px;
  line-height: 1.2;
  font-weight: 700;
}
.player-unsupported-card p {
  margin: 0 0 12px;
  color: rgba(255,255,255,0.72);
  line-height: 1.7;
}
.player-unsupported-hint {
  margin-bottom: 20px !important;
  color: rgba(148, 163, 184, 0.92) !important;
}
.player-top-overlay {
  position: absolute; top: 0; left: 0; right: 0; z-index: 10;
  display: flex; align-items: center; gap: 8px;
  padding: 16px 20px; height: 80px;
  background: linear-gradient(to bottom, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.4) 60%, transparent 100%);
  transition: opacity 0.3s ease;
}
.player-top-overlay:hover { opacity: 1 !important; }
.player-title {
  flex: 1;
  min-width: 0;
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 1px 4px rgba(0,0,0,0.6);
}
.player-source-select {
  width: min(360px, 34vw);
  flex-shrink: 0;
}
.player-source-select .n-base-selection {
  background: rgba(15, 23, 42, 0.72);
  border-color: rgba(255, 255, 255, 0.18);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.player-source-select .n-base-selection-label {
  color: rgba(255,255,255,0.9);
}
.player-back-btn {
  color: #fff; flex-shrink: 0; width: 36px; height: 36px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%; background: rgba(255,255,255,0.1);
  border: none; cursor: pointer; transition: background 0.2s ease;
}
.player-back-btn:hover { background: rgba(255,255,255,0.2); }

@media (max-width: 720px) {
  .player-top-overlay {
    height: auto;
    align-items: flex-start;
    flex-wrap: wrap;
    padding: 12px;
  }
  .player-title {
    flex-basis: calc(100% - 44px);
  }
  .player-source-select {
    width: 100%;
    margin-left: 44px;
  }
}
</style>
