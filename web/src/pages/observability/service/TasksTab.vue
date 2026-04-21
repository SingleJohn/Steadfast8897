<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NProgress,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  type DataTableColumns,
} from 'naive-ui'

import { useTaskStream } from '@/composables/useTaskStream'
import { useToast } from '@/composables/useToast'
import {
  getTaskChain,
  listTaskHistory,
  startTask,
  stopTask,
  updateTaskChain,
  type ChainConfig,
  type TaskKind,
  type TaskRunRow,
  type TaskSnapshot,
  type TaskStatus,
} from '@/api/tasks'

/**
 * 观测中心 · 作业调度 Tab
 *   - 顶部：实时 SSE 驱动的作业卡片（状态/进度/最近一次运行）
 *   - 底部：task_runs 历史表（按 kind 过滤）
 *
 * 刮削不在这里——它由 scrape_queue + ScrapeWorker 持续驱动，入口在"队列管道"Tab。
 */

const { tasks, connected, lastError } = useTaskStream()
const { showToast } = useToast()
const busy = ref<Partial<Record<TaskKind, boolean>>>({})

// 哪些作业允许在作业调度面板直接 Start：
//   - scan 由库扫描路径触发，不支持
//   - scrape 不再作为一等作业（由 scrape_queue + ScrapeWorker 持续驱动，
//     全库触发入口在"观测中心 > 队列管道"面板的"刮削全部缺失元数据"按钮）
//   - update 的 apply 动作需要先做 backup（由 OverviewPage 专门处理），这里只暴露 check
const startKinds: TaskKind[] = ['probe', 'backfill', 'update']

function canStart(kind: TaskKind, status: TaskStatus): boolean {
  if (!startKinds.includes(kind)) return false
  return status !== 'running' && status !== 'stopping' && status !== 'queued'
}

function startLabel(kind: TaskKind): string {
  if (kind === 'update') return '检查更新'
  return '启动'
}

async function handleStart(t: TaskSnapshot) {
  busy.value[t.kind] = true
  try {
    const params: Record<string, unknown> = {}
    if (t.kind === 'update') params.action = 'check'
    await startTask(t.kind, params)
    showToast('已启动', 'success')
  } catch (e) {
    showToast((e as Error).message, 'error')
  } finally {
    busy.value[t.kind] = false
  }
}

async function handleStop(t: TaskSnapshot) {
  busy.value[t.kind] = true
  try {
    await stopTask(t.kind)
    showToast('已请求停止', 'info')
  } catch (e) {
    showToast((e as Error).message, 'error')
  } finally {
    busy.value[t.kind] = false
  }
}

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

