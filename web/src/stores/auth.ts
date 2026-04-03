import { useStorage } from '@vueuse/core'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  const token = useStorage<string | null>('accessToken', null)
  const role = useStorage<'admin' | 'viewer' | null>('auth_role', null)
  const userId = useStorage<string | null>('userId', null)
  const userName = useStorage<string | null>('userName', null)
  const isAdmin = useStorage<boolean>('isAdmin', false)

  const unlockedBackends = ref<Set<string>>(new Set())

  function setToken(t: string) {
    token.value = t
  }

  function setAuthInfo(info: {
    token: string
    role?: 'admin' | 'viewer'
  }) {
    token.value = info.token
    role.value = info.role || null
  }

  function login(userIdVal: string, userNameVal: string, tokenVal: string, isAdminVal: boolean) {
    token.value = tokenVal
    userId.value = userIdVal
    userName.value = userNameVal
    isAdmin.value = isAdminVal
  }

  function clearToken() {
    token.value = null
    role.value = null
    userId.value = null
    userName.value = null
    isAdmin.value = false
    unlockedBackends.value.clear()
  }

  function logout() {
    clearToken()
    window.location.href = '/#/login'
  }

  function unlockBackend(backendId: string) {
    unlockedBackends.value.add(backendId)
  }

  function lockBackend(backendId: string) {
    unlockedBackends.value.delete(backendId)
  }

  function isBackendUnlocked(backendId: string): boolean {
    return unlockedBackends.value.has(backendId)
  }

  function lockAllBackends() {
    unlockedBackends.value.clear()
  }

  const isAuthenticated = computed(() => !!token.value)
  const isGlobalAdmin = computed(() => isAdmin.value === true || role.value === 'admin')
  const isViewer = computed(() => role.value === 'viewer')

  return {
    token,
    role,
    userId,
    userName,
    isAdmin,
    isAuthenticated,
    isGlobalAdmin,
    isViewer,
    setToken,
    setAuthInfo,
    login,
    clearToken,
    logout,
    unlockBackend,
    lockBackend,
    isBackendUnlocked,
    lockAllBackends,
  }
})
