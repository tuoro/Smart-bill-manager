import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi, setToken, setStoredUser, getStoredUser, clearAuth } from '@/api/auth'
import type { User } from '@/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(getStoredUser())
  const loading = ref(false)

  const isAuthenticated = computed(() => !!user.value)

  async function login(username: string, password: string): Promise<{ success: boolean; message: string }> {
    loading.value = true
    try {
      const res = await authApi.login(username, password)
      if (res.data.success && res.data.token && res.data.user) {
        setToken(res.data.token)
        setStoredUser(res.data.user)
        user.value = res.data.user
        return { success: true, message: '登录成功' }
      }
      return { success: false, message: res.data.message || '登录失败' }
    } catch (error: unknown) {
      const errMessage = error instanceof Error ? error.message : '登录失败，请检查网络连接'
      if (typeof error === 'object' && error !== null && 'response' in error) {
        const axiosError = error as { response?: { data?: { message?: string } } }
        return { success: false, message: axiosError.response?.data?.message || errMessage }
      }
      return { success: false, message: errMessage }
    } finally {
      loading.value = false
    }
  }

  async function verifyToken(): Promise<boolean> {
    const storedUser = getStoredUser()
    if (!storedUser) return false

    try {
      await authApi.verify()
      user.value = storedUser
      return true
    } catch {
      clearAuth()
      user.value = null
      return false
    }
  }

  function logout() {
    clearAuth()
    user.value = null
  }

  async function checkSetupRequired(): Promise<{ setupRequired: boolean } | null> {
    try {
      const res = await authApi.checkSetupRequired()
      if (res.data.success) {
        return { setupRequired: res.data.data.setupRequired }
      }
      return null
    } catch (error) {
      console.error('Failed to check setup status:', error)
      return null
    }
  }

  return {
    user,
    loading,
    isAuthenticated,
    login,
    verifyToken,
    logout,
    checkSetupRequired
  }
})
