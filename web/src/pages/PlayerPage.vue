<script setup lang="ts">
import { ref, watch, computed, defineAsyncComponent, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSelect } from 'naive-ui'
import { getImageUrl, getItem, getItems, getPlaybackInfo, getStreamUrl, getSubtitleUrl } from '../api/client'
import PlayerLoading from '../components/PlayerLoading.vue'
import ExternalPlayMenu from './item-detail/components/ExternalPlayMenu.vue'
import { resolveUnsupportedReason } from '@/utils/playerSupport'

export interface TrackInfo {
  index: number
  language?: string
  title?: string
  isDefault: boolean
  // 字幕轨专用(音轨忽略)：外挂文本字幕的就绪播放地址与格式。
  isExternal?: boolean
  isTextSubtitle?: boolean
  codec?: string
  url?: string
}

interface MediaStreamInfo {
  Codec?: string
  Type?: string
  Index: number
  Language?: string
  Title?: string
  IsDefault?: boolean
  IsExternal?: boolean
  IsTextSubtitleStream?: boolean
  DeliveryUrl?: string
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
const ArtVideoPlayer = defineAsyncComponent(() => import('../components/ArtVideoPlayer.vue'))

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
const currentItem = ref<Record<string, any> | null>(null)
const autoplayNext = ref(localStorage.getItem('fyms-autoplay-next') !== '0')

// 初始缓冲态:加载层在「拉取元数据」与「首帧前缓冲」两阶段显示,首帧后永久隐藏。
const playbackStarted = ref(false)
const isBuffering = ref(false)
const bufferedSeconds = ref(0)
const loadSpeedBps = ref(0)

// 结束态:下一集倒计时(Netflix 风格)与重播页
const playerRef = ref<{ replay: () => void } | null>(null)
const nextCountdown = ref(0)
const pendingNextId = ref('')
const showReplay = ref(false)
let nextTimer: ReturnType<typeof setInterval> | undefined

const isEpisode = computed(() => currentItem.value?.Type === 'Episode')

// 码率缺失(常见于未刮削的 mkv)时用 体积/时长 兜底,使「缓冲增长×码率」仍能测速。
const effectiveBitrate = computed(() => {
  const s = selectedSource.value
  if (!s) return 0
  if (s.Bitrate && s.Bitrate > 0) return s.Bitrate
  const ticks = Number(currentItem.value?.RunTimeTicks) || 0
  const size = s.Size || 0
  if (ticks > 0 && size > 0) return Math.round((size * 8) / (ticks / 10_000_000))
  return 0
})

const showLoadingOverlay = computed(() =>
  !error.value && !browserUnsupportedReason.value && (loading.value || (!!streamUrl.value && !playbackStarted.value)),
)

// 播放中(首帧后)的拖动/卡顿:用迷你环替代 ArtPlayer 默认动画。
const showMiniBuffer = computed(() =>
  !error.value && !browserUnsupportedReason.value && playbackStarted.value && isBuffering.value,
)

const loadingBackdrop = computed(() => {
  const it = currentItem.value
  if (!it) return ''
  if (Array.isArray(it.BackdropImageTags) && it.BackdropImageTags.length) return getImageUrl(it.Id, 'Backdrop', 1280)
  if (it.ParentBackdropItemId) return getImageUrl(it.ParentBackdropItemId, 'Backdrop', 1280)
  const primaryId = it.SeriesPrimaryImageItemId || it.Id
  if (it.ImageTags?.Primary || it.SeriesPrimaryImageItemId) return getImageUrl(primaryId, 'Primary', 780)
  return ''
})

const loadPhaseText = computed(() => (loading.value || !streamUrl.value ? '正在准备播放' : '正在缓冲'))

const loadSpeedText = computed(() => {
  const bps = loadSpeedBps.value
  if (bps <= 0) return ''
  const mb = bps / 1024 / 1024
  if (mb >= 1) return `${mb.toFixed(1)} MB/s`
  return `${Math.max(1, Math.round(bps / 1024))} KB/s`
})

const bufferedText = computed(() => (bufferedSeconds.value > 0 ? `已缓冲 ${bufferedSeconds.value.toFixed(1)}s` : ''))

// 进度条:缓冲提前量向 15s「可流畅起播」目标推进。
const loadProgress = computed(() => Math.min(100, (bufferedSeconds.value / 15) * 100))

function onBuffering(active: boolean) {
  isBuffering.value = active
  if (!active) {
    playbackStarted.value = true
    startPendingSpeedProbe()
  }
}

function onLoadStats(stats: { bufferedSeconds: number; speedBps: number }) {
  bufferedSeconds.value = stats.bufferedSeconds
  // 实测探针进行中不被码率估算覆盖;探针结束后由估算持续反映实时速率(含空闲回落)。
  if (!probing.value) loadSpeedBps.value = stats.speedBps
}

// 直放尝试后真正失败(预判放行但浏览器实际解不了)→ 回退到外部播放器提示。
function onPlayerUnsupported() {
  if (browserUnsupportedReason.value) return
  browserUnsupportedReason.value = '该资源在当前浏览器播放失败(可能是编码或封装不被支持)，建议使用外部播放器。'
}

function toggleAutoplay() {
  autoplayNext.value = !autoplayNext.value
  localStorage.setItem('fyms-autoplay-next', autoplayNext.value ? '1' : '0')
}

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
      isExternal: !!s.IsExternal,
      isTextSubtitle: !!s.IsTextSubtitleStream,
      codec: s.Codec,
      // 仅外挂文本字幕带 DeliveryUrl；内封字幕无地址(不转码无法提取)。
      url: s.DeliveryUrl ? getSubtitleUrl(s.DeliveryUrl) : '',
    }))
}

