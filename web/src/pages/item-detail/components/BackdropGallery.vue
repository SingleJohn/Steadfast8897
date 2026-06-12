<script setup lang="ts">
import { ref } from 'vue'
import type { BackdropImage } from '../types'

defineProps<{
  itemName: string
  images: BackdropImage[]
  activeIndex: number
}>()

const emit = defineEmits<{
  preview: [index: number]
}>()

const stripRef = ref<HTMLElement | null>(null)
const dragging = ref(false)
const suppressClick = ref(false)
const dragState = {
  pointerId: -1,
  startX: 0,
  scrollLeft: 0,
  moved: false,
}

function handlePointerDown(event: PointerEvent) {
  if (event.button !== 0 || !stripRef.value) return
  dragState.pointerId = event.pointerId
  dragState.startX = event.clientX
  dragState.scrollLeft = stripRef.value.scrollLeft
  dragState.moved = false
  suppressClick.value = false
  dragging.value = true
  stripRef.value.setPointerCapture(event.pointerId)
}

function handlePointerMove(event: PointerEvent) {
  if (!dragging.value || event.pointerId !== dragState.pointerId || !stripRef.value) return
  const deltaX = event.clientX - dragState.startX
  if (Math.abs(deltaX) > 4) {
    dragState.moved = true
    suppressClick.value = true
  }
  stripRef.value.scrollLeft = dragState.scrollLeft - deltaX
}

function endDrag(event: PointerEvent) {
  if (event.pointerId !== dragState.pointerId) return
  if (stripRef.value?.hasPointerCapture(event.pointerId)) {
    stripRef.value.releasePointerCapture(event.pointerId)
  }
  dragging.value = false
  dragState.pointerId = -1
  window.setTimeout(() => {
    suppressClick.value = false
  }, 0)
}

function handlePreview(index: number) {
  if (suppressClick.value) return
  emit('preview', index)
}
</script>

<template>
  <div v-if="images.length > 1" class="backdrop-gallery-section">
    <div class="section-title-row">
      <h3 class="section-heading section-heading-light">剧照</h3>
      <span class="section-count">{{ images.length }} 张</span>
    </div>
    <div
      ref="stripRef"
      class="backdrop-strip"
      :class="{ dragging }"
      role="list"
      aria-label="剧照列表"
      @pointerdown="handlePointerDown"
      @pointermove="handlePointerMove"
      @pointerup="endDrag"
      @pointercancel="endDrag"
    >
      <button
        v-for="img in images"
        :key="`${img.index}-${img.tag || 'main'}`"
        type="button"
        class="backdrop-thumb"
        :class="{ active: activeIndex === img.index }"
        :aria-label="`查看第 ${img.index + 1} 张剧照`"
        @click="handlePreview(img.index)"
      >
        <img
          :src="img.thumb"
          :alt="`${itemName} 剧照 ${img.index + 1}`"
          width="420"
          height="236"
          loading="lazy"
          draggable="false"
        />
        <span class="backdrop-thumb-index">{{ String(img.index + 1).padStart(2, '0') }}</span>
      </button>
    </div>
  </div>
</template>
