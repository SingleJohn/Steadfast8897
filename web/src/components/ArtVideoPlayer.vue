<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import Artplayer from 'artplayer'
import type Hls from 'hls.js'
import {
  reportPlaybackProgress,
  reportPlaybackStart,
  reportPlaybackStopped,
} from '../api/client'

const TICKS_PER_SECOND = 10_000_000
const PLAYBACK_PROGRESS_INTERVAL_MS = 10_000

export interface AudioTrack {
  index: number
  language?: string
  title?: string
  isDefault: boolean
}

export interface SubtitleTrack {
  index: number
  language?: string
  title?: string
  isDefault: boolean
}

const props = withDefaults(
  defineProps<{
    src: string
    itemId: string
    mediaSourceId?: string
    playSessionId?: string
    container?: string
    startPositionTicks?: number
    audioTracks?: AudioTrack[]
    subtitleTracks?: SubtitleTrack[]
  }>(),
  {
    mediaSourceId: '',
    playSessionId: '',
    container: '',
    startPositionTicks: 0,
    audioTracks: () => [],
    subtitleTracks: () => [],
  },
)

const emit = defineEmits<{
  ended: []
  'position-change': [ticks: number]
}>()

const playerRootRef = ref<HTMLDivElement | null>(null)
let art: Artplayer | null = null
let hlsInstance: Hls | null = null
let progressTimer: ReturnType<typeof setInterval> | undefined
let hasReportedStart = false
let lastReportedTicks = 0
let currentPlaybackKey = ''

function currentTicks() {
  if (!art) return 0
  return Math.floor(art.currentTime * TICKS_PER_SECOND)
}

function startPositionSeconds() {
  return Math.max(0, (props.startPositionTicks ?? 0) / TICKS_PER_SECOND)
}

function seekToStartPosition() {
  if (!art) return
  const seconds = startPositionSeconds()
  if (seconds <= 0) return
  art.currentTime = seconds
  lastReportedTicks = props.startPositionTicks ?? 0
  emit('position-change', lastReportedTicks)
}

function destroyHls() {
  if (hlsInstance) {
    hlsInstance.destroy()
    hlsInstance = null
  }
}

function clearProgressTimer() {
  if (progressTimer !== undefined) {
    clearInterval(progressTimer)
    progressTimer = undefined
  }
}

function getPlaybackPayload(positionTicks = currentTicks(), isPaused?: boolean) {
  return {
    itemId: props.itemId,
    positionTicks,
    ...(isPaused !== undefined ? { isPaused } : {}),
    ...(props.mediaSourceId ? { mediaSourceId: props.mediaSourceId } : {}),
    ...(props.playSessionId ? { playSessionId: props.playSessionId } : {}),
  }
}

function stopPlaybackReporting() {
  clearProgressTimer()
  if (!hasReportedStart) return
  hasReportedStart = false
  void reportPlaybackStopped(getPlaybackPayload(lastReportedTicks || currentTicks()))
}

function startProgressTimer() {
  clearProgressTimer()
  progressTimer = setInterval(() => {
    if (!art) return
    const ticks = currentTicks()
    lastReportedTicks = ticks
    void reportPlaybackProgress(getPlaybackPayload(ticks, art.video.paused))
  }, PLAYBACK_PROGRESS_INTERVAL_MS)
}

function startPlaybackReporting() {
  if (hasReportedStart) return
  hasReportedStart = true
  const ticks = currentTicks() || props.startPositionTicks || 0
  lastReportedTicks = ticks
  void reportPlaybackStart(getPlaybackPayload(ticks))
  startProgressTimer()
}

function isHlsSource(url: string, container?: string) {
  const normalizedContainer = (container || '').trim().toLowerCase()
  if (normalizedContainer === 'm3u8' || normalizedContainer === 'm3u') {
    return true
  }
  return /\.m3u8($|\?)/i.test(url)
}

async function bindHls(video: HTMLVideoElement, url: string) {
  destroyHls()
  const HlsModule = await import('hls.js')
  const HlsCtor = HlsModule.default
  if (HlsCtor.isSupported()) {
    const instance = new HlsCtor()
    instance.loadSource(url)
    instance.attachMedia(video)
    hlsInstance = instance
    return
  }
  if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = url
    return
  }
  throw new Error('HLS_NOT_SUPPORTED')
}

