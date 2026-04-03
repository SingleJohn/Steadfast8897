export interface PathMapping {
  from: string
  to: string
}

export interface EmbyUpstreamConfig {
  mode: 'external' | 'self'
  host: string
  base_path: string
  api_key: string
}

export interface RouteMatchConfig {
  real_path_prefix: string[]
  real_path_regex: string[]
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

export interface PathRuleSetConfig {
  id: string
  name: string
  mappings: PathMapping[]
}

export interface CacheConfig {
  enabled: boolean
  dir: string
  max_size_bytes: number
  default_ttl_seconds: number
  respect_upstream: boolean
  include_prefixes: string[]
  include_images: boolean
}

export interface SecurityRealIPConfig {
  header: string
  trust_any_proxy: boolean
  trusted_proxies: string[]
}

export interface SecurityToggleRule {
  enabled: boolean
  extra_path_prefixes: string[]
  extra_path_regex: string[]
}

export interface SecurityRuleMatch {
  source_id: string[]
  ip_cidr: string[]
  ua_contains: string[]
  ua_regex: string[]
  path_prefix: string[]
  path_regex: string[]
  method: string[]
}

export interface SecurityRuleActionRateLimit {
  key: string
  rps: number
  burst: number
  status: number
}

export interface SecurityRuleAction {
  type: 'allow' | 'deny' | 'rate_limit' | string
  status: number
  message: string
  rate_limit: SecurityRuleActionRateLimit
}

export interface SecurityRule {
  name: string
  enabled: boolean
  match: SecurityRuleMatch
  action: SecurityRuleAction
}

export interface SecurityConfig {
  enabled: boolean
  real_ip: SecurityRealIPConfig
  disable_download: SecurityToggleRule
  disable_offline: SecurityToggleRule
  rules: SecurityRule[]
}

export interface StrmProfileConfig {
  id: string
  name: string
  enabled: boolean
  base_url: string
  backend_id: string
  s3_backend_id?: string
  output_dir: string
  source_prefix: string
  output_under_source_path?: boolean
  strip_source_extension: boolean
  prune_deleted: boolean
  sync_interval_seconds: number
  video_extensions: string[]

