<script setup lang="ts">
import { NIcon, NModal } from 'naive-ui'
import { ChevronBackOutline, ChevronForwardOutline, CloseOutline } from '@vicons/ionicons5'
import type { BackdropImage } from '../types'

defineProps<{
  show: boolean
  image: BackdropImage | null
  itemName: string
  activeIndex: number
  total: number
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  prev: []
  next: []
}>()
</script>

<template>
  <n-modal :show="show" class="media-lightbox-modal" :mask-closable="true" @update:show="emit('update:show', $event)">
    <div
      v-if="image"
      class="backdrop-lightbox"
      tabindex="0"
      @keydown.esc="emit('update:show', false)"
      @keydown.left.prevent="emit('prev')"
      @keydown.right.prevent="emit('next')"
    >
      <button type="button" class="lightbox-close" aria-label="关闭背景图预览" @click="emit('update:show', false)">
        <n-icon :size="24"><CloseOutline /></n-icon>
      </button>
      <button
        v-if="total > 1"
        type="button"
        class="lightbox-nav lightbox-nav-prev"
        aria-label="上一张背景图"
        @click="emit('prev')"
      >
        <n-icon :size="28"><ChevronBackOutline /></n-icon>
      </button>
      <img
        :src="image.src"
        :alt="`${itemName} 背景图 ${activeIndex + 1}`"
        class="lightbox-image"
        width="1920"
        height="1080"
      />
      <button
        v-if="total > 1"
        type="button"
        class="lightbox-nav lightbox-nav-next"
        aria-label="下一张背景图"
        @click="emit('next')"
      >
        <n-icon :size="28"><ChevronForwardOutline /></n-icon>
      </button>
      <div class="lightbox-counter">{{ activeIndex + 1 }} / {{ total }}</div>
    </div>
  </n-modal>
</template>

