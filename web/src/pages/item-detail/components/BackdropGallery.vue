<script setup lang="ts">
import type { BackdropImage } from '../types'

defineProps<{
  itemName: string
  images: BackdropImage[]
  activeIndex: number
}>()

const emit = defineEmits<{
  preview: [index: number]
}>()
</script>

<template>
  <div v-if="images.length > 1" class="backdrop-gallery-section">
    <div class="section-title-row">
      <h3 class="section-heading section-heading-light">背景图</h3>
      <span class="section-count">{{ images.length }} 张</span>
    </div>
    <div class="backdrop-strip" role="list" aria-label="背景图列表">
      <button
        v-for="img in images"
        :key="`${img.index}-${img.tag || 'main'}`"
        type="button"
        class="backdrop-thumb"
        :class="{ active: activeIndex === img.index }"
        :aria-label="`查看第 ${img.index + 1} 张背景图`"
        @click="emit('preview', img.index)"
      >
        <img
          :src="img.thumb"
          :alt="`${itemName} 背景图 ${img.index + 1}`"
          width="420"
          height="236"
          loading="lazy"
        />
        <span class="backdrop-thumb-index">{{ String(img.index + 1).padStart(2, '0') }}</span>
      </button>
    </div>
  </div>
</template>

