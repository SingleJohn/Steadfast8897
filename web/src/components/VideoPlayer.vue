<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted } from 'vue';
import {
  reportPlaybackStart,
  reportPlaybackProgress,
  reportPlaybackStopped,
} from '../api/client';

const TICKS_PER_SECOND = 10_000_000;

export interface AudioTrack {
  index: number;
  language?: string;
  title?: string;
  isDefault: boolean;
}

export interface SubtitleTrack {
  index: number;
  language?: string;
  title?: string;
  isDefault: boolean;
}

function formatTime(seconds: number): string {
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
  }
  return `${m}:${String(sec).padStart(2, '0')}`;
}

const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2];

const props = withDefaults(
  defineProps<{
    src: string;
    itemId: string;
    startPositionTicks?: number;
    audioTracks?: AudioTrack[];
    subtitleTracks?: SubtitleTrack[];
  }>(),
  {
    startPositionTicks: 0,
    audioTracks: () => [],
    subtitleTracks: () => [],
  }
);

const emit = defineEmits<{
  ended: [];
}>();

const videoRef = ref<HTMLVideoElement | null>(null);
const containerRef = ref<HTMLDivElement | null>(null);
const progressRef = ref<HTMLDivElement | null>(null);
let hideTimerRef: ReturnType<typeof setTimeout> | undefined;
let reportTimerRef: ReturnType<typeof setInterval> | undefined;

const playing = ref(false);
const currentTime = ref(0);
const duration = ref(0);
const buffered = ref(0);
const volume = ref(1);
const muted = ref(false);
const isFullscreen = ref(false);
const controlsVisible = ref(true);
const speed = ref(1);
const speedDropdown = ref(false);
const audioDropdown = ref(false);
const subtitleDropdown = ref(false);
const selectedAudio = ref(
  props.audioTracks.find((t) => t.isDefault)?.index ?? props.audioTracks[0]?.index ?? -1
);
const selectedSubtitle = ref(
  props.subtitleTracks.find((t) => t.isDefault)?.index ?? -1
);
const dragging = ref(false);
const initialSeekApplied = ref(false);
const initialPlayAttempted = ref(false);
const waitingInitialSeek = ref(false);

function showControls() {
  controlsVisible.value = true;
  if (hideTimerRef !== undefined) clearTimeout(hideTimerRef);
  hideTimerRef = setTimeout(() => {
    if (videoRef.value && !videoRef.value.paused) {
      controlsVisible.value = false;
    }
  }, 3000);
}

watch([speedDropdown, audioDropdown, subtitleDropdown], () => {
  if (speedDropdown.value || audioDropdown.value || subtitleDropdown.value) {
    if (hideTimerRef !== undefined) clearTimeout(hideTimerRef);
    controlsVisible.value = true;
  }
});

function handleMouseMove() {
  showControls();
}

function handleMouseLeave() {
  if (
    playing.value &&
    !speedDropdown.value &&
    !audioDropdown.value &&
    !subtitleDropdown.value
  ) {
    if (hideTimerRef !== undefined) clearTimeout(hideTimerRef);
    hideTimerRef = setTimeout(() => {
      controlsVisible.value = false;
    }, 1000);
  }
}

function applyInitialSeek() {
  const video = videoRef.value;
  if (!video) return false;

  const positionTicks = props.startPositionTicks ?? 0;
  if (positionTicks <= 0) {
    initialSeekApplied.value = true;
    return true;
  }

  if (video.readyState < 1) return false;

  const targetTime = positionTicks / TICKS_PER_SECOND;
  if (Math.abs(video.currentTime - targetTime) > 0.25) {
    video.currentTime = targetTime;
    waitingInitialSeek.value = true;
    return false;
  }

  initialSeekApplied.value = true;
  waitingInitialSeek.value = false;
  return true;
}

async function startInitialPlayback() {
  const video = videoRef.value;
  if (!video || initialPlayAttempted.value) return;
  if (video.readyState < 2) return;

  initialPlayAttempted.value = true;
  try {
    await video.play();
  } catch {
    // 浏览器策略拦截时静默失败,保留手动播放入口即可。
  }
}

function syncInitialPlayback() {
  if (!initialSeekApplied.value) {
    if (!applyInitialSeek()) return;
  }
  void startInitialPlayback();
}

