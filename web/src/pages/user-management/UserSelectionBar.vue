<script setup lang="ts">
import { computed } from 'vue'
import { NCheckbox, NButton } from 'naive-ui'

const props = defineProps<{
  filteredCount: number        // 当前筛选条件下的全部用户数（跨页）
  pageSelectableCount: number  // 当前页可选用户数
  selectedCount: number        // 已选总数（跨页）
  allPageSelected: boolean     // 当前页可选项是否已全选
  pageIndeterminate: boolean   // 当前页部分选中
  allFilteredSelected: boolean // 是否已选中全部筛选结果
}>()

const emit = defineEmits<{
  (e: 'toggle-page', checked: boolean): void
  (e: 'select-all-filtered'): void
  (e: 'clear'): void
}>()

// 当前页已全选，但筛选结果还有更多页未选 —— 提示可一键选择全部。
const canSelectAllFiltered = computed(() =>
  props.allPageSelected
  && !props.allFilteredSelected
  && props.filteredCount > props.pageSelectableCount,
)
</script>

<template>
  <div class="selection-bar">
    <n-checkbox
      :checked="allPageSelected"
      :indeterminate="pageIndeterminate"
      :disabled="pageSelectableCount === 0"
      @update:checked="emit('toggle-page', $event)"
    >
      本页全选
    </n-checkbox>

    <span v-if="selectedCount > 0" class="sel-count">已选 {{ selectedCount }} 个</span>

    <n-button v-if="canSelectAllFiltered" text type="primary" size="small" @click="emit('select-all-filtered')">
      选择全部 {{ filteredCount }} 个匹配用户
    </n-button>
    <span v-else-if="allFilteredSelected && filteredCount > pageSelectableCount" class="all-hint">
      已选中全部 {{ filteredCount }} 个匹配用户
    </span>

    <div style="flex: 1" />

    <n-button v-if="selectedCount > 0" text size="small" @click="emit('clear')">清除选择</n-button>
  </div>
</template>

<style scoped>
.selection-bar {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 8px 12px;
  margin-bottom: 12px;
  background: var(--app-modal-panel-bg-soft, rgba(128,128,128,0.04));
  border: 1px solid var(--app-border);
  border-radius: 8px;
  font-size: 13px;
}

.sel-count { color: var(--app-text-muted); }
.all-hint { color: var(--app-primary); font-weight: 500; }

@media (max-width: 640px) {
  .selection-bar { flex-wrap: wrap; gap: 8px; }
}
</style>
