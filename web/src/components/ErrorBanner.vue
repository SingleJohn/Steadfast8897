<script setup lang="ts">
import { NAlert } from 'naive-ui'

defineProps<{
  message: string
  title?: string
}>()
</script>

<template>
  <n-alert type="error" :title="title" :show-icon="false">
    {{ message }}
  </n-alert>
</template>

