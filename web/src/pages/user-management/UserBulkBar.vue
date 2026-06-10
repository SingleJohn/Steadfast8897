<script setup lang="ts">
import { NButton, NSpace } from 'naive-ui'

defineProps<{
  selectedCount: number
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'enable'): void
  (e: 'disable'): void
  (e: 'show'): void
  (e: 'hide'): void
  (e: 'library'): void
  (e: 'policy'): void
  (e: 'delete'): void
}>()
</script>

<template>
  <div v-if="selectedCount > 0" class="bulk-bar">
    <div class="bulk-meta">
      已选择 <strong>{{ selectedCount }}</strong> 个用户
    </div>
    <n-space :size="8" wrap>
      <n-button size="small" secondary :loading="loading" @click="emit('enable')">启用</n-button>
      <n-button size="small" secondary :loading="loading" @click="emit('disable')">禁用</n-button>
      <n-button size="small" secondary :loading="loading" @click="emit('show')">显示</n-button>
      <n-button size="small" secondary :loading="loading" @click="emit('hide')">隐藏</n-button>
      <n-button size="small" secondary :loading="loading" @click="emit('library')">媒体库</n-button>
      <n-button size="small" type="primary" secondary :loading="loading" @click="emit('policy')">修改策略</n-button>
      <n-button size="small" type="error" ghost :loading="loading" @click="emit('delete')">删除</n-button>
    </n-space>
  </div>
</template>

<style scoped>
.bulk-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 12px;
  margin-bottom: 16px;
  background: var(--app-modal-panel-bg, var(--app-surface-1));
  border: 1px solid var(--app-border);
  border-radius: 8px;
}

.bulk-meta {
  font-size: 13px;
  color: var(--app-text-muted);
}

.bulk-meta strong {
  color: var(--app-text);
}

@media (max-width: 640px) {
  .bulk-bar {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
