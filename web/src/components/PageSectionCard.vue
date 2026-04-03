<script setup lang="ts">
import { NCard } from 'naive-ui'

defineProps<{
  title?: string
  bordered?: boolean
  size?: 'small' | 'medium' | 'large'
  contentStyle?: string
}>()
</script>

<template>
  <n-card
    :title="title"
    :bordered="bordered ?? false"
    :size="size || 'small'"
    :content-style="contentStyle"
    class="glass-card page-section-card"
  >
    <template #header-extra>
      <slot name="header-extra" />
    </template>
    <slot />
  </n-card>
</template>

<style scoped>
.page-section-card {
  overflow: hidden;
}
</style>
