<script setup lang="ts">
import { computed, provide } from 'vue'
import { NSelect, useMessage } from 'naive-ui'
import { useRoute } from 'vue-router'

import { AppIcons } from '@/icons/appIcons'
import { useObservability } from '@/composables/useObservability'
import { OBS_KEY } from '@/composables/observabilityContext'
import PageShell from '@/components/PageShell.vue'
import RequestDetailModal from '@/pages/observability/RequestDetailModal.vue'

/**
 * 观测中心父容器：
 *   - 持有唯一一份 useObservability（source/tag 过滤器、数据、定时器在此生命周期内）
 *   - PageShell 的 title/description 随当前子路由变化
 *   - 子路由通过 inject(OBS_KEY) 消费状态
 *   - RequestDetailModal 在此挂载，因为它会被多个子页面触发
 */

const message = useMessage()
const obs = useObservability(message)
provide(OBS_KEY, obs)

// 解构一些父容器直接用到的 ref，避免 template 里 .value 与 v-model 的取舍问题
const {
  sourceId,
  sourceOptions,
  tag,
  onTagChange,
  showDetail,
  selectedLog,
  selectedIPLocation,
  selectedSourceLabel,
  selectedSourceUpstream,
} = obs

const route = useRoute()

type RouteMeta = { title?: string; icon?: string; description?: string }

// 只有消费 source/tag 过滤器的子路由显示过滤器；playback/stats/logs 是独立数据源，隐藏过滤器避免误导
const ROUTES_WITH_SOURCE_FILTER = new Set([
  'observability_traffic',
  'observability_redirect',
  'observability_ip_stats',
])
const showSourceFilter = computed(() => {
  const name = typeof route.name === 'string' ? route.name : ''
  return ROUTES_WITH_SOURCE_FILTER.has(name)
})

const pageTitle = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.title || '观测中心'
})

const pageIcon = computed(() => {
  const meta = route.meta as RouteMeta
  const key = meta?.icon
  if (key && key in AppIcons) return AppIcons[key as keyof typeof AppIcons]
  return AppIcons.observability
})

const pageDescription = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.description || '流量分析、播放监控与系统日志。'
})
</script>

<template>
  <page-shell :title="pageTitle" :icon="pageIcon" :description="pageDescription" :divider="false">
    <template v-if="showSourceFilter" #actions>
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

    <router-view v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <component :is="Component" />
      </transition>
    </router-view>
  </page-shell>

  <request-detail-modal
    v-model:show="showDetail"
    :selected-log="selectedLog"
    :ip-location="selectedIPLocation"
    :source-label="selectedSourceLabel"
    :source-upstream="selectedSourceUpstream"
  />
</template>

<style scoped>
.source-select {
  width: 180px;
  margin-right: 8px;
}

.tag-select {
  width: 140px;
}

@media (max-width: 768px) {
  .source-select {
    width: min(45vw, 180px);
  }

  .tag-select {
    width: min(35vw, 140px);
  }
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
