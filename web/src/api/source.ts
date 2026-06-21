import { requestJson } from './client'

export type SourceConfig = {
  ID: number
  Name: string
  SourceURL?: string
  ImportStatus: string
  Enabled: boolean
  ImportedAt: string
  UpdatedAt: string
}

export type SourceProvider = {
  ID: number
  ConfigID?: number
  SourceKey: string
  Name: string
  ProviderKind: string
  RuntimeKind: string
  API: string
  Enabled: boolean
  Visible: boolean
  Searchable: boolean
  HealthStatus: string
  LastCheckAt?: string
  LastError?: string
  Categories?: unknown[]
}

export type SourceView = {
  Id: number
  PublicUUID: string
  Name: string
  DisplayName: string
  CustomName?: string
  Dimension: string
  MatchValue: string
  MatchValues: string[]
  CollectionType: string
  ProviderIds: number[]
  Enabled: boolean
  ExposeToEmby: boolean
  SortOrder: number
  ItemCount: number
  HasCover: boolean
  CoverUrl?: string
}

export type DimensionValue = {
  Value: string
  Count: number
  AlreadyAdded: boolean
}

export type ImportTVBoxResult = {
  config: SourceConfig
  providers: SourceProvider[]
  accepted: number
  skipped: number
}

export type ProviderPage = {
  page: number
  page_count: number
  total: number
  items: Array<Record<string, unknown>>
}

export async function importTVBoxConfig(payload: { name?: string; source_url?: string; raw_json?: string }) {
  return requestJson<ImportTVBoxResult>('/SourceConfigs/ImportTVBox', {
    method: 'POST',
    body: JSON.stringify(payload),
    timeoutMs: 120_000,
  })
}

export async function listSourceConfigs() {
  const res = await requestJson<{ items: SourceConfig[] }>('/SourceConfigs')
  return res.items || []
}

export async function setSourceConfigEnabled(id: number, enabled: boolean) {
  return requestJson<SourceConfig>(`/SourceConfigs/${id}/${enabled ? 'Enable' : 'Disable'}`, { method: 'POST' })
}

export async function listSourceProviders() {
  const res = await requestJson<{ items: SourceProvider[] }>('/SourceProviders')
  return res.items || []
}

export async function setSourceProviderEnabled(id: number, enabled: boolean) {
  return requestJson<SourceProvider>(`/SourceProviders/${id}/${enabled ? 'Enable' : 'Disable'}`, { method: 'POST' })
}

export async function healthCheckSourceProvider(id: number) {
  return requestJson<SourceProvider>(`/SourceProviders/${id}/HealthCheck`, { method: 'POST', timeoutMs: 120_000 })
}

export async function searchSourceProvider(id: number, keyword: string, page = 1) {
  return requestJson<{ page: ProviderPage; items: unknown[] }>(`/SourceProviders/${id}/Search`, {
    method: 'POST',
    body: JSON.stringify({ keyword, page }),
    timeoutMs: 120_000,
  })
}

export async function listSourceProviderCategories(id: number) {
  const res = await requestJson<{ items: Array<{ id: string; name: string }> }>(`/SourceProviders/${id}/Categories`, {
    timeoutMs: 120_000,
  })
  return res.items || []
}

export async function listSourceViews() {
  const res = await requestJson<{ items: SourceView[] }>('/Library/SourceViews')
  return res.items || []
}

export async function discoverSourceViewValues(dimension: string, search = '', minCount = 1) {
  const params = new URLSearchParams({ dimension, search, minCount: String(minCount) })
  const res = await requestJson<{ values: DimensionValue[] }>(`/Library/SourceViews/Discover?${params.toString()}`)
  return res.values || []
}

export async function createSourceView(payload: Record<string, unknown>) {
  return requestJson<SourceView>('/Library/SourceViews', { method: 'POST', body: JSON.stringify(payload) })
}

export async function updateSourceView(id: number, payload: Record<string, unknown>) {
  return requestJson<SourceView>(`/Library/SourceViews/${id}`, { method: 'PUT', body: JSON.stringify(payload) })
}

export async function deleteSourceView(id: number) {
  return requestJson<void>(`/Library/SourceViews/${id}`, { method: 'DELETE' })
}

export async function renameSourceView(id: number, name: string) {
  return requestJson<SourceView>(`/Library/SourceViews/${id}/Rename`, {
    method: 'POST',
    body: JSON.stringify({ Name: name }),
  })
}

export async function generateSourceViewCover(id: number, style: string, options?: Record<string, unknown>) {
  return requestJson<{ ImageTag: string; Style: string }>(`/Library/SourceViews/${id}/Image/Generate`, {
    method: 'POST',
    body: JSON.stringify({ Style: style, Options: options }),
    timeoutMs: 120_000,
  })
}

export async function deleteSourceViewCover(id: number) {
  return requestJson<void>(`/Library/SourceViews/${id}/Image`, { method: 'DELETE' })
}

export async function updateSourceViewDisplayOrder(ids: number[]) {
  return requestJson<void>('/Library/SourceViews/DisplayOrder', {
    method: 'POST',
    body: JSON.stringify({ OrderedIds: ids }),
  })
}
