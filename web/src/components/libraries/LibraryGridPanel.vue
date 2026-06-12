<script setup lang="ts">
import { NButton, NIcon } from 'naive-ui'
import { MoveOutline } from '@vicons/ionicons5'

import LibraryCard from '@/components/LibraryCard.vue'

defineProps<{
  libraries: any[]
  showItemCount: boolean
  savingLibraryOrder: boolean
  draggingLibraryId: string | null
  dragOverLibraryId: string | null
  scanProgForLib: (libId: string) => any
}>()

const emit = defineEmits<{
  addLibrary: []
  generateCovers: []
  edit: [id: string]
  dragStart: [index: number, event: DragEvent]
  dragOver: [index: number, event: DragEvent]
  drop: [event: DragEvent]
  dragEnd: []
  handleKeydown: [index: number, event: KeyboardEvent]
}>()
</script>

<template>
  <div>
    <div class="libraries-actions">
      <n-button secondary @click="emit('addLibrary')">添加媒体库</n-button>
      <n-button secondary :disabled="libraries.length === 0" @click="emit('generateCovers')">
        生成所有封面
      </n-button>
    </div>

    <div v-if="libraries.length === 0" class="lib-empty-card">
      <div class="lib-empty">尚未配置媒体库。点击“添加媒体库”开始使用。</div>
    </div>
    <div v-else class="lib-grid">
      <div
        v-for="(lib, idx) in libraries"
        :key="lib.ItemId"
        class="lib-card-wrapper"
        :class="{
          'lib-card-wrapper-dragging': draggingLibraryId === lib.ItemId,
          'lib-card-wrapper-over': dragOverLibraryId === lib.ItemId && draggingLibraryId !== lib.ItemId,
        }"
        @dragover="emit('dragOver', idx, $event)"
        @drop="emit('drop', $event)"
      >
        <LibraryCard
          :lib="lib"
          :scan-prog="scanProgForLib(lib.ItemId)"
          :show-item-count="showItemCount"
          @click="emit('edit', $event)"
        />
        <button
          type="button"
          class="lib-drag-handle"
          :draggable="libraries.length > 1 && !savingLibraryOrder"
          :disabled="libraries.length <= 1 || savingLibraryOrder"
          :aria-label="`拖动排序：${lib.Name}`"
          title="拖动排序"
          @click.stop
          @keydown.stop="emit('handleKeydown', idx, $event)"
          @dragstart.stop="emit('dragStart', idx, $event)"
          @dragend.stop="emit('dragEnd')"
        >
          <n-icon size="17" aria-hidden="true">
            <MoveOutline />
          </n-icon>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.libraries-actions {
  display: flex;
  justify-content: center;
  gap: 8px;
  margin-bottom: 18px;
}

.lib-grid {
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(2, 1fr);
}
@media (min-width: 600px)  { .lib-grid { grid-template-columns: repeat(3, 1fr); } }
@media (min-width: 960px)  { .lib-grid { grid-template-columns: repeat(4, 1fr); } }
@media (min-width: 1400px) { .lib-grid { grid-template-columns: repeat(5, 1fr); } }

.lib-card-wrapper {
  position: relative;
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.lib-card-wrapper-dragging {
  opacity: 0.58;
  transform: scale(0.98);
}
.lib-card-wrapper-over::after {
  content: "";
  position: absolute;
  inset: -6px;
  z-index: 40;
  pointer-events: none;
  border: 1px solid var(--app-primary, #10b981);
  border-radius: 12px;
  background: color-mix(in srgb, var(--app-primary, #10b981) 12%, transparent);
}
.lib-drag-handle {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 45;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border: 1px solid rgba(255,255,255,0.16);
  border-radius: 4px;
  background: rgba(0,0,0,0.58);
  color: rgba(255,255,255,0.84);
  line-height: 1;
  cursor: grab;
  opacity: 0.72;
  box-shadow: 0 6px 16px rgba(0,0,0,0.28);
  transition: opacity 0.16s ease, background-color 0.16s ease, border-color 0.16s ease, transform 0.16s ease;
}
.lib-drag-handle:hover,
.lib-drag-handle:focus-visible {
  opacity: 1;
  background: rgba(0,0,0,0.74);
  border-color: rgba(255,255,255,0.3);
}
.lib-drag-handle:focus-visible {
  outline: 2px solid var(--app-primary, #10b981);
  outline-offset: 2px;
}
.lib-drag-handle:active {
  cursor: grabbing;
  transform: scale(0.96);
}
.lib-drag-handle:disabled {
  cursor: default;
  opacity: 0.36;
}

.lib-empty-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 8px 0;
}
.lib-empty { padding: 20px 24px; color: var(--app-text-muted); font-size: 14px; }

@media (prefers-reduced-motion: reduce) {
  .lib-card-wrapper,
  .lib-drag-handle {
    transition: none;
  }
}

@media (max-width: 640px) {
  .libraries-actions {
    justify-content: stretch;
    flex-direction: column;
  }
}
</style>
