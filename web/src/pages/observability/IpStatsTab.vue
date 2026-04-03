<script setup lang="ts">
import { NButton, NDatePicker, NDivider, NGrid, NGridItem, NSelect, NSpace, NText } from 'naive-ui'

import ErrorBanner from '@/components/ErrorBanner.vue'
import PageSectionCard from '@/components/PageSectionCard.vue'
import IpStatsDistributionGrid from '@/pages/observability/IPStatsDistributionGrid.vue'
import type { IPStatsSummary } from '@/types'

const ipStatsMode = defineModel<'all' | 'redirect302'>('ipStatsMode', { required: true })
const ipStatsRange = defineModel<[number, number] | null>('ipStatsRange', { required: true })

defineProps<{
  ipStatsError: string | null
  ipStatsLoading: boolean
  ipStatsSummary: IPStatsSummary | null
  ipStatsScopeLabel: string
  ipStatsRangeLabel: string
  ipStatsUseCumulative: boolean
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()
</script>

<template>
  <n-space vertical :size="16">
    <error-banner v-if="ipStatsError" :message="ipStatsError" />

    <page-section-card content-style="padding: 0;">
      <div class="filter-bar" style="padding-top: 12px; padding-bottom: 12px;">
        <n-grid cols="1 s:2 m:4" x-gap="12" y-gap="12" responsive="screen" align="center">
          <n-grid-item>
            <n-select
              v-model:value="ipStatsMode"
              :options="[
                { label: '全部请求', value: 'all' },
                { label: '仅 302', value: 'redirect302' },
              ]"
              size="small"
            />
          </n-grid-item>
          <n-grid-item>
            <n-date-picker v-model:value="ipStatsRange" type="datetimerange" clearable size="small" style="width: 100%" />
          </n-grid-item>
          <n-grid-item>
            <n-space align="center">
              <n-button size="small" :loading="ipStatsLoading" @click="emit('refresh')">刷新</n-button>
              <n-divider vertical />
              <n-text depth="3" size="small">Total: {{ ipStatsSummary?.total || 0 }}</n-text>
              <n-divider vertical />
              <n-text depth="3" size="small">待解析: {{ ipStatsSummary?.pending_enrich || 0 }}</n-text>
              <n-divider vertical />
              <n-text depth="3" size="small">口径: {{ ipStatsScopeLabel }}</n-text>
            </n-space>
          </n-grid-item>
          <n-grid-item>
            <n-text depth="3" size="small">{{ ipStatsRangeLabel }}</n-text>
          </n-grid-item>
        </n-grid>
      </div>
      <div class="ip-stats-hint" style="padding: 0 16px 16px;">
        <n-text depth="2" size="small">
          {{ ipStatsUseCumulative ? '默认汇总所有请求数据（无时间/模式过滤），选了时间范围或非默认模式则切换到根据请求日志动态计算的细粒度口径。' : '当前展示动态计算的细粒度口径统计。' }}
        </n-text>
      </div>
    </page-section-card>

    <ip-stats-distribution-grid :summary="ipStatsSummary" :loading="ipStatsLoading" />
  </n-space>
</template>

<style scoped>
.filter-bar {
  padding: 0 16px;
}
</style>