function statusType(s: TaskStatus) {
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

function formatTime(ms?: number) {
  if (!ms) return '-'
  const d = new Date(ms)
  return d.toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(ms?: number) {
  if (!ms || ms < 0) return '-'
  const s = Math.floor(ms / 1000)
  if (s < 60) return s + 's'
  const m = Math.floor(s / 60)
  if (m < 60) return m + 'm ' + (s % 60) + 's'
  const h = Math.floor(m / 60)
  return h + 'h ' + (m % 60) + 'm'
}

function countersText(c?: Record<string, number>) {
  if (!c) return ''
  const parts: string[] = []
  for (const [k, v] of Object.entries(c)) {
    if (v == null || v === 0) continue
    parts.push(`${k}=${v}`)
  }
  return parts.join(' · ')
}

// ───────────── 历史表 ─────────────

const historyKind = ref<TaskKind | ''>('')
const historyRows = ref<TaskRunRow[]>([])
const historyLoading = ref(false)

const kindOptions = [
  { label: '全部', value: '' },
  ...(Object.keys(kindLabels) as TaskKind[]).map((k) => ({ label: kindLabels[k], value: k })),
]

async function loadHistory() {
  historyLoading.value = true
  try {
    const res = await listTaskHistory({
      kind: (historyKind.value || undefined) as TaskKind | undefined,
      limit: 100,
    })
    historyRows.value = res.items ?? []
  } catch {
    historyRows.value = []
  } finally {
    historyLoading.value = false
  }
}

const historyColumns = computed<DataTableColumns<TaskRunRow>>(() => [
  { title: 'ID', key: 'id', width: 72 },
  {
    title: '任务',
    key: 'kind',
    width: 110,
    render: (r) => {
      const label = kindLabels[r.kind] ?? r.kind
      return r.stage ? `${label} · ${r.stage}` : label
    },
  },
  {
    title: '状态',
    key: 'status',
    width: 96,
    render: (r) =>
      h(
        NTag,
        { type: statusType(r.status) as any, size: 'small', bordered: false, round: true },
        { default: () => statusLabels[r.status] ?? r.status },
      ),
  },
  { title: '触发', key: 'trigger', width: 80 },
  {
    title: '进度',
    key: 'processed',
    width: 140,
    render: (r) => {
      if (r.total > 0) return `${r.processed} / ${r.total}`
      return String(r.processed)
    },
  },
  {
    title: '成功/失败',
    key: 'success',
    width: 110,
    render: (r) => `${r.success} / ${r.failed}`,
  },
  {
    title: '开始时间',
    key: 'startedAt',
    width: 168,
    render: (r) => formatTime(r.startedAt),
  },
  {
    title: '耗时',
    key: 'durationMs',
    width: 88,
    render: (r) => formatDuration(r.durationMs),
  },
  {
    title: '错误',
    key: 'error',
    ellipsis: { tooltip: true },
    render: (r) => r.error || r.message || '-',
  },
])

// ───────────── 任务链 ─────────────

const chain = ref<ChainConfig | null>(null)
const chainBusy = ref(false)

async function loadChain() {
  try {
    chain.value = await getTaskChain()
  } catch (e) {
    showToast((e as Error).message, 'error')
  }
}

async function toggleChain(enabled: boolean) {
  chainBusy.value = true
  try {
    chain.value = await updateTaskChain({ enabled })
    showToast(enabled ? '任务链已启用' : '任务链已停用', 'success')
  } catch (e) {
    showToast((e as Error).message, 'error')
    await loadChain() // 回滚到服务端真实状态
  } finally {
    chainBusy.value = false
  }
}

function describeRule(r: ChainConfig['rules'][number]): string {
  const u = kindLabels[r.upstream] ?? r.upstream
  const t = kindLabels[r.target] ?? r.target
  const suffix = r.params ? ` (${Object.entries(r.params).map(([k, v]) => `${k}=${JSON.stringify(v)}`).join(', ')})` : ''
  return `${u} ${statusLabels[r.status] ?? r.status} → ${t}${suffix}`
}

onMounted(() => {
  loadHistory()
  loadChain()
})

function brief(t: TaskSnapshot) {
  if (t.status === 'running' && t.stage) return `阶段 ${t.stage}`
  if (t.current) return t.current
  if (t.phase) return t.phase
  return ''
}
</script>

<template>
  <n-space vertical :size="16">
    <n-alert v-if="lastError && !connected" type="warning" :show-icon="false">
      实时连接已断开：{{ lastError }}，正在尝试重连。
    </n-alert>

    <n-card v-if="chain" size="small" title="任务链">
      <template #header-extra>
        <n-space :size="8" align="center">
          <span class="dimmed">扫描 → 探测 → 封面回填</span>
          <n-switch
            :value="chain.enabled"
            :loading="chainBusy"
            @update:value="toggleChain"
          />
        </n-space>
      </template>
      <ul class="chain-rules">
        <li v-for="(r, i) in chain.rules" :key="i" class="chain-rule">
          <span class="chain-index">{{ i + 1 }}</span>
          <span class="chain-body">{{ describeRule(r) }}</span>
        </li>
      </ul>
    </n-card>

    <div class="task-grid">
      <n-card
        v-for="t in tasks"
        :key="t.kind"
        size="small"
        class="task-card"
        :title="kindLabels[t.kind]"
      >
        <template #header-extra>
          <n-tag :type="statusType(t.status) as any" size="small" bordered>
            {{ statusLabels[t.status] }}
          </n-tag>
        </template>

        <div class="card-actions">
          <n-button
            v-if="canStart(t.kind, t.status)"
            size="tiny"
            type="primary"
            secondary
            :loading="busy[t.kind]"
            @click="handleStart(t)"
          >
            {{ startLabel(t.kind) }}
          </n-button>
          <n-button
            v-if="t.cancellable"
            size="tiny"
            type="warning"
            secondary
            :loading="busy[t.kind]"
            @click="handleStop(t)"
          >
            停止
          </n-button>
        </div>

        <div v-if="t.status === 'running' || t.status === 'stopping'" class="progress-row">
          <n-progress type="line" :percentage="t.percent" :height="10" />
          <div class="progress-meta">
            <span>{{ t.processed }} / {{ t.total || '?' }}</span>
            <span v-if="(t.success ?? 0) + (t.failed ?? 0) > 0" class="dimmed">
              成 {{ t.success ?? 0 }} · 败 {{ t.failed ?? 0 }}
            </span>
          </div>
        </div>

        <div class="task-meta">
          <div v-if="brief(t)" class="task-brief">{{ brief(t) }}</div>
          <div v-if="t.error" class="task-error">{{ t.error }}</div>
          <div v-if="t.startedAt" class="dimmed">开始：{{ formatTime(t.startedAt) }}</div>
          <div v-if="t.completedAt" class="dimmed">结束：{{ formatTime(t.completedAt) }}</div>
          <div v-if="countersText(t.counters)" class="dimmed">{{ countersText(t.counters) }}</div>
        </div>
      </n-card>
    </div>

    <n-card size="small" title="历史运行">
      <template #header-extra>
        <n-space :size="8">
          <n-select
            v-model:value="historyKind"
            :options="kindOptions"
            size="small"
            style="width: 140px"
            @update:value="loadHistory"
          />
          <n-button size="small" :loading="historyLoading" @click="loadHistory">刷新</n-button>
        </n-space>
      </template>

      <n-empty v-if="!historyLoading && historyRows.length === 0" description="暂无运行记录" />
      <n-data-table
        v-else
        :columns="historyColumns"
        :data="historyRows"
        :loading="historyLoading"
        :bordered="false"
        size="small"
        :row-key="(row: TaskRunRow) => row.id"
      />
    </n-card>
  </n-space>
</template>

<style scoped>
.task-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}
.card-actions {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}
.task-card .progress-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 8px;
}
.progress-meta {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--n-text-color-2, #64748b);
}
.task-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
}
.task-brief {
  color: var(--n-text-color-2, #64748b);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.task-error {
  color: var(--app-error, #ef4444);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dimmed {
  color: var(--n-text-color-3, #94a3b8);
}
.chain-rules {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.chain-rule {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
}
.chain-index {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--n-color-target, #eef2f7);
  font-size: 12px;
  font-weight: 600;
  color: var(--n-text-color-2, #64748b);
}
.chain-body {
  color: var(--n-text-color-1, #1e293b);
  font-family: var(--n-font-family-mono, ui-monospace, SFMono-Regular, monospace);
}
</style>