watch(
  () => [props.itemId, props.startPositionTicks] as const,
  ([itemId, startPositionTicks], _prev, onCleanup) => {
    initialSeekApplied.value = false;
    initialPlayAttempted.value = false;
    waitingInitialSeek.value = false;

    const positionTicks = startPositionTicks ?? 0;
    reportPlaybackStart(itemId, positionTicks);

    if (reportTimerRef !== undefined) clearInterval(reportTimerRef);
    reportTimerRef = setInterval(() => {
      const video = videoRef.value;
      if (video) {
        const ticks = Math.floor(video.currentTime * TICKS_PER_SECOND);
        reportPlaybackProgress(itemId, ticks, video.paused);
      }
    }, 10000);

    onCleanup(() => {
      if (reportTimerRef !== undefined) clearInterval(reportTimerRef);
      const video = videoRef.value;
      if (video) {
        const ticks = Math.floor(video.currentTime * TICKS_PER_SECOND);
        reportPlaybackStopped(itemId, ticks);
      }
    });
  },
  { immediate: true }
);

watch(
  () => props.startPositionTicks,
  (startPositionTicks) => {
    if (!initialSeekApplied.value && startPositionTicks) syncInitialPlayback();
  }
);

function onPlay() {
  playing.value = true;
  showControls();
}
function onPause() {
  playing.value = false;
}
function onTimeUpdate() {
  const video = videoRef.value;
  if (!video) return;
  currentTime.value = video.currentTime;
  if (video.buffered.length > 0) {
    buffered.value = video.buffered.end(video.buffered.length - 1);
  }
}
function onDurationChange() {
  const video = videoRef.value;
  if (!video) return;
  duration.value = video.duration || 0;
}
function onLoadedMetadata() {
  const video = videoRef.value;
  if (!video) return;
  duration.value = video.duration || 0;
  applyInitialSeek();
  syncInitialPlayback();
}
function onCanPlay() {
  syncInitialPlayback();
}
function onSeeked() {
  if (!waitingInitialSeek.value) return;
  waitingInitialSeek.value = false;
  initialSeekApplied.value = true;
  syncInitialPlayback();
}
function onVolumeChange() {
  const video = videoRef.value;
  if (!video) return;
  volume.value = video.volume;
  muted.value = video.muted;
}
function onEndedHandler() {
  playing.value = false;
  emit('ended');
}

onMounted(() => {
  const video = videoRef.value;
  if (!video) return;

  video.addEventListener('play', onPlay);
  video.addEventListener('pause', onPause);
  video.addEventListener('timeupdate', onTimeUpdate);
  video.addEventListener('durationchange', onDurationChange);
  video.addEventListener('volumechange', onVolumeChange);
  video.addEventListener('ended', onEndedHandler);
});

onUnmounted(() => {
  const video = videoRef.value;
  if (video) {
    video.removeEventListener('play', onPlay);
    video.removeEventListener('pause', onPause);
    video.removeEventListener('timeupdate', onTimeUpdate);
    video.removeEventListener('durationchange', onDurationChange);
    video.removeEventListener('volumechange', onVolumeChange);
    video.removeEventListener('ended', onEndedHandler);
  }
});

function onFsChange() {
  isFullscreen.value = !!document.fullscreenElement;
}

onMounted(() => {
  document.addEventListener('fullscreenchange', onFsChange);
});

onUnmounted(() => {
  document.removeEventListener('fullscreenchange', onFsChange);
});

function onDocMouseDown(e: MouseEvent) {
  const target = e.target as HTMLElement;
  if (!target.closest('.osd-dropdown-speed') && !target.closest('.osd-btn-speed')) {
    speedDropdown.value = false;
  }
  if (!target.closest('.osd-dropdown-audio') && !target.closest('.osd-btn-audio')) {
    audioDropdown.value = false;
  }
  if (!target.closest('.osd-dropdown-subtitle') && !target.closest('.osd-btn-subtitle')) {
    subtitleDropdown.value = false;
  }
}

onMounted(() => {
  document.addEventListener('mousedown', onDocMouseDown);
});

onUnmounted(() => {
  document.removeEventListener('mousedown', onDocMouseDown);
});

function toggleFullscreen() {
  if (!containerRef.value) return;
  if (document.fullscreenElement) {
    document.exitFullscreen();
  } else {
    containerRef.value.requestFullscreen();
  }
}

