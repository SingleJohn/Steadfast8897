<script setup lang="ts">
import { NEmpty } from 'naive-ui'

defineProps<{
  description?: string
}>()
</script>

<template>
  <div class="empty-state">
    <n-empty :description="description || '暂无数据'">
      <template v-if="$slots.default" #default>
        <slot />
      </template>
    </n-empty>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  padding: 40px 20px;
}
</style>
