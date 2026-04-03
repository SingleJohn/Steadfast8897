import type { MessageApi } from 'naive-ui'
import { computed, ref } from 'vue'

import { ApiError } from '@/api/client'
import { getConfig, saveConfig } from '@/api/config'
import type { Config } from '@/types'

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function normalizeLoadedConfig(cfg: Config) {
  cfg.schema_version = typeof cfg.schema_version === 'number' ? cfg.schema_version : 2

  cfg.sources = Array.isArray(cfg.sources) ? cfg.sources : []
  for (const src of cfg.sources) {
    src.id = String(src.id || '')
    src.name = String(src.name || '')
    src.enabled = Boolean(src.enabled)
    src.listen_host = String(src.listen_host || '')
    src.listen_port = typeof src.listen_port === 'number' ? src.listen_port : 0
    src.stream_path_prefix = String(src.stream_path_prefix || '')
    src.upstream = src.upstream || { mode: 'external', host: '', base_path: '', api_key: '' }
    src.upstream.mode = src.upstream.mode === 'self' ? 'self' : 'external'
    src.upstream.host = String(src.upstream.host || '')
    src.upstream.base_path = String(src.upstream.base_path || '')
    src.upstream.api_key = String(src.upstream.api_key || '')
    src.routes = Array.isArray(src.routes) ? src.routes : []
    for (const r of src.routes) {
      r.id = String(r.id || '')
      r.enabled = Boolean(r.enabled)
      r.priority = typeof r.priority === 'number' ? r.priority : 0
      r.match = r.match || { real_path_prefix: [], real_path_regex: [] }
      r.match.real_path_prefix = Array.isArray(r.match.real_path_prefix) ? r.match.real_path_prefix : []
      r.match.real_path_regex = Array.isArray(r.match.real_path_regex) ? r.match.real_path_regex : []
      r.path_rule_set_id = String(r.path_rule_set_id || '')
      r.pool_id = String(r.pool_id || '')
      r.require_mapping = Boolean(r.require_mapping)
    }
  }

  cfg.path_rule_sets = Array.isArray(cfg.path_rule_sets) ? cfg.path_rule_sets : []
  for (const rs of cfg.path_rule_sets) {
    rs.id = String(rs.id || '')
    rs.name = String(rs.name || '')
    rs.mappings = Array.isArray(rs.mappings) ? rs.mappings : []
  }

  cfg.backends = Array.isArray(cfg.backends) ? cfg.backends : []
  for (const b of cfg.backends) {
    b.id = String(b.id || '')
    b.name = String(b.name || '')
    b.type = String(b.type || '')
    b.enabled = Boolean(b.enabled)
    if (b.type === 's3') {
      b.s3 = b.s3 || {
        endpoint: '',
        region: 'us-east-1',
        bucket: '',
        access_key: '',
        secret_key: '',
        sign_expiry_minutes: 60,
        force_path_style: false,
        key_prefix: '',
      }
    }
    if (b.type === 'aliyun_cdn') {
      b.aliyun_cdn = b.aliyun_cdn || {
        base_url: '',
        path_escape: true,
        auth: { enabled: true, secret: '', expires_seconds: 1800, rand: '0', uid: '0', param_name: 'auth_key' },
      }
    }
    if (b.type === 'gdrive') {
      b.gdrive = b.gdrive || {
        client_id: '',
        client_secret: '',
        refresh_token: '',
        base_url: '',
        drive_id: '',
        include_all_drives: false,
        link_ttl_seconds: 3600,
        cache_enabled: true,
        cache_ttl_seconds: 10800,
        use_worker: false,
      }
      b.gdrive.use_worker = Boolean(b.gdrive.use_worker)
    }
    if (b.type === 'pan123') {
      b.pan123 = b.pan123 || {
        client_id: '',
        client_secret: '',
        root_folder_id: '0',
        direct_link_mode: 'api',
        compose_base_url: '',
        compose_hide_uid: false,
        link_ttl_seconds: 1800,
        sign_enabled: true,
        private_key: '',
        uid: '',
        valid_duration_minutes: 30,
        cache_enabled: true,
        cache_ttl_seconds: 10800,
      }
      b.pan123.sign_enabled = Boolean(b.pan123.sign_enabled)
      b.pan123.cache_enabled = Boolean(b.pan123.cache_enabled)
      b.pan123.compose_hide_uid = Boolean(b.pan123.compose_hide_uid)
      b.pan123.root_folder_id = String(b.pan123.root_folder_id || '0')
      b.pan123.direct_link_mode = String(b.pan123.direct_link_mode || 'api')
      if (b.pan123.direct_link_mode !== 'compose') b.pan123.direct_link_mode = 'api'
    }
    if (b.type === '115_open') {
      b['115_open'] = b['115_open'] || {
        access_token: '',
        refresh_token: '',
        root_folder_id: '0',
        order_by: 'file_name',
        order_direction: 'asc',
        custom_page_size: 200,
        link_ttl_seconds: 1800,
        link_mode: 'redirect',
        cache_enabled: true,
        cache_ttl_seconds: 10800,
      }
      b['115_open'].order_direction = b['115_open'].order_direction === 'desc' ? 'desc' : 'asc'
      b['115_open'].link_mode = b['115_open'].link_mode === 'relay' ? 'relay' : 'redirect'
      b['115_open'].root_folder_id = String(b['115_open'].root_folder_id || '0')
      b['115_open'].order_by = String(b['115_open'].order_by || 'file_name')
      b['115_open'].cache_enabled = Boolean(b['115_open'].cache_enabled)
      if (typeof b['115_open'].custom_page_size !== 'number') b['115_open'].custom_page_size = 200
      if (typeof b['115_open'].link_ttl_seconds !== 'number') b['115_open'].link_ttl_seconds = 1800
      if (typeof b['115_open'].cache_ttl_seconds !== 'number') b['115_open'].cache_ttl_seconds = 10800
    }
    if (b.type === '115_cookie') {
      b['115_cookie'] = b['115_cookie'] || {
        root_folder_id: '0',
        order_by: 'file_name',
        order_direction: 'asc',
        custom_page_size: 200,
        link_ttl_seconds: 1800,
        link_mode: 'redirect',
        cache_enabled: true,
        cache_ttl_seconds: 10800,
      }
      b['115_cookie'].order_direction = b['115_cookie'].order_direction === 'desc' ? 'desc' : 'asc'
      b['115_cookie'].link_mode = b['115_cookie'].link_mode === 'relay' ? 'relay' : 'redirect'
      b['115_cookie'].root_folder_id = String(b['115_cookie'].root_folder_id || '0')
      b['115_cookie'].order_by = String(b['115_cookie'].order_by || 'file_name')
      b['115_cookie'].cache_enabled = Boolean(b['115_cookie'].cache_enabled)
      if (typeof b['115_cookie'].custom_page_size !== 'number') b['115_cookie'].custom_page_size = 200
      if (typeof b['115_cookie'].link_ttl_seconds !== 'number') b['115_cookie'].link_ttl_seconds = 1800
      if (typeof b['115_cookie'].cache_ttl_seconds !== 'number') b['115_cookie'].cache_ttl_seconds = 10800
    }
    if (b.type === '115_sub') {
      b['115_sub'] = b['115_sub'] || {
        primary_backend_id: '',
        selection_strategy: 'round_robin',
        user_affinity_enabled: false,
        user_single_drive_only: false,
        disable_primary_fallback: false,
        auto_mirror_dir: true,
        mirror_dir_prefix: 'emby_cache',
        link_ttl_seconds: 1800,
        link_mode: 'redirect',
        cache_enabled: true,
        cache_ttl_seconds: 10800,
      }
      b['115_sub'].primary_backend_id = String(b['115_sub'].primary_backend_id || '')
      b['115_sub'].selection_strategy = b['115_sub'].selection_strategy === 'user_affinity_rr' ? 'user_affinity_rr' : 'round_robin'
      b['115_sub'].user_affinity_enabled = Boolean(b['115_sub'].user_affinity_enabled)
      b['115_sub'].user_single_drive_only = Boolean(b['115_sub'].user_single_drive_only)
      b['115_sub'].disable_primary_fallback = Boolean(b['115_sub'].disable_primary_fallback)
      b['115_sub'].auto_mirror_dir = Boolean(b['115_sub'].auto_mirror_dir)
      b['115_sub'].mirror_dir_prefix = String(b['115_sub'].mirror_dir_prefix || '').replace(/^\/+|\/+$/g, '') || 'emby_cache'
      b['115_sub'].link_mode = b['115_sub'].link_mode === 'relay' ? 'relay' : 'redirect'
      b['115_sub'].cache_enabled = Boolean(b['115_sub'].cache_enabled)
      if (typeof b['115_sub'].link_ttl_seconds !== 'number') b['115_sub'].link_ttl_seconds = 1800
      if (typeof b['115_sub'].cache_ttl_seconds !== 'number') b['115_sub'].cache_ttl_seconds = 10800
    }
  }

  cfg.resource_pools = Array.isArray(cfg.resource_pools) ? cfg.resource_pools : []
  for (const p of cfg.resource_pools) {
    p.id = String(p.id || '')
    p.name = String(p.name || '')
    p.primary_backend_id = String(p.primary_backend_id || '')
    p.standby_backend_id = String(p.standby_backend_id || '')
  }

  cfg.cache = cfg.cache || { enabled: false, dir: '', max_size_bytes: 0, default_ttl_seconds: 0, respect_upstream: false, include_prefixes: [], include_images: false } as any
  cfg.cache.include_prefixes = Array.isArray(cfg.cache.include_prefixes) ? cfg.cache.include_prefixes : []

  cfg.admin = cfg.admin || { host: '', port: 0 } as any
  cfg.admin.path_prefix = typeof cfg.admin.path_prefix === 'string' ? cfg.admin.path_prefix : ''
  cfg.admin.viewer_password = typeof cfg.admin.viewer_password === 'string' ? cfg.admin.viewer_password : ''

  cfg.observability = cfg.observability || { request_log_retention_days: 7, stat_retention_days: 30, db_batch_size: 100, db_flush_interval_ms: 1000 } as any
  cfg.observability.debug_user_resolution = Boolean(cfg.observability.debug_user_resolution)
  cfg.observability.user_resolution_ip_fallback = Boolean(cfg.observability.user_resolution_ip_fallback)
  ;(cfg.observability as any).ip_lookup_vip_enabled = Boolean((cfg.observability as any).ip_lookup_vip_enabled)
  ;(cfg.observability as any).ip_lookup_vip_token =
    typeof (cfg.observability as any).ip_lookup_vip_token === 'string' ? (cfg.observability as any).ip_lookup_vip_token : ''
  ;(cfg.observability as any).ip_lookup_cache_days =
    typeof (cfg.observability as any).ip_lookup_cache_days === 'number' && Number.isFinite((cfg.observability as any).ip_lookup_cache_days)
      ? (cfg.observability as any).ip_lookup_cache_days
      : 30

  cfg.security = cfg.security || { enabled: false, real_ip: { header: '', trust_any_proxy: false, trusted_proxies: [] }, disable_download: { enabled: false, extra_path_prefixes: [], extra_path_regex: [] }, disable_offline: { enabled: false, extra_path_prefixes: [], extra_path_regex: [] }, rules: [] } as any
  cfg.security.real_ip = cfg.security.real_ip || { header: '', trust_any_proxy: false, trusted_proxies: [] } as any
  cfg.security.real_ip.trusted_proxies = Array.isArray(cfg.security.real_ip.trusted_proxies)
    ? cfg.security.real_ip.trusted_proxies
    : []

  cfg.security.disable_download = cfg.security.disable_download || { enabled: false, extra_path_prefixes: [], extra_path_regex: [] } as any
  cfg.security.disable_download.extra_path_prefixes = Array.isArray(cfg.security.disable_download.extra_path_prefixes)
    ? cfg.security.disable_download.extra_path_prefixes
    : []
  cfg.security.disable_download.extra_path_regex = Array.isArray(cfg.security.disable_download.extra_path_regex)
    ? cfg.security.disable_download.extra_path_regex
    : []

  cfg.security.disable_offline = cfg.security.disable_offline || { enabled: false, extra_path_prefixes: [], extra_path_regex: [] } as any
  cfg.security.disable_offline.extra_path_prefixes = Array.isArray(cfg.security.disable_offline.extra_path_prefixes)
    ? cfg.security.disable_offline.extra_path_prefixes
    : []
  cfg.security.disable_offline.extra_path_regex = Array.isArray(cfg.security.disable_offline.extra_path_regex)
    ? cfg.security.disable_offline.extra_path_regex
    : []

  cfg.security.rules = Array.isArray(cfg.security.rules) ? cfg.security.rules : []
  for (const r of cfg.security.rules) {
    r.match =
      r.match || ({
        source_id: [],
        ip_cidr: [],
        ua_contains: [],
        ua_regex: [],
        path_prefix: [],
        path_regex: [],
        method: [],
      } as any)
    ;(r.match as any).source_id = Array.isArray((r.match as any).source_id) ? (r.match as any).source_id : []
    r.match.ip_cidr = Array.isArray(r.match.ip_cidr) ? r.match.ip_cidr : []
    r.match.ua_contains = Array.isArray(r.match.ua_contains) ? r.match.ua_contains : []
    r.match.ua_regex = Array.isArray(r.match.ua_regex) ? r.match.ua_regex : []
    r.match.path_prefix = Array.isArray(r.match.path_prefix) ? r.match.path_prefix : []
    r.match.path_regex = Array.isArray(r.match.path_regex) ? r.match.path_regex : []
    r.match.method = Array.isArray(r.match.method) ? r.match.method : []
  }

  cfg.notify = cfg.notify || { enabled: false, channels: [] } as any
  cfg.notify.enabled = Boolean(cfg.notify.enabled)
  cfg.notify.channels = Array.isArray(cfg.notify.channels) ? cfg.notify.channels : []
  for (const ch of cfg.notify.channels) {
    ch.id = String((ch as any).id || '')
    ch.name = String((ch as any).name || '')
    ch.type = String((ch as any).type || '')
    ch.enabled = Boolean((ch as any).enabled)
  }

  cfg.security_alerts = cfg.security_alerts || { enabled: false, tick_seconds: 60, rules: [] } as any
  cfg.security_alerts.enabled = Boolean(cfg.security_alerts.enabled)
  cfg.security_alerts.tick_seconds = typeof cfg.security_alerts.tick_seconds === 'number' ? cfg.security_alerts.tick_seconds : 60
  cfg.security_alerts.rules = Array.isArray(cfg.security_alerts.rules) ? cfg.security_alerts.rules : []
  for (const rule of cfg.security_alerts.rules) {
    ;(rule as any).scope = (rule as any).scope || { source_ids: [] }
    ;(rule as any).scope.source_ids = Array.isArray((rule as any).scope.source_ids) ? (rule as any).scope.source_ids : []
    rule.channels = Array.isArray(rule.channels) ? rule.channels : []
  }

  cfg.strm_profiles = Array.isArray(cfg.strm_profiles) ? cfg.strm_profiles : []
  for (const p of cfg.strm_profiles) {
    p.id = String((p as any).id || '')
    p.name = String((p as any).name || '')
    p.enabled = Boolean((p as any).enabled)
    p.base_url = String((p as any).base_url || '')
    p.backend_id = String((p as any).backend_id || (p as any).s3_backend_id || '')
    p.s3_backend_id = p.backend_id
    p.output_dir = String((p as any).output_dir || '')
    p.source_prefix = String((p as any).source_prefix || '')
    p.strip_source_extension = Boolean((p as any).strip_source_extension)
    p.prune_deleted = Boolean((p as any).prune_deleted)
    p.sync_interval_seconds = typeof (p as any).sync_interval_seconds === 'number' ? (p as any).sync_interval_seconds : 300
    p.video_extensions = Array.isArray((p as any).video_extensions) ? (p as any).video_extensions : []

    p.concurrency = typeof (p as any).concurrency === 'number' ? (p as any).concurrency : 8
    if (!Number.isFinite(p.concurrency) || p.concurrency <= 0) p.concurrency = 8
    if (p.concurrency > 64) p.concurrency = 64
    p.download_metadata = Boolean((p as any).download_metadata)
    p.overwrite_existing = Boolean((p as any).overwrite_existing)
    p.metadata_extensions = Array.isArray((p as any).metadata_extensions) ? (p as any).metadata_extensions : []
  }

  cfg.gdrive_worker = cfg.gdrive_worker || { base_url: '', sign_key: '', sync_token: '' } as any
  cfg.gdrive_worker.base_url = String(cfg.gdrive_worker.base_url || '')
  cfg.gdrive_worker.sign_key = String(cfg.gdrive_worker.sign_key || '')
  cfg.gdrive_worker.sync_token = String(cfg.gdrive_worker.sync_token || '')
}

