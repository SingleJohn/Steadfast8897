import type { MessageApi } from 'naive-ui'
import { computed, onMounted, ref, watchEffect } from 'vue'

import { cdnSign, type CdnSignResponse } from '@/api/cdn'
import { s3Check } from '@/api/s3'
import { useConfigDraft } from '@/composables/useConfigDraft'
import type { BackendConfig, ResourcePoolConfig } from '@/types'

function genId(prefix: string) {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
}

function backendIdPrefix(type: BackendConfig['type']) {
  if (type === 's3') return 's3'
  if (type === 'gdrive') return 'gdrive'
  if (type === 'local') return 'local'
  if (type === 'local_agent') return 'lagent'
  if (type === 'pan123') return 'pan123'
  if (type === '115_open') return 'open115'
  if (type === '115_cookie') return 'cookie115'
  if (type === '115_sub') return 'sub115'
  return 'cdn'
}

const autoBackendIdRe = /^(s3|cdn|gdrive|local|lagent|pan123|open115|cookie115|sub115)_[a-z0-9]+_[a-z0-9]{4}$/

function ensureBackendIDPrefix(b: BackendConfig, pools: ResourcePoolConfig[]) {
  const id = String(b.id || '')
  if (!autoBackendIdRe.test(id)) return
  const expected = backendIdPrefix(b.type)
  const current = id.split('_', 2)[0]
  if (current === expected) return
  const newID = expected + id.slice(current.length)
  b.id = newID
  for (const p of pools) {
    if (p.primary_backend_id === id) p.primary_backend_id = newID
    if (p.standby_backend_id === id) p.standby_backend_id = newID
  }
}

function ensureBackendShape(b: BackendConfig) {
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
    b.aliyun_cdn = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
  }
  if (b.type === 'aliyun_cdn') {
    b.aliyun_cdn = b.aliyun_cdn || {
      base_url: '',
      path_escape: true,
      auth: { enabled: true, secret: '', expires_seconds: 1800, rand: '0', uid: '0', param_name: 'auth_key' },
    }
    b.s3 = undefined
    b.gdrive = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
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
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
  }
  if (b.type === 'local') {
    b.local = b.local || {
      base_dir: '',
      base_url: '',
      link_ttl_seconds: 3600,
      sign_secret: '',
    }
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
  }
  if (b.type === 'local_agent') {
    b.local_agent = b.local_agent || {
      base_dir: '',
      public_base_url: '',
      agent_api_url: '',
      link_ttl_seconds: 3600,
      sign_secret: '',
      sync_token: '',
    }
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
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
    b.pan123!.direct_link_mode = b.pan123!.direct_link_mode === 'compose' ? 'compose' : 'api'
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local = undefined
    b.local_agent = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
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
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_cookie'] = undefined
    b['115_sub'] = undefined
    return
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
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_sub'] = undefined
    return
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
    b['115_sub'].selection_strategy = b['115_sub'].selection_strategy === 'user_affinity_rr' ? 'user_affinity_rr' : 'round_robin'
    b['115_sub'].link_mode = b['115_sub'].link_mode === 'relay' ? 'relay' : 'redirect'
    b['115_sub'].user_affinity_enabled = Boolean(b['115_sub'].user_affinity_enabled)
    b['115_sub'].user_single_drive_only = Boolean(b['115_sub'].user_single_drive_only)
    b['115_sub'].disable_primary_fallback = Boolean(b['115_sub'].disable_primary_fallback)
    b['115_sub'].auto_mirror_dir = Boolean(b['115_sub'].auto_mirror_dir)
    b.s3 = undefined
    b.aliyun_cdn = undefined
    b.gdrive = undefined
    b.local = undefined
    b.local_agent = undefined
    b.pan123 = undefined
    b['115_open'] = undefined
    b['115_cookie'] = undefined
  }
}