function onKeyDown(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement).tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

  const video = videoRef.value;
  if (!video) return;

  switch (e.key) {
    case ' ':
      e.preventDefault();
      video.paused ? video.play() : video.pause();
      showControls();
      break;
    case 'ArrowLeft':
      e.preventDefault();
      video.currentTime = Math.max(0, video.currentTime - 10);
      showControls();
      break;
    case 'ArrowRight':
      e.preventDefault();
      video.currentTime = Math.min(video.duration || 0, video.currentTime + 10);
      showControls();
      break;
    case 'ArrowUp':
      e.preventDefault();
      video.volume = Math.min(1, video.volume + 0.1);
      showControls();
      break;
    case 'ArrowDown':
      e.preventDefault();
      video.volume = Math.max(0, video.volume - 0.1);
      showControls();
      break;
    case 'f':
    case 'F':
      e.preventDefault();
      toggleFullscreen();
      break;
    case 'm':
    case 'M':
      e.preventDefault();
      video.muted = !video.muted;
      showControls();
      break;
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown);
});

onUnmounted(() => {
  window.removeEventListener('keydown', onKeyDown);
});

function onDragMove(e: MouseEvent) {
  const bar = progressRef.value;
  const video = videoRef.value;
  if (!bar || !video) return;
  const rect = bar.getBoundingClientRect();
  const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
  video.currentTime = ratio * (video.duration || 0);
}

function onDragUp() {
  dragging.value = false;
}

watch(dragging, (isDragging, _prev, onCleanup) => {
  if (!isDragging) return;
  window.addEventListener('mousemove', onDragMove);
  window.addEventListener('mouseup', onDragUp);
  onCleanup(() => {
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragUp);
  });
});

function togglePlay() {
  const video = videoRef.value;
  if (!video) return;
  video.paused ? video.play() : video.pause();
  showControls();
}

function handleProgressClick(e: MouseEvent) {
  const bar = progressRef.value;
  const video = videoRef.value;
  if (!bar || !video) return;
  const rect = bar.getBoundingClientRect();
  const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
  video.currentTime = ratio * (video.duration || 0);
}

function handleProgressMouseDown(e: MouseEvent) {
  e.preventDefault();
  dragging.value = true;
  handleProgressClick(e);
}

function handleSpeedChange(s: number) {
  speed.value = s;
  speedDropdown.value = false;
  if (videoRef.value) videoRef.value.playbackRate = s;
}

function handleAudioChange(index: number) {
  selectedAudio.value = index;
  audioDropdown.value = false;
}

function handleSubtitleChange(index: number) {
  selectedSubtitle.value = index;
  subtitleDropdown.value = false;
}

function handleVolumeInput(e: Event) {
  const v = parseFloat((e.target as HTMLInputElement).value);
  if (videoRef.value) {
    videoRef.value.volume = v;
    videoRef.value.muted = v === 0;
  }
}

const progress = computed(() =>
  duration.value > 0 ? (currentTime.value / duration.value) * 100 : 0
);
const bufferedPercent = computed(() =>
  duration.value > 0 ? (buffered.value / duration.value) * 100 : 0
);

function toggleMute() {
  const v = videoRef.value;
  if (v) v.muted = !v.muted;
}
</script>

