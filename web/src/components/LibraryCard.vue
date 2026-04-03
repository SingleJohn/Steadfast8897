<script setup lang="ts">
import { computed, ref } from 'vue'
import { NProgress } from 'naive-ui'

const props = defineProps<{
  lib: any
  scanProg?: any
}>()

const emit = defineEmits<{ click: [libId: string] }>()

const imgFailed = ref(false)

const coverUrl = computed(() => {
  const tag = props.lib.ImageTag
  if (tag) return `/Items/${props.lib.ItemId}/Images/Primary?maxWidth=600&format=jpg&quality=90&tag=${tag}`
  return `/Items/${props.lib.ItemId}/Images/Primary?maxWidth=600&format=jpg&quality=90`
})

const showCover = computed(() => !imgFailed.value)

function onImgError() {
  imgFailed.value = true
}

function handleClick() {
  emit('click', props.lib.ItemId)
}

const typeLabel = computed(() => {
  const ct = props.lib.CollectionType
  if (ct === 'movies') return '电影'
  if (ct === 'tvshows') return '电视剧'
  if (ct === 'music') return '音乐'
  return '媒体'
})

const typeBadge = computed(() => {
  const ct = props.lib.CollectionType
  if (ct === 'movies') return '影片'
  if (ct === 'tvshows') return '剧集'
  if (ct === 'music') return '音乐'
  return '媒体'
})

const emptyGradient = computed(() => {
  const ct = props.lib.CollectionType
  if (ct === 'movies') return 'linear-gradient(135deg, #1a1a2e 0%, #16213e 40%, #0f3460 100%)'
  if (ct === 'tvshows') return 'linear-gradient(135deg, #1a1a2e 0%, #1b2838 40%, #1a3a4a 100%)'
  if (ct === 'music') return 'linear-gradient(135deg, #1a1a2e 0%, #2d1b3d 40%, #4a1942 100%)'
  return 'linear-gradient(135deg, #1a1a2e 0%, #1e293b 40%, #334155 100%)'
})

const folderCount = computed(() => props.lib.Locations?.length || 0)
const isScanning = computed(() => props.scanProg?.Status === 'scanning')
const scanPct = computed(() => props.scanProg?.Percentage || 0)
</script>

<template>
  <div class="lc" @click="handleClick">
    <div class="lc-cover">
      <!-- 封面图（用 img 标签检测是否加载成功） -->
      <img
        v-if="showCover"
        :src="coverUrl"
        alt=""
        class="lc-img"
        @error="onImgError"
      />
      <!-- 无封面：渐变背景 + SVG 图标 -->
      <div v-if="imgFailed" class="lc-placeholder" :style="{ background: emptyGradient }">
        <svg v-if="lib.CollectionType === 'movies'" class="lc-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="2" width="20" height="20" rx="2.18" ry="2.18" />
          <path d="M7 2v20" /><path d="M17 2v20" />
          <path d="M2 12h20" /><path d="M2 7h5" /><path d="M2 17h5" />
          <path d="M17 7h5" /><path d="M17 17h5" />
        </svg>
        <svg v-else-if="lib.CollectionType === 'tvshows'" class="lc-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="7" width="20" height="15" rx="2" ry="2" />
          <polyline points="17 2 12 7 7 2" />
        </svg>
        <svg v-else-if="lib.CollectionType === 'music'" class="lc-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="18" cy="16" r="3" />
        </svg>
        <svg v-else class="lc-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
        </svg>
      </div>

      <!-- 底部渐变 + badge + 名称 -->
      <div class="lc-gradient" :class="{ 'lc-gradient-noimg': imgFailed }">
        <span class="lc-badge">{{ typeBadge }}</span>
        <h3 class="lc-name">{{ lib.Name }}</h3>
      </div>

      <!-- hover 遮罩 -->
      <div class="lc-hover-overlay">
        <span class="lc-edit-btn">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7" />
            <path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z" />
          </svg>
          <span>编辑</span>
        </span>
      </div>

      <!-- 扫描进度条 -->
      <div v-if="isScanning" class="lc-scan-bar">
        <n-progress type="line" :percentage="scanPct" :show-indicator="false" :height="3" />
      </div>
    </div>

    <!-- 底部信息栏（带背景） -->
    <div class="lc-info">
      <span class="lc-meta">
        {{ typeLabel }} · {{ folderCount }} 个文件夹
        <template v-if="isScanning"> · 扫描中 {{ scanPct }}%</template>
        <template v-else-if="scanProg?.Status === 'completed'"> · ✓ 扫描完成</template>
      </span>
    </div>
  </div>
</template>

<style scoped>
.lc {
  display: block;
  text-decoration: none;
  color: unset;
  border-radius: 10px;
  overflow: hidden;
  background: var(--app-surface-1, #0f172a);
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  transition: transform 0.22s ease, box-shadow 0.22s ease;
  cursor: pointer;
}
.lc:hover {
  transform: translateY(-4px);
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
}

/* 封面区 16:9 */
.lc-cover {
  position: relative;
  width: 100%;
  padding-bottom: 56.25%;
  overflow: hidden;
  background: #0f172a;
}

/* 封面图片（img 标签，绝对定位撑满） */
.lc-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.35s ease;
}
.lc:hover .lc-img {
  transform: scale(1.05);
}

/* 无封面占位 */
.lc-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.lc-icon {
  width: 56px;
  height: 56px;
  color: rgba(255, 255, 255, 0.12);
}

/* 底部渐变遮罩 */
.lc-gradient {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 14px 16px;
  background: linear-gradient(0deg, rgba(0,0,0,0.78) 0%, rgba(0,0,0,0.2) 50%, transparent 100%);
  z-index: 1;
  pointer-events: none;
}
.lc-gradient-noimg {
  background: linear-gradient(0deg, rgba(0,0,0,0.5) 0%, transparent 60%);
}

.lc-badge {
  align-self: flex-start;
  padding: 3px 10px;
  border-radius: 4px;
  background: var(--app-primary, #10b981);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
  line-height: 1.4;
}

.lc-name {
  margin: 0;
  font-size: 1.2em;
  font-weight: 800;
  color: #fff;
  text-shadow: 0 2px 10px rgba(0,0,0,0.6);
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* hover 编辑遮罩 */
.lc-hover-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0,0,0,0.4);
  opacity: 0;
  transition: opacity 0.2s ease;
  z-index: 2;
}
.lc:hover .lc-hover-overlay {
  opacity: 1;
}

.lc-edit-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 18px;
  background: rgba(255,255,255,0.15);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-radius: 20px;
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
}

/* 扫描进度条 */
.lc-scan-bar {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 3;
}

/* 底部信息栏 */
.lc-info {
  padding: 10px 14px;
}

.lc-meta {
  display: block;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
