<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { NCard, NProgress, NTag } from 'naive-ui'

import { useTaskStream } from '@/composables/useTaskStream'
import type { TaskKind, TaskSnapshot, TaskStatus } from '@/api/tasks'

/**
 * OverviewPage 用的作业调度紧凑卡片。
 *  - 连接 SSE 后自动实时刷新
 *  - 运行中作业优先排在上方，显示进度条
 *  - 无任何活动时显示"全部空闲"，整卡仍可点击跳转作业调度
 */

const router = useRouter()
const { tasks, runningCount, connected } = useTaskStream()

const kindLabels: Record<TaskKind, string> = {
  scan: '扫描',
  scrape: '刮削',
  probe: '视频探测',
  backfill: '存量回填',
  update: '自更新',
  cleanup: '媒体库清理',
}

const statusLabels: Record<TaskStatus, string> = {
  idle: '空闲',
  queued: '排队',
  running: '运行中',
  stopping: '停止中',
  succeeded: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

function statusTagType(s: TaskStatus) {
  switch (s) {
    case 'running':
    case 'stopping':
    case 'queued':
      return 'info'
    case 'succeeded':
      return 'success'
    case 'failed':
      return 'error'
    case 'cancelled':
      return 'warning'
    default:
      return 'default'
  }
}

const sorted = computed(() => {
  const order: Record<TaskStatus, number> = {
    running: 0,
    stopping: 1,
    queued: 2,
    failed: 3,
    succeeded: 4,
    cancelled: 5,
    idle: 6,
  }
  return [...tasks.value].sort((a, b) => order[a.status] - order[b.status])
})

const headline = computed(() => {
  if (runningCount.value > 0) return `${runningCount.value} 个作业运行中`
  return '全部空闲'
})

function goTasksTab() {
  router.push({ name: 'obs_svc_tasks' })
}

function brief(t: TaskSnapshot) {
  if (t.stage) return t.stage
  if (t.phase) return t.phase
  if (t.current) return t.current
  return ''
}
</script>

<template>
  <n-card class="section-card task-center-card" title="作业调度" size="small" @click="goTasksTab">
    <template #header-extra>
      <span class="subtle-count">{{ headline }}</span>
      <span v-if="!connected" class="conn-dot" title="未连接实时流"></span>
    </template>

    <ul v-if="sorted.length > 0" class="task-list">
      <li v-for="t in sorted" :key="t.kind" class="task-item">
        <div class="task-head">
          <span class="task-name">{{ kindLabels[t.kind] }}</span>
          <n-tag :type="statusTagType(t.status) as any" size="small" round :bordered="false">
            {{ statusLabels[t.status] }}
          </n-tag>
        </div>
        <div v-if="t.status === 'running' || t.status === 'stopping'" class="task-body">
          <n-progress
            type="line"
            :percentage="t.percent"
            :show-indicator="false"
            class="task-bar"
          />
          <span class="task-pct">{{ t.percent }}%</span>
          <span v-if="t.total > 0" class="task-meta">{{ t.processed }}/{{ t.total }}</span>
        </div>
        <div v-else-if="brief(t)" class="task-body">
          <span class="task-brief">{{ brief(t) }}</span>
        </div>
      </li>
    </ul>
    <div v-else class="empty-state">全部空闲</div>
  </n-card>
</template>

<style scoped>
.task-center-card {
  cursor: pointer;
}
.task-center-card:hover {
  border-color: var(--app-primary, #3b82f6);
}

.task-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.task-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.task-head {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: space-between;
}
.task-name {
  font-weight: 600;
  font-size: 13px;
}
.task-body {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--n-text-color-2, #64748b);
}
.task-bar {
  flex: 1;
  min-width: 60px;
}
.task-pct {
  min-width: 36px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.task-meta {
  font-variant-numeric: tabular-nums;
}
.task-brief {
  color: var(--n-text-color-3, #94a3b8);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.subtle-count {
  font-size: 12px;
  color: var(--n-text-color-3, #94a3b8);
}
.conn-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--app-warning, #f59e0b);
  margin-left: 6px;
}
.empty-state {
  text-align: center;
  padding: 16px 0;
  color: var(--n-text-color-3, #94a3b8);
  font-size: 13px;
}
</style>
