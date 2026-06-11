<script setup lang="ts">
import { computed, provide } from 'vue'
import { NSelect, useMessage } from 'naive-ui'
import { useRoute } from 'vue-router'

import { AppIcons } from '@/icons/appIcons'
import { useGatewayObservability } from '@/composables/useGatewayObservability'
import { GW_OBS_KEY } from '@/composables/observabilityContext'
import PageShell from '@/components/PageShell.vue'
import RequestDetailModal from './RequestDetailModal.vue'

/**
 * 网关观测父容器：
 *   - 持有唯一一份 useGatewayObservability（source/tag 过滤器、数据、定时器）
 *   - 三个子路由（traffic / redirect / ip-stats）共享 source/tag 过滤器
 *   - RequestDetailModal 在此挂载（所有网关子页共用）
 */

const message = useMessage()
const obs = useGatewayObservability(message)
provide(GW_OBS_KEY, obs)

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

const pageTitle = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.title || '网关观测'
})

const pageIcon = computed(() => {
  const meta = route.meta as RouteMeta
  const key = meta?.icon
  if (key && key in AppIcons) return AppIcons[key as keyof typeof AppIcons]
  return AppIcons.observability
})

const pageDescription = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.description || '网关流量、重定向与 IP 来源分析。'
})
</script>

<template>
  <page-shell :title="pageTitle" :icon="pageIcon" :description="pageDescription" :divider="false">
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

    <router-view v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <suspense>
          <component :is="Component" />
        </suspense>
      </transition>
    </router-view>

    <!-- NModal teleport 到 body，放在 page-shell 内仅为保证本组件单根节点：
         多根节点会让父级 <transition mode="out-in"> 的 leave 回调无法结算，
         离开本页后下一个路由组件不挂载 → 白屏（需整页刷新）。 -->
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
