import { requestJson } from '@/api/client'
import type { IPInfoLite, RedirectSummary, RequestLog } from '@/types'

export interface RedirectSummaryParams {
  source_id?: string
  since?: string
  until?: string
  limit?: number
  with_ip_info?: boolean
}

interface GoRedirectSummary {
  total: number
  by_backend: Record<string, number>
  top_users: Array<{ key: string; count: number }>
  top_ips: Array<{ key: string; count: number }>
}

export async function getRedirectSummary(params: RedirectSummaryParams = {}): Promise<RedirectSummary> {
  const search = new URLSearchParams()
  if (params.source_id) search.set('source_id', params.source_id)

  let hours = 24
  if (params.since && params.until) {
    const sinceMs = new Date(params.since).getTime()
    const untilMs = new Date(params.until).getTime()
    if (sinceMs && untilMs) {
      hours = Math.max(1, Math.ceil((untilMs - sinceMs) / 3_600_000))
    }
  } else if (params.since) {
    const sinceMs = new Date(params.since).getTime()
    if (sinceMs) {
      hours = Math.max(1, Math.ceil((Date.now() - sinceMs) / 3_600_000))
    }
  }
  search.set('hours', String(hours))

  const raw = await requestJson<GoRedirectSummary | null>(`/Gateway/Redirects/Summary?${search.toString()}`)

  const now = new Date()
  const since = new Date(now.getTime() - hours * 3_600_000)

  return {
    since: since.toISOString(),
    until: now.toISOString(),
    total_302: raw?.total || 0,
    by_backend: Object.entries(raw?.by_backend || {}).map(([backend, count]) => ({ backend, count })),
    top_users: (raw?.top_users || []).map((u) => ({
      emby_user_id: u.key,
      emby_user_name: u.key,
      count: u.count,
    })),
    top_user_backend: [],
    top_ips: (raw?.top_ips || []).map((ip) => ({
      client_ip: ip.key,
      count: ip.count,
    })),
    top_uas: [],
  }
}

export interface ListRedirectLogsParams {
  source_id?: string
  user_id?: string
  user_name?: string
  backend?: string
  ip?: string
  ua_contains?: string
  path_prefix?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
  with_ip_info?: boolean
}

export interface ListRedirectLogsResponse {
  items: RequestLog[]
  next_offset: number
  ip_infos?: Record<string, IPInfoLite>
}

export async function listRedirectLogs(params: ListRedirectLogsParams): Promise<ListRedirectLogsResponse> {
  const search = new URLSearchParams()
  if (params.source_id) search.set('source_id', params.source_id)
  if (typeof params.limit === 'number') search.set('limit', String(params.limit))
  if (typeof params.offset === 'number') search.set('offset', String(params.offset))

  const raw = await requestJson<{ items: RequestLog[]; total: number } | null>(`/Gateway/Redirects/Logs?${search.toString()}`)

  const items = Array.isArray(raw?.items) ? raw.items : []
  const offset = typeof params.offset === 'number' ? params.offset : 0
  const limit = typeof params.limit === 'number' ? params.limit : 50

  return {
    items,
    next_offset: items.length >= limit ? offset + items.length : offset,
  }
}

export interface RedirectTraceAggMetricRow {
  stage: string
  count: number
  avg_ms: number
  p50_ms: number
  p95_ms: number
  max_ms: number
}

export interface RedirectTraceAggBackendRow {
  backend: string
  count: number
  success: number
  success_rate: number
  avg_ms: number
  p50_ms: number
  p95_ms: number
  max_ms: number
}

export interface RedirectTraceAggResponse {
  source_id: string
  backend: string
  since: string
  until: string
  sample_limit: number
  sampled: number
  parsed: number
  skipped: number
  request_stages: RedirectTraceAggMetricRow[]
  attempt_stages: RedirectTraceAggMetricRow[]
  by_backend: RedirectTraceAggBackendRow[]
}

export interface RedirectTraceAggParams {
  source_id?: string
  backend?: string
  since?: string
  until?: string
  limit?: number
}

export async function getRedirectTraceAgg(_params: RedirectTraceAggParams = {}): Promise<RedirectTraceAggResponse> {
  return {
    source_id: '',
    backend: '',
    since: '',
    until: '',
    sample_limit: 0,
    sampled: 0,
    parsed: 0,
    skipped: 0,
    request_stages: [],
    attempt_stages: [],
    by_backend: [],
  }
}
