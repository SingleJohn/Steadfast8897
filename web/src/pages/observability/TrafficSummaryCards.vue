<script setup lang="ts">
import { NGrid, NGridItem } from 'naive-ui'
import { GlobeOutline, AlertCircleOutline, CheckmarkCircleOutline } from '@vicons/ionicons5'

import StatCard from '@/components/StatCard.vue'

defineProps<{
  statsSummary: {
    totalRequests: number
    total4xx: number
    total5xx: number
  }
}>()
</script>

<template>
  <n-grid cols="2 s:4" x-gap="16" y-gap="16" responsive="screen">
    <n-grid-item>
      <stat-card title="总请求" :value="statsSummary.totalRequests" :icon="GlobeOutline" type="primary" />
    </n-grid-item>
    <n-grid-item>
      <stat-card
        title="成功 (2xx/3xx)"
        :value="statsSummary.totalRequests - statsSummary.total4xx - statsSummary.total5xx"
        :icon="CheckmarkCircleOutline"
        type="success"
      />
    </n-grid-item>
    <n-grid-item>
      <stat-card title="客户端错误 (4xx)" :value="statsSummary.total4xx" :icon="AlertCircleOutline" type="warning" />
    </n-grid-item>
    <n-grid-item>
      <stat-card title="服务端错误 (5xx)" :value="statsSummary.total5xx" :icon="AlertCircleOutline" type="error" />
    </n-grid-item>
  </n-grid>
</template>
