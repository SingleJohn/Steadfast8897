<script setup lang="ts">
import { ref, watch } from 'vue'
import { NSelect, NTabs, NTabPane, useMessage } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'

import { AppIcons } from '@/icons/appIcons'
import { useObservability } from '@/composables/useObservability'
import PageShell from '@/components/PageShell.vue'
import IpStatsTab from '@/pages/observability/IpStatsTab.vue'
import RedirectTab from '@/pages/observability/RedirectTab.vue'
import RequestDetailModal from '@/pages/observability/RequestDetailModal.vue'
import TrafficTab from '@/pages/observability/TrafficTab.vue'
import PlaybackTab from '@/pages/observability/PlaybackTab.vue'
import StatsTab from '@/pages/observability/StatsTab.vue'
import SystemLogsTab from '@/pages/observability/SystemLogsTab.vue'

const message = useMessage()
const route = useRoute()
const router = useRouter()

// 批次 1：tab 由路由 meta 驱动（路由切换就是 tab 切换）；保留 query.tab 作 fallback。
// n-tab-pane 的 name 对应的 route：
const tabToRoute: Record<string, string> = {
  traffic: 'observability_traffic',
  redirect302: 'observability_redirect',
  playback: 'observability_playback',
  stats: 'observability_stats',
  logs: 'observability_logs',
}

function pickTabFromRoute(): string {
  const metaTab = (route.meta as { tab?: string })?.tab
  if (metaTab) return metaTab
  const q = route.query.tab
  return typeof q === 'string' && q ? q : 'traffic'
}

const topTab = ref<string>(pickTabFromRoute())

watch(
  () => route.fullPath,
  () => { topTab.value = pickTabFromRoute() },
)

function onTopTabChange(tab: string) {
  topTab.value = tab
  const targetName = tabToRoute[tab]
  if (targetName && route.name !== targetName) {
    void router.push({ name: targetName })
  }
}

const {
  tag,
  sourceId,
  sourceOptions,
  logsColumns,
  logsItems,
  logsLoading,
  logsOffset,
  canNextPage,
  rowProps,
  isLive,
  status,
  ip,
  pathPrefix,
  keyword,
  range,
  statsSummary,
  redirectIsLive,
  redirectBackend,
  redirectUserID,
  redirectUserName,
  redirectIP,
  redirectUAContains,
  redirectPathPrefix,
  redirectLimit,
  redirectRange,
  redirectSummary,
  redirectSummaryError,
  redirectLogsError,
  redirectSummaryLoading,
  redirectLogsLoading,
  redirectLogsItems,
  redirectLogsColumns,
  redirectLogsOffset,
  canNextRedirectPage,
  redirectBackendOptions,
  topRedirectBackends,
  redirectWindowLabel,
  redirectTraceAggLoading,
  redirectTraceAggError,
  redirectTraceRequestStages,
  redirectTraceAttemptStages,
  redirectTraceByBackend,
  redirectTraceAggMeta,
  ipStatsMode,
  ipStatsRange,
  ipStatsError,
  ipStatsLoading,
  ipStatsSummary,
  ipStatsScopeLabel,
  ipStatsRangeLabel,
  ipStatsUseCumulative,
  showDetail,
  selectedLog,
  selectedIPLocation,
  selectedSourceLabel,
  selectedSourceUpstream,
  onTagChange,
  refreshLogs,
  resetFilters,
  nextPage,
  refreshRedirectSummary,
  refreshRedirectLogs,
  refreshRedirectTraceAgg,
  resetRedirectFilters,
  nextRedirectPage,
  refreshIPStats,
} = useObservability(message)
</script>

