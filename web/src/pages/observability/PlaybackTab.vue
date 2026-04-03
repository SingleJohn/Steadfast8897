<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { NCard, NTag, NDataTable, type DataTableColumns } from 'naive-ui'
import { getActiveSessions, getRecentPlayback } from '@/api/client'
import EmptyState from '@/components/EmptyState.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'

const sessions = ref<any[]>([])
const recentPlayback = ref<any[]>([])
const recentLoading = ref(true)
const recentError = ref<string | null>(null)

const playingSessions = computed(() =>
  sessions.value.filter((s: any) => s.NowPlayingItem),
)

function fmt(sec: number) {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const ss = sec % 60
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
    : `${m}:${String(ss).padStart(2, '0')}`
}

function recentDurationStr(r: any) {
  const dur = r.play_duration
  if (dur >= 3600) return `${Math.floor(dur / 3600)}h ${Math.floor((dur % 3600) / 60)}m`
  if (dur >= 60) return `${Math.floor(dur / 60)}m ${dur % 60}s`
  return `${dur}s`
}

function mediaLabel(r: any) {
  return r.series_name ? `${r.series_name} - ${r.item_name}` : r.item_name
}

function progressPct(s: any) {
  const total = s.NowPlayingItem?.RunTimeTicks
  const pos = s.PlayState?.PositionTicks || 0
  if (!total || total <= 0) return 0
  return Math.min((pos / total) * 100, 100)
}

function videoInfo(s: any) {
  const streams = s.NowPlayingItem?.MediaStreams || []
  const video = streams.find((st: any) => st.Type === 'video')
  if (!video) return null
  const h = video.Height || 0
  const res = h >= 2160 ? '4K' : h >= 1080 ? '1080P' : h >= 720 ? '720P' : h > 0 ? `${h}P` : ''
  const codec = (video.Codec || '').toUpperCase()
  return `${res} ${codec}`.trim() || null
}

function audioInfo(s: any) {
  const streams = s.NowPlayingItem?.MediaStreams || []
  const audio = streams.find((st: any) => st.Type === 'audio' && st.IsDefault) || streams.find((st: any) => st.Type === 'audio')
  if (!audio) return null
  const codec = (audio.Codec || '').toUpperCase()
  const ch = audio.Channels
  const chStr = ch === 6 ? '5.1' : ch === 8 ? '7.1' : ch === 2 ? 'Stereo' : ch ? `${ch}ch` : ''
  return `${codec} ${chStr}`.trim() || null
}

const recentColumns: DataTableColumns<any> = [
  {
    title: '时间',
    key: 'date',
    width: 170,
    render: (r) => new Date(r.date).toLocaleString(),
  },
  { title: '用户', key: 'user_name', width: 110 },
  {
    title: '媒体',
    key: 'item_name',
    ellipsis: { tooltip: true },
    render: (r) => mediaLabel(r),
  },
  {
    title: '类型',
    key: 'item_type',
    width: 80,
    render: (r) => r.item_type === 'Episode' ? '剧集' : r.item_type === 'Movie' ? '电影' : (r.item_type || '-'),
  },
  { title: '客户端', key: 'client_name', width: 120, render: (r) => r.client_name || '-' },
  { title: '设备', key: 'device_name', width: 120, render: (r) => r.device_name || '-' },
  { title: 'IP', key: 'client_ip', width: 130, render: (r) => r.client_ip || '-' },
  {
    title: '时长',
    key: 'play_duration',
    width: 90,
    align: 'right',
    render: (r) => recentDurationStr(r),
  },
]

let refreshTimer: ReturnType<typeof setInterval> | null = null

async function refreshSessions() {
  try {
    const data = await getActiveSessions()
    if (Array.isArray(data)) sessions.value = data
  } catch {
    // Keep the last successful snapshot when polling fails.
  }
}

async function refreshRecentPlayback(showLoading = false) {
  if (showLoading) recentLoading.value = true
  recentError.value = null
  try {
    const data = await getRecentPlayback(50)
    recentPlayback.value = Array.isArray(data) ? data : []
  } catch (e) {
    recentError.value = e instanceof Error ? e.message : '加载最近播放记录失败'
  } finally {
    recentLoading.value = false
  }
}

