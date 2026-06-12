<script setup lang="ts">
import { NIcon } from 'naive-ui'
import { MoveOutline } from '@vicons/ionicons5'

defineProps<{
  orderList: { kind: 'library' | 'platform'; id: string; name: string; type: string }[]
  savingOrder: boolean
  draggingOrderKey: string | null
  dragOverOrderKey: string | null
}>()

const emit = defineEmits<{
  dragStart: [index: number, event: DragEvent]
  dragOver: [index: number, event: DragEvent]
  drop: [event: DragEvent]
  dragEnd: []
  handleKeydown: [index: number, event: KeyboardEvent]
}>()

function orderKey(e: { kind: string; id: string }) {
  return `${e.kind}:${e.id}`
}
</script>

<template>
  <div class="settings-card">
    <div class="settings-card-header">
      <div>
        <h3 class="settings-card-title">整体排序</h3>
        <div class="settings-card-desc">调整实际媒体库与虚拟库在播放器中的统一显示顺序。保存后立即生效；未参与排序的新库会自动排在末尾。</div>
      </div>
    </div>
    <div v-if="orderList.length === 0" class="setting-desc order-empty">暂无可排序的媒体库</div>
    <div v-else>
      <div
        v-for="(e, idx) in orderList"
        :key="orderKey(e)"
        class="order-row"
        :class="{
          'order-row-dragging': draggingOrderKey === orderKey(e),
          'order-row-over': dragOverOrderKey === orderKey(e) && draggingOrderKey !== orderKey(e),
        }"
        @dragover="emit('dragOver', idx, $event)"
        @drop="emit('drop', $event)"
      >
        <button
          type="button"
          class="order-drag-handle"
          :draggable="orderList.length > 1 && !savingOrder"
          :disabled="orderList.length <= 1 || savingOrder"
          :aria-label="`拖动排序：${e.name}`"
          title="拖动排序"
          @click.stop
          @keydown.stop="emit('handleKeydown', idx, $event)"
          @dragstart.stop="emit('dragStart', idx, $event)"
          @dragend.stop="emit('dragEnd')"
        >
          <n-icon size="16" aria-hidden="true"><MoveOutline /></n-icon>
        </button>
        <span class="order-kind-badge" :class="e.kind === 'platform' ? 'is-platform' : 'is-library'">{{ e.type }}</span>
        <span class="order-name">{{ e.name }}</span>
      </div>
      <div class="setting-desc order-tip">拖动左侧手柄即可调整顺序，松手后自动保存。</div>
    </div>
  </div>
</template>

<style scoped>
.settings-card {
  background: var(--app-surface-1, var(--bg-card));
  border: 1px solid var(--app-border, rgba(255,255,255,0.06));
  border-radius: var(--app-radius, 10px);
  padding: 20px 24px;
}
.settings-card-header { margin-bottom: 10px; }
.settings-card-title { font-size: 15px; font-weight: 600; color: var(--app-text); margin: 0 0 6px; }
.settings-card-desc,
.setting-desc { font-size: 12px; color: var(--app-text-muted); }
.settings-card-desc { font-size: 13px; }
.order-empty { padding: 12px 0; }
.order-tip { margin-top: 10px; }

.order-row { display: flex; align-items: center; gap: 8px; padding: 10px 8px; border-bottom: 1px solid var(--app-border, rgba(255,255,255,0.04)); border-radius: 8px; transition: background-color 0.16s ease, opacity 0.16s ease; }
.order-row:last-child { border-bottom: none; }
.order-row-dragging { opacity: 0.55; }
.order-row-over { background: color-mix(in srgb, var(--app-primary, #10b981) 12%, transparent); box-shadow: inset 0 0 0 1px var(--app-primary, #10b981); }
.order-drag-handle {
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; flex-shrink: 0;
  border: 1px solid var(--app-border, rgba(255,255,255,0.16));
  border-radius: 6px; background: var(--app-modal-panel-bg-soft, rgba(255,255,255,0.04));
  color: var(--app-text-muted); cursor: grab; line-height: 1;
  transition: opacity 0.16s ease, border-color 0.16s ease, background-color 0.16s ease;
}
.order-drag-handle:hover, .order-drag-handle:focus-visible { color: var(--app-text); border-color: rgba(var(--app-primary-rgb), 0.4); }
.order-drag-handle:focus-visible { outline: 2px solid var(--app-primary, #10b981); outline-offset: 2px; }
.order-drag-handle:active { cursor: grabbing; }
.order-drag-handle:disabled { cursor: default; opacity: 0.4; }
.order-name { flex: 1; min-width: 0; font-size: 14px; color: var(--app-text); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.order-kind-badge { font-size: 10px; border-radius: 4px; padding: 1px 6px; flex-shrink: 0; }
.order-kind-badge.is-library { color: var(--app-primary); border: 1px solid rgba(var(--app-primary-rgb), 0.4); }
.order-kind-badge.is-platform { color: var(--app-text-muted); border: 1px solid var(--app-border, rgba(255,255,255,0.12)); }

@media (prefers-reduced-motion: reduce) {
  .order-row,
  .order-drag-handle {
    transition: none;
  }
}
</style>