<template>
  <page-shell
    title="观测中心"
    :icon="AppIcons.observability"
    description="流量分析、播放监控与系统日志。"
    :divider="false"
  >
    <template #actions>
      <n-select
        v-model:value="sourceId"
        :options="sourceOptions"
        size="small"
        class="source-select"
        :disabled="tag === 'admin'"
      />
      <n-select
        :value="tag"
        :options="[
          { label: 'Proxy 流量', value: 'proxy' },
          { label: 'Admin 操作', value: 'admin' },
        ]"
        size="small"
        class="tag-select"
        @update:value="onTagChange"
      />
    </template>

    <n-tabs
      :value="topTab"
      type="segment"
      size="large"
      class="obs-tabs"
      @update:value="onTopTabChange"
    >
      <n-tab-pane name="traffic" tab="流量">
        <traffic-tab
          v-model:is-live="isLive"
          v-model:status="status"
          v-model:ip="ip"
          v-model:path-prefix="pathPrefix"
          v-model:keyword="keyword"
          v-model:range="range"
          :stats-summary="statsSummary"
          :logs-columns="logsColumns"
          :logs-items="logsItems"
          :logs-loading="logsLoading"
          :row-props="rowProps"
          :logs-offset="logsOffset"
          :can-next-page="canNextPage"
          @refresh="refreshLogs(true)"
          @search="refreshLogs(true)"
          @reset="resetFilters"
          @next-page="nextPage"
        />
      </n-tab-pane>

      <n-tab-pane name="redirect302" tab="重定向">
        <redirect-tab
          v-model:redirect-is-live="redirectIsLive"
          v-model:redirect-backend="redirectBackend"
          v-model:redirect-user-id="redirectUserID"
          v-model:redirect-user-name="redirectUserName"
          v-model:redirect-ip="redirectIP"
          v-model:redirect-ua-contains="redirectUAContains"
          v-model:redirect-path-prefix="redirectPathPrefix"
          v-model:redirect-limit="redirectLimit"
          v-model:redirect-range="redirectRange"
          :redirect-summary="redirectSummary"
          :redirect-summary-error="redirectSummaryError"
          :redirect-logs-error="redirectLogsError"
          :redirect-summary-loading="redirectSummaryLoading"
          :redirect-logs-loading="redirectLogsLoading"
          :redirect-logs-items="redirectLogsItems"
          :redirect-logs-columns="redirectLogsColumns"
          :row-props="rowProps"
          :redirect-logs-offset="redirectLogsOffset"
          :can-next-redirect-page="canNextRedirectPage"
          :redirect-backend-options="redirectBackendOptions"
          :top-redirect-backends="topRedirectBackends"
          :redirect-window-label="redirectWindowLabel"
          :redirect-trace-agg-loading="redirectTraceAggLoading"
          :redirect-trace-agg-error="redirectTraceAggError"
          :redirect-trace-request-stages="redirectTraceRequestStages"
          :redirect-trace-attempt-stages="redirectTraceAttemptStages"
          :redirect-trace-by-backend="redirectTraceByBackend"
          :redirect-trace-agg-meta="redirectTraceAggMeta"
          @refresh-all="
            () => {
              refreshRedirectSummary()
              refreshRedirectLogs(true)
              refreshRedirectTraceAgg()
            }
          "
          @search="
            () => {
              refreshRedirectSummary()
              refreshRedirectLogs(true)
              refreshRedirectTraceAgg()
            }
          "
          @reset="resetRedirectFilters"
          @next-page="nextRedirectPage"
        />

        <div style="margin-top: 24px">
          <ip-stats-tab
            v-model:ip-stats-mode="ipStatsMode"
            v-model:ip-stats-range="ipStatsRange"
            :ip-stats-error="ipStatsError"
            :ip-stats-loading="ipStatsLoading"
            :ip-stats-summary="ipStatsSummary"
            :ip-stats-scope-label="ipStatsScopeLabel"
            :ip-stats-range-label="ipStatsRangeLabel"
            :ip-stats-use-cumulative="ipStatsUseCumulative"
            @refresh="refreshIPStats(true)"
          />
        </div>
      </n-tab-pane>

      <n-tab-pane name="playback" tab="播放">
        <playback-tab />
      </n-tab-pane>

      <n-tab-pane name="stats" tab="统计">
        <stats-tab />
      </n-tab-pane>

      <n-tab-pane name="logs" tab="日志">
        <system-logs-tab />
      </n-tab-pane>
    </n-tabs>

    <request-detail-modal
      v-model:show="showDetail"
      :selected-log="selectedLog"
      :ip-location="selectedIPLocation"
      :source-label="selectedSourceLabel"
      :source-upstream="selectedSourceUpstream"
    />
  </page-shell>
</template>

<style scoped>
.source-select {
  width: 180px;
  margin-right: 8px;
}

.tag-select {
  width: 140px;
}

.obs-tabs {
  margin-top: 8px;
}

@media (max-width: 768px) {
  .source-select {
    width: min(45vw, 180px);
  }

  .tag-select {
    width: min(35vw, 140px);
  }
}
</style>
