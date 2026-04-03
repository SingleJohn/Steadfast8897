import { useAuthStore } from '@/stores/auth'

export function useAuth() {
  const store = useAuthStore()

  return {
    auth: store,
    login: store.login,
    logout: store.logout,
  }
}
