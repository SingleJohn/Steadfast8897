<script setup lang="ts">
import { NButton, NCard, NIcon } from 'naive-ui'
import { LayersOutline, ReorderFourOutline } from '@vicons/ionicons5'
import type { FieldPriorityMap } from '@/api/client'

defineProps<{
  fieldNames: string[]
  fieldPriority: FieldPriorityMap
  providerMeta: Record<string, any>
  fieldDragging: { field: string; index: number } | null
  fieldDragOver: { field: string; index: number } | null
  fieldLabel: (field: string) => string
  providerLabel: (provider: string) => string
}>()

const emit = defineEmits<{
  reset: []
  save: []
  dragStart: [field: string, index: number, event: DragEvent]
  dragOver: [field: string, index: number, event: DragEvent]
  drop: [field: string, index: number, event: DragEvent]
  dragEnd: []
  dragLeave: [field: string, index: number]
}>()
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card metadata-card">
    <template #header>
      <div class="card-header-wrap">
        <div class="icon-box field">
          <n-icon :size="18"><LayersOutline /></n-icon>
        </div>
        <div class="header-copy">
          <div class="header-title">字段填充优先级</div>
          <div class="header-desc">多源合并时,每个字段按此顺序取首个非空值。</div>
        </div>
      </div>
    </template>
    <template #header-extra>
      <n-button quaternary size="small" @click="emit('reset')">重置为默认</n-button>
    </template>

    <div class="field-priority-list">
      <div v-for="f in fieldNames" :key="f" class="field-priority-row">
        <div class="field-priority-label">{{ fieldLabel(f) }}</div>
        <div class="field-priority-pills">
          <div
            v-for="(pname, pidx) in (fieldPriority[f] || [])"
            :key="pname"
            class="field-priority-pill"
            :class="{
              dragging: fieldDragging && fieldDragging.field === f && fieldDragging.index === pidx,
              'drag-over': fieldDragOver && fieldDragOver.field === f && fieldDragOver.index === pidx && !(fieldDragging && fieldDragging.field === f && fieldDragging.index === pidx),
            }"
            :style="{ '--accent': providerMeta[pname]?.accent }"
            draggable="true"
            title="拖拽调整顺序"
            @dragstart="emit('dragStart', f, pidx, $event)"
            @dragover="emit('dragOver', f, pidx, $event)"
            @drop="emit('drop', f, pidx, $event)"
            @dragend="emit('dragEnd')"
            @dragleave="emit('dragLeave', f, pidx)"
          >
            <n-icon class="pill-handle"><ReorderFourOutline /></n-icon>
            <span class="pill-order">{{ pidx + 1 }}</span>
            <span class="pill-name">{{ providerLabel(pname) }}</span>
          </div>
          <div v-if="!(fieldPriority[f] || []).length" class="hint-text">无启用源</div>
        </div>
      </div>
    </div>

    <div class="card-actions">
      <n-button type="primary" size="small" @click="emit('save')">保存字段顺序</n-button>
    </div>
  </n-card>
</template>
