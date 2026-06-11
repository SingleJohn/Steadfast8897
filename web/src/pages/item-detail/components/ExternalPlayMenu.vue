<script setup lang="ts">
import { computed, ref } from 'vue'
import { NPopover, useMessage } from 'naive-ui'
import { getAbsoluteUrl, getExternalStreamUrl, getSubtitleUrl } from '@/api/client'
import {
  type ExternalPlayer,
  type LaunchContext,
  copyText,
  detectOS,
  launchExternal,
  playersForOS,
} from '@/utils/externalPlayers'

interface MediaStreamLike {
  Type?: string
  IsExternal?: boolean
  IsTextSubtitleStream?: boolean
  DeliveryUrl?: string
}

const props = withDefaults(
  defineProps<{
    itemId: string
    source: { Id: string; Container?: string; MediaStreams?: MediaStreamLike[] } | null
    title?: string
    positionTicks?: number
    // 浏览器无法直出时高亮入口(主按钮观感)。
    highlight?: boolean
  }>(),
  { title: '', positionTicks: 0, highlight: false },
)

const message = useMessage()
const show = ref(false)
const showAll = ref(false)

const os = detectOS()

const ctx = computed<LaunchContext | null>(() => {
  const s = props.source
  if (!props.itemId || !s?.Id) return null
  const streamUrl = getExternalStreamUrl(props.itemId, s)
  const sub = (s.MediaStreams || []).find((m) => m.Type === 'Subtitle' && m.IsExternal && m.DeliveryUrl)
  const subUrl = sub?.DeliveryUrl ? getAbsoluteUrl(getSubtitleUrl(sub.DeliveryUrl)) : ''
  return {
    streamUrl,
    subUrl,
    title: props.title,
    positionMs: props.positionTicks > 0 ? Math.floor(props.positionTicks / 10000) : 0,
  }
})

const osPlayers = computed(() => {
  const matched = playersForOS(os)
  return matched.length ? matched : playersForOS(null)
})
const listed = computed(() => (showAll.value ? playersForOS(null) : osPlayers.value))
const hasMore = computed(() => !showAll.value && playersForOS(null).length > osPlayers.value.length)

function pick(player: ExternalPlayer) {
  if (!ctx.value) return
  launchExternal(player, ctx.value, os)
  show.value = false
}

async function onCopy() {
  if (!ctx.value) return
  const ok = await copyText(ctx.value.streamUrl)
  if (ok) message.success('已复制直链')
  else message.error('复制失败')
  show.value = false
}
</script>

<template>
  <n-popover
    v-if="ctx"
    v-model:show="show"
    trigger="click"
    placement="bottom"
    :show-arrow="false"
    raw
  >
    <template #trigger>
      <button
        type="button"
        class="ext-play-trigger"
        :class="{ 'ext-play-highlight': highlight }"
        aria-label="用外部播放器打开"
        title="外部播放器"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
          <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
        </svg>
        <span>外部播放</span>
      </button>
    </template>

    <div class="ext-play-menu">
      <div class="ext-play-hint">用外部播放器打开当前版本</div>
      <button
        v-for="p in listed"
        :key="p.id"
        type="button"
        class="ext-play-item"
        @click="pick(p)"
      >
        {{ p.name }}
      </button>
      <button v-if="hasMore" type="button" class="ext-play-more" @click="showAll = true">
        显示全部播放器…
      </button>
      <div class="ext-play-divider" />
      <button type="button" class="ext-play-item ext-play-copy" @click="onCopy">复制直链</button>
    </div>
  </n-popover>
</template>

<style>
/* 非 scoped:NPopover 内容 teleport 到 body */
.ext-play-menu {
  min-width: 200px;
  max-width: 280px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-radius: 14px;
  background: rgba(20, 20, 22, 0.78);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(22px) saturate(1.2);
  -webkit-backdrop-filter: blur(22px) saturate(1.2);
}
.ext-play-hint {
  padding: 6px 10px 8px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}
.ext-play-item {
  appearance: none;
  border: 0;
  background: transparent;
  color: rgba(255, 255, 255, 0.9);
  text-align: left;
  font-size: 14px;
  font-weight: 600;
  padding: 9px 10px;
  border-radius: 9px;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}
.ext-play-item:hover {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
}
.ext-play-more {
  appearance: none;
  border: 0;
  background: transparent;
  color: rgba(56, 189, 248, 0.92);
  text-align: left;
  font-size: 12.5px;
  padding: 7px 10px;
  border-radius: 9px;
  cursor: pointer;
}
.ext-play-more:hover {
  background: rgba(56, 189, 248, 0.12);
}
.ext-play-divider {
  height: 1px;
  margin: 6px 4px;
  background: rgba(255, 255, 255, 0.08);
}
.ext-play-copy {
  color: rgba(186, 230, 253, 0.96);
}
</style>

<style scoped>
/* 自带按钮样式(对齐详情页 .btn-action),使组件在详情页与播放页都能独立使用。 */
.ext-play-trigger {
  height: 52px;
  padding: 0 20px;
  border-radius: 14px;
  font-family: inherit;
  font-size: 15px;
  font-weight: 600;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  cursor: pointer;
  color: #fff;
  background: rgba(18, 18, 18, 0.52);
  border: 1px solid rgba(255, 255, 255, 0.07);
  box-shadow: 0 8px 22px rgba(0, 0, 0, 0.28);
  backdrop-filter: blur(20px) saturate(1.2);
  -webkit-backdrop-filter: blur(20px) saturate(1.2);
  box-sizing: border-box;
  transition: background 0.2s ease, filter 0.2s ease;
}
.ext-play-trigger:hover {
  background: rgba(34, 34, 34, 0.72);
}
.ext-play-highlight,
.ext-play-highlight:hover {
  background: rgba(14, 165, 233, 0.28);
  border-color: rgba(14, 165, 233, 0.7);
}
</style>
