<script setup lang="ts">
import {
  NButton,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NSpace,
  NTag,
  useMessage,
} from 'naive-ui'
import { LockClosedOutline, LockOpenOutline, CheckmarkCircleOutline, CloseCircleOutline } from '@vicons/ionicons5'
import { computed, ref, watch } from 'vue'
import { requestJson } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  backendId: string
  backendName: string
  hasPassword: boolean
}>()

const emit = defineEmits<{
  (e: 'unlocked'): void
}>()

const auth = useAuthStore()
const message = useMessage()

const loading = ref(false)
const saving = ref(false)
const verifying = ref(false)

const passwordStatus = ref<{
  has_password: boolean
  updated_at: string
} | null>(null)

const newPassword = ref('')
const currentPasswordForUpdate = ref('')
const unlockPassword = ref('')
const deletePassword = ref('')
const activeAction = ref<'unlock' | 'edit' | 'delete' | null>(null)

// 只有全局管理员可以管理后端密码
const canManagePassword = computed(() => auth.isGlobalAdmin)

// 检查后端是否已解锁
const isUnlocked = computed(() => auth.isBackendUnlocked(props.backendId))
const hasExistingPassword = computed(() => Boolean(passwordStatus.value?.has_password || props.hasPassword))
const isLocked = computed(() => hasExistingPassword.value && !isUnlocked.value)
const statusType = computed(() => {
  if (isLocked.value) return 'warning' as const
  if (hasExistingPassword.value) return 'success' as const
  return 'default' as const
})
const statusText = computed(() => {
  if (isLocked.value) return '已加密'
  if (hasExistingPassword.value) return '已解锁'
  return '未设置密码'
})
const statusHint = computed(() => {
  if (isLocked.value) return '需输入密码后才能查看和编辑该后端配置'
  if (hasExistingPassword.value) return '当前后端受密码保护，可锁定或管理密码'
  return '可按需为该后端启用单独密码保护'
})

watch(() => props.backendId, () => {
  if (props.backendId && canManagePassword.value) {
    loadPasswordStatus()
  }
  resetActionState()
}, { immediate: true })

watch(() => props.hasPassword, (val) => {
  if (!val && activeAction.value === 'unlock') {
    activeAction.value = null
  }
}, { immediate: true })

async function loadPasswordStatus() {
  if (!props.backendId || !canManagePassword.value) return

  loading.value = true
  try {
    const res = await requestJson<{
      backend_id: string
      has_password: boolean
      updated_at: string
    }>(`/api/backend/${props.backendId}/password`, {
      method: 'GET',
    })
    passwordStatus.value = {
      has_password: res.has_password,
      updated_at: res.updated_at,
    }
  } catch (e) {
    console.error('加载密码状态失败:', e)
    passwordStatus.value = null
  } finally {
    loading.value = false
  }
}

async function setPassword() {
  if (!props.backendId) {
    message.error('后端 ID 无效')
    return
  }

  const password = newPassword.value.trim()
  const currentPassword = currentPasswordForUpdate.value.trim()

  if (!password) {
    message.error('请输入新密码')
    return
  }

  saving.value = true
  try {
    if (hasExistingPassword.value) {
      if (!currentPassword) {
        message.error('请输入当前密码以确认修改')
        return
      }

      const valid = await verifyBackendPassword(currentPassword)
      if (!valid) {
        message.error('当前密码错误，无法修改')
        return
      }
    }

    await requestJson(`/api/backend/${props.backendId}/password`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ password }),
    })

    if (password) {
      message.success('后端密码已设置')
      // 设置密码后，锁定该后端
      auth.lockBackend(props.backendId)
      activeAction.value = 'unlock'
    } else {
      message.success('后端密码已删除')
      // 删除密码后，解锁该后端
      auth.unlockBackend(props.backendId)
      activeAction.value = null
    }

    resetActionState()
    await loadPasswordStatus()
  } catch (e) {
    message.error(`操作失败: ${(e as Error).message}`)
  } finally {
    saving.value = false
  }
}

