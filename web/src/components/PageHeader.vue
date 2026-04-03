<script setup lang="ts">
import { NDivider, NH2, NIcon, NSpace, NText } from 'naive-ui'
import type { IconComponent } from '@/icons/appIcons'

withDefaults(
  defineProps<{
    title: string
    description?: string
    icon?: IconComponent
    divider?: boolean
  }>(),
  { divider: true },
)
</script>

<template>
  <div class="root">
    <n-space align="center" justify="space-between" :wrap="false">
      <n-space align="center" :size="12" :wrap="false" style="min-width: 0">
        <span v-if="icon" class="icon">
          <n-icon :size="18"><component :is="icon" /></n-icon>
        </span>
        <div style="min-width: 0">
          <n-h2 class="title">{{ title }}</n-h2>
          <n-text v-if="description" depth="3" class="desc">{{ description }}</n-text>
        </div>
      </n-space>
      <n-space align="center" :size="8" :wrap="false">
        <slot name="actions" />
      </n-space>
    </n-space>
    <n-divider v-if="divider" style="margin: 12px 0 0" />
  </div>
</template>

<style scoped>
.root { padding: 2px 0 0; }
.icon {
  width: 36px;
  height: 36px;
  border-radius: calc(var(--app-radius, 10px) + 2px);
  display: grid;
  place-items: center;
  background: var(--app-surface-2);
  border: 1px solid var(--app-border);
  box-shadow: var(--app-shadow-0);
  flex: 0 0 auto;
}
.title {
  margin: 0;
  font-size: 20px;
  line-height: 28px;
  letter-spacing: 0.2px;
}
.desc {
  display: block;
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
