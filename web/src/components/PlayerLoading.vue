<script setup lang="ts">
withDefaults(
  defineProps<{
    backdrop?: string
    title?: string
    phaseText?: string
    speedText?: string
    bufferedText?: string
    progress?: number
  }>(),
  {
    backdrop: '',
    title: '',
    phaseText: '正在准备播放',
    speedText: '',
    bufferedText: '',
    progress: 0,
  },
)

const emit = defineEmits<{ back: [] }>()
</script>

<template>
  <div class="ploading">
    <div v-if="backdrop" class="ploading-bg" :style="{ backgroundImage: `url(${backdrop})` }" />
    <div class="ploading-bg-overlay" />

    <button type="button" class="ploading-back" title="返回" @click="emit('back')">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="15 18 9 12 15 6" />
      </svg>
    </button>

    <div class="ploading-center">
      <div class="ploading-spinner" />

      <div v-if="speedText" class="ploading-speed">
        <span class="ploading-speed-value">{{ speedText.split(' ')[0] }}</span>
        <span class="ploading-speed-unit">{{ speedText.split(' ')[1] }}</span>
      </div>

      <div v-if="title" class="ploading-title">{{ title }}</div>

      <div class="ploading-status">
        <span class="ploading-phase">{{ phaseText }}</span>
        <span v-if="bufferedText" class="ploading-sep">·</span>
        <span v-if="bufferedText" class="ploading-buffered">{{ bufferedText }}</span>
      </div>

      <div class="ploading-bar">
        <div class="ploading-bar-fill" :style="{ width: `${Math.min(100, Math.max(0, progress))}%` }" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.ploading {
  position: absolute;
  inset: 0;
  z-index: 30;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}
.ploading-bg {
  position: absolute;
  inset: -40px;
  background-size: cover;
  background-position: center;
  filter: blur(36px) saturate(1.15);
  transform: scale(1.12);
  opacity: 0.55;
}
.ploading-bg-overlay {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(circle at 50% 40%, rgba(56, 189, 248, 0.1), transparent 55%),
    linear-gradient(180deg, rgba(2, 6, 23, 0.82) 0%, rgba(0, 0, 0, 0.94) 100%);
}
.ploading-back {
  position: absolute;
  top: 18px;
  left: 20px;
  z-index: 2;
  width: 38px;
  height: 38px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  border: none;
  cursor: pointer;
  color: #fff;
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  transition: background 0.2s ease;
}
.ploading-back:hover { background: rgba(255, 255, 255, 0.22); }

.ploading-center {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 0 24px;
  max-width: 440px;
  width: 100%;
}

/* 三层嵌套旋转环(loading-146 变体):三条弧分别用不同颜色、不同转速。 */
.ploading-spinner {
  width: 76px;
  aspect-ratio: 1;
  display: grid;
  border: 5px solid transparent;
  border-radius: 50%;
  border-right-color: #38bdf8;
  filter: drop-shadow(0 0 8px rgba(56, 189, 248, 0.35));
  animation: ploading-spin 1s infinite linear;
}
.ploading-spinner::before,
.ploading-spinner::after {
  content: "";
  grid-area: 1 / 1;
  margin: 3px;
  border: inherit;
  border-radius: 50%;
  border-right-color: #818cf8;
  animation: ploading-spin 2s infinite;
}
.ploading-spinner::after {
  margin: 11px;
  border-right-color: #2dd4bf;
  animation-duration: 3s;
}

.ploading-speed {
  display: flex;
  align-items: baseline;
  gap: 5px;
  margin-top: 2px;
}
.ploading-speed-value {
  font-size: 28px;
  font-weight: 700;
  line-height: 1;
  color: #fff;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.5px;
}
.ploading-speed-unit {
  font-size: 13px;
  font-weight: 600;
  color: rgba(148, 163, 184, 0.95);
  letter-spacing: 0.5px;
}

.ploading-title {
  max-width: 100%;
  font-size: 17px;
  font-weight: 600;
  color: #fff;
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 1px 6px rgba(0, 0, 0, 0.6);
}
.ploading-status {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: rgba(148, 163, 184, 0.92);
  font-variant-numeric: tabular-nums;
}
.ploading-phase { color: rgba(226, 232, 240, 0.92); }
.ploading-sep { opacity: 0.5; }

.ploading-bar {
  width: 100%;
  height: 4px;
  border-radius: 4px;
  background: rgba(148, 163, 184, 0.16);
  overflow: hidden;
}
.ploading-bar-fill {
  height: 100%;
  border-radius: 4px;
  background: linear-gradient(90deg, #2dd4bf, #38bdf8, #818cf8);
  box-shadow: 0 0 10px rgba(56, 189, 248, 0.6);
  transition: width 0.45s ease;
}

@keyframes ploading-spin {
  100% { transform: rotate(1turn); }
}
</style>
