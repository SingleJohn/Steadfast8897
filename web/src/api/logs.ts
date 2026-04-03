import { requestJson } from '@/api/client'
import type { IPInfoLite, RequestLog } from '@/types'

export interface ListLogsParams {
  tag?: string
  source_id?: string
  status?: number
  ip?: string
  path_prefix?: string
  q?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
  with_ip_info?: boolean
}

export interface ListLogsResponse {
  items: RequestLog[]
  next_offset: number
  warnings?: string[]
  ip_infos?: Record<string, IPInfoLite>
}

export async function listLogs(params: ListLogsParams): Promise<ListLogsResponse> {
  const search = new URLSearchParams()
  if (params.tag) search.set('tag', params.tag)
  if (params.source_id) search.set('source_id', params.source_id)
  if (typeof params.status === 'number') search.set('status', String(params.status))
  if (typeof params.limit === 'number') search.set('limit', String(params.limit))
  if (typeof params.offset === 'number') search.set('offset', String(params.offset))

  const raw = await requestJson<{ items: RequestLog[]; total: number } | null>(`/Gateway/Logs?${search.toString()}`)

  const items = Array.isArray(raw?.items) ? raw.items : []
  const offset = typeof params.offset === 'number' ? params.offset : 0
  const limit = typeof params.limit === 'number' ? params.limit : 50

  return {
    items,
    next_offset: items.length >= limit ? offset + items.length : offset,
  }
}
