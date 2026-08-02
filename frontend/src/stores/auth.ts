import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { authApi } from '@/api/auth'
import { clearAuth, getStoredUser, getToken, setStoredUser, setToken } from '@/api/storage'
import type { User } from '@/types'

const SETUP_CACHE_TTL_MS = 5 * 60 * 1000

type SessionResponse = {
  success: boolean
  message?: string
  user?: User
  token?: string
}

export type AuthActionResult = { success: boolean; message: string }

const getErrorMessage = (error: unknown, fallback: string) => {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const axiosError = error as { response?: { data?: { message?: string } } }
    if (axiosError.response?.data?.message) return axiosError.response.data.message
  }
  return error instanceof Error ? error.message : fallback
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(getStoredUser())
  const hasToken = ref(Boolean(getToken()))
  const loading = ref(false)
  const setupRequiredCache = ref<{ setupRequired: boolean; timestamp: number } | null>(null)

  const isAuthenticated = computed(() => hasToken.value && !!user.value)

  function setSession(nextUser: User, token: string) {
    setToken(token)
    setStoredUser(nextUser)
    user.value = nextUser
    hasToken.value = true
    setupRequiredCache.value = { setupRequired: false, timestamp: Date.now() }
  }

  function clearSession() {
    clearAuth()
    user.value = null
    hasToken.value = false
  }

  async function establishSession(
    request: () => Promise<{ data: SessionResponse }>,
    successMessage: string,
    failureMessage: string,
  ): Promise<AuthActionResult> {
    loading.value = true
    try {
      const response = await request()
      if (!response.data.success) {
        return { success: false, message: response.data.message || failureMessage }
      }
      if (!response.data.token || !response.data.user) {
        return { success: false, message: '操作成功，但返回的会话数据不完整，请重试' }
      }
      setSession(response.data.user, response.data.token)
      return { success: true, message: successMessage }
    } catch (error: unknown) {
      return { success: false, message: getErrorMessage(error, failureMessage) }
    } finally {
      loading.value = false
    }
  }

  const login = (username: string, password: string) =>
    establishSession(() => authApi.login(username, password), '登录成功', '登录失败，请检查网络连接')

  const registerWithInvite = (inviteCode: string, username: string, password: string) =>
    establishSession(
      () => authApi.inviteRegister(inviteCode, username, password),
      '注册成功',
      '注册失败，请稍后重试',
    )

  const setupAdmin = (username: string, password: string) =>
    establishSession(() => authApi.setup(username, password), '管理员账户创建成功', '创建失败，请稍后重试')

  async function verifyToken(): Promise<boolean> {
    const storedUser = getStoredUser()
    const storedToken = getToken()
    if (!storedUser || !storedToken) {
      clearSession()
      return false
    }

    try {
      await authApi.verify()
      user.value = storedUser
      hasToken.value = true
      return true
    } catch {
      clearSession()
      return false
    }
  }

  function logout() {
    clearSession()
  }

  async function checkSetupRequired(): Promise<{ setupRequired: boolean } | null> {
    if (setupRequiredCache.value && Date.now() - setupRequiredCache.value.timestamp < SETUP_CACHE_TTL_MS) {
      return { setupRequired: setupRequiredCache.value.setupRequired }
    }

    try {
      const res = await authApi.checkSetupRequired()
      if (res.data.success && res.data.data) {
        const result = { setupRequired: res.data.data.setupRequired }
        setupRequiredCache.value = { setupRequired: result.setupRequired, timestamp: Date.now() }
        return result
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
    clearSession,
    login,
    registerWithInvite,
    setupAdmin,
    verifyToken,
    logout,
    checkSetupRequired,
  }
})

