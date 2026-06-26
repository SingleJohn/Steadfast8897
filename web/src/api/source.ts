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

export type SourceConfigImpactLibraryView = {
  ID: number
  Name: string
  DisplayName?: string
  ProviderIDs: number[]
  RemovedProviderIDs: number[]
}

export type SourceConfigImpact = {
  ConfigID: number
  ProviderCount: number
  ParserCount: number
  SourceItemCount: number
  PlaySourceCount: number
  RuntimeArtifactCount: number
  RuntimeInvocationCount: number
  AffectedLibraryViewCount: number
  AffectedLibraryViews: SourceConfigImpactLibraryView[]
  ProviderIDs: number[]
  RuntimeInvocationsRetained: boolean
}

export type SourceConfigDeleteResult = {
  Config: SourceConfig
  Impact: SourceConfigImpact
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
  Health?: SourceProviderHealthSummary
}

export type SourceProviderBatchHealthResult = {
  provider_id: number
  provider_name: string
  status: string
  runtime_status?: string
  home_status?: string
  category_status?: string
  search_status?: string
  play_ready_status?: string
  error_type?: string
  message?: string
  latency_ms: number
  categories_count: number
}

export type SourceProviderHealthMethodSummary = {
  status: string
  error_type?: string
  message?: string
  categories_count?: number
  filters_count?: number
  items_count?: number
  latency_ms?: number
}

export type SourceProviderHealthSummary = {
  runtime_status?: string
  home_status?: string
  category_status?: string
  search_status?: string
  play_ready_status?: string
  overall_status?: string
  message?: string
  checked_at?: string
  home?: SourceProviderHealthMethodSummary
  category?: SourceProviderHealthMethodSummary
  search?: SourceProviderHealthMethodSummary
}

export type SourceProviderListOptions = {
  health_status?: string
  runtime_status?: string
  home_status?: string
  category_status?: string
  runtime_kind?: string
  provider_kind?: string
  keyword?: string
}

export type SourceProviderDiagnoseMethod = {
  method: string
  status: 'ok' | 'empty' | 'error' | 'unsupported' | 'skipped'
  error_type?: string
  message?: string
  latency_ms: number
  categories_count: number
  filters_count: number
  items_count: number
  sample_items?: Array<{
    source_item_id?: string
    title?: string
    item_type?: string
    year?: number
    poster_hash?: string
    remarks?: string
  }>
  metrics?: Record<string, unknown>
}

export type SourceProviderDiagnoseResult = {
  provider_id: number
  provider_name: string
  source_key: string
  runtime_kind: string
  provider_kind: string
  overall_status: string
  results: SourceProviderDiagnoseMethod[]
  duration_ms: number
}

export type SourceProviderDiagnosePayload = {
  methods?: string[]
  category_id?: string
  keyword?: string
  source_item_id?: string
  detail_id?: string
  timeout_ms?: number
}

export type SourceProviderHomeProfileSlice = {
  method: string
  status: 'ok' | 'empty' | 'error' | 'unsupported' | 'skipped'
  ok: boolean
  error_type?: string
  error_message?: string
  categories_count: number
  filters_count: number
  items_count: number
  duration_ms: number
}

export type SourceProviderHomeProfile = {
  provider_id: number
  runtime_kind: string
  categories: Array<{ id: string; name: string }>
  filters?: unknown
  filters_count: number
  home_items: Array<{
    source_item_id?: string
    title?: string
    item_type?: string
    year?: number
    poster_url?: string
    remarks?: string
  }>
  home_item_source: string
  sources: {
    home_content: SourceProviderHomeProfileSlice
    home_video_content: SourceProviderHomeProfileSlice
  }
}

export type SourceProviderDeleteImpact = Omit<SourceConfigImpact, 'ConfigID' | 'ParserCount'>

export type SourceProviderDeleteResult = {
  items: SourceProvider[]
  impact: SourceProviderDeleteImpact
  count: number
}