<template>
  <div
    ref="containerRef"
    :style="{
      position: 'relative',
      width: '100%',
      height: '100%',
      backgroundColor: '#000',
      overflow: 'hidden',
      cursor: controlsVisible ? 'default' : 'none',
    }"
    @mousemove="handleMouseMove"
    @mouseleave="handleMouseLeave"
  >
    <video
      ref="videoRef"
      :src="src"
      preload="auto"
      playsinline
      :style="{
        width: '100%',
        height: '100%',
        objectFit: 'contain',
        display: 'block',
      }"
      @loadedmetadata="onLoadedMetadata"
      @canplay="onCanPlay"
      @seeked="onSeeked"
      @click="togglePlay"
    />

    <div
      :style="{
        position: 'absolute',
        top: 0,
        left: 0,
        right: 0,
        height: '80px',
        background: 'linear-gradient(to bottom, rgba(0,0,0,0.6), transparent)',
        opacity: controlsVisible ? 1 : 0,
        transition: 'opacity 0.3s ease',
        pointerEvents: controlsVisible ? 'auto' : 'none',
        display: 'flex',
        alignItems: 'flex-start',
        padding: '12px 16px',
      }"
    />

    <div
      :style="{
        position: 'absolute',
        bottom: 0,
        left: 0,
        right: 0,
        background: 'linear-gradient(transparent, rgba(0,0,0,0.85))',
        padding: '24px 16px 12px',
        opacity: controlsVisible ? 1 : 0,
        transition: 'opacity 0.3s ease',
        pointerEvents: controlsVisible ? 'auto' : 'none',
      }"
      @click.stop
    >
      <div
        ref="progressRef"
        class="osd-progress-bar"
        :style="{
          position: 'relative',
          width: '100%',
          borderRadius: '5px',
          backgroundColor: 'rgba(255,255,255,0.2)',
          marginBottom: '10px',
        }"
        @mousedown="handleProgressMouseDown"
      >
        <div
          :style="{
            position: 'absolute',
            top: 0,
            left: 0,
            height: '100%',
            width: `${bufferedPercent}%`,
            backgroundColor: 'rgba(255,255,255,0.1)',
            borderRadius: '5px',
            pointerEvents: 'none',
          }"
        />
        <div
          :style="{
            position: 'absolute',
            top: 0,
            left: 0,
            height: '100%',
            width: `${progress}%`,
            backgroundColor: '#00a4dc',
            borderRadius: '5px',
            pointerEvents: 'none',
          }"
        />
        <div class="osd-progress-thumb" :style="{ left: `${progress}%` }" />
      </div>

      <div :style="{ display: 'flex', alignItems: 'center', gap: '4px' }">
        <div :style="{ display: 'flex', alignItems: 'center', gap: '4px' }">
          <button
            class="osd-ctrl-btn"
            type="button"
            :title="playing ? '暂停' : '播放'"
            @click="togglePlay"
          >
            <svg v-if="playing" width="24" height="24" viewBox="0 0 24 24" fill="white">
              <rect x="6" y="4" width="4" height="16" rx="1" />
              <rect x="14" y="4" width="4" height="16" rx="1" />
            </svg>
            <svg v-else width="24" height="24" viewBox="0 0 24 24" fill="white">
              <path d="M8 5v14l11-7z" />
            </svg>
          </button>
          <span
            :style="{
              color: 'rgba(255,255,255,0.85)',
              fontSize: '13px',
              fontVariantNumeric: 'tabular-nums',
              whiteSpace: 'nowrap',
              padding: '0 4px',
              userSelect: 'none',
            }"
          >
            {{ formatTime(currentTime) }} / {{ formatTime(duration) }}
          </span>
        </div>

        <div :style="{ flex: 1 }" />

        <div
          :style="{
            display: 'flex',
            alignItems: 'center',
            gap: '2px',
            position: 'relative',
          }"
        >
          <div :style="{ position: 'relative' }">
            <button
              class="osd-ctrl-btn osd-btn-speed"
              type="button"
              title="播放速度"
              :style="{ fontSize: '13px', fontWeight: 600, minWidth: '36px' }"
              @click="
                speedDropdown = !speedDropdown;
                audioDropdown = false;
                subtitleDropdown = false;
              "
            >
              {{ speed }}x
            </button>
            <div v-if="speedDropdown" class="osd-dropdown osd-dropdown-speed" style="right: 0">
              <div
                v-for="s in SPEEDS"
                :key="s"
                class="osd-dropdown-item"
                :class="{ active: s === speed }"
                @click="handleSpeedChange(s)"
              >
                {{ s }}x
              </div>
            </div>
          </div>

          <div v-if="audioTracks.length > 0" :style="{ position: 'relative' }">
            <button
              class="osd-ctrl-btn osd-btn-audio"
              type="button"
              title="音频轨道"
              @click="
                audioDropdown = !audioDropdown;
                speedDropdown = false;
                subtitleDropdown = false;
              "
            >
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="white"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path d="M9 18V5l12-2v13" />
                <circle cx="6" cy="18" r="3" fill="white" />
                <circle cx="18" cy="16" r="3" fill="white" />
              </svg>
            </button>
            <div v-if="audioDropdown" class="osd-dropdown osd-dropdown-audio" style="right: 0">
              <div
                v-for="t in audioTracks"
                :key="t.index"
                class="osd-dropdown-item"
                :class="{ active: t.index === selectedAudio }"
                @click="handleAudioChange(t.index)"
              >
                {{ t.title || t.language || `音轨 ${t.index}` }}
              </div>
            </div>
          </div>

          <div :style="{ position: 'relative' }">
            <button
              class="osd-ctrl-btn osd-btn-subtitle"
              type="button"
              title="字幕轨道"
              @click="
                subtitleDropdown = !subtitleDropdown;
                speedDropdown = false;
                audioDropdown = false;
              "
            >
              <svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="white"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <rect x="2" y="4" width="20" height="16" rx="2" />
                <path d="M6 12h4" />
                <path d="M14 12h4" />
                <path d="M6 16h12" />
              </svg>
            </button>
            <div v-if="subtitleDropdown" class="osd-dropdown osd-dropdown-subtitle" style="right: 0">
              <div
                class="osd-dropdown-item"
                :class="{ active: selectedSubtitle === -1 }"
                @click="handleSubtitleChange(-1)"
              >
                关闭
              </div>
              <div
                v-for="t in subtitleTracks"
                :key="t.index"
                class="osd-dropdown-item"
                :class="{ active: t.index === selectedSubtitle }"
                @click="handleSubtitleChange(t.index)"
              >
                {{ t.title || t.language || `字幕 ${t.index}` }}
              </div>
            </div>
          </div>

          <div :style="{ display: 'flex', alignItems: 'center', gap: '2px' }">
            <button
              class="osd-ctrl-btn"
              type="button"
              :title="muted ? '取消静音' : '静音'"
              @click="toggleMute"
            >
              <svg
                v-if="muted || volume === 0"
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="white"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path d="M11 5L6 9H2v6h4l5 4V5z" fill="white" />
                <line x1="23" y1="9" x2="17" y2="15" />
                <line x1="17" y1="9" x2="23" y2="15" />
              </svg>
              <svg
                v-else
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="white"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path d="M11 5L6 9H2v6h4l5 4V5z" fill="white" />
                <path d="M15.54 8.46a5 5 0 010 7.07" />
                <path d="M19.07 4.93a10 10 0 010 14.14" />
              </svg>
            </button>
            <input
              type="range"
              class="osd-volume-slider"
              min="0"
              max="1"
              step="0.01"
              :value="muted ? 0 : volume"
              @input="handleVolumeInput"
            />
          </div>

          <button
            class="osd-ctrl-btn"
            type="button"
            :title="isFullscreen ? '退出全屏' : '全屏'"
            @click="toggleFullscreen"
          >
            <svg
              v-if="isFullscreen"
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="white"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M8 3v3a2 2 0 01-2 2H3" />
              <path d="M21 8h-3a2 2 0 01-2-2V3" />
              <path d="M3 16h3a2 2 0 012 2v3" />
              <path d="M16 21v-3a2 2 0 012-2h3" />
            </svg>
            <svg
              v-else
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="white"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M8 3H5a2 2 0 00-2 2v3" />
              <path d="M21 8V5a2 2 0 00-2-2h-3" />
              <path d="M3 16v3a2 2 0 002 2h3" />
              <path d="M16 21h3a2 2 0 002-2v-3" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.osd-progress-bar {
  height: 6px;
  transition: height 0.15s ease;
  cursor: pointer;
}
.osd-progress-bar:hover {
  height: 10px;
}
.osd-progress-bar:hover .osd-progress-thumb {
  opacity: 1;
  transform: scale(1);
}
.osd-progress-thumb {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #fff;
  position: absolute;
  top: 50%;
  transform: scale(0);
  margin-top: -7px;
  margin-left: -7px;
  opacity: 0;
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
  pointer-events: none;
  box-shadow: 0 0 4px rgba(0, 0, 0, 0.5);
}
.osd-volume-slider {
  -webkit-appearance: none;
  appearance: none;
  width: 80px;
  height: 4px;
  border-radius: 2px;
  background: rgba(255, 255, 255, 0.3);
  outline: none;
  cursor: pointer;
}
.osd-volume-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #fff;
  cursor: pointer;
}
.osd-volume-slider::-moz-range-thumb {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #fff;
  border: none;
  cursor: pointer;
}
.osd-ctrl-btn {
  background: none;
  border: none;
  color: #fff;
  cursor: pointer;
  padding: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.15s;
}
.osd-ctrl-btn:hover {
  background: rgba(255, 255, 255, 0.1);
}
.osd-dropdown {
  position: absolute;
  bottom: 100%;
  margin-bottom: 8px;
  background: rgba(20, 20, 20, 0.92);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px 0;
  min-width: 140px;
  z-index: 100;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
}
.osd-dropdown-item {
  padding: 8px 16px;
  color: rgba(255, 255, 255, 0.8);
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 8px;
  transition: background 0.1s;
}
.osd-dropdown-item:hover {
  background: rgba(255, 255, 255, 0.08);
}
.osd-dropdown-item.active {
  color: #00a4dc;
}
.osd-dropdown-item.active::before {
  content: '';
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #00a4dc;
  flex-shrink: 0;
}
</style>
