<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NCollapse,
  NCollapseItem,
  NDivider,
  NEmpty,
  NForm,
  NFormItem,
  NGrid,
  NGridItem,
  NIcon,
  NInput,
  NInputNumber,
  NSelect,
  NSpace,
  NPopconfirm,
  NTabPane,
  NTabs,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from 'naive-ui'
import {
  AddOutline,
  CheckmarkCircleOutline,
  CloseOutline,
  GlobeOutline,
  LinkOutline,
} from '@vicons/ionicons5'
import { computed, h, ref, watch } from 'vue'

import { ApiError } from '@/api/client'
import { getConfig } from '@/api/config'
import { getCookie115Credential, upsertCookie115Credential } from '@/api/cookie115'
import { deleteSub115Drive, listSub115Drives, upsertSub115Drive, type Sub115DriveItem } from '@/api/sub115'
import { AliyunIcon } from '@/icons/AliyunIcon'
import { CloudStorageIcon } from '@/icons/CloudStorageIcon'
import { GDriveIcon } from '@/icons/GDriveIcon'
import { LocalStorageIcon } from '@/icons/LocalStorageIcon'
import { Pan123Icon } from '@/icons/Pan123Icon'
import { Pan115Icon } from '@/icons/Pan115Icon'
import type { Config } from '@/types'

const props = defineProps<{
  draft: Config
  backendTypeOptions: Array<{ label: string; value: string }>
  pan123DirectLinkModeOptions: Array<{ label: string; value: string }>
  open115LinkModeOptions: Array<{ label: string; value: string }>
  sub115SelectionStrategyOptions: Array<{ label: string; value: string }>
  s3CheckStatus: Record<string, 'unknown' | 'checking' | 'success' | 'error'>
}>()

const emit = defineEmits<{
  (e: 'add-backend'): void
  (e: 'check-s3', id: string): void
  (e: 'open-cdn-test', id: string): void
}>()
const message = useMessage()

const activeBackendId = ref('')

const activeBackend = computed(() => {
  if (props.draft.backends.length === 0) return null
  const idx = props.draft.backends.findIndex((item, index) => backendTabName(item.id, index) === activeBackendId.value)
  if (idx >= 0) return props.draft.backends[idx]
  return props.draft.backends[0]
})

const activeBackendIndex = computed(() => {
  if (!activeBackend.value) return -1
  return props.draft.backends.findIndex((item) => item === activeBackend.value)
})

const open115BackendOptions = computed(() => {
  const currentID = String(activeBackend.value?.id || '')
  return (props.draft.backends || [])
    .filter((b) => b.enabled && (b.type === '115_open' || b.type === '115_cookie') && b.id !== currentID)
    .map((b) => ({ label: b.name ? `${b.name} (${b.id})` : `${b.id} [${b.type}]`, value: b.id }))
})

const cookie115BackendOptions = computed(() => {
  return (props.draft.backends || [])
    .filter((b) => b.type === '115_cookie')
    .map((b) => ({ label: b.name ? `${b.name} (${b.id})` : `${b.id} [115_cookie]`, value: b.id }))
})

watch(
  () => props.draft.backends.map((item, index) => backendTabName(item.id, index)),
  (names, oldNames) => {
    if (names.length === 0) {
      activeBackendId.value = ''
      return
    }
    if (!names.includes(activeBackendId.value)) {
      const prevIndex = oldNames?.indexOf(activeBackendId.value) ?? -1
      if (prevIndex >= 0) {
        activeBackendId.value = names[Math.min(prevIndex, names.length - 1)]
        return
      }
      activeBackendId.value = names[0]
    }
  },
  { immediate: true },
)

function backendTabName(id: string, idx: number) {
  return id?.trim() || `backend-${idx}`
}

function removeActiveBackend() {
  if (activeBackendIndex.value < 0) return
  props.draft.backends.splice(activeBackendIndex.value, 1)
}

function backendTypeIcon(type: string) {
  if (type === 's3') return CloudStorageIcon
  if (type === 'aliyun_cdn') return AliyunIcon
  if (type === 'gdrive') return GDriveIcon
  if (type === 'pan123') return Pan123Icon
  if (type === '115_open') return Pan115Icon
  if (type === '115_cookie') return Pan115Icon
  if (type === '115_sub') return Pan115Icon
  return LocalStorageIcon
}

function renderBackendTypeLabel(option: { label?: string; value?: string }) {
  const type = String(option.value || '')
  const Icon = backendTypeIcon(type)
  return h(
    'div',
    { style: 'display: inline-flex; align-items: center; gap: 8px;' },
    [
      h(Icon, { style: 'width: 16px; height: 16px; flex-shrink: 0;' }),
      h('span', null, String(option.label || '')),
    ],
  )
}

const backendDotColor: Record<string, string> = {
  s3: '#289DEF',
  aliyun_cdn: '#FF8826',
  gdrive: '#18984D',
  local: '#1296db',
  local_agent: '#1296db',
  pan123: '#3C80FF',
  '115_open': '#00A870',
  '115_cookie': '#0B8A6E',
  '115_sub': '#1E9A75',
}

const cookie115Loading = ref(false)
const cookie115Saving = ref(false)
const cookie115Status = ref<{
  has_cookie: boolean
  expires_at: number
  last_error: string
} | null>(null)
const cookie115Input = ref('')
const cookie115ExpiresSeconds = ref(30 * 24 * 3600)
const cookie115UnsavedHint = ref('')

const activeCookie115BackendID = computed(() => {
  const b = activeBackend.value
  if (!b || b.type !== '115_cookie') return ''
  return String(b.id || '').trim()
})

