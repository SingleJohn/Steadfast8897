import { requestJson } from '@/api/client'
import type { DailyStat } from '@/types'

export interface DailyStatsResponse {
  items: DailyStat[]
}

export async function getDailyStats(tag: string, days: number, source_id?: string): Promise<DailyStatsResponse> {
  const search = new URLSearchParams()
  search.set('tag', tag)
  search.set('days', String(days))
  if (source_id) search.set('source_id', source_id)

  const raw = await requestJson<DailyStat[] | { items: DailyStat[] } | null>(`/Gateway/Stats/Daily?${search.toString()}`)

  if (!raw) return { items: [] }
  if (Array.isArray(raw)) return { items: raw }
  return { items: Array.isArray(raw.items) ? raw.items : [] }
}
