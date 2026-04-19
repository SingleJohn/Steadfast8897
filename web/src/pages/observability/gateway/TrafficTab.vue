<script setup lang="ts">
import { inject } from 'vue'
import {
  NButton,
  NCollapse,
  NCollapseItem,
  NDataTable,
  NDatePicker,
  NDivider,
  NGrid,
  NGridItem,
  NIcon,
  NInput,
  NSelect,
  NSpace,
  NSwitch,
  NText,
} from 'naive-ui'
import { FunnelOutline, RefreshOutline } from '@vicons/ionicons5'

import PageSectionCard from '@/components/PageSectionCard.vue'
import TrafficSummaryCards from './TrafficSummaryCards.vue'
import { GW_OBS_KEY } from '@/composables/observabilityContext'

const obs = inject(GW_OBS_KEY)
if (!obs) throw new Error('TrafficTab must be used within GatewayObsLayout')

// 解构 obs 中的 ref 和函数：解构后在 template 中可自动解包 ref
const {
  isLive,
  status,
  ip,
  pathPrefix,
  keyword,
  range,
  statsSummary,
  logsColumns,
  logsItems,
  logsLoading,
  rowProps,
  logsOffset,
  canNextPage,
  refreshLogs,
  resetFilters,
  nextPage,
} = obs

function onRefresh() { void refreshLogs(true) }
function onSearch() { void refreshLogs(true) }
function onReset() { resetFilters() }
function onNext() { void nextPage() }
</script>

<template>
  <n-space vertical :size="24">
    <traffic-summary-cards :stats-summary="statsSummary" />

    <page-section-card class="log-card" content-style="padding: 0;">
      <div class="filter-bar">
        <n-collapse arrow-placement="right" :default-expanded-names="['filter']">
          <template #header-extra>
            <n-space align="center">
              <n-text depth="3" size="small">实时追踪</n-text>
              <n-switch v-model:value="isLive" size="small" />
              <n-divider vertical />
              <n-button quaternary circle size="small" @click="onRefresh">
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
              </n-button>
            </n-space>
          </template>
          <n-collapse-item title="筛选条件" name="filter">
            <template #header>
              <n-space align="center" :size="8">
                <n-icon><FunnelOutline /></n-icon>
                <span>日志筛选</span>
              </n-space>
            </template>
            <div class="filter-content">
              <n-grid cols="1 s:2 m:4" x-gap="16" y-gap="12" responsive="screen">
                <n-grid-item>
                  <n-select v-model:value="status" clearable placeholder="状态码" :options="[{label:'200',value:200},{label:'302',value:302},{label:'403',value:403},{label:'404',value:404},{label:'500',value:500}]" :disabled="isLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="pathPrefix" placeholder="路径前缀 (/Videos/...)" :disabled="isLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="keyword" placeholder="关键字(路径/Query/UA/Headers/IP)" :disabled="isLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-input v-model:value="ip" placeholder="IP 地址" :disabled="isLive" />
                </n-grid-item>
                <n-grid-item>
                  <n-date-picker v-model:value="range" type="datetimerange" clearable :disabled="isLive" />
                </n-grid-item>
              </n-grid>
              <n-space justify="end" style="margin-top: 16px">
                <n-button size="small" @click="onReset" :disabled="isLive">重置</n-button>
                <n-button type="primary" size="small" @click="onSearch" :disabled="isLive">查询</n-button>
              </n-space>
            </div>
          </n-collapse-item>
        </n-collapse>
      </div>

      <n-divider style="margin: 0" />

      <n-data-table
        :columns="logsColumns"
        :data="logsItems"
        :loading="logsLoading && !isLive"
        :row-props="rowProps"
        :max-height="600"
        :bordered="false"
        size="small"
        class="log-table"
      />

      <div class="pagination-bar">
        <n-text depth="3" size="small">
          {{ isLive ? 'Live Mode' : `Offset: ${logsOffset}` }}
        </n-text>
        <n-button size="small" :disabled="!canNextPage" @click="onNext">加载更多</n-button>
      </div>
    </page-section-card>
  </n-space>
</template>

<style scoped>
.filter-bar {
  padding: 0 16px;
}

.filter-content {
  padding-bottom: 16px;
}

.log-table :deep(.n-data-table-th) {
  background-color: var(--c-slate-100);
  font-weight: 600;
  color: var(--app-text-muted);
}
.app-dark .log-table :deep(.n-data-table-th) {
  background-color: var(--c-slate-900);
}

.log-table :deep(.n-data-table-td) {
  padding-top: 12px;
  padding-bottom: 12px;
}

:deep(.path-cell) {
  font-family: monospace;
  font-size: 13px;
  color: var(--app-text);
}

:deep(.tabular-nums) {
  font-variant-numeric: tabular-nums;
}

.pagination-bar {
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-top: 1px solid var(--app-border);
}
</style>
