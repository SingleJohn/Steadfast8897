<script setup lang="ts">
import { NButton, NCard, NSpace, NText } from 'naive-ui'

withDefaults(
  defineProps<{
    dirty: boolean
    disabled?: boolean
    primaryLoading?: boolean
    primaryDisabled?: boolean
    secondaryDisabled?: boolean
    dirtyText?: string
    cleanText?: string
    primaryLabel?: string
    secondaryLabel?: string
  }>(),
  {
    disabled: false,
    primaryLoading: false,
    primaryDisabled: false,
    secondaryDisabled: false,
    dirtyText: '有未保存的修改',
    cleanText: '已同步到最新配置',
    primaryLabel: '保存',
    secondaryLabel: '重置',
  },
)

const emit = defineEmits<{
  (e: 'primary'): void
  (e: 'secondary'): void
}>()
</script>

<template>
  <div class="bar">
    <n-card size="small" class="glass-card">
      <n-space justify="space-between" align="center" class="bar-row">
        <n-text depth="3" class="status-text">{{ dirty ? dirtyText : cleanText }}</n-text>
        <n-space :size="8" class="action-group">
          <n-button :disabled="disabled || secondaryDisabled" @click="emit('secondary')">
            {{ secondaryLabel }}
          </n-button>
          <n-button
            type="primary"
            :disabled="disabled || primaryDisabled"
            :loading="primaryLoading"
            @click="emit('primary')"
          >
            {{ primaryLabel }}
          </n-button>
        </n-space>
      </n-space>
    </n-card>
  </div>
</template>

<style scoped>
.bar {
  position: sticky;
  bottom: 0;
  padding-top: 12px;
  z-index: 10;
}

.bar-row {
  width: 100%;
}

.status-text {
  min-width: 0;
}

@media (max-width: 768px) {
  .bar-row {
    align-items: stretch;
    flex-direction: column;
    gap: 10px;
  }

  .action-group {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>

