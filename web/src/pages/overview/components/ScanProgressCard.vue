<script setup lang="ts">
import { NCard, NProgress } from 'naive-ui'
import type { ScanProgressItem } from '../types'

defineProps<{
  items: ScanProgressItem[]
  libraryNameFor: (libraryId: string) => string
}>()
</script>

<template>
  <n-card v-if="items.length > 0" class="section-card" title="扫描进度" size="small">
    <template #header-extra>
      <span class="subtle-count">{{ items.length }}</span>
    </template>
    <ul class="scan-list">
      <li v-for="sp in items" :key="sp.LibraryId" class="scan-item">
        <div class="scan-name">{{ libraryNameFor(sp.LibraryId) }}</div>
        <template v-if="sp.Status === 'scanning'">
          <n-progress type="line" :percentage="sp.Percentage" :show-indicator="false" class="scan-bar" />
          <span class="scan-pct">{{ sp.Percentage }}%</span>
          <span class="scan-detail">{{ sp.ProcessedItems }}/{{ sp.TotalItems }}</span>
        </template>
        <span v-else-if="sp.Status === 'completed'" class="scan-tag scan-tag-ok">已完成</span>
        <span v-else-if="sp.Status === 'failed'" class="scan-tag scan-tag-err">失败 · {{ sp.Error }}</span>
        <span v-else class="scan-tag">{{ sp.Status }}</span>
      </li>
    </ul>
  </n-card>
</template>