async function loadCookie115Credential() {
  const backendID = activeCookie115BackendID.value
  if (!backendID) {
    cookie115Status.value = null
    cookie115UnsavedHint.value = ''
    return
  }
  const persisted = await isBackendPersisted(backendID, '115_cookie', true)
  if (!persisted) {
    cookie115Status.value = null
    cookie115UnsavedHint.value = '该 115_cookie 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理 Cookie。'
    return
  }
  cookie115UnsavedHint.value = ''
  cookie115Loading.value = true
  try {
    const resp = await getCookie115Credential(backendID)
    cookie115Status.value = {
      has_cookie: Boolean(resp.has_cookie),
      expires_at: Number(resp.expires_at || 0),
      last_error: String(resp.last_error || ''),
    }
    cookie115Input.value = String(resp.cookie || '')
  } catch (e) {
    cookie115Status.value = null
    if (e instanceof ApiError && e.status === 404) {
      cookie115UnsavedHint.value = '该 115_cookie 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理 Cookie。'
      return
    }
    message.error(`读取 Cookie 状态失败: ${(e as Error).message || '未知错误'}`)
  } finally {
    cookie115Loading.value = false
  }
}

async function saveCookie115Credential() {
  const backendID = activeCookie115BackendID.value
  if (!backendID) {
    message.error('当前 115_cookie 后端 ID 无效')
    return
  }
  if (!(await isBackendPersisted(backendID, '115_cookie', false))) {
    cookie115UnsavedHint.value = '该 115_cookie 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理 Cookie。'
    return
  }
  const cookie = String(cookie115Input.value || '').trim()
  if (!cookie) {
    message.error('Cookie 不能为空')
    return
  }
  cookie115Saving.value = true
  try {
    await upsertCookie115Credential({
      backend_id: backendID,
      cookie,
      expires_seconds: Math.max(60, Number(cookie115ExpiresSeconds.value || 60)),
    })
    message.success('主盘 Cookie 已更新')
    await loadCookie115Credential()
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      cookie115UnsavedHint.value = '该 115_cookie 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理 Cookie。'
      return
    }
    message.error(`保存 Cookie 失败: ${(e as Error).message || '未知错误'}`)
  } finally {
    cookie115Saving.value = false
  }
}

const sub115OwnerTypeOptions = [
  { label: '系统盘', value: 'system' },
  { label: '用户盘', value: 'user' },
]
const sub115CredentialModeOptions = [
  { label: '直填 Cookie', value: 'inline' },
  { label: '引用 115_cookie 后端凭据', value: 'backend_ref' },
]