// unsupportedDetails 汇总当前版本的容器/编码等技术规格,在无法直放时展示,便于排查。
const unsupportedDetails = computed(() => {
  const s = selectedSource.value
  if (!s) return [] as { label: string; value: string }[]
  // 容器/视频编码/音频编码固定展示(空显示「未知」,缺失本身也是排查线索)。
  const rows: { label: string; value: string }[] = [
    { label: '容器', value: s.Container ? String(s.Container).toUpperCase() : '未知' },
    { label: '视频编码', value: s.FymsVideoCodec ? s.FymsVideoCodec.toUpperCase() : '未知' },
    { label: '音频编码', value: s.FymsAudioCodec ? s.FymsAudioCodec.toUpperCase() : '未知' },
  ]
  if (s.FymsResolution) rows.push({ label: '分辨率', value: s.FymsResolution })
  if (s.FymsHdrFormat) rows.push({ label: 'HDR', value: s.FymsHdrFormat })
  if (s.Protocol) rows.push({ label: '协议', value: String(s.Protocol).toUpperCase() })
  return rows
})

// 实测网速:自己 fetch 一段流、按 ReadableStream 读到的真实字节数计速。
// 直出 <video> 浏览器不暴露下载字节,只能这样拿真实速率;HLS 走 hls.bandwidthEstimate 不用此法。
let speedProbeController: AbortController | null = null
let pendingSpeedProbe: { url: string; container?: string } | null = null
const probing = ref(false)
const probeFileSize = ref(0) // 探针从响应 Content-Length 拿到的真实文件大小,用于推算码率。

function stopSpeedProbe() {
  if (speedProbeController) {
    speedProbeController.abort()
    speedProbeController = null
  }
  pendingSpeedProbe = null
  probing.value = false
}

function startPendingSpeedProbe() {
  const probe = pendingSpeedProbe
  pendingSpeedProbe = null
  if (!probe) return
  void startSpeedProbe(probe.url, probe.container)
}

