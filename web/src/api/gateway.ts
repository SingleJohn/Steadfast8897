const API_BASE = ''

function getToken(): string | null {
  return localStorage.getItem('accessToken')
}

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['X-Emby-Token'] = token
  return headers
}

async function request<T>(path: string, options: RequestInit & { timeoutMs?: number } = {}): Promise<T> {
  const { timeoutMs = 60_000, ...init } = options
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeoutMs)
  try {
    const res = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers: { ...getAuthHeaders(), ...init.headers },
      signal: controller.signal,
    })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new Error(text || `HTTP ${res.status}`)
    }
    return res.json()
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      throw new Error(`Request timeout: ${path}`)
    }
    throw e
  } finally {
    window.clearTimeout(timer)
  }
}

// --- Gateway Config ---

export interface GatewayConfig {
  sources: EmbySourceConfig[]
  path_rule_sets: PathRuleSetConfig[]
  backends: BackendConfig[]
  resource_pools: ResourcePoolConfig[]
  observability: ObservabilityConfig
}

export interface EmbySourceConfig {
  id: string
  name: string
  enabled: boolean
  listen_host: string
  listen_port: number
  stream_path_prefix: string
  upstream: EmbyUpstreamConfig
  routes: RouteRuleConfig[]
}

export interface EmbyUpstreamConfig {
  host: string
  base_path: string
  api_key: string
}

export interface RouteRuleConfig {
  id: string
  enabled: boolean
  priority: number
  match: RouteMatchConfig
  path_rule_set_id: string
  pool_id: string
  require_mapping: boolean
}

export interface RouteMatchConfig {
  real_path_prefix: string[]
  real_path_regex: string[]
}

export interface PathRuleSetConfig {
  id: string
  name: string
  mappings: PathMapping[]
}

export interface PathMapping {
  from: string
  to: string
}

export interface ResourcePoolConfig {
  id: string
  name: string
  primary_backend_id: string
  standby_backend_id: string
}

export interface BackendConfig {
  id: string
  name: string
  type: string
  enabled: boolean
  s3?: S3BackendConfig
  aliyun_cdn?: AliyunCDNBackendConfig
  gdrive?: GDriveBackendConfig
  local?: LocalBackendConfig
  local_agent?: LocalAgentBackendConfig
  pan123?: Pan123BackendConfig
  '115_open'?: Open115BackendConfig
  '115_cookie'?: Cookie115BackendConfig
  '115_sub'?: Sub115BackendConfig
}

export interface S3BackendConfig {
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key: string
  sign_expiry_minutes: number
  force_path_style: boolean
  key_prefix: string
}

export interface AliyunCDNBackendConfig {
  base_url: string
  auth: { type: string; key: string; expiry_seconds: number }
}

export interface GDriveBackendConfig {
  client_id: string
  client_secret: string
  refresh_token: string
  base_url: string
  drive_id: string
  include_all_drives: boolean
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface LocalBackendConfig {
  base_dir: string
  base_url: string
  link_ttl_seconds: number
  sign_secret: string
}

export interface LocalAgentBackendConfig {
  base_dir: string
  public_base_url: string
  agent_api_url: string
  link_ttl_seconds: number
  sign_secret: string
  sync_token: string
}

export interface Pan123BackendConfig {
  client_id: string
  client_secret: string
  root_folder_id: string
  direct_link_mode: string
  compose_base_url: string
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Open115BackendConfig {
  access_token: string
  refresh_token: string
  root_folder_id: string
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Cookie115BackendConfig {
  root_folder_id: string
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Sub115BackendConfig {
  primary_backend_id: string
  selection_strategy: string
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface ObservabilityConfig {
  request_log_retention_days: number
  stat_retention_days: number
  db_batch_size: number
  db_flush_interval_ms: number
}

export function getGatewayConfig(): Promise<GatewayConfig> {
  return request('/Gateway/Config')
}

export function saveGatewayConfig(config: GatewayConfig): Promise<{ status: string }> {
  return request('/Gateway/Config', {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

// --- Request Logs ---

export interface RequestLogEntry {
  id: number
  created_at: string
  tag: string
  source_id: string
  client_ip: string
  method: string
  path: string
  query: string
  status: number
  latency_ms: number
  bytes_in: number
  bytes_out: number
  emby_user_id: string
  emby_user_name: string
  redirect_backend: string
  redirect_source: string
  redirect_location: string
  redirect_trace: string
  object_key: string
  route_id: string
  pool_id: string
  user_agent: string
  referer: string
}

export interface LogsResponse {
  items: RequestLogEntry[]
  total: number
}

export function getGatewayLogs(params: Record<string, string> = {}): Promise<LogsResponse> {
  const qs = new URLSearchParams(params).toString()
  return request(`/Gateway/Logs?${qs}`)
}

export function getRedirectLogs(params: Record<string, string> = {}): Promise<LogsResponse> {
  const qs = new URLSearchParams(params).toString()
  return request(`/Gateway/Redirects/Logs?${qs}`)
}

// --- Stats ---

export interface DailyStat {
  id: number
  day: string
  tag: string
  source_id: string
  requests: number
  redirects302: number
  status4xx: number
  status5xx: number
  bytes_in: number
  bytes_out: number
  latency_ms_sum: number
  latency_ms_max: number
  latency_ms_min: number
}

export function getDailyStats(params: Record<string, string> = {}): Promise<DailyStat[]> {
  const qs = new URLSearchParams(params).toString()
  return request(`/Gateway/Stats/Daily?${qs}`)
}

// --- Redirects ---

export interface RedirectSummary {
  total: number
  by_backend: Record<string, number>
  top_users: { key: string; count: number }[]
  top_ips: { key: string; count: number }[]
}

export function getRedirectSummary(params: Record<string, string> = {}): Promise<RedirectSummary> {
  const qs = new URLSearchParams(params).toString()
  return request(`/Gateway/Redirects/Summary?${qs}`)
}

// --- Backends ---

export interface BackendInfo {
  id: string
  name: string
  type: string
}

export function listBackends(): Promise<BackendInfo[]> {
  return request('/Gateway/Backends')
}

// --- Health ---

export interface SourceHealth {
  id: string
  name: string
  status: string
}

export function getEmbyHealth(): Promise<SourceHealth[]> {
  return request('/Gateway/Health/Emby')
}

export function checkEmbyConnection(host: string, apiKey: string): Promise<{ success: boolean; error?: string }> {
  return request('/Gateway/Emby/Check', {
    method: 'POST',
    body: JSON.stringify({ host, api_key: apiKey }),
  })
}
