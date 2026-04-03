import { requestJson } from '@/api/client'
import type { Config } from '@/types'

export function getConfig() {
  return requestJson<Config>('/Gateway/Config')
}

export function saveConfig(config: Config) {
  return requestJson<{ status: string }>('/Gateway/Config', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(config),
  })
}

export function flushCache(source_id?: string) {
  const search = new URLSearchParams()
  if (source_id) search.set('source_id', source_id)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return requestJson<{ status: string; deleted: number }>(`/Gateway/Cache/Flush${suffix}`, {
    method: 'POST',
  })
}