onMounted(() => {
  void refreshSessions()
  void refreshRecentPlayback(true)
  refreshTimer = setInterval(() => {
    void refreshSessions()
    void refreshRecentPlayback()
  }, 5000)
})

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div class="playback-tab">
    <!-- Now Playing -->
    <n-card class="glass-card section-card" :bordered="false">
      <template #header>
        <div class="section-header">
          <span class="section-title">正在播放</span>
          <n-tag :bordered="false" round size="small" :type="playingSessions.length > 0 ? 'success' : 'default'">
            {{ playingSessions.length }}
          </n-tag>
        </div>
      </template>

      <empty-state v-if="playingSessions.length === 0" description="当前没有正在播放的用户" />

      <div v-else class="playing-grid">
        <div
          v-for="s in playingSessions"
          :key="s.Id"
          class="playing-card"
        >
          <div class="playing-card__body">
            <img
              v-if="s.NowPlayingItem && (s.NowPlayingItem.PrimaryImageItemId || s.NowPlayingItem.Id)"
              :src="`/Items/${s.NowPlayingItem.PrimaryImageItemId || s.NowPlayingItem.Id}/Images/Primary?maxWidth=120&quality=90`"
              alt=""
              class="playing-card__poster"
              @error="(e: Event) => ((e.target as HTMLImageElement).style.display = 'none')"
            />
            <div class="playing-card__info">
              <div class="playing-card__title">
                {{ s.NowPlayingItem.SeriesName || s.NowPlayingItem.Name }}
              </div>
              <div
                v-if="s.NowPlayingItem.ParentIndexNumber != null && s.NowPlayingItem.IndexNumber != null"
                class="playing-card__episode"
              >
                S{{ s.NowPlayingItem.ParentIndexNumber }}:E{{ s.NowPlayingItem.IndexNumber }}
                - {{ s.NowPlayingItem.Name }}
              </div>
              <div class="playing-card__time">
                {{ fmt(Math.floor((s.PlayState?.PositionTicks || 0) / 10_000_000)) }}
                /
                {{ fmt(Math.floor(s.NowPlayingItem.RunTimeTicks / 10_000_000)) }}
              </div>
              <div class="playing-card__progress-track">
                <div
                  class="playing-card__progress-bar"
                  :class="{ 'playing-card__progress-bar--paused': s.PlayState?.IsPaused }"
                  :style="{ width: `${progressPct(s)}%` }"
                />
              </div>
            </div>
          </div>

          <div class="playing-card__footer">
            <div class="playing-card__meta">
              <span>{{ s.UserName }}</span>
              <span class="playing-card__meta-sep">·</span>
              <span>{{ s.Client }}{{ s.ApplicationVersion ? ` ${s.ApplicationVersion}` : '' }}</span>
              <span class="playing-card__meta-sep">·</span>
              <span>{{ s.DeviceName }}</span>
            </div>
            <div class="playing-card__tags">
              <n-tag v-if="s.NowPlayingItem.Container" size="tiny" :bordered="false" round>
                {{ s.NowPlayingItem.Container?.toUpperCase() }}
                {{ s.NowPlayingItem.Bitrate ? `${Math.round(s.NowPlayingItem.Bitrate / 1_000_000)} Mbps` : '' }}
              </n-tag>
              <n-tag v-if="videoInfo(s)" size="tiny" :bordered="false" round>{{ videoInfo(s) }}</n-tag>
              <n-tag v-if="audioInfo(s)" size="tiny" :bordered="false" round>{{ audioInfo(s) }}</n-tag>
              <n-tag size="tiny" type="success" :bordered="false" round>直接播放</n-tag>
            </div>
          </div>
        </div>
      </div>
    </n-card>

    <!-- Recent Playback -->
    <n-card class="glass-card section-card" title="最近播放记录" :bordered="false">
      <error-banner v-if="recentError" :message="recentError" style="margin-bottom: 12px" />
      <n-data-table
        :columns="recentColumns"
        :data="recentPlayback"
        :loading="recentLoading"
        :bordered="false"
        :single-line="false"
        size="small"
        :pagination="recentPlayback.length > 20 ? { pageSize: 20 } : false"
        :row-class-name="() => 'recent-row'"
        style="--n-td-color: transparent; --n-th-color: transparent; --n-td-color-hover: rgba(148,163,184,0.06)"
      />
      <empty-state v-if="!recentLoading && recentPlayback.length === 0" description="暂无播放记录" />
    </n-card>
  </div>
</template>

<style scoped>
.playback-tab {
  display: flex;
  flex-direction: column;
  gap: var(--app-section-gap);
}

.section-card {
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
}

/* Playing Grid */
.playing-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
}

.playing-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  overflow: hidden;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.playing-card:hover {
  border-color: var(--app-border-hover);
  box-shadow: var(--app-shadow-1);
}

.playing-card__body {
  display: flex;
  gap: 12px;
  padding: 14px 16px 10px;
}

.playing-card__poster {
  width: 56px;
  height: 84px;
  object-fit: cover;
  border-radius: 6px;
  flex-shrink: 0;
  background: rgba(148, 163, 184, 0.1);
}

.playing-card__info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.playing-card__title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.playing-card__episode {
  font-size: 12px;
  color: var(--app-text-muted);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.playing-card__time {
  font-size: 12px;
  color: var(--app-text-muted);
  margin-top: 6px;
  font-variant-numeric: tabular-nums;
}

.playing-card__progress-track {
  height: 4px;
  background: rgba(148, 163, 184, 0.12);
  border-radius: 2px;
  overflow: hidden;
  margin-top: 6px;
}

.playing-card__progress-bar {
  height: 100%;
  background: var(--app-primary);
  border-radius: 2px;
  transition: width 1s linear;
}

.playing-card__progress-bar--paused {
  background: var(--app-warning);
}

.playing-card__footer {
  padding: 8px 16px 12px;
  border-top: 1px solid var(--app-border);
}

.playing-card__meta {
  font-size: 12px;
  color: var(--app-text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  line-height: 1.6;
}

.playing-card__meta-sep {
  opacity: 0.4;
}

.playing-card__tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 6px;
}

/* Table overrides */
:deep(.n-data-table-th) {
  font-weight: 500 !important;
  font-size: 13px !important;
  color: var(--app-text-muted) !important;
}

:deep(.n-data-table-td) {
  font-size: 13px !important;
}

@media (max-width: 640px) {
  .playing-grid {
    grid-template-columns: 1fr;
  }
}
</style>