async function startSpeedProbe(url: string, container?: string) {
  stopSpeedProbe()
  const c = (container || '').trim().toLowerCase()
  if (!url || c === 'm3u8' || c === 'm3u') return // HLS 用 bandwidthEstimate
  const controller = new AbortController()
  speedProbeController = controller
  probing.value = true
  const PROBE_BYTES = 4 * 1024 * 1024
  try {
    // 不带 Range 头:避免后缀范围 416 与跨域预检;读够 PROBE_BYTES 后主动中止,不会下整文件。
    const resp = await fetch(url, { signal: controller.signal, cache: 'no-store' })
    if (!resp.ok || !resp.body) {
      await resp.body?.cancel().catch(() => {})
      probing.value = false
      return
    }
    const len = Number(resp.headers.get('Content-Length') || 0)
    if (len > 0) probeFileSize.value = len // 真实文件大小 → 供码率推算
    const reader = resp.body.getReader()
    const startTs = performance.now()
    let lastTs = startTs
    let windowBytes = 0
    let totalBytes = 0
    let smoothed = 0
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      const n = value?.length || 0
      windowBytes += n
      totalBytes += n
      const now = performance.now()
      if (now - lastTs >= 400) {
        const sample = windowBytes / ((now - lastTs) / 1000)
        smoothed = smoothed > 0 ? smoothed * 0.5 + sample * 0.5 : sample
        loadSpeedBps.value = smoothed
        windowBytes = 0
        lastTs = now
      }
      if (totalBytes >= PROBE_BYTES || now - startTs > 8000) break
    }
    await reader.cancel().catch(() => {})
  } catch {
    // CORS / 中断 / 网络错误 → 回退码率估算。
  } finally {
    if (speedProbeController === controller) speedProbeController = null
    probing.value = false
  }
}

