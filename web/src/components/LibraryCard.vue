<script setup lang="ts">
import { computed, ref } from 'vue'
import { NProgress } from 'naive-ui'

const props = defineProps<{
  lib: any
  scanProg?: any
  showItemCount?: boolean
}>()

const emit = defineEmits<{ click: [libId: string] }>()

const imgFailed = ref(false)
const foldersOpen = ref(false)
const countFormatter = new Intl.NumberFormat('zh-CN')

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
  if (ct === 'mixed') return '混合'
  if (ct === 'music') return '音乐'
  return '媒体'
})

const emptyGradient = computed(() => {
  const ct = props.lib.CollectionType
  if (ct === 'movies') return 'linear-gradient(135deg, #1a1a2e 0%, #16213e 40%, #0f3460 100%)'
  if (ct === 'tvshows') return 'linear-gradient(135deg, #1a1a2e 0%, #1b2838 40%, #1a3a4a 100%)'
  if (ct === 'mixed') return 'linear-gradient(135deg, #1a1a2e 0%, #24324a 42%, #14505a 100%)'
  if (ct === 'music') return 'linear-gradient(135deg, #1a1a2e 0%, #2d1b3d 40%, #4a1942 100%)'
  return 'linear-gradient(135deg, #1a1a2e 0%, #1e293b 40%, #334155 100%)'
})

const locations = computed<string[]>(() => {
  const raw = props.lib.Locations
  if (!Array.isArray(raw)) return []
  return raw.filter((path) => typeof path === 'string' && path.trim())
})

const folderCount = computed(() => locations.value.length)
const itemCount = computed(() => props.lib.ItemCount ?? props.lib.RecursiveItemCount ?? props.lib.ChildCount ?? 0)
const formattedItemCount = computed(() => countFormatter.format(Number(itemCount.value) || 0))
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
      <div v-if="imgFailed" class="lc-placeholder" :style="{ background: emptyGradient }" />

      <!-- 底部渐变 + 名称 -->
      <div class="lc-gradient" :class="{ 'lc-gradient-noimg': imgFailed }">
        <h3 class="lc-name">{{ lib.Name }}</h3>
      </div>

      <div v-if="showItemCount !== false" class="lc-count-badge" :title="`${formattedItemCount} 个媒体项目`">
        {{ formattedItemCount }} 项
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
        <span class="lc-type-label">{{ typeLabel }}</span>
        <span class="lc-separator">·</span>
        <span
          class="lc-folder-wrap"
          :class="{ 'lc-folder-wrap-open': foldersOpen }"
          @click.stop
          @keydown.stop
          @mouseleave="foldersOpen = false"
        >
          <button
            type="button"
            class="lc-folder-trigger"
            :aria-expanded="foldersOpen"
            :aria-label="`查看 ${lib.Name} 的 ${folderCount} 个文件夹`"
            @click="foldersOpen = !foldersOpen"
          >
            {{ folderCount }} 个文件夹
          </button>
          <span class="lc-folder-popover" role="tooltip">
            <span v-if="locations.length === 0" class="lc-folder-empty">未配置文件夹</span>
            <template v-else>
              <span v-for="path in locations" :key="path" class="lc-folder-path" translate="no">
                {{ path }}
              </span>
            </template>
          </span>
        </span>
        <template v-if="isScanning">
          <span class="lc-separator">·</span>
          <span class="lc-scan-state">扫描中 {{ scanPct }}%</span>
        </template>
        <template v-else-if="scanProg?.Status === 'completed'">
          <span class="lc-separator">·</span>
          <span class="lc-scan-state">✓ 扫描完成</span>
        </template>
      </span>
    </div>
  </div>
</template>

<style scoped>
.lc {
  position: relative;
  z-index: 0;
  display: block;
  text-decoration: none;
  color: unset;
  border-radius: 10px;
  overflow: visible;
  background: var(--app-surface-1, #0f172a);
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  transition: transform 0.22s ease, box-shadow 0.22s ease;
  cursor: pointer;
}
.lc:hover {
  z-index: 20;
  transform: translateY(-4px);
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
}

.lc:focus-within {
  z-index: 20;
}

/* 封面区 16:9 */
.lc-cover {
  position: relative;
  width: 100%;
  padding-bottom: 56.25%;
  overflow: hidden;
  border-radius: 10px 10px 0 0;
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

.lc-count-badge {
  position: absolute;
  top: 10px;
  right: 10px;
  z-index: 2;
  max-width: calc(100% - 20px);
  padding: 4px 8px;
  border: 1px solid rgba(255,255,255,0.18);
  border-radius: 4px;
  background: rgba(0,0,0,0.58);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  position: relative;
  z-index: 4;
  border-radius: 0 0 10px 10px;
  background: var(--app-surface-1, #0f172a);
}

.lc-meta {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 4px;
  font-size: 12px;
  color: var(--app-text-muted);
  overflow: visible;
  white-space: nowrap;
}

.lc-type-label,
.lc-separator,
.lc-scan-state {
  flex-shrink: 0;
}

.lc-folder-wrap {
  position: relative;
  display: inline-flex;
  max-width: min(100%, 180px);
  vertical-align: bottom;
}

.lc-folder-trigger {
  appearance: none;
  border: 0;
  margin: 0;
  padding: 0;
  max-width: 100%;
  background: transparent;
  color: var(--app-text-muted);
  font: inherit;
  line-height: inherit;
  cursor: pointer;
  text-decoration: underline;
  text-decoration-color: transparent;
  text-underline-offset: 3px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.16s ease, text-decoration-color 0.16s ease;
}

.lc-folder-trigger:hover,
.lc-folder-trigger:focus-visible {
  color: var(--app-text);
  text-decoration-color: currentColor;
}

.lc-folder-trigger:focus-visible {
  outline: 2px solid var(--app-primary, #10b981);
  outline-offset: 2px;
  border-radius: 3px;
}

.lc-folder-popover {
  position: absolute;
  left: 0;
  bottom: calc(100% + 10px);
  display: none;
  width: min(320px, calc(100vw - 48px));
  max-height: 180px;
  overflow: auto;
  padding: 10px 12px;
  border: 1px solid var(--app-border, rgba(255,255,255,0.12));
  border-radius: 6px;
  background: var(--app-surface-2, #111827);
  box-shadow: 0 14px 34px rgba(0,0,0,0.38);
  color: var(--app-text);
  white-space: normal;
}

.lc-folder-wrap:hover .lc-folder-popover,
.lc-folder-wrap:focus-within .lc-folder-popover,
.lc-folder-wrap-open .lc-folder-popover {
  display: grid;
  gap: 6px;
}

.lc-folder-path,
.lc-folder-empty {
  display: block;
  min-width: 0;
  font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
  font-size: 11px;
  line-height: 1.45;
  word-break: break-all;
}

.lc-folder-empty {
  color: var(--app-text-muted);
  font-family: inherit;
}

@media (prefers-reduced-motion: reduce) {
  .lc,
  .lc-img,
  .lc-hover-overlay,
  .lc-folder-trigger {
    transition: none;
  }
}
</style>