function buildArt(autoplay = true) {
  if (!playerRootRef.value) return

  destroyArt()

  art = new Artplayer({
    container: playerRootRef.value,
    url: props.src,
    type: isHlsSource(props.src, props.container) ? 'm3u8' : undefined,
    customType: {
      m3u8: (video, url) => {
        void bindHls(video, url).catch(() => {
          if (art) {
            art.notice.show = '当前浏览器不支持播放该 HLS 资源'
          }
        })
      },
    },
    autoplay,
    autoSize: true,
    fullscreen: true,
    fullscreenWeb: true,
    playbackRate: true,
    aspectRatio: true,
    setting: true,
    miniProgressBar: true,
    backdrop: true,
    mutex: true,
    hotkey: true,
    playsInline: true,
    lang: 'zh-cn',
    moreVideoAttr: {
      preload: 'auto',
      crossOrigin: 'anonymous',
    },
  })

  art.on('ready', () => {
    seekToStartPosition()
  })

  art.on('video:timeupdate', () => {
    lastReportedTicks = currentTicks()
    emit('position-change', lastReportedTicks)
  })

  art.on('play', () => {
    startPlaybackReporting()
  })

  art.on('pause', () => {
    if (!art || !hasReportedStart) return
    lastReportedTicks = currentTicks()
    void reportPlaybackProgress(getPlaybackPayload(lastReportedTicks, true))
  })

  art.on('ended', () => {
    lastReportedTicks = currentTicks()
    stopPlaybackReporting()
    emit('ended')
  })

  art.on('error', () => {
    lastReportedTicks = currentTicks()
  })
}

function destroyArt() {
  stopPlaybackReporting()
  destroyHls()
  if (art) {
    art.destroy(false)
    art = null
  }
}

watch(
  () => [props.itemId, props.mediaSourceId, props.playSessionId] as const,
  ([itemId, mediaSourceId, playSessionId]) => {
    const nextKey = [itemId, mediaSourceId, playSessionId].join(':')
    if (nextKey === currentPlaybackKey) return
    currentPlaybackKey = nextKey
    stopPlaybackReporting()
    hasReportedStart = false
    lastReportedTicks = props.startPositionTicks ?? 0
  },
  { immediate: true },
)

watch(
  () => [props.src, props.container] as const,
  async ([src, container], prev) => {
    if (!src || !art) return
    if (!prev) return
    const [prevSrc, prevContainer] = prev
    if (src === prevSrc && container === prevContainer) return
    stopPlaybackReporting()
    hasReportedStart = false
    lastReportedTicks = props.startPositionTicks ?? 0
    const shouldResumePlay = art.playing || !art.video.paused
    const typeChanged = isHlsSource(src, container) !== isHlsSource(prevSrc, prevContainer)
    if (typeChanged) {
      buildArt(shouldResumePlay)
      return
    }
    destroyHls()
    await art.switchUrl(src)
    seekToStartPosition()
    if (shouldResumePlay) {
      void art.play().catch(() => {
        // 浏览器可能拦截自动播放,保留手动播放入口。
      })
    }
  },
)

watch(
  () => props.startPositionTicks,
  (value) => {
    if (!art) return
    if ((value ?? 0) <= 0) return
    seekToStartPosition()
  },
)

onMounted(() => {
  buildArt()
})

onUnmounted(() => {
  destroyArt()
})
</script>

<template>
  <div ref="playerRootRef" class="art-video-player" />
</template>

<style scoped>
.art-video-player {
  width: 100%;
  height: 100%;
  background:
    radial-gradient(circle at top, rgba(14, 165, 233, 0.14), transparent 30%),
    linear-gradient(180deg, rgba(2, 6, 23, 0.96) 0%, rgba(0, 0, 0, 1) 100%);
}

:deep(.art-video-player .art-mask),
:deep(.art-video-player .art-poster),
:deep(.art-video-player .art-video-player) {
  background-color: transparent;
}

:deep(.art-video-player .art-bottom),
:deep(.art-video-player .art-top) {
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
</style>
