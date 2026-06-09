<script setup lang="ts">
import { NIcon, NInput, NSelect } from 'naive-ui'
import { GridOutline, ListOutline, SearchOutline } from '@vicons/ionicons5'

defineProps<{
  searchTerm: string
  statusFilter: string
  groupFilter: string
  viewMode: 'card' | 'table'
  statusOptions: Array<{ label: string; value: string }>
  groupOptions: Array<{ label: string; value: string }>
  menuProps?: Record<string, any>
}>()

const emit = defineEmits<{
  (e: 'update:searchTerm', value: string): void
  (e: 'update:statusFilter', value: string): void
  (e: 'update:groupFilter', value: string): void
  (e: 'update:viewMode', value: 'card' | 'table'): void
}>()
</script>

<template>
  <div class="management-toolbar">
    <n-input
      :value="searchTerm"
      placeholder="搜索用户"
      clearable
      size="small"
      class="toolbar-search"
      @update:value="emit('update:searchTerm', $event)"
    >
      <template #suffix><n-icon :size="16"><SearchOutline /></n-icon></template>
    </n-input>
    <n-select
      :value="statusFilter"
      :options="statusOptions"
      size="small"
      class="toolbar-select status-select"
      :menu-props="menuProps"
      @update:value="emit('update:statusFilter', $event)"
    />
    <n-select
      :value="groupFilter"
      :options="groupOptions"
      size="small"
      class="toolbar-select group-select"
      :menu-props="menuProps"
      @update:value="emit('update:groupFilter', $event)"
    />
    <div class="view-switch" aria-label="视图切换">
      <button type="button" :class="{ active: viewMode === 'card' }" title="卡片视图" @click="emit('update:viewMode', 'card')">
        <n-icon :size="16"><GridOutline /></n-icon>
      </button>
      <button type="button" :class="{ active: viewMode === 'table' }" title="表格视图" @click="emit('update:viewMode', 'table')">
        <n-icon :size="16"><ListOutline /></n-icon>
      </button>
    </div>
  </div>
</template>

<style scoped>
.management-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0 18px;
}

.toolbar-search {
  width: min(34vw, 260px);
  min-width: 210px;
}

.toolbar-select {
  width: 142px;
}

.group-select {
  width: 202px;
}

.view-switch {
  display: inline-flex;
  align-items: center;
  overflow: hidden;
  height: 32px;
  border: 1px solid var(--app-border);
  border-radius: 7px;
  background: var(--app-surface-1);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}

.view-switch button {
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  background: transparent;
  color: var(--app-text-muted);
  cursor: pointer;
}

.view-switch button + button {
  border-left: 1px solid var(--app-border);
}

.view-switch button.active {
  background: var(--app-primary);
  color: #fff;
}

@media (max-width: 640px) {
  .management-toolbar {
    align-items: stretch;
    flex-wrap: wrap;
  }

  .toolbar-search,
  .toolbar-select,
  .group-select {
    width: 100%;
    min-width: 0;
  }
}
</style>