async function deletePasswordWithVerify() {
  if (!props.backendId) {
    message.error('后端 ID 无效')
    return
  }

  const password = deletePassword.value.trim()
  if (!password) {
    message.error('请输入当前密码以确认删除')
    return
  }

  // 先验证密码
  verifying.value = true
  try {
    const valid = await verifyBackendPassword(password)
    if (!valid) {
      message.error('密码错误，无法删除')
      verifying.value = false
      return
    }

    // 密码验证成功，删除密码
    await requestJson(`/api/backend/${props.backendId}/password`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ password: '' }),
    })

    message.success('后端密码已删除')
    auth.unlockBackend(props.backendId)
    resetActionState()
    await loadPasswordStatus()
  } catch (e) {
    message.error(`操作失败: ${(e as Error).message}`)
  } finally {
    verifying.value = false
  }
}

async function verifyPassword() {
  if (!props.backendId) {
    message.error('后端 ID 无效')
    return
  }

  const password = unlockPassword.value.trim()
  if (!password) {
    message.error('请输入密码')
    return
  }

  verifying.value = true
  try {
    const valid = await verifyBackendPassword(password)
    if (valid) {
      message.success('密码验证成功')
      auth.unlockBackend(props.backendId)
      activeAction.value = null
      unlockPassword.value = ''
      emit('unlocked')
    } else {
      message.error('密码错误')
    }
  } catch (e) {
    message.error(`验证失败: ${(e as Error).message}`)
  } finally {
    verifying.value = false
  }
}

