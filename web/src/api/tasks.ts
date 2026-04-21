// 作业调度 API 封装。后端契约见 internal/services/taskcenter/task.go。
import { requestJson } from './client'

export type TaskKind = 'scan' | 'scrape' | 'probe' | 'backfill' | 'update' | 'cleanup'

export type TaskStatus =
  | 'idle'
  | 'queued'
  | 'running'
  | 'stopping'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export type TaskTrigger = 'manual' | 'auto' | 'startup' | 'chain'

export interface TaskSnapshot {
  kind: TaskKind
  runId?: number
  status: TaskStatus
  stage?: string
  phase?: string
  total: number
  processed: number
  success?: number
  failed?: number
  percent: number
  current?: string
  counters?: Record<string, number>
  message?: string
  error?: string
  startedAt?: number
  completedAt?: number
  cancellable: boolean
  children?: TaskSnapshot[]
}

export interface TaskRunRow {
  id: number
  kind: TaskKind
  stage?: string
  parentId?: number
  status: TaskStatus
  trigger: TaskTrigger
  total: number
  processed: number
  success: number
  failed: number
  counters?: Record<string, number>
  message?: string
  error?: string
  payload?: Record<string, unknown>
  startedAt: number
  completedAt?: number
  durationMs?: number
}

export async function listTasks() {
  return requestJson<{ items: TaskSnapshot[] }>('/Tasks')
}

export async function getTask(kind: TaskKind) {
  return requestJson<TaskSnapshot>(`/Tasks/${kind}`)
}

export interface TaskHistoryQuery {
  kind?: TaskKind
  parentId?: number | null // null 表示只查顶层
  limit?: number
}

export async function listTaskHistory(q: TaskHistoryQuery = {}) {
  const params = new URLSearchParams()
  if (q.kind) params.set('kind', q.kind)
  if (q.parentId != null) params.set('parent_id', String(q.parentId))
  if (q.limit != null) params.set('limit', String(q.limit))
  const qs = params.toString()
  return requestJson<{ items: TaskRunRow[] }>(`/Tasks/history${qs ? '?' + qs : ''}`)
}

// 作业调度 SSE URL。EventSource 不支持自定义 header，token 通过 query 传。
export function taskStreamUrl(token: string | null): string {
  const base = '/Tasks/stream'
  return token ? `${base}?api_key=${encodeURIComponent(token)}` : base
}

export interface StartTaskResult {
  runId: number
  snapshot: TaskSnapshot
}

export async function startTask(kind: TaskKind, params: Record<string, unknown> = {}) {
  return requestJson<StartTaskResult>(`/Tasks/${kind}/start`, {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

export async function stopTask(kind: TaskKind) {
  return requestJson<{ snapshot: TaskSnapshot }>(`/Tasks/${kind}/stop`, {
    method: 'POST',
  })
}

// ───── 任务链 ─────

export interface ChainRule {
  upstream: TaskKind
  status: TaskStatus
  target: TaskKind
  params?: Record<string, unknown>
}

export interface ChainConfig {
  enabled: boolean
  rules: ChainRule[]
}

export async function getTaskChain() {
  return requestJson<ChainConfig>('/Tasks/chain')
}

export async function updateTaskChain(patch: Partial<ChainConfig>) {
  return requestJson<ChainConfig>('/Tasks/chain', {
    method: 'POST',
    body: JSON.stringify(patch),
  })
}