export type SourceParser = {
  ID: number
  ConfigID?: number
  SourceType: string
  Name: string
  ParserType: number
  URL: string
  BaseURL?: string
  TimeoutMS: number
  Enabled: boolean
  TrustStatus: string
  Status: string
  LastCheckAt?: string
  LastError?: string
  UpdatedAt: string
}

export type SourceRuntimeInvocation = {
  ID: number
  ProviderID?: number
  RuntimeKind: string
  Method: string
  Status: string
  ErrorType?: string
  ErrorMessage?: string
  DurationMS: number
  EngineOK?: boolean
  WorkerPID?: number
  ArtifactIDs: number[]
  URLHash?: string
  InvokedAt: string
}

export type SourceRuntimeArtifact = {
  ID: number
  ProviderID?: number
  SourceType: string
  ArtifactKind: string
  Name: string
  SourceURL: string
  MD5: string
  SHA256: string
  ByteSize: number
  TrustStatus: string
  Status: string
  LastFetchedAt: string
  LastError?: string
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

export type SourceViewPreviewProvider = {
  provider_id: number
  provider_name: string
  source_key: string
  health_status: string
  item_count: number
}

export type SourceViewPreviewItem = {
  public_uuid: string
  provider_id: number
  provider_name: string
  title: string
  item_type: string
  year?: number
  normalized_kind?: string
  region?: string
  poster_url?: string
}

export type SourceViewPreview = {
  item_count: number
  providers: SourceViewPreviewProvider[]
  items: SourceViewPreviewItem[]
}

export type DimensionValue = {
  Value: string
  Count: number
  AlreadyAdded: boolean
}

export type ImportTVBoxResult = {
  config: SourceConfig
  providers: SourceProvider[]
  parsers?: SourceParser[]
  accepted: number
  skipped: number
}

export type ProviderPage = {
  page: number
  page_count: number
  total: number
  items: Array<Record<string, unknown>>
}

export type FederatedSearchProviderSummary = {
  total: number
  success: number
  failed: number
}

export type FederatedSearchError = {
  provider_id: number
  provider_name: string
  source_key: string
  error_type: string
  message: string
  latency_ms: number
}

export type FederatedSearchItemProvider = {
  id: number
  name: string
  source_key: string
  health_status: string
  item_uuid: string
  source_item_id: string
  remarks?: string
}

export type FederatedSearchItem = {
  public_uuid: string
  title: string
  year?: number
  item_type: string
  normalized_kind: string
  region?: string
  poster_url?: string
  remarks?: string
  provider_count: number
  providers: FederatedSearchItemProvider[]
  score: number
}

export type FederatedSearchResponse = {
  keyword: string
  total: number
  items: FederatedSearchItem[]
  errors?: FederatedSearchError[]
  provider: FederatedSearchProviderSummary
  latency_ms: number
  truncated: boolean
  cache_write: boolean
}

export async function importTVBoxConfig(payload: { name?: string; source_url?: string; raw_json?: string }) {
  return requestJson<ImportTVBoxResult>('/SourceConfigs/ImportTVBox', {
    method: 'POST',
    body: JSON.stringify(payload),
    timeoutMs: 120_000,
  })
}

export type ImportCMSListPayload = {
  name?: string
  source_url?: string
  raw_text?: string
  format?: 'auto' | 'libretv_settings' | 'csv' | 'txt' | 'json'
  default_enabled?: boolean
}

export async function importCMSListConfig(payload: ImportCMSListPayload) {
  return requestJson<ImportTVBoxResult>('/SourceConfigs/ImportCMSList', {
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

export async function getSourceConfigImpact(id: number) {
  return requestJson<SourceConfigImpact>(`/SourceConfigs/${id}/Impact`)
}

export async function deleteSourceConfig(id: number) {
  return requestJson<SourceConfigDeleteResult>(`/SourceConfigs/${id}?confirm=true`, { method: 'DELETE' })
}

export async function listSourceProviders(options: SourceProviderListOptions = {}) {
  const pageSize = 500
  const items: SourceProvider[] = []
  for (let offset = 0; ; offset += pageSize) {
    const params = new URLSearchParams({ limit: String(pageSize), offset: String(offset) })
    for (const [key, value] of Object.entries(options)) {
      const text = String(value || '').trim()
      if (text) params.set(key, text)
    }
    const res = await requestJson<{ items: SourceProvider[] }>(`/SourceProviders?${params.toString()}`)
    const pageItems = res.items || []
    items.push(...pageItems)
    if (pageItems.length < pageSize) break
  }
  return items
}

export async function listSourceParsers() {
  const res = await requestJson<{ items: SourceParser[] }>('/SourceParsers')
  return res.items || []
}

export async function setSourceParserEnabled(id: number, enabled: boolean) {
  return requestJson<SourceParser>(`/SourceParsers/${id}/${enabled ? 'Enable' : 'Disable'}`, { method: 'POST' })
}

export async function listSourceRuntimeInvocations(limit = 100) {
  const params = new URLSearchParams({ limit: String(limit) })
  const res = await requestJson<{ items: SourceRuntimeInvocation[] }>(`/SourceRuntime/Invocations?${params.toString()}`)
  return res.items || []
}

export async function listSourceRuntimeArtifacts() {
  const res = await requestJson<{ items: SourceRuntimeArtifact[] }>('/SourceRuntime/Artifacts')
  return res.items || []
}

export async function trustSourceRuntimeArtifact(id: number) {
  return requestJson<SourceRuntimeArtifact>(`/SourceRuntime/Artifacts/${id}/Trust`, { method: 'POST' })
}

export async function setSourceProviderEnabled(id: number, enabled: boolean) {
  return requestJson<SourceProvider>(`/SourceProviders/${id}/${enabled ? 'Enable' : 'Disable'}`, { method: 'POST' })
}

export async function batchSetSourceProvidersEnabled(ids: number[], enabled: boolean) {
  const path = enabled ? '/SourceProviders/BatchEnable' : '/SourceProviders/BatchDisable'
  return requestJson<{ items: SourceProvider[]; count: number }>(path, {
    method: 'POST',
    body: JSON.stringify({ provider_ids: ids }),
  })
}

export async function batchHealthCheckSourceProviders(ids: number[]) {
  return requestJson<{ items: SourceProviderBatchHealthResult[]; count: number }>('/SourceProviders/BatchHealthCheck', {
    method: 'POST',
    body: JSON.stringify({ provider_ids: ids }),
    timeoutMs: 120_000,
  })
}

export async function batchDeleteSourceProviders(ids: number[]) {
  return requestJson<SourceProviderDeleteResult>('/SourceProviders/BatchDelete', {
    method: 'POST',
    body: JSON.stringify({ provider_ids: ids }),
  })
}

export async function healthCheckSourceProvider(id: number) {
  return requestJson<SourceProvider>(`/SourceProviders/${id}/HealthCheck`, { method: 'POST', timeoutMs: 120_000 })
}

export async function diagnoseSourceProvider(id: number, payload: SourceProviderDiagnosePayload = {}) {
  return requestJson<SourceProviderDiagnoseResult>(`/SourceProviders/${id}/Diagnose`, {
    method: 'POST',
    body: JSON.stringify(payload),
    timeoutMs: 120_000,
  })
}

export async function getSourceProviderHomeProfile(id: number) {
  return requestJson<SourceProviderHomeProfile>(`/SourceProviders/${id}/HomeProfile`, {
    timeoutMs: 120_000,
  })
}

export async function searchSourceProvider(id: number, keyword: string, page = 1) {
  return requestJson<{ page: ProviderPage; items: unknown[] }>(`/SourceProviders/${id}/Search`, {
    method: 'POST',
    body: JSON.stringify({ keyword, page }),
    timeoutMs: 120_000,
  })
}

export async function federatedSourceSearch(keyword: string, limit = 50) {
  return requestJson<FederatedSearchResponse>('/SourceSearch', {
    method: 'POST',
    body: JSON.stringify({ keyword, limit }),
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

export async function previewSourceView(payload: Record<string, unknown>) {
  return requestJson<SourceViewPreview>('/Library/SourceViews/Preview', { method: 'POST', body: JSON.stringify(payload) })
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