  concurrency: number
  download_metadata: boolean
  overwrite_existing: boolean
  metadata_extensions: string[]
}

export interface PlaybackConfig {
  backend: 's3' | 'cdn' | string
  fallback: 's3' | 'none' | string
}

export interface CdnAuthConfig {
  enabled: boolean
  secret: string
  expires_seconds: number
  rand: string
  uid: string
  param_name: string
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

export interface AliyunCdnBackendConfig {
  base_url: string
  path_escape: boolean
  auth: CdnAuthConfig
}

export interface GoogleDriveBackendConfig {
  client_id: string
  client_secret: string
  refresh_token: string
  base_url: string
  drive_id: string
  include_all_drives: boolean
  link_ttl_seconds: number
  cache_enabled: boolean
  cache_ttl_seconds: number
  use_worker: boolean
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
  direct_link_mode: 'api' | 'compose' | string
  compose_base_url: string
  compose_hide_uid: boolean
  link_ttl_seconds: number
  sign_enabled: boolean
  private_key: string
  uid: string
  valid_duration_minutes: number
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Open115BackendConfig {
  access_token: string
  refresh_token: string
  root_folder_id: string
  order_by: string
  order_direction: 'asc' | 'desc' | string
  custom_page_size: number
  link_ttl_seconds: number
  link_mode: 'redirect' | 'relay' | string
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Cookie115BackendConfig {
  root_folder_id: string
  order_by: string
  order_direction: 'asc' | 'desc' | string
  custom_page_size: number
  link_ttl_seconds: number
  link_mode: 'redirect' | 'relay' | string
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface Sub115BackendConfig {
  primary_backend_id: string
  selection_strategy: 'round_robin' | 'user_affinity_rr' | string
  user_affinity_enabled: boolean
  user_single_drive_only: boolean
  disable_primary_fallback: boolean
  auto_mirror_dir: boolean
  mirror_dir_prefix: string
  link_ttl_seconds: number
  link_mode: 'redirect' | 'relay' | string
  cache_enabled: boolean
  cache_ttl_seconds: number
}

export interface GDriveWorkerConfig {
  base_url: string
  sign_key: string
  sync_token: string
}

export interface BackendConfig {
  id: string
  name: string
  type: 's3' | 'aliyun_cdn' | 'gdrive' | 'local' | 'local_agent' | 'pan123' | '115_open' | '115_cookie' | '115_sub' | string
  enabled: boolean
  has_secondary_password?: boolean  // 是否设置了二级密码（只读）
  s3?: S3BackendConfig
  aliyun_cdn?: AliyunCdnBackendConfig
  gdrive?: GoogleDriveBackendConfig
  local?: LocalBackendConfig
  local_agent?: LocalAgentBackendConfig
  pan123?: Pan123BackendConfig
  '115_open'?: Open115BackendConfig
  '115_cookie'?: Cookie115BackendConfig
  '115_sub'?: Sub115BackendConfig
}

export interface LoginResponse {
  token: string
  role: 'admin' | 'viewer' | 'backend_admin'
  scope: 'global' | 'backend'
  backend_id?: string
  backend_name?: string
}

export interface ResourcePoolConfig {
  id: string
  name: string
  primary_backend_id: string
  standby_backend_id: string
}

export type NotifyChannelType = 'wecom_webhook' | 'dingtalk' | string

export interface NotifyWeComWebhookConfig {
  key: string
}

export interface NotifyDingTalkConfig {
  token: string
  secret: string
}

export interface NotifyTelegramBotConfig {
  bot_token: string
  chat_id: string
  api_base?: string
  proxy_url?: string
}

export interface NotifyBarkConfig {
  token?: string
  push?: string
}

export interface NotifyServerChanConfig {
  key: string
}

export interface NotifyChannel {
  id: string
  name: string
  type: NotifyChannelType
  enabled: boolean
  wecom_webhook?: NotifyWeComWebhookConfig
  dingtalk?: NotifyDingTalkConfig
  telegram_bot?: NotifyTelegramBotConfig
  bark?: NotifyBarkConfig
  serverchan?: NotifyServerChanConfig
}

export interface NotifyConfig {
  enabled: boolean
  channels: NotifyChannel[]
}

export type SecurityAlertRuleType = 'user_frequency' | 'identity_drift' | 'failure_rate' | 'security_event_frequency' | string

export interface UserFrequencyRule {
  max_requests: number
}

export interface IdentityDriftRule {
  min_total: number
  max_unique_ips: number
  max_unique_users: number
  max_unique_uas: number
}

export interface FailureRateRule {
  min_total: number
  max_error_rate: number
}

export interface SecurityAlertRule {
  id: string
  name: string
  enabled: boolean
  type: SecurityAlertRuleType
  group_by: 'emby_user_id' | 'client_ip' | string
  window_seconds: number
  cooldown_seconds: number
  channels: string[]
  scope: { source_ids: string[] }
  user_frequency?: UserFrequencyRule
  identity_drift?: IdentityDriftRule
  failure_rate?: FailureRateRule
}

export interface SecurityAlertsConfig {
  enabled: boolean
  tick_seconds: number
  rules: SecurityAlertRule[]
}

export interface Config {
  schema_version: number
  admin: {
    host: string
    port: number
    password?: string
    viewer_password?: string
    path_prefix?: string
  }
  observability: {
    request_log_retention_days: number
    stat_retention_days: number
    db_batch_size: number
    db_flush_interval_ms: number
    debug_user_resolution: boolean
    user_resolution_ip_fallback: boolean
    ip_lookup_vip_enabled: boolean
    ip_lookup_vip_token: string
    ip_lookup_cache_days: number
  }
  cache: CacheConfig
  security: SecurityConfig
  notify: NotifyConfig
  security_alerts: SecurityAlertsConfig

  sources: EmbySourceConfig[]
  path_rule_sets: PathRuleSetConfig[]
  backends: BackendConfig[]
  gdrive_worker: GDriveWorkerConfig
  resource_pools: ResourcePoolConfig[]
  strm_profiles: StrmProfileConfig[]
}

export interface RequestLog {
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
  headers: string
}

export interface SecurityEvent {
  id: number
  created_at: string
  source_id: string
  client_ip: string
  method: string
  path: string
  query: string
  status: number
  decision: string
  rule_name: string
  message: string
  emby_user_id: string
  emby_user_name: string
  user_agent: string
  referer: string
  headers: string
}

export interface SecurityEventCount {
  key: string
  count: number
}

export interface SecurityEventsSummary {
  since: string
  until: string
  total: number
  by_rule: SecurityEventCount[]
  top_ips: SecurityEventCount[]
  by_decision: SecurityEventCount[]
  by_source: SecurityEventCount[]
}

export interface IPInfoLite {
  ip: string
  country: string
  country_code: string
  prov: string
  city: string
  area: string
  big_area: string
  isp: string
  ip_type: string
  lng: string
  lat: string
  expires_at: string
}

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
  last_updated_at: string
}

export interface RedirectBackendCount {
  backend: string
  count: number
}

export interface RedirectUserCount {
  emby_user_id: string
  emby_user_name: string
  count: number
}

export interface RedirectUserBackendCount {
  emby_user_id: string
  emby_user_name: string
  backend: string
  count: number
}

export interface RedirectIPCount {
  client_ip: string
  count: number
}

export interface RedirectUACount {
  user_agent: string
  count: number
}

export interface RedirectSummary {
  since: string
  until: string
  total_302: number
  by_backend: RedirectBackendCount[]
  top_users: RedirectUserCount[]
  top_user_backend: RedirectUserBackendCount[]
  top_ips: RedirectIPCount[]
  top_uas: RedirectUACount[]
  ip_infos?: Record<string, IPInfoLite>
}

export interface IPStatsTopIP {
  client_ip: string
  count: number
  country: string
  prov: string
  city: string
  area: string
  big_area: string
  isp: string
  ip_type: string
}

export interface IPStatsCountryBucket {
  country: string
  count: number
  percent: number
}

export interface IPStatsProvBucket {
  country: string
  prov: string
  count: number
  percent: number
}

export interface IPStatsCityBucket {
  country: string
  prov: string
  city: string
  count: number
  percent: number
}

export interface IPStatsAreaBucket {
  country: string
  prov: string
  city: string
  area: string
  count: number
  percent: number
}

export interface IPStatsBigAreaBucket {
  big_area: string
  count: number
  percent: number
}

export interface IPStatsISPBucket {
  isp: string
  count: number
  percent: number
}

export interface IPStatsIPTypeBucket {
  ip_type: string
  count: number
  percent: number
}

export interface IPStatsSummary {
  scope?: string
  source?: string
  tag: string
  mode: 'all' | 'redirect302' | string
  source_id: string
  since: string
  until: string
  total: number
  pending_enrich: number
  top_ips: IPStatsTopIP[]
  by_country: IPStatsCountryBucket[]
  by_prov: IPStatsProvBucket[]
  by_city: IPStatsCityBucket[]
  by_area: IPStatsAreaBucket[]
  by_big_area: IPStatsBigAreaBucket[]
  by_isp: IPStatsISPBucket[]
  by_ip_type: IPStatsIPTypeBucket[]
}