export function useBackendsPage(message: MessageApi) {
  const { loading, saving, loadError, draft, hasChanges, refresh, resetDraft, saveDraft } = useConfigDraft(message)

  const s3CheckStatus = ref<Record<string, 'unknown' | 'checking' | 'success' | 'error'>>({})
  const cdnTestOpen = ref(false)
  const cdnTestBackendId = ref('')
  const cdnTestKey = ref('')
  const cdnTestUid = ref('')
  const cdnTestLoading = ref(false)
  const cdnTestResult = ref<CdnSignResponse | null>(null)

  const backendOptions = computed(() => {
    const list = draft.value?.backends || []
    return list
      .filter((b) => b.enabled)
      .map((b) => ({ label: b.name ? `${b.name} (${b.id})` : b.id, value: b.id }))
  })

  const backendTypeOptions = [
    { label: 'S3 对象存储', value: 's3' },
    { label: '阿里云 CDN', value: 'aliyun_cdn' },
    { label: 'Google Drive', value: 'gdrive' },
    { label: '本地存储', value: 'local' },
    { label: 'Local Agent 代理', value: 'local_agent' },
    { label: '123 网盘 Open', value: 'pan123' },
    { label: '115 网盘 Open', value: '115_open' },
    { label: '115 网盘 Cookie', value: '115_cookie' },
    { label: '115 分流聚合', value: '115_sub' },
  ]

  const pan123DirectLinkModeOptions = [
    { label: 'API 获取直链', value: 'api' },
    { label: '拼接直链 (UID + objectKey)', value: 'compose' },
  ]
  const open115LinkModeOptions = [
    { label: '302 重定向', value: 'redirect' },
    { label: '网关中转 (Relay)', value: 'relay' },
  ]
  const sub115SelectionStrategyOptions = [
    { label: '全局轮询 (round_robin)', value: 'round_robin' },
    { label: '按用户亲和轮询 (user_affinity_rr)', value: 'user_affinity_rr' },
  ]

  function addBackend(type: BackendConfig['type']) {
    if (!draft.value) return
    const b: BackendConfig = {
      id: genId(backendIdPrefix(type)),
      name: '',
      type,
      enabled: true,
    }
    ensureBackendShape(b)
    draft.value.backends.push(b)
  }

  function addPool() {
    if (!draft.value) return
    const p: ResourcePoolConfig = {
      id: genId('pool'),
      name: '',
      primary_backend_id: '',
      standby_backend_id: '',
    }
    draft.value.resource_pools.push(p)
  }

  async function checkS3Backend(id: string) {
    const backendId = String(id || '')
    if (!backendId) return
    s3CheckStatus.value = { ...s3CheckStatus.value, [backendId]: 'checking' }
    try {
      const resp = await s3Check(backendId)
      s3CheckStatus.value = { ...s3CheckStatus.value, [backendId]: 'success' }
      message.success(resp.message || '连接成功')
    } catch (e) {
      s3CheckStatus.value = { ...s3CheckStatus.value, [backendId]: 'error' }
      message.error(`连接失败: ${(e as Error).message}`)
    }
  }

  function openCdnTest(id: string) {
    cdnTestBackendId.value = String(id || '')
    cdnTestKey.value = ''
    cdnTestUid.value = ''
    cdnTestResult.value = null
    cdnTestOpen.value = true
  }

  async function runCdnTest() {
    const backendId = String(cdnTestBackendId.value || '')
    const key = String(cdnTestKey.value || '').trim()
    if (!backendId) {
      message.error('backend_id 为空')
      return
    }
    if (!key) {
      message.error('对象 key 为空')
      return
    }
    cdnTestLoading.value = true
    try {
      const res = await cdnSign({ backend_id: backendId, key, uid: cdnTestUid.value })
      cdnTestResult.value = res
      message.success('签名成功')
    } catch (e) {
      message.error(`签名失败: ${(e as Error).message}`)
      cdnTestResult.value = null
    } finally {
      cdnTestLoading.value = false
    }
  }

  onMounted(() => {
    refresh()
  })

  watchEffect(() => {
    if (!draft.value) return
    for (const b of draft.value.backends) {
      ensureBackendShape(b)
      ensureBackendIDPrefix(b, draft.value.resource_pools)
    }
  })

  return {
    loading,
    saving,
    loadError,
    draft,
    hasChanges,
    resetDraft,
    saveDraft,
    s3CheckStatus,
    cdnTestOpen,
    cdnTestKey,
    cdnTestUid,
    cdnTestLoading,
    cdnTestResult,
    backendOptions,
    backendTypeOptions,
    pan123DirectLinkModeOptions,
    open115LinkModeOptions,
    sub115SelectionStrategyOptions,
    addBackend,
    addPool,
    checkS3Backend,
    openCdnTest,
    runCdnTest,
  }
}
