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
  isExternal?: boolean
  isTextSubtitle?: boolean
  codec?: string
  // 已带 api_key、可直接拉取的外挂字幕地址；内封字幕为空。
  url?: string
}

const props = withDefaults(
  defineProps<{
    src: string
    itemId: string
    mediaSourceId?: string
    playSessionId?: string
    container?: string
    startPositionTicks?: number
    bitrate?: number
    sizeBytes?: number
    audioTracks?: AudioTrack[]
    subtitleTracks?: SubtitleTrack[]
  }>(),
  {
    mediaSourceId: '',
    playSessionId: '',
    container: '',
    startPositionTicks: 0,
    bitrate: 0,
    sizeBytes: 0,
    audioTracks: () => [],
    subtitleTracks: () => [],
  },
)

const emit = defineEmits<{
  ended: []
  'position-change': [ticks: number]
  // 初始缓冲状态:true=正在缓冲(显示加载层),首次 false 后由父级永久隐藏加载层。
  buffering: [active: boolean]
  // 加载统计:已缓冲提前量(秒)与下载速率(字节/秒)。
  loadstats: [stats: { bufferedSeconds: number; speedBps: number }]
  // 直放真正失败(解码/封装致命错误):父级回退到外部播放器提示。
  unsupported: []
}>()

const playerRootRef = ref<HTMLDivElement | null>(null)
const wrapRef = ref<HTMLDivElement | null>(null)
// 手势反馈层状态
const seekHint = ref<{ dir: 'forward' | 'backward'; seconds: number } | null>(null)
const speedBoostActive = ref(false)
const HOLD_SPEED = 2
const RATE_KEY = 'fyms-playback-rate'
let art: Artplayer | null = null
let hlsInstance: Hls | null = null
let progressTimer: ReturnType<typeof setInterval> | undefined
let hasReportedStart = false
let lastReportedTicks = 0
let currentPlaybackKey = ''

// 初始缓冲测速:采样 buffered 增长量 × 码率推算下载速率(HLS 优先用 bandwidthEstimate)。
let statsTimer: ReturnType<typeof setInterval> | undefined
let lastSampleTs = 0
let lastBufferedEnd = 0
let smoothedBps = 0

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

// bufferedEnd 返回覆盖当前播放点的缓冲区末端(秒),用于计算缓冲提前量与增长速率。
function bufferedEnd(): number {
  const video = art?.video
  if (!video) return 0
  const ranges = video.buffered
  if (!ranges || ranges.length === 0) return 0
  const t = video.currentTime
  for (let i = 0; i < ranges.length; i++) {
    if (t >= ranges.start(i) - 0.5 && t <= ranges.end(i) + 0.5) return ranges.end(i)
  }
  return ranges.end(ranges.length - 1)
}

function stopStatsTimer() {
  if (statsTimer !== undefined) {
    clearInterval(statsTimer)
    statsTimer = undefined
  }
}

// currentBitrate 优先用元数据码率;缺失时用 文件大小 × 8 / 视频真实时长 兜底推算。
function currentBitrate(): number {
  if (props.bitrate > 0) return props.bitrate
  const dur = art?.video?.duration
  if (props.sizeBytes > 0 && dur && isFinite(dur) && dur > 0) {
    return (props.sizeBytes * 8) / dur
  }
  return 0
}

// 测速贯穿整个播放周期(加载层 + 播放中):稳定播放时缓冲区随播放点同步推进,
// 「缓冲增长 × 码率」恰好反映当前下载吞吐(≈ 码率);初始缓冲/补缓时则飙高。HLS 用 bandwidthEstimate。
function startStatsTimer() {
  stopStatsTimer()
  lastSampleTs = performance.now()
  lastBufferedEnd = bufferedEnd()
  smoothedBps = 0
  statsTimer = setInterval(() => {
    if (!art?.video) return
    const now = performance.now()
    const end = bufferedEnd()
    const dt = (now - lastSampleTs) / 1000

    let sample = 0
    const estimate = (hlsInstance as unknown as { bandwidthEstimate?: number } | null)?.bandwidthEstimate
    const br = currentBitrate()
    if (estimate && estimate > 0) {
      sample = estimate / 8
    } else if (br > 0 && dt > 0) {
      // 限制单次缓冲增长上限,避免 seek 跳变导致的瞬时尖峰。
      const grown = Math.min(Math.max(0, end - lastBufferedEnd), dt * 30)
      sample = (grown * br) / 8 / dt
    }

    // EWMA 平滑,读数不跳变。
    smoothedBps = smoothedBps > 0 ? smoothedBps * 0.55 + sample * 0.45 : sample

    const ahead = Math.max(0, end - art.video.currentTime)
    emit('loadstats', { bufferedSeconds: ahead, speedBps: smoothedBps })
    lastSampleTs = now
    lastBufferedEnd = end
  }, 600)
}

