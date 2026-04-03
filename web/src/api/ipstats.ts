import { requestJson } from '@/api/client'
import type { IPStatsSummary } from '@/types'

export interface IPStatsSummaryParams {
  tag?: string
  mode?: 'all' | 'redirect302' | string
  source_id?: string
  since?: string
  until?: string
  limit?: number
  scope?: string
}

const EMPTY_SUMMARY: IPStatsSummary = {
  tag: 'proxy',
  mode: 'all',
  source_id: '',
  since: '',
  until: '',
  total: 0,
  pending_enrich: 0,
  top_ips: [],
  by_country: [],
  by_prov: [],
  by_city: [],
  by_area: [],
  by_big_area: [],
  by_isp: [],
  by_ip_type: [],
}

export async function getIPStatsSummary(params: IPStatsSummaryParams = {}): Promise<IPStatsSummary> {
  try {
    const search = new URLSearchParams()
    if (params.tag) search.set('tag', params.tag)
    if (params.mode) search.set('mode', params.mode)
    if (params.source_id) search.set('source_id', params.source_id)
    if (params.since) search.set('since', params.since)
    if (params.until) search.set('until', params.until)
    if (typeof params.limit === 'number') search.set('limit', String(params.limit))
    if (params.scope) search.set('scope', params.scope)
    return await requestJson<IPStatsSummary>(`/Gateway/IPStats/Summary?${search.toString()}`)
  } catch {
    return EMPTY_SUMMARY
  }
}