function toApiError(e: unknown, fallbackMessage: string) {
  if (e instanceof ApiError) return e
  return new ApiError((e as Error).message || fallbackMessage, 0)
}

export function useConfigDraft(message?: MessageApi) {
  const loading = ref(false)
  const saving = ref(false)
  const loadError = ref<string | null>(null)

  const original = ref<Config | null>(null)
  const draft = ref<Config | null>(null)

  const hasChanges = computed(() => {
    if (!original.value || !draft.value) return false
    return JSON.stringify(original.value) !== JSON.stringify(draft.value)
  })

  async function refresh() {
    loading.value = true
    loadError.value = null
    try {
      const cfg = await getConfig()
      normalizeLoadedConfig(cfg)
      original.value = cfg
      draft.value = deepClone(cfg)
    } catch (e) {
      const err = toApiError(e, '加载失败')
      loadError.value = err.message
      message?.error(err.message)
    } finally {
      loading.value = false
    }
  }

  function resetDraft() {
    if (!original.value) return
    draft.value = deepClone(original.value)
  }

  async function saveDraft(options?: { successMessage?: string; silent?: boolean }) {
    if (!draft.value) return false
    saving.value = true
    try {
      await saveConfig(draft.value)
      if (!options?.silent) {
        message?.success(options?.successMessage || '保存成功')
      }
      await refresh()
      return true
    } catch (e) {
      const err = toApiError(e, '保存失败')
      message?.error(err.message)
      return false
    } finally {
      saving.value = false
    }
  }

  return {
    loading,
    saving,
    loadError,
    original,
    draft,
    hasChanges,
    refresh,
    resetDraft,
    saveDraft,
  }
}