// setBuffering 全程上报缓冲状态:初始缓冲、拖动、卡顿都触发。
// 父级据 playbackStarted 区分「初始全屏加载层」与「播放中迷你环」。
function setBuffering(active: boolean) {
  emit('buffering', active)
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

const SUBTITLE_SETTING_NAME = 'fyms-subtitle'

// subtitleTypeFromCodec 把存储的字幕 codec 归一为 ArtPlayer 识别的类型。
// ArtPlayer 内部会把 srt/ass 转成 vtt 渲染(ass 富样式会丢失，仅保留文本)。
function subtitleTypeFromCodec(codec?: string): 'vtt' | 'srt' | 'ass' {
  const c = (codec || '').trim().toLowerCase().replace(/^\./, '')
  if (c === 'vtt' || c === 'webvtt') return 'vtt'
  if (c === 'ass' || c === 'ssa') return 'ass'
  return 'srt'
}

function subtitleLabel(track: SubtitleTrack): string {
  return track.title || track.language || `字幕 ${track.index}`
}

// playableSubtitles 仅保留带就绪地址的外挂文本字幕；内封字幕无法在不转码前提下提取。
function playableSubtitles(): SubtitleTrack[] {
  return (props.subtitleTracks || []).filter((t) => !!t.url)
}

function applySubtitleSelection(item: { value: string; subType?: 'vtt' | 'srt' | 'ass'; html: string }) {
  if (!art) return
  if (!item.value) {
    art.subtitle.show = false
    return
  }
  void art.subtitle.switch(item.value, { type: item.subType, name: item.html, escape: false })
  art.subtitle.show = true
}

// refreshSubtitleSetting 依据当前字幕轨重建设置面板里的「字幕」选择器，并应用默认轨。
// 切换版本(switchUrl 不重建实例)或字幕轨变化时调用，保证菜单与轨道同步。
function refreshSubtitleSetting() {
  if (!art) return
  const subs = playableSubtitles()
  const def = subs.find((s) => s.isDefault) || null
  const selector = [
    { html: '关闭', value: '', default: !def },
    ...subs.map((s) => ({
      html: subtitleLabel(s),
      value: s.url as string,
      subType: subtitleTypeFromCodec(s.codec),
      default: def ? s.index === def.index : false,
    })),
  ]

  try { art.setting.remove(SUBTITLE_SETTING_NAME) } catch { /* 首次无此项 */ }
  if (subs.length) {
    art.setting.add({
      name: SUBTITLE_SETTING_NAME,
      html: '字幕',
      tooltip: def ? subtitleLabel(def) : '关闭',
      width: 250,
      selector,
      onSelect(item: { html: string;[key: string]: any }) {
        applySubtitleSelection({ value: item.value, subType: item.subType, html: item.html })
        return item.html
      },
    })
  }

  if (def?.url) {
    void art.subtitle.switch(def.url, { type: subtitleTypeFromCodec(def.codec), name: subtitleLabel(def), escape: false })
    art.subtitle.show = true
  } else {
    art.subtitle.show = false
  }
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

// ---- 倍速记忆 ----
function applyStoredRate() {
  if (!art) return
  const saved = Number(localStorage.getItem(RATE_KEY))
  if (saved && saved > 0 && saved !== 1) art.playbackRate = saved as never
}

// ---- 控制栏前进/后退按钮(ArtPlayer 不自带) ----
function setupControls() {
  if (!art) return
  art.controls.add({
    name: 'fyms-backward',
    position: 'left',
    index: 11,
    tooltip: '后退 10 秒',
    html: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>',
    click: () => { if (art) { art.backward = 10 } },
  })
  art.controls.add({
    name: 'fyms-forward',
    position: 'left',
    index: 12,
    tooltip: '前进 10 秒',
    html: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>',
    click: () => { if (art) { art.forward = 10 } },
  })
}

// ---- 扩展快捷键(桌面端,ArtPlayer 默认未绑 k/j/l/f/m) ----
function setupHotkeys() {
  if (!art) return
  art.hotkey.add('k', () => art && art.toggle())
  art.hotkey.add('j', () => { if (art) { art.backward = 10; art.notice.show = '« 10 秒' } })
  art.hotkey.add('l', () => { if (art) { art.forward = 10; art.notice.show = '» 10 秒' } })
  art.hotkey.add('f', () => { if (art) art.fullscreen = !art.fullscreen })
  art.hotkey.add('m', () => { if (art) art.muted = !art.muted })
}

// ---- 手势:长按倍速 + 双击左右快进退 ----
let holdTimer: ReturnType<typeof setTimeout> | undefined
let holdRatePrev = 1
let holdActive = false
let suppressClick = false
let seekDir: 'forward' | 'backward' | null = null
let seekAccum = 0
let seekHintTimer: ReturnType<typeof setTimeout> | undefined

function isControlTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null
  return !!el?.closest?.('.art-bottom, .art-top, .art-controls, .art-control, .art-settings, .art-setting, .art-layers, .art-contextmenus, .art-info')
}

function clearHoldTimer() {
  if (holdTimer) { clearTimeout(holdTimer); holdTimer = undefined }
}

function endHold() {
  clearHoldTimer()
  if (holdActive) {
    holdActive = false
    speedBoostActive.value = false
    if (art) art.playbackRate = holdRatePrev as never
  }
}

function onPointerDown(e: PointerEvent) {
  suppressClick = false
  if (!art || isControlTarget(e.target)) return
  if (e.pointerType === 'mouse' && e.button !== 0) return
  clearHoldTimer()
  holdTimer = setTimeout(() => {
    if (!art) return
    holdActive = true
    suppressClick = true
    holdRatePrev = art.playbackRate as number
    speedBoostActive.value = true
    art.playbackRate = HOLD_SPEED as never
  }, 350)
}

function onClickCapture(e: MouseEvent) {
  // 长按结束后浏览器仍会补发 click,拦掉避免误触发暂停。
  if (suppressClick) {
    suppressClick = false
    e.stopPropagation()
    e.preventDefault()
  }
}

function onDblClick(e: MouseEvent) {
  if (!art || !wrapRef.value || isControlTarget(e.target)) return
  const rect = wrapRef.value.getBoundingClientRect()
  const ratio = (e.clientX - rect.left) / rect.width
  if (ratio > 0.35 && ratio < 0.65) return // 中间:让 ArtPlayer 处理全屏
  // 侧边:拦截全屏,执行快进/快退并叠加
  e.stopPropagation()
  e.preventDefault()
  const dir: 'forward' | 'backward' = ratio <= 0.35 ? 'backward' : 'forward'
  if (dir !== seekDir) { seekAccum = 0; seekDir = dir }
  seekAccum += 10
  if (dir === 'forward') art.forward = 10
  else art.backward = 10
  seekHint.value = { dir, seconds: seekAccum }
  if (seekHintTimer) clearTimeout(seekHintTimer)
  seekHintTimer = setTimeout(() => { seekHint.value = null; seekDir = null; seekAccum = 0 }, 800)
}

function bindGestures() {
  const el = playerRootRef.value
  if (!el) return
  el.addEventListener('pointerdown', onPointerDown)
  el.addEventListener('pointerup', endHold)
  el.addEventListener('pointercancel', endHold)
  el.addEventListener('pointerleave', endHold)
  el.addEventListener('click', onClickCapture, true)
  el.addEventListener('dblclick', onDblClick, true)
}

function unbindGestures() {
  const el = playerRootRef.value
  if (!el) return
  el.removeEventListener('pointerdown', onPointerDown)
  el.removeEventListener('pointerup', endHold)
  el.removeEventListener('pointercancel', endHold)
  el.removeEventListener('pointerleave', endHold)
  el.removeEventListener('click', onClickCapture, true)
  el.removeEventListener('dblclick', onDblClick, true)
}

// 供父级在结束页触发重播。
function replay() {
  if (!art) return
  art.currentTime = 0
  void art.play().catch(() => {})
}
defineExpose({ replay })

function buildArt(autoplay = true) {
  if (!playerRootRef.value) return

  destroyArt()

  const artOptions: ConstructorParameters<typeof Artplayer>[0] = {
    container: playerRootRef.value,
    url: props.src,
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
    pip: true,
    screenshot: true,
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
  }

  if (isHlsSource(props.src, props.container)) {
    artOptions.type = 'm3u8'
  }

  art = new Artplayer(artOptions)

  art.on('ready', () => {
    seekToStartPosition()
    refreshSubtitleSetting()
    applyStoredRate()
    setupHotkeys()
    setupControls()
  })

  // 倍速记忆:用户切换倍速时持久化;长按 2x 期间不写入,避免覆盖偏好。
  art.on('video:ratechange', () => {
    if (!art || speedBoostActive.value) return
    localStorage.setItem(RATE_KEY, String(art.playbackRate))
  })

  // 首帧可播放即结束初始缓冲;等待数据时(仅初始阶段)重新显示加载层。
  art.on('video:playing', () => setBuffering(false))
  art.on('video:canplaythrough', () => setBuffering(false))
  art.on('video:waiting', () => setBuffering(true))

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

  // seek 后重置测速基线,避免缓冲区跳变产生尖峰。
  art.on('video:seeked', () => {
    lastBufferedEnd = bufferedEnd()
    lastSampleTs = performance.now()
  })

  art.on('error', () => {
    lastReportedTicks = currentTicks()
  })

  // 原生 video 致命错误:解码失败(3)或源/格式不被支持(4)→ 通知父级回退。
  art.on('video:error', () => {
    const code = art?.video?.error?.code
    if (code === 3 || code === 4) {
      stopStatsTimer()
      emit('unsupported')
    }
  })

  setBuffering(true)
  startStatsTimer()
}

function destroyArt() {
  stopPlaybackReporting()
  stopStatsTimer()
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
    setBuffering(true)
    startStatsTimer()
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

// 字幕轨变化(切换版本等)时同步设置面板。初次构建由 ready 事件兜底，故不用 immediate。
watch(
  () => props.subtitleTracks,
  () => { refreshSubtitleSetting() },
  { deep: true },
)

onMounted(() => {
  buildArt()
  bindGestures()
})

onUnmounted(() => {
  endHold()
  if (seekHintTimer) clearTimeout(seekHintTimer)
  unbindGestures()
  destroyArt()
})
</script>

<template>
  <div ref="wrapRef" class="art-video-wrap">
    <div ref="playerRootRef" class="art-video-player" />

    <transition name="gesture-fade">
      <div v-if="seekHint" class="gesture-seek" :class="seekHint.dir">
        <span class="gesture-seek-arrow">{{ seekHint.dir === 'forward' ? '»' : '«' }}</span>
        <span class="gesture-seek-text">{{ seekHint.seconds }} 秒</span>
      </div>
    </transition>

    <transition name="gesture-fade">
      <div v-if="speedBoostActive" class="gesture-speed">{{ HOLD_SPEED }}x 倍速 ▶▶</div>
    </transition>
  </div>
</template>

<style scoped>
.art-video-wrap {
  position: relative;
  width: 100%;
  height: 100%;
}
.art-video-player {
  width: 100%;
  height: 100%;
  background:
    radial-gradient(circle at top, rgba(14, 165, 233, 0.14), transparent 30%),
    linear-gradient(180deg, rgba(2, 6, 23, 0.96) 0%, rgba(0, 0, 0, 1) 100%);
}

/* 手势反馈层:不拦截点击 */
.gesture-seek,
.gesture-speed {
  position: absolute;
  z-index: 20;
  pointer-events: none;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #fff;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.6);
}
.gesture-seek {
  top: 50%;
  transform: translateY(-50%);
  flex-direction: column;
  gap: 4px;
  padding: 18px 30px;
  border-radius: 16px;
  background: rgba(2, 6, 23, 0.5);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}
.gesture-seek.backward { left: 12%; }
.gesture-seek.forward { right: 12%; }
.gesture-seek-arrow { font-size: 30px; line-height: 1; letter-spacing: -4px; }
.gesture-seek-text { font-size: 15px; }
.gesture-speed {
  top: 24px;
  left: 50%;
  transform: translateX(-50%);
  padding: 8px 16px;
  border-radius: 18px;
  font-size: 14px;
  background: rgba(14, 165, 233, 0.32);
  border: 1px solid rgba(56, 189, 248, 0.7);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
}
.gesture-fade-enter-active,
.gesture-fade-leave-active { transition: opacity 0.2s ease; }
.gesture-fade-enter-from,
.gesture-fade-leave-to { opacity: 0; }

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

/* 隐藏 ArtPlayer 自带加载动画(初始/拖动/卡顿),统一用 FYMS 自定义加载层。 */
:deep(.art-video-player .art-loading) {
  display: none !important;
}
</style>
