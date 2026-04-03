import { requestJson } from '@/api/client'

export interface EmbyHealthItem {
  source_id: string
  source_name: string
  upstream_host: string
  ok: boolean
  status: number
  latency_ms: number
  checked_at: string
  error?: string
}

export interface EmbyHealthResponse {
  items: EmbyHealthItem[]
}

export function getEmbyHealth() {
  return requestJson<EmbyHealthResponse>('/Gateway/Health/Emby')
}

export function checkEmbySource(source_id: string) {
  const search = new URLSearchParams()
  search.set('source_id', source_id)
  return requestJson<EmbyHealthItem>(`/Gateway/Emby/Check?${search.toString()}`, { method: 'POST' })
}
