<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NSelect } from 'naive-ui'
import { getImageUrl, getItem, getItems, getPlaybackInfo, getStreamUrl, getSubtitleUrl } from '../api/client'
import ArtVideoPlayer from '../components/ArtVideoPlayer.vue'
import PlayerLoading from '../components/PlayerLoading.vue'

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
const bufferedSeconds = ref(0)
const loadSpeedBps = ref(0)

const isEpisode = computed(() => currentItem.value?.Type === 'Episode')

const showLoadingOverlay = computed(() =>
  !error.value && !browserUnsupportedReason.value && (loading.value || (!!streamUrl.value && !playbackStarted.value)),
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
  if (!active) playbackStarted.value = true
}

function onLoadStats(stats: { bufferedSeconds: number; speedBps: number }) {
  bufferedSeconds.value = stats.bufferedSeconds
  // 持续反映实时速率(含空闲时回落,使顶栏速率标签真实);加载层阶段为 0 时显示「···」。
  loadSpeedBps.value = stats.speedBps
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
      isExternal: !!s.IsExternal,
      isTextSubtitle: !!s.IsTextSubtitleStream,
      codec: s.Codec,
      // 仅外挂文本字幕带 DeliveryUrl；内封字幕无地址(不转码无法提取)。
      url: s.DeliveryUrl ? getSubtitleUrl(s.DeliveryUrl) : '',
    }))
}

// 编码/容器兼容性按「是否浏览器可解」分类,而非按容器一刀切:
// MKV 等封装 Chromium 多能解,真正决定能否直放的是内部编码。
// 'no' = 确定不支持(拦截) / 'ok' = 浏览器可解 / 'unknown' = 交给浏览器尝试。
function classifyVideoCodec(c: string): 'ok' | 'no' | 'unknown' {
  if (!c) return 'unknown'
  if (/h264|avc|x264/.test(c)) return 'ok'
  if (/vp0?8|vp0?9/.test(c)) return 'ok'
  if (/av0?1/.test(c)) return 'ok' // 现代浏览器普遍支持 AV1 解码
  if (/hevc|h265|x265|265|vc-?1|mpeg-?2|mpeg2|wmv|divx|xvid|mpeg-?4|msmpeg/.test(c)) return 'no'
  return 'unknown'
}

function classifyAudioCodec(c: string): 'ok' | 'no' | 'unknown' {
  if (!c) return 'unknown'
  if (/aac|mp3|mp2|opus|vorbis|flac|alac|pcm/.test(c)) return 'ok'
  if (/eac-?3|ac-?3|ac3|dts|truehd|mlp/.test(c)) return 'no'
  return 'unknown'
}

// 'no' = 浏览器无法解封装(拦截) / 'attempt' = 让浏览器尝试(含 mkv 与未知)。
function classifyContainer(c: string): 'ok' | 'no' | 'attempt' {
  if (!c) return 'attempt'
  if (/mp4|m4v|mov|webm|ogg|ogv/.test(c)) return 'ok'
  if (/mkv|matroska/.test(c)) return 'attempt' // Chromium 多数能直放 h264+aac 的 mkv
  if (/m2ts|mts|\bts\b|avi|wmv|asf|flv|rmvb|\brm\b|vob|3gp/.test(c)) return 'no'
  return 'attempt'
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

  // 编码层优先:确定不支持的编码,容器再友好也放不了。
  if (classifyVideoCodec(videoCodec) === 'no') {
    return `当前浏览器无法解码该视频编码(${videoCodec.toUpperCase()})，建议使用外部播放器。`
  }
  if (classifyAudioCodec(audioCodec) === 'no') {
    return `当前浏览器无法解码该音频编码(${audioCodec.toUpperCase()})，建议使用外部播放器。`
  }
  // 容器层:仅拦确定无法解封装的;mkv / 未知编码放行让浏览器尝试,失败由运行时兜底。
  if (classifyContainer(container) === 'no') {
    return `当前浏览器无法解封装该容器(${container.toUpperCase()})，建议使用外部播放器。`
  }
  return ''
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
  playbackStarted.value = false
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

async function onEnded() {
  if (autoplayNext.value) {
    const nextId = await resolveNextEpisodeId()
    if (nextId) {
      router.replace({ name: 'player', params: { itemId: nextId }, query: { from: 'start' } })
      return
    }
  }
  router.back()
}

function onPositionChange(ticks: number) { currentPositionTicks.value = ticks }
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
      <span v-if="loadSpeedText" class="player-speed-chip" title="实时下载速率">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <line x1="12" y1="19" x2="12" y2="5" />
          <polyline points="5 12 12 19 19 12" />
        </svg>
        {{ loadSpeedText }}
      </span>
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
          <p class="player-unsupported-hint">FYMS 不做服务端转码，建议改用 Infuse 等外部播放器播放该资源。</p>
          <n-button secondary style="min-width: 100px" @click="goBack">返回</n-button>
        </div>
      </div>
      <ArtVideoPlayer
        v-else-if="streamUrl"
        :src="streamUrl"
        :item-id="resolvedItemId"
        :media-source-id="selectedSource?.Id || ''"
        :play-session-id="playSessionId"
        :container="selectedSource?.Container || ''"
        :start-position-ticks="startPosition"
        :bitrate="selectedSource?.Bitrate || 0"
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
.player-speed-chip {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 11px;
  border-radius: 15px;
  border: 1px solid rgba(56, 189, 248, 0.35);
  background: rgba(15, 23, 42, 0.66);
  color: rgba(186, 230, 253, 0.95);
  font-size: 12.5px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.player-speed-chip svg { opacity: 0.85; }
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
