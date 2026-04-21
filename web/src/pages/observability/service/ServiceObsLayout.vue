<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import { AppIcons } from '@/icons/appIcons'
import PageShell from '@/components/PageShell.vue'

/**
 * 服务观测父容器：
 *   - 4 个子路由（playback / stats / logs / tasks）各自独立数据源，本容器不持有状态
 *   - 仅提供统一的 PageShell 外观与路由切换动画
 */

const route = useRoute()

type RouteMeta = { title?: string; icon?: string; description?: string }

const pageTitle = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.title || '服务观测'
})

const pageIcon = computed(() => {
  const meta = route.meta as RouteMeta
  const key = meta?.icon
  if (key && key in AppIcons) return AppIcons[key as keyof typeof AppIcons]
  return AppIcons.observability
})

const pageDescription = computed(() => {
  const meta = route.meta as RouteMeta
  return meta?.description || '播放会话、统计、系统日志与作业调度、队列管道。'
})
</script>

<template>
  <page-shell :title="pageTitle" :icon="pageIcon" :description="pageDescription" :divider="false">
    <router-view v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <component :is="Component" />
      </transition>
    </router-view>
  </page-shell>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