function genSub115DriveID() {
  return `drv_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
}

const sub115IncludeDisabled = ref(true)
const sub115Loading = ref(false)
const sub115Saving = ref(false)
const sub115DeletingDriveID = ref('')
const sub115LoadError = ref('')
const sub115Drives = ref<Sub115DriveItem[]>([])

const sub115EditorOpen = ref(false)
const sub115EditorMode = ref<'create' | 'edit'>('create')
const sub115Editor = ref({
  drive_id: '',
  name: '',
  owner_type: 'system',
  owner_id: '',
  root_folder_id: '0',
  credential_mode: 'inline',
  credential_backend_id: '',
  enabled: true,
  weight: 1,
  priority: 100,
  cookie: '',
})
const sub115CookieTouched = ref(false)

const activeSub115BackendID = computed(() => {
  const b = activeBackend.value
  if (!b || b.type !== '115_sub') return ''
  return String(b.id || '').trim()
})

async function isBackendPersisted(backendID: string, type: string, silent = true) {
  const id = String(backendID || '').trim()
  if (!id) return false
  try {
    const cfg = await getConfig()
    const exists = Array.isArray(cfg.backends) && cfg.backends.some((item) => {
      const itemID = String(item.id || '').trim()
      const itemType = String(item.type || '').trim().toLowerCase()
      return itemID === id && itemType === String(type || '').trim().toLowerCase()
    })
    if (!exists && !silent) {
      message.warning('该后端尚未保存到服务端，请先点击页面底部“保存配置”')
    }
    return exists
  } catch {
    return true
  }
}

function resetSub115Editor() {
  sub115Editor.value = {
    drive_id: genSub115DriveID(),
    name: '',
    owner_type: 'system',
    owner_id: '',
    root_folder_id: '0',
    credential_mode: 'inline',
    credential_backend_id: '',
    enabled: true,
    weight: 1,
    priority: 100,
    cookie: '',
  }
  sub115CookieTouched.value = false
}

function openCreateSub115Drive() {
  sub115EditorMode.value = 'create'
  resetSub115Editor()
  sub115EditorOpen.value = true
}

function openEditSub115Drive(item: Sub115DriveItem) {
  sub115EditorMode.value = 'edit'
  sub115Editor.value = {
    drive_id: String(item.drive_id || ''),
    name: String(item.name || ''),
    owner_type: String(item.owner_type || 'system') === 'user' ? 'user' : 'system',
    owner_id: String(item.owner_id || ''),
    root_folder_id: String(item.root_folder_id || '0'),
    credential_mode: String(item.credential_mode || 'inline') === 'backend_ref' ? 'backend_ref' : 'inline',
    credential_backend_id: String(item.credential_backend_id || ''),
    enabled: Boolean(item.enabled),
    weight: Number(item.weight || 1),
    priority: Number(item.priority ?? 100),
    cookie: '',
  }
  sub115CookieTouched.value = false
  sub115EditorOpen.value = true
}

async function loadSub115Drives() {
  const backendID = activeSub115BackendID.value
  if (!backendID) {
    sub115Drives.value = []
    sub115LoadError.value = ''
    return
  }
  if (!(await isBackendPersisted(backendID, '115_sub', true))) {
    sub115Drives.value = []
    sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
    return
  }
  sub115Loading.value = true
  sub115LoadError.value = ''
  try {
    const resp = await listSub115Drives(backendID, sub115IncludeDisabled.value)
    sub115Drives.value = Array.isArray(resp.items) ? resp.items : []
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
      sub115Drives.value = []
      return
    }
    sub115LoadError.value = (e as Error).message || '加载附盘失败'
    sub115Drives.value = []
  } finally {
    sub115Loading.value = false
  }
}

async function saveSub115Drive() {
  const backendID = activeSub115BackendID.value
  if (!backendID) {
    message.error('当前 115_sub 后端 ID 无效')
    return
  }
  if (!(await isBackendPersisted(backendID, '115_sub', false))) {
    sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
    return
  }
  const driveID = String(sub115Editor.value.drive_id || '').trim()
  if (!driveID) {
    message.error('drive_id 不能为空')
    return
  }
  const ownerType = sub115Editor.value.owner_type === 'user' ? 'user' : 'system'
  const ownerID = String(sub115Editor.value.owner_id || '').trim()
  if (ownerType === 'user' && !ownerID) {
    message.error('用户盘必须填写 owner_id')
    return
  }
  const credentialMode = sub115Editor.value.credential_mode === 'backend_ref' ? 'backend_ref' : 'inline'
  const credentialBackendID = String(sub115Editor.value.credential_backend_id || '').trim()
  if (credentialMode === 'backend_ref' && !credentialBackendID) {
    message.error('引用凭据模式下必须选择 115_cookie 后端')
    return
  }
  const payload: {
    backend_id: string
    drive_id: string
    name?: string
    owner_type?: string
    owner_id?: string
    root_folder_id?: string
    credential_mode?: string
    credential_backend_id?: string
    enabled?: boolean
    weight?: number
    priority?: number
    cookie?: string
  } = {
    backend_id: backendID,
    drive_id: driveID,
    name: String(sub115Editor.value.name || '').trim(),
    owner_type: ownerType,
    owner_id: ownerType === 'user' ? ownerID : '',
    root_folder_id: String(sub115Editor.value.root_folder_id || '0').trim() || '0',
    credential_mode: credentialMode,
    credential_backend_id: credentialMode === 'backend_ref' ? credentialBackendID : '',
    enabled: Boolean(sub115Editor.value.enabled),
    weight: Math.max(1, Number(sub115Editor.value.weight || 1)),
    priority: Math.max(0, Number(sub115Editor.value.priority || 0)),
  }
  if (payload.credential_mode === 'inline' && sub115CookieTouched.value) {
    payload.cookie = String(sub115Editor.value.cookie || '').trim()
  }

  sub115Saving.value = true
  try {
    await upsertSub115Drive(payload)
    message.success(sub115EditorMode.value === 'create' ? '附盘已新增' : '附盘已更新')
    sub115EditorOpen.value = false
    await loadSub115Drives()
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
      return
    }
    message.error(`保存失败: ${(e as Error).message || '未知错误'}`)
  } finally {
    sub115Saving.value = false
  }
}

async function removeSub115DriveByID(driveID: string) {
  const backendID = activeSub115BackendID.value
  const d = String(driveID || '').trim()
  if (!backendID || !d) return
  if (!(await isBackendPersisted(backendID, '115_sub', false))) {
    sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
    return
  }
  sub115DeletingDriveID.value = d
  try {
    await deleteSub115Drive(backendID, d)
    message.success('附盘已删除')
    await loadSub115Drives()
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      sub115LoadError.value = '该 115_sub 后端尚未保存到服务端。请先点击页面底部“保存配置”，再管理附盘。'
      return
    }
    message.error(`删除失败: ${(e as Error).message || '未知错误'}`)
  } finally {
    sub115DeletingDriveID.value = ''
  }
}

watch(
  () => [activeSub115BackendID.value, sub115IncludeDisabled.value],
  () => {
    loadSub115Drives()
  },
  { immediate: true },
)

watch(
  () => activeCookie115BackendID.value,
  () => {
    cookie115Input.value = ''
    loadCookie115Credential()
  },
  { immediate: true },
)

watch(
  () => activeSub115BackendID.value,
  () => {
    sub115EditorOpen.value = false
    sub115CookieTouched.value = false
  },
)


watch(
  () => sub115Editor.value.credential_mode,
  (mode) => {
    if (mode === 'backend_ref') {
      sub115CookieTouched.value = false
      sub115Editor.value.cookie = ''
    }
  },
)
</script>

<template>
  <n-card :bordered="false" class="glass-card section-card" title="存储后端 (Backends)">
    <template #header-extra>
      <n-button size="small" secondary @click="emit('add-backend')">
        <template #icon><n-icon><AddOutline /></n-icon></template>
        新增
      </n-button>
    </template>

    <n-space v-if="draft.backends.length === 0" vertical :size="8">
      <n-empty description="暂无 Backend 配置" />
    </n-space>

    <div v-else class="backend-tabs-layout">
      <n-tabs v-model:value="activeBackendId" type="card" animated class="backend-tabs" placement="top">
        <n-tab-pane
          v-for="(b, idx) in draft.backends"
          :key="b.id || idx"
          :name="backendTabName(b.id, idx)"
        >
          <template #tab>
            <span class="tab-dot" :style="{ background: backendDotColor[b.type] || '#94a3b8' }" />
            <span class="tab-label-text">{{ b.name?.trim() || '未命名' }}</span>
            <n-tag v-if="!b.enabled" size="tiny" :bordered="false" type="warning">停用</n-tag>
          </template>
        </n-tab-pane>
      </n-tabs>

      <n-card
        v-if="activeBackend"
        size="small"
        :bordered="false"
        class="glass-card backend-card"
      >
        <template #header>
          <n-space align="center" :size="12">
            <div class="icon-box" :class="activeBackend.type">
              <component
                :is="activeBackend.type === 's3' ? CloudStorageIcon
                  : activeBackend.type === 'aliyun_cdn' ? AliyunIcon
                  : activeBackend.type === 'gdrive' ? GDriveIcon
                  : activeBackend.type === 'pan123' ? Pan123Icon
                  : activeBackend.type === '115_open' ? Pan115Icon
                  : activeBackend.type === '115_cookie' ? Pan115Icon
                  : activeBackend.type === '115_sub' ? Pan115Icon
                  : LocalStorageIcon"
                style="width: 20px; height: 20px"
              />
            </div>
            <div class="header-info">
              <n-input v-model:value="activeBackend.name" placeholder="名称" size="small" class="name-input" />
              <n-text depth="3" class="id-text">{{ activeBackend.id }}</n-text>
            </div>
          </n-space>
        </template>
        <template #header-extra>
          <n-space align="center" :size="8" class="card-actions">
            <n-switch v-model:value="activeBackend.enabled" size="small" />
            <n-divider vertical />
            <n-button quaternary circle size="small" type="error" @click="removeActiveBackend">
              <template #icon><n-icon><CloseOutline /></n-icon></template>
            </n-button>
          </n-space>
        </template>

        <n-form label-placement="left" label-width="150" size="small" class="backend-body backend-compact-form">
          <n-form-item label="类型" :show-label="false">
            <n-select
              v-model:value="activeBackend.type"
              :options="backendTypeOptions"
              :render-label="renderBackendTypeLabel"
              size="small"
            />
          </n-form-item>

          <div v-if="activeBackend.type === 's3' && activeBackend.s3">
            <n-collapse :default-expanded-names="['s3-basic']" display-directive="show">
              <n-collapse-item title="基本连接" name="s3-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Endpoint">
                      <n-input v-model:value="activeBackend.s3.endpoint" placeholder="https://oss-cn-shanghai.aliyuncs.com" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="Region">
                      <n-input v-model:value="activeBackend.s3.region" placeholder="auto" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="Bucket">
                      <n-input v-model:value="activeBackend.s3.bucket" placeholder="bucket-name" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="凭证与高级设置" name="s3-advanced">
                <n-form-item label="Access Key">
                  <n-input v-model:value="activeBackend.s3.access_key" placeholder="AK..." />
                </n-form-item>
                <n-form-item label="Secret Key">
                  <n-input v-model:value="activeBackend.s3.secret_key" type="password" show-password-on="click" placeholder="SK..." />
                </n-form-item>
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="签名有效期 (分)">
                      <n-input-number v-model:value="activeBackend.s3.sign_expiry_minutes" :min="1" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="Path Style">
                      <n-switch v-model:value="activeBackend.s3.force_path_style" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
                <n-form-item label="Key Prefix">
                  <n-input v-model:value="activeBackend.s3.key_prefix" placeholder="可选前缀" />
                </n-form-item>
              </n-collapse-item>
            </n-collapse>

            <div class="action-row">
              <n-button size="small" secondary block :loading="s3CheckStatus[activeBackend.id] === 'checking'" @click="emit('check-s3', activeBackend.id)">
                <template #icon>
                  <n-icon v-if="s3CheckStatus[activeBackend.id] === 'success'" color="var(--app-success)"><CheckmarkCircleOutline /></n-icon>
                  <n-icon v-else><LinkOutline /></n-icon>
                </template>
                {{ s3CheckStatus[activeBackend.id] === 'success' ? '连接成功' : s3CheckStatus[activeBackend.id] === 'error' ? '连接失败' : '连接测试' }}
              </n-button>
            </div>
          </div>

          <div v-else-if="activeBackend.type === 'aliyun_cdn' && activeBackend.aliyun_cdn">
            <n-collapse :default-expanded-names="['cdn-basic']" display-directive="show">
              <n-collapse-item title="基本配置" name="cdn-basic">
                <n-form-item label="Base URL">
                  <n-input v-model:value="activeBackend.aliyun_cdn.base_url" placeholder="https://cdn.example.com" />
                </n-form-item>
              </n-collapse-item>
              <n-collapse-item title="鉴权设置 (Type A)" name="cdn-auth">
                <n-form-item label="启用鉴权">
                  <n-switch v-model:value="activeBackend.aliyun_cdn.auth.enabled" />
                </n-form-item>
                <n-form-item label="鉴权密钥 (Secret)">
                  <n-input v-model:value="activeBackend.aliyun_cdn.auth.secret" type="password" show-password-on="click" />
                </n-form-item>
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="过期时间 (秒)">
                      <n-input-number v-model:value="activeBackend.aliyun_cdn.auth.expires_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="参数名">
                      <n-input v-model:value="activeBackend.aliyun_cdn.auth.param_name" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
                <n-form-item label="路径转义">
                  <n-switch v-model:value="activeBackend.aliyun_cdn.path_escape" />
                </n-form-item>
              </n-collapse-item>
            </n-collapse>

            <div class="action-row">
              <n-button size="small" secondary block @click="emit('open-cdn-test', activeBackend.id)">
                <template #icon><n-icon><GlobeOutline /></n-icon></template>
                生成签名测试
              </n-button>
            </div>
          </div>

          <div v-else-if="activeBackend.type === 'gdrive' && activeBackend.gdrive">
            <n-alert type="info" :show-icon="false" class="section-tip">
              GDrive 使用 302 跳转，默认代理地址避免 Google 病毒扫描拦截。启用 Worker 后改用全局 GDrive Worker URL。共享盘请填 Drive ID。
            </n-alert>
            <n-collapse :default-expanded-names="['gdrive-oauth']" display-directive="show">
              <n-collapse-item title="OAuth 凭证" name="gdrive-oauth">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Client ID">
                      <n-input v-model:value="activeBackend.gdrive.client_id" placeholder="Google OAuth Client ID" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Client Secret">
                      <n-input v-model:value="activeBackend.gdrive.client_secret" type="password" show-password-on="click" placeholder="Google OAuth Client Secret" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Refresh Token">
                      <n-input v-model:value="activeBackend.gdrive.refresh_token" type="password" show-password-on="click" placeholder="Google OAuth Refresh Token" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="高级设置" name="gdrive-advanced">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="启用 Worker 代理">
                      <n-space align="center">
                        <n-switch v-model:value="activeBackend.gdrive.use_worker" />
                        <n-text depth="3">开启后使用全局 GDrive Worker Base URL 进行 302 跳转</n-text>
                      </n-space>
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Base URL (可选)">
                      <n-input v-model:value="activeBackend.gdrive.base_url" placeholder="https://example.com" />
                      <template #feedback>
                        <n-text depth="3">仅在未启用 Worker 代理时生效</n-text>
                      </template>
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="共享盘 Drive ID (可选)">
                      <n-input v-model:value="activeBackend.gdrive.drive_id" placeholder="例如：0AGW1q8hHv29tUk9PVA" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="共享盘">
                      <n-switch v-model:value="activeBackend.gdrive.include_all_drives" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="链接有效期 (秒)">
                      <n-input-number v-model:value="activeBackend.gdrive.link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="路径缓存">
                      <n-switch v-model:value="activeBackend.gdrive.cache_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="缓存时间 (秒)">
                      <n-input-number v-model:value="activeBackend.gdrive.cache_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === 'local' && activeBackend.local">
            <n-alert type="info" :show-icon="false" class="section-tip">
              本地存储使用 302 跳转，token 加密授权，不暴露本地目录结构。
            </n-alert>
            <n-collapse :default-expanded-names="['local-basic']" display-directive="show">
              <n-collapse-item title="基本配置" name="local-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Base Dir">
                      <n-input v-model:value="activeBackend.local.base_dir" placeholder="/mnt" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Base URL (可选)">
                      <n-input v-model:value="activeBackend.local.base_url" placeholder="https://example.com" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="安全设置" name="local-security">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="链接有效期 (秒)">
                      <n-input-number v-model:value="activeBackend.local.link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="签名密钥">
                      <n-input v-model:value="activeBackend.local.sign_secret" type="password" show-password-on="click" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === 'local_agent' && activeBackend.local_agent">
            <n-alert type="info" :show-icon="false" class="section-tip">
              local-agent 场景：网关(A机)签发链接，存储(B机)直接服务客户端。
            </n-alert>
            <n-collapse :default-expanded-names="['la-basic']" display-directive="show">
              <n-collapse-item title="基本配置" name="la-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Base Dir (B 机目录)">
                      <n-input v-model:value="activeBackend.local_agent.base_dir" placeholder="/mnt/media" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Public Base URL (给客户端)">
                      <n-input v-model:value="activeBackend.local_agent.public_base_url" placeholder="https://storage-b.example.com" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Agent API URL (网关访问代理)">
                      <n-input v-model:value="activeBackend.local_agent.agent_api_url" placeholder="http://10.0.0.12:19090" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="安全设置" name="la-security">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="链接有效期 (秒)">
                      <n-input-number v-model:value="activeBackend.local_agent.link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="签名密钥">
                      <n-input v-model:value="activeBackend.local_agent.sign_secret" type="password" show-password-on="click" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Sync Token">
                      <n-input v-model:value="activeBackend.local_agent.sync_token" type="password" show-password-on="click" placeholder="与 B 机启动参数一致" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === 'pan123' && activeBackend.pan123">
            <n-alert type="info" :show-icon="false" class="section-tip">
              123 Open 通过 API 获取直链并返回 302。可切换"拼接直链"模式减少 API 调用，建议开启 URL 鉴权。
            </n-alert>
            <n-collapse :default-expanded-names="['pan-basic']" display-directive="show">
              <n-collapse-item title="基本配置" name="pan-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Client ID">
                      <n-input v-model:value="activeBackend.pan123.client_id" placeholder="123 Open Client ID" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Client Secret">
                      <n-input v-model:value="activeBackend.pan123.client_secret" type="password" show-password-on="click" placeholder="123 Open Client Secret" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="Root Folder ID">
                      <n-input v-model:value="activeBackend.pan123.root_folder_id" placeholder="0" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="直链模式">
                      <n-select v-model:value="activeBackend.pan123.direct_link_mode" :options="pan123DirectLinkModeOptions" />
                    </n-form-item>
                  </n-grid-item>
                  <template v-if="activeBackend.pan123.direct_link_mode === 'compose'">
                    <n-grid-item span="2">
                      <n-form-item label="自定义域名（含协议，可选）">
                        <n-input v-model:value="activeBackend.pan123.compose_base_url" placeholder="http://downxxxxx.com" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="隐藏 UID">
                        <n-switch
                          v-model:value="activeBackend.pan123.compose_hide_uid"
                          :disabled="!String(activeBackend.pan123.compose_base_url || '').trim() && !activeBackend.pan123.compose_hide_uid"
                        />
                      </n-form-item>
                    </n-grid-item>
                  </template>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="URL 鉴权" name="pan-auth">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="启用 URL 鉴权">
                      <n-switch v-model:value="activeBackend.pan123.sign_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="UID">
                      <n-input v-model:value="activeBackend.pan123.uid" placeholder="123 账号 UID" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="签名有效期 (分钟)">
                      <n-input-number v-model:value="activeBackend.pan123.valid_duration_minutes" :min="1" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Private Key">
                      <n-input v-model:value="activeBackend.pan123.private_key" type="password" show-password-on="click" placeholder="URL 鉴权密钥" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="缓存设置" name="pan-cache">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="302 缓存有效期 (秒)">
                      <n-input-number v-model:value="activeBackend.pan123.link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="映射缓存">
                      <n-switch v-model:value="activeBackend.pan123.cache_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="objectKey→fileID 映射缓存时间 (秒)">
                      <n-input-number v-model:value="activeBackend.pan123.cache_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === '115_open' && activeBackend['115_open']">
            <n-alert type="info" :show-icon="false" class="section-tip">
              115 Open 默认走 302，若客户端不兼容可切换为 Relay 中转模式（网关会携带 Referer 访问 115）。
            </n-alert>
            <n-collapse :default-expanded-names="['open115-basic']" display-directive="show">
              <n-collapse-item title="鉴权配置" name="open115-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Access Token">
                      <n-input v-model:value="activeBackend['115_open'].access_token" type="password" show-password-on="click" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2">
                    <n-form-item label="Refresh Token">
                      <n-input v-model:value="activeBackend['115_open'].refresh_token" type="password" show-password-on="click" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="Root Folder ID">
                      <n-input v-model:value="activeBackend['115_open'].root_folder_id" placeholder="0" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="链路模式">
                      <n-select v-model:value="activeBackend['115_open'].link_mode" :options="open115LinkModeOptions" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="列表与缓存" name="open115-advanced">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="排序字段">
                      <n-input v-model:value="activeBackend['115_open'].order_by" placeholder="file_name" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="排序方向">
                      <n-select
                        v-model:value="activeBackend['115_open'].order_direction"
                        :options="[
                          { label: '升序', value: 'asc' },
                          { label: '降序', value: 'desc' },
                        ]"
                      />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="分页大小">
                      <n-input-number v-model:value="activeBackend['115_open'].custom_page_size" :min="1" :max="1000" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="302 缓存有效期 (秒)">
                      <n-input-number v-model:value="activeBackend['115_open'].link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="路径缓存">
                      <n-switch v-model:value="activeBackend['115_open'].cache_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="缓存时间 (秒)">
                      <n-input-number v-model:value="activeBackend['115_open'].cache_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === '115_cookie' && activeBackend['115_cookie']">
            <n-alert type="info" :show-icon="false" class="section-tip">
              115 Cookie 主盘：目录、元信息与直链全部通过第三方 Cookie API 获取。Cookie 凭据独立存库，不跟随 revision 保存。
            </n-alert>
            <n-collapse :default-expanded-names="['cookie115-basic']" display-directive="show">
              <n-collapse-item title="基础配置" name="cookie115-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="Root Folder ID">
                      <n-input v-model:value="activeBackend['115_cookie'].root_folder_id" placeholder="0" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="链路模式">
                      <n-select v-model:value="activeBackend['115_cookie'].link_mode" :options="open115LinkModeOptions" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="排序字段">
                      <n-input v-model:value="activeBackend['115_cookie'].order_by" placeholder="file_name" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="排序方向">
                      <n-select
                        v-model:value="activeBackend['115_cookie'].order_direction"
                        :options="[
                          { label: '升序', value: 'asc' },
                          { label: '降序', value: 'desc' },
                        ]"
                      />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="分页大小">
                      <n-input-number v-model:value="activeBackend['115_cookie'].custom_page_size" :min="1" :max="1000" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="302 缓存有效期 (秒)">
                      <n-input-number v-model:value="activeBackend['115_cookie'].link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="路径缓存" name="cookie115-cache">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="启用缓存">
                      <n-switch v-model:value="activeBackend['115_cookie'].cache_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="缓存时间 (秒)">
                      <n-input-number v-model:value="activeBackend['115_cookie'].cache_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="Cookie 凭据" name="cookie115-credential">
                <n-alert v-if="cookie115UnsavedHint" type="warning" :show-icon="false" class="section-tip">
                  {{ cookie115UnsavedHint }}
                </n-alert>
                <n-space justify="space-between" align="center" class="sub115-toolbar">
                  <n-text depth="3">
                    当前状态:
                    <n-tag size="tiny" :type="cookie115Status?.has_cookie ? 'success' : 'error'">
                      {{ cookie115Status?.has_cookie ? '已配置' : '未配置' }}
                    </n-tag>
                  </n-text>
                  <n-button size="small" secondary :loading="cookie115Loading" @click="loadCookie115Credential">刷新状态</n-button>
                </n-space>
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="Cookie">
                      <n-input
                        v-model:value="cookie115Input"
                        type="password"
                        show-password-on="click"
                        placeholder="UID=...; CID=...; SEID=..."
                      />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="过期秒数 (元数据)">
                      <n-input-number v-model:value="cookie115ExpiresSeconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="预计过期时间">
                      <n-text depth="3">
                        {{ cookie115Status?.expires_at ? new Date(cookie115Status.expires_at * 1000).toLocaleString() : '-' }}
                      </n-text>
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
                <n-alert v-if="cookie115Status?.last_error" type="warning" :show-icon="false" class="sub115-drive-error">
                  最近错误: {{ cookie115Status.last_error }}
                </n-alert>
                <n-space justify="end">
                  <n-button size="small" type="primary" :loading="cookie115Saving" @click="saveCookie115Credential">保存 Cookie</n-button>
                </n-space>
              </n-collapse-item>
            </n-collapse>
          </div>

          <div v-else-if="activeBackend.type === '115_sub' && activeBackend['115_sub']">
            <n-alert type="info" :show-icon="false" class="section-tip">
              115 分流聚合: 主盘用于媒体目录与兜底，附盘通过独立 API 动态维护（不跟随配置 revision）。
            </n-alert>
            <n-collapse :default-expanded-names="['sub115-basic']" display-directive="show">
              <n-collapse-item title="基础策略" name="sub115-basic">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item span="2">
                    <n-form-item label="主盘后端 (115_open/115_cookie)">
                      <n-select
                        v-model:value="activeBackend['115_sub'].primary_backend_id"
                        :options="open115BackendOptions"
                        placeholder="选择一个主盘后端"
                      />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="选择策略">
                      <n-select
                        v-model:value="activeBackend['115_sub'].selection_strategy"
                        :options="sub115SelectionStrategyOptions"
                      />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="用户亲和">
                      <n-switch v-model:value="activeBackend['115_sub'].user_affinity_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="用户单盘绑定">
                      <n-switch v-model:value="activeBackend['115_sub'].user_single_drive_only" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="关闭主盘兜底">
                      <n-switch v-model:value="activeBackend['115_sub'].disable_primary_fallback" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="链路模式">
                      <n-select v-model:value="activeBackend['115_sub'].link_mode" :options="open115LinkModeOptions" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="302 缓存有效期 (秒)">
                      <n-input-number v-model:value="activeBackend['115_sub'].link_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="镜像与缓存" name="sub115-advanced">
                <n-grid cols="2" x-gap="12">
                  <n-grid-item>
                    <n-form-item label="自动镜像目录">
                      <n-switch v-model:value="activeBackend['115_sub'].auto_mirror_dir" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="映射缓存">
                      <n-switch v-model:value="activeBackend['115_sub'].cache_enabled" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item span="2" v-if="activeBackend['115_sub'].auto_mirror_dir">
                    <n-form-item label="镜像目录前缀">
                      <n-input v-model:value="activeBackend['115_sub'].mirror_dir_prefix" placeholder="emby_cache" />
                    </n-form-item>
                  </n-grid-item>
                  <n-grid-item>
                    <n-form-item label="映射缓存时间 (秒)">
                      <n-input-number v-model:value="activeBackend['115_sub'].cache_ttl_seconds" :min="60" />
                    </n-form-item>
                  </n-grid-item>
                </n-grid>
              </n-collapse-item>
              <n-collapse-item title="附盘池管理 (动态)" name="sub115-drives">
                <n-alert type="warning" :show-icon="false" class="section-tip">
                  附盘配置通过独立 API 存库，不随 revision 保存。若当前 `115_sub` 尚未保存到服务端，请先点击页面底部“保存配置”。
                </n-alert>

                <n-space justify="space-between" align="center" class="sub115-toolbar">
                  <n-space align="center">
                    <n-button size="small" secondary :loading="sub115Loading" @click="loadSub115Drives">刷新</n-button>
                    <n-space align="center" :size="6">
                      <n-text depth="3">显示停用</n-text>
                      <n-switch v-model:value="sub115IncludeDisabled" size="small" />
                    </n-space>
                  </n-space>
                  <n-button size="small" type="primary" secondary @click="openCreateSub115Drive">新增附盘</n-button>
                </n-space>

                <n-card v-if="sub115EditorOpen" size="small" class="sub115-editor-card" :bordered="false">
                  <template #header>
                    {{ sub115EditorMode === 'create' ? '新增附盘' : `编辑附盘 ${sub115Editor.drive_id}` }}
                  </template>
                  <n-grid cols="2" x-gap="12">
                    <n-grid-item>
                      <n-form-item label="Drive ID">
                        <n-input
                          v-model:value="sub115Editor.drive_id"
                          placeholder="自动生成"
                          disabled
                        />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="显示名称">
                        <n-input v-model:value="sub115Editor.name" placeholder="可选" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="盘归属">
                        <n-select v-model:value="sub115Editor.owner_type" :options="sub115OwnerTypeOptions" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item v-if="sub115Editor.owner_type === 'user'">
                      <n-form-item label="Owner ID">
                        <n-input v-model:value="sub115Editor.owner_id" placeholder="Emby 用户 ID" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="Root Folder ID">
                        <n-input v-model:value="sub115Editor.root_folder_id" placeholder="0" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item span="2">
                      <n-form-item label="凭据来源">
                        <n-select v-model:value="sub115Editor.credential_mode" :options="sub115CredentialModeOptions" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item v-if="sub115Editor.credential_mode === 'backend_ref'" span="2">
                      <n-form-item label="凭据后端 (115_cookie)">
                        <n-select
                          v-model:value="sub115Editor.credential_backend_id"
                          :options="cookie115BackendOptions"
                          placeholder="选择一个 115_cookie 后端"
                        />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="启用">
                        <n-switch v-model:value="sub115Editor.enabled" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="Weight">
                        <n-input-number v-model:value="sub115Editor.weight" :min="1" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item>
                      <n-form-item label="Priority">
                        <n-input-number v-model:value="sub115Editor.priority" :min="0" />
                      </n-form-item>
                    </n-grid-item>
                    <n-grid-item v-if="sub115Editor.credential_mode === 'inline'" span="2">
                      <n-form-item label="Cookie">
                        <n-input
                          v-model:value="sub115Editor.cookie"
                          type="password"
                          show-password-on="click"
                          placeholder="留空则不改；如果要清空，请先输入任意字符再删空后保存"
                          @update:value="sub115CookieTouched = true"
                        />
                      </n-form-item>
                    </n-grid-item>
                  </n-grid>
                  <n-space justify="end">
                    <n-button size="small" @click="sub115EditorOpen = false">取消</n-button>
                    <n-button size="small" type="primary" :loading="sub115Saving" @click="saveSub115Drive">保存</n-button>
                  </n-space>
                </n-card>

                <n-alert v-if="sub115LoadError" type="error" :show-icon="false" class="sub115-load-error">
                  {{ sub115LoadError }}
                </n-alert>

                <n-empty
                  v-if="!sub115Loading && !sub115LoadError && sub115Drives.length === 0"
                  description="暂无附盘，点击“新增附盘”创建"
                />
                <n-text v-else-if="sub115Loading" depth="3">附盘列表加载中...</n-text>

                <n-space v-else vertical :size="8" class="sub115-drive-list">
                  <n-card
                    v-for="item in sub115Drives"
                    :key="item.drive_id"
                    size="small"
                    :bordered="false"
                    class="sub115-drive-card"
                  >
                    <template #header>
                      <n-space align="center" :size="6">
                        <n-text strong>{{ item.name || item.drive_id }}</n-text>
                        <n-tag size="tiny">{{ item.drive_id }}</n-tag>
                        <n-tag size="tiny" :type="item.owner_type === 'user' ? 'info' : 'default'">
                          {{ item.owner_type === 'user' ? `用户盘:${item.owner_id || '-'}` : '系统盘' }}
                        </n-tag>
                        <n-tag size="tiny" :type="item.credential_mode === 'backend_ref' ? 'info' : 'default'">
                          {{ item.credential_mode === 'backend_ref' ? `引用凭据:${item.credential_backend_id || '-'}` : '直填凭据' }}
                        </n-tag>
                        <n-tag size="tiny" :type="item.enabled ? 'success' : 'warning'">{{ item.enabled ? '启用' : '停用' }}</n-tag>
                        <n-tag size="tiny" :type="item.has_cookie ? 'success' : 'error'">
                          {{ item.has_cookie ? 'Cookie 已配置' : 'Cookie 缺失' }}
                        </n-tag>
                      </n-space>
                    </template>
                    <template #header-extra>
                      <n-space :size="6">
                        <n-button size="tiny" tertiary @click="openEditSub115Drive(item)">编辑</n-button>
                        <n-popconfirm @positive-click="removeSub115DriveByID(item.drive_id)">
                          <template #trigger>
                            <n-button
                              size="tiny"
                              tertiary
                              type="error"
                              :loading="sub115DeletingDriveID === item.drive_id"
                            >
                              删除
                            </n-button>
                          </template>
                          确认删除该附盘？
                        </n-popconfirm>
                      </n-space>
                    </template>

                    <n-grid cols="2" x-gap="12">
                      <n-grid-item>
                        <n-text depth="3">Root Folder ID: {{ item.root_folder_id || '0' }}</n-text>
                      </n-grid-item>
                      <n-grid-item>
                        <n-text depth="3">Weight/Priority: {{ item.weight }} / {{ item.priority }}</n-text>
                      </n-grid-item>
                    </n-grid>
                    <n-alert
                      v-if="item.drive_last_error || item.credential_last_error"
                      type="warning"
                      :show-icon="false"
                      class="sub115-drive-error"
                    >
                      <div v-if="item.drive_last_error">盘错误: {{ item.drive_last_error }}</div>
                      <div v-if="item.credential_last_error">凭据错误: {{ item.credential_last_error }}</div>
                    </n-alert>
                  </n-card>
                </n-space>
              </n-collapse-item>
            </n-collapse>
          </div>
        </n-form>
      </n-card>
    </div>
  </n-card>
</template>

<style scoped>
.backend-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
  transition: all 0.2s ease;
}

.backend-card:hover {
  border-color: var(--app-border-hover);
  box-shadow: var(--app-shadow-card);
}

.backend-tabs-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.backend-tabs {
  margin-top: -4px;
}

.backend-tabs :deep(.n-tabs-nav-scroll-content) {
  gap: 4px;
}

.backend-tabs :deep(.n-tabs-tab) {
  max-width: 280px;
  border-radius: 8px !important;
  padding: 6px 14px !important;
  transition: all 0.2s ease;
  border: 1px solid transparent !important;
}

.backend-tabs :deep(.n-tabs-tab--active) {
  background: var(--app-primary-alpha, rgba(99, 102, 241, 0.08)) !important;
  border-color: var(--app-primary-border, rgba(99, 102, 241, 0.2)) !important;
}

.backend-tabs :deep(.n-tabs-tab:hover:not(.n-tabs-tab--active)) {
  background: var(--app-surface-1);
}

.backend-tabs :deep(.n-tabs-tab__label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tab-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  flex-shrink: 0;
}

.tab-label-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 160px;
  display: inline-block;
  vertical-align: middle;
}

.icon-box {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  font-size: 18px;
  background: var(--c-slate-100);
  color: var(--c-slate-500);
}

.app-dark .icon-box {
  background: var(--c-slate-800);
}

.icon-box.s3 {
  background: rgba(40, 157, 239, 0.1);
}

.icon-box.aliyun_cdn {
  background: rgba(255, 136, 38, 0.1);
}

.icon-box.gdrive {
  background: rgba(24, 152, 77, 0.1);
}

.icon-box.local {
  background: rgba(18, 150, 219, 0.1);
}

.icon-box.local_agent {
  background: rgba(18, 150, 219, 0.1);
}

.icon-box.pan123 {
  background: rgba(60, 128, 255, 0.1);
}

.header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.id-text {
  font-family: monospace;
  font-size: 11px;
  opacity: 0.5;
}

.backend-body {
  padding-top: 4px;
}

.backend-compact-form :deep(.n-form-item) {
  margin-bottom: 10px;
}

.backend-compact-form :deep(.n-form-item-label) {
  font-size: 12px;
}

.section-tip {
  margin-bottom: 12px;
  font-size: 12px;
  line-height: 1.5;
}

.action-row {
  margin-top: 12px;
}

.sub115-toolbar {
  margin-bottom: 10px;
}

.sub115-editor-card {
  margin-bottom: 10px;
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
}

.sub115-load-error {
  margin-bottom: 10px;
}

.sub115-drive-list {
  margin-top: 8px;
}

.sub115-drive-card {
  background: var(--app-surface-1);
  border: 1px solid var(--app-border);
}

.sub115-drive-error {
  margin-top: 8px;
}

.name-input {
  width: clamp(120px, 30vw, 220px);
}

@media (max-width: 768px) {
  .header-info {
    min-width: 0;
  }

  .name-input {
    width: min(52vw, 220px);
  }

  .card-actions {
    flex-wrap: nowrap;
    justify-content: flex-end;
  }

  .backend-card :deep(.n-card-header__main) {
    min-width: 0;
  }

  .backend-card :deep(.n-form-item) {
    margin-bottom: 12px;
  }

  .backend-compact-form :deep(.n-form-item-label) {
    width: 110px !important;
  }

  .backend-tabs :deep(.n-tabs-tab) {
    max-width: 220px;
  }
}
</style>
