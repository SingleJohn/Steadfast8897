<script setup lang="ts">
import PageHeader from '@/components/PageHeader.vue'
import type { IconComponent } from '@/icons/appIcons'

withDefaults(
  defineProps<{
    title: string
    description?: string
    icon?: IconComponent
    divider?: boolean
    bodyClass?: string
  }>(),
  { divider: false, bodyClass: '' },
)
</script>

<template>
  <div class="page-shell">
    <page-header :title="title" :description="description" :icon="icon" :divider="divider">
      <template #actions>
        <slot name="actions" />
      </template>
    </page-header>

    <div class="page-shell-body" :class="bodyClass">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.page-shell-body { margin-top: 8px; }
</style>