async function verifyBackendPassword(password: string) {
  const res = await requestJson<{ valid: boolean }>(`/api/backend/${props.backendId}/verify-password`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  return res.valid
}

function openAction(action: 'unlock' | 'edit' | 'delete') {
  activeAction.value = activeAction.value === action ? null : action
  if (activeAction.value !== 'unlock') {
    unlockPassword.value = ''
  }
  if (activeAction.value !== 'edit') {
    newPassword.value = ''
    currentPasswordForUpdate.value = ''
  }
  if (activeAction.value !== 'delete') {
    deletePassword.value = ''
  }
}

function resetActionState() {
  activeAction.value = null
  unlockPassword.value = ''
  newPassword.value = ''
  currentPasswordForUpdate.value = ''
  deletePassword.value = ''
}

function lockBackend() {
  auth.lockBackend(props.backendId)
  openAction('unlock')
}
</script>

<template>
  <div v-if="hasExistingPassword || canManagePassword" class="password-panel">
    <div class="password-toolbar">
      <n-space align="center" :size="10" class="password-status">
        <n-icon v-if="isLocked" size="16" color="var(--n-warning-color)"><LockClosedOutline /></n-icon>
        <n-icon v-else-if="hasExistingPassword" size="16" color="var(--app-success)"><LockOpenOutline /></n-icon>
        <n-icon v-else size="16" color="var(--app-text-muted)"><CloseCircleOutline /></n-icon>
        <span class="status-title">{{ statusText }}</span>
        <n-tag :type="statusType" size="small" :bordered="false">
          <template #icon>
            <n-icon v-if="hasExistingPassword"><CheckmarkCircleOutline /></n-icon>
            <n-icon v-else><CloseCircleOutline /></n-icon>
          </template>
          {{ hasExistingPassword ? '已启用保护' : '未启用保护' }}
        </n-tag>
      </n-space>

      <n-space align="center" :size="8" class="password-actions">
        <n-button
          v-if="isLocked"
          type="primary"
          size="tiny"
          @click="openAction('unlock')"
        >
          解锁
        </n-button>
        <n-button
          v-if="hasExistingPassword && isUnlocked"
          size="tiny"
          secondary
          @click="lockBackend"
        >
          锁定
        </n-button>
        <n-button
          v-if="canManagePassword"
          :type="hasExistingPassword ? 'warning' : 'primary'"
          size="tiny"
          secondary
          @click="openAction('edit')"
        >
          {{ hasExistingPassword ? '修改密码' : '设置密码' }}
        </n-button>
        <n-button
          v-if="canManagePassword && hasExistingPassword"
          type="error"
          size="tiny"
          secondary
          @click="openAction('delete')"
        >
          删除密码
        </n-button>
      </n-space>
    </div>

    <div class="password-meta">
      <span>{{ statusHint }}</span>
      <span v-if="passwordStatus?.has_password && passwordStatus.updated_at">
        最后更新：{{ new Date(passwordStatus.updated_at).toLocaleString('zh-CN') }}
      </span>
    </div>

    <div v-if="activeAction" class="password-inline-form">
      <n-form v-if="activeAction === 'unlock'" @submit.prevent="verifyPassword">
        <n-space align="end" :size="10" wrap>
          <n-form-item label="后端密码" :show-label="true" class="inline-form-item">
            <n-input
              v-model:value="unlockPassword"
              type="password"
              show-password-on="click"
              placeholder="输入密码后解锁"
              :disabled="verifying"
              @keyup.enter="verifyPassword"
            />
          </n-form-item>
          <n-space :size="8">
            <n-button
              type="primary"
              size="small"
              :loading="verifying"
              :disabled="!unlockPassword.trim()"
              @click="verifyPassword"
            >
              确认解锁
            </n-button>
            <n-button size="small" :disabled="verifying" @click="resetActionState">
              取消
            </n-button>
          </n-space>
        </n-space>
      </n-form>

      <n-form v-else-if="activeAction === 'edit'" @submit.prevent="setPassword">
        <n-space align="end" :size="10" wrap>
          <n-form-item v-if="hasExistingPassword" label="当前密码" :show-label="true" class="inline-form-item">
            <n-input
              v-model:value="currentPasswordForUpdate"
              type="password"
              show-password-on="click"
              placeholder="输入当前密码"
              :disabled="saving"
            />
          </n-form-item>
          <n-form-item label="新密码" :show-label="true" class="inline-form-item">
            <n-input
              v-model:value="newPassword"
              type="password"
              show-password-on="click"
              placeholder="输入新密码"
              :disabled="saving"
            />
          </n-form-item>
          <n-space :size="8">
            <n-button
              type="primary"
              size="small"
              :loading="saving"
              :disabled="!newPassword.trim() || (hasExistingPassword && !currentPasswordForUpdate.trim())"
              @click="setPassword"
            >
              确认
            </n-button>
            <n-button size="small" :disabled="saving" @click="resetActionState">
              取消
            </n-button>
          </n-space>
        </n-space>
      </n-form>

      <n-form v-else-if="activeAction === 'delete'" @submit.prevent="deletePasswordWithVerify">
        <n-space align="end" :size="10" wrap>
          <n-form-item label="当前密码" :show-label="true" class="inline-form-item">
            <n-input
              v-model:value="deletePassword"
              type="password"
              show-password-on="click"
              placeholder="输入当前密码后删除"
              :disabled="verifying"
            />
          </n-form-item>
          <n-space :size="8">
            <n-button
              type="error"
              size="small"
              :loading="verifying"
              :disabled="!deletePassword.trim()"
              @click="deletePasswordWithVerify"
            >
              确认删除
            </n-button>
            <n-button size="small" :disabled="verifying" @click="resetActionState">
              取消
            </n-button>
          </n-space>
        </n-space>
      </n-form>
    </div>
  </div>
</template>

<style scoped>
.password-panel {
  margin-bottom: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  background: rgba(var(--app-primary-rgb), 0.02);
  border: 1px solid rgba(var(--app-primary-rgb), 0.08);
}

.password-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.password-status {
  min-width: 0;
}

.status-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--app-text);
}

.password-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--app-text-muted);
}

.password-inline-form {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed rgba(var(--app-primary-rgb), 0.12);
}

.inline-form-item {
  min-width: 220px;
  margin-bottom: 0;
}
</style>