function applyMediaSource(source: MediaSourceInfo, positionTicks: number) {
  const id = resolvedItemId.value
  selectedSource.value = source
  selectedSourceId.value = source.Id
  resolveSourceTracks(source)
  browserUnsupportedReason.value = resolveUnsupportedReason(source)
  startPosition.value = Math.max(0, positionTicks)
  streamUrl.value = id ? getStreamUrl(id, source.Id, source.DirectStreamUrl) : ''
  if (streamUrl.value) {
    pendingSpeedProbe = { url: streamUrl.value, container: source.Container }
    if (playbackStarted.value) startPendingSpeedProbe()
  }
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
  stopSpeedProbe()
  resetEndState()
  probeFileSize.value = 0
  playbackStarted.value = false
  isBuffering.value = false
  bufferedSeconds.value = 0
  loadSpeedBps.value = 0
  try {
    const [item, playbackInfo] = await Promise.all([
      getItem(id),
      getPlaybackInfo(id) as Promise<PlaybackInfoResponse>,
    ])
    title.value = formatTitle(item as Record<string, unknown>)
    currentItem.value = item as Record<string, any>
    startPosition.value = shouldResume.value ? item.UserData?.PlaybackPositionTicks || 0 : 0
    currentPositionTicks.value = startPosition.value
    mediaSources.value = playbackInfo.MediaSources || []
    // 详情页选定的版本通过 query.mediaSourceId 传入；命中则用之，否则默认首个。
    const wantSourceId = typeof route.query.mediaSourceId === 'string' ? route.query.mediaSourceId : ''
    const source = (wantSourceId && mediaSources.value.find((s) => s.Id === wantSourceId)) || mediaSources.value[0]
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

// resolveNextEpisodeId 找当前剧集的下一集:优先同季下一集,本季播完则取下一季首集。
async function resolveNextEpisodeId(): Promise<string> {
  const it = currentItem.value
  if (!it || it.Type !== 'Episode') return ''
  const seasonId = (it.SeasonId || it.ParentId) as string | undefined
  if (!seasonId) return ''
  try {
    const eps = (await getItems({ ParentId: seasonId, SortBy: 'IndexNumber', SortOrder: 'Ascending' })).Items || []
    const idx = eps.findIndex((e: any) => e.Id === it.Id)
    if (idx >= 0 && idx + 1 < eps.length) return eps[idx + 1].Id

    const seriesId = it.SeriesId as string | undefined
    if (seriesId) {
      const seasons = (await getItems({ ParentId: seriesId, SortBy: 'IndexNumber', SortOrder: 'Ascending' })).Items || []
      const sIdx = seasons.findIndex((s: any) => s.Id === seasonId)
      if (sIdx >= 0 && sIdx + 1 < seasons.length) {
        const nextEps = (await getItems({ ParentId: seasons[sIdx + 1].Id, SortBy: 'IndexNumber', SortOrder: 'Ascending' })).Items || []
        if (nextEps.length) return nextEps[0].Id
      }
    }
  } catch {
    // 查询失败时退化为返回上一页,不阻断。
  }
  return ''
}

function clearNextTimer() {
  if (nextTimer) { clearInterval(nextTimer); nextTimer = undefined }
}

function resetEndState() {
  clearNextTimer()
  nextCountdown.value = 0
  pendingNextId.value = ''
  showReplay.value = false
}

async function onEnded() {
  const nextId = autoplayNext.value ? await resolveNextEpisodeId() : ''
  if (nextId) {
    pendingNextId.value = nextId
    nextCountdown.value = 5
    clearNextTimer()
    nextTimer = setInterval(() => {
      nextCountdown.value -= 1
      if (nextCountdown.value <= 0) playNext()
    }, 1000)
  } else {
    showReplay.value = true
  }
}

function playNext() {
  const id = pendingNextId.value
  resetEndState()
  if (id) router.replace({ name: 'player', params: { itemId: id }, query: { from: 'start' } })
}

function cancelNext() {
  clearNextTimer()
  nextCountdown.value = 0
  pendingNextId.value = ''
  showReplay.value = true // 取消连播后给重播/返回入口
}

function doReplay() {
  resetEndState()
  playerRef.value?.replay()
}

function onPositionChange(ticks: number) { currentPositionTicks.value = ticks }

onUnmounted(() => { stopSpeedProbe(); clearNextTimer() })
</script>

<template>
  <div v-if="error" class="player-fullscreen">
    <div class="player-center">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" />
      </svg>
      <p style="color: rgba(255,255,255,0.8); font-size: 15px; margin: 8px 0 0; text-align: center">{{ error }}</p>
      <n-button secondary style="margin-top: 12px; min-width: 100px" @click="goBack">返回</n-button>
    </div>
  </div>

  <div v-else class="player-fullscreen">
    <div v-if="streamUrl && !showLoadingOverlay" class="player-top-overlay">
      <button type="button" class="player-back-btn" title="返回" @click="goBack">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="15 18 9 12 15 6" />
        </svg>
      </button>
      <span class="player-title">{{ title }}</span>
      <button
        v-if="isEpisode"
        type="button"
        class="player-autoplay-btn"
        :class="{ active: autoplayNext }"
        :title="autoplayNext ? '自动连播:开' : '自动连播:关'"
        @click="toggleAutoplay"
      >
        连播 {{ autoplayNext ? '开' : '关' }}
      </button>
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
          <ul v-if="unsupportedDetails.length" class="player-unsupported-specs">
            <li v-for="row in unsupportedDetails" :key="row.label">
              <span class="spec-label">{{ row.label }}</span>
              <span class="spec-value">{{ row.value }}</span>
            </li>
          </ul>
          <p class="player-unsupported-hint">FYMS 不做服务端转码，建议改用外部播放器播放该资源。</p>
          <div class="player-unsupported-actions">
            <ExternalPlayMenu
              :item-id="resolvedItemId"
              :source="selectedSource"
              :title="title"
              :position-ticks="currentPositionTicks"
              :highlight="true"
            />
            <n-button secondary style="min-width: 100px" @click="goBack">返回</n-button>
          </div>
        </div>
      </div>
      <ArtVideoPlayer
        v-else-if="streamUrl"
        ref="playerRef"
        :src="streamUrl"
        :item-id="resolvedItemId"
        :media-source-id="selectedSource?.Id || ''"
        :play-session-id="playSessionId"
        :container="selectedSource?.Container || ''"
        :start-position-ticks="startPosition"
        :bitrate="effectiveBitrate"
        :size-bytes="probeFileSize"
        :speed-text="loadSpeedText"
        :audio-tracks="audioTracks"
        :subtitle-tracks="subtitleTracks"
        @ended="onEnded"
        @position-change="onPositionChange"
        @buffering="onBuffering"
        @loadstats="onLoadStats"
        @unsupported="onPlayerUnsupported"
      />
    </div>

    <PlayerLoading
      v-if="showLoadingOverlay"
      :backdrop="loadingBackdrop"
      :title="title"
      :phase-text="loadPhaseText"
      :speed-text="loadSpeedText"
      :buffered-text="bufferedText"
      :progress="loadProgress"
      @back="goBack"
    />
    <PlayerLoading v-else-if="showMiniBuffer" compact :speed-text="loadSpeedText" />

    <!-- Netflix 式下一集倒计时 -->
    <div v-if="nextCountdown > 0" class="next-ep-card">
      <div class="next-ep-info">
        <span class="next-ep-label">即将播放下一集</span>
        <span class="next-ep-count">{{ nextCountdown }}s</span>
      </div>
      <div class="next-ep-actions">
        <button type="button" class="next-ep-btn primary" @click="playNext">立即播放</button>
        <button type="button" class="next-ep-btn" @click="cancelNext">取消</button>
      </div>
    </div>

    <!-- 重播页(无下一集 / 取消连播) -->
    <div v-if="showReplay" class="replay-overlay">
      <div class="replay-card">
        <button type="button" class="replay-btn primary" @click="doReplay">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
          <span>重播</span>
        </button>
        <button type="button" class="replay-btn" @click="goBack">返回</button>
      </div>
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
.player-unsupported-specs {
  list-style: none;
  margin: 0 0 16px;
  padding: 14px 16px;
  border-radius: 12px;
  background: rgba(2, 6, 23, 0.45);
  border: 1px solid rgba(148, 163, 184, 0.16);
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 8px 18px;
}
.player-unsupported-specs li {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  font-size: 13px;
}
.player-unsupported-specs .spec-label {
  color: rgba(148, 163, 184, 0.85);
  flex-shrink: 0;
}
.player-unsupported-specs .spec-value {
  color: #fff;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  text-align: right;
  word-break: break-all;
}
.player-unsupported-hint {
  margin-bottom: 20px !important;
  color: rgba(148, 163, 184, 0.92) !important;
}
.player-unsupported-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  flex-wrap: wrap;
}
.player-top-overlay {
  position: absolute; top: 0; left: 0; right: 0; z-index: 100000;
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
.player-autoplay-btn {
  flex-shrink: 0;
  height: 30px;
  padding: 0 12px;
  border-radius: 15px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: rgba(15, 23, 42, 0.72);
  color: rgba(255, 255, 255, 0.7);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  transition: color 0.2s ease, border-color 0.2s ease, background 0.2s ease;
}
.player-autoplay-btn.active {
  color: #fff;
  border-color: rgba(14, 165, 233, 0.7);
  background: rgba(14, 165, 233, 0.28);
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
.next-ep-card {
  position: absolute;
  right: 28px;
  bottom: 96px;
  z-index: 100010;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 18px;
  min-width: 240px;
  border-radius: 16px;
  background: rgba(8, 12, 22, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.2);
  box-shadow: 0 16px 50px rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
.next-ep-info {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}
.next-ep-label { color: rgba(226, 232, 240, 0.95); font-size: 14px; font-weight: 600; }
.next-ep-count { color: #38bdf8; font-size: 18px; font-weight: 700; font-variant-numeric: tabular-nums; }
.next-ep-actions { display: flex; gap: 8px; }
.next-ep-btn {
  flex: 1;
  height: 34px;
  border-radius: 9px;
  border: 1px solid rgba(148, 163, 184, 0.25);
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.85);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}
.next-ep-btn:hover { background: rgba(255, 255, 255, 0.14); }
.next-ep-btn.primary {
  background: linear-gradient(135deg, #0ea5e9, #6366f1);
  border-color: transparent;
  color: #fff;
}
.replay-overlay {
  position: absolute;
  inset: 0;
  z-index: 100020;
  display: flex;
  align-items: center;
  justify-content: center;
  background: radial-gradient(circle at center, rgba(2, 6, 23, 0.55), rgba(0, 0, 0, 0.82));
}
.replay-card { display: flex; gap: 16px; }
.replay-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 46px;
  padding: 0 24px;
  border-radius: 24px;
  border: 1px solid rgba(148, 163, 184, 0.3);
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.9);
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.2s ease, transform 0.15s ease;
}
.replay-btn:hover { background: rgba(255, 255, 255, 0.18); transform: translateY(-1px); }
.replay-btn.primary {
  background: linear-gradient(135deg, #0ea5e9, #6366f1);
  border-color: transparent;
  color: #fff;
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
