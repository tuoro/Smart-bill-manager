import axios from 'axios'
import type { AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'
import type { ApiResponse, User } from '@/types'

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'
export const FILE_BASE_URL = import.meta.env.VITE_FILE_URL || ''
const API_TIMEOUT_MS = Number(import.meta.env.VITE_API_TIMEOUT_MS || 15000)
const API_CONCURRENCY = Math.max(1, Number(import.meta.env.VITE_API_CONCURRENCY || 6))

const ACT_AS_USER_ID_KEY = 'sbm_act_as_user_id'
const ACT_AS_USERNAME_KEY = 'sbm_act_as_username'
const ACT_AS_EVENT = 'sbm-act-as-change'

// Get stored token
const getToken = (): string | null => {
  return localStorage.getItem('token')
}

export const getActAsUserId = (): string | null => localStorage.getItem(ACT_AS_USER_ID_KEY)
export const getActAsUsername = (): string | null => localStorage.getItem(ACT_AS_USERNAME_KEY)

export const setActAsUser = (userId: string, username?: string) => {
  const trimmed = String(userId || '').trim()
  if (!trimmed) return
  localStorage.setItem(ACT_AS_USER_ID_KEY, trimmed)
  localStorage.setItem(ACT_AS_USERNAME_KEY, String(username || '').trim())
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(ACT_AS_EVENT))
}

export const clearActAs = () => {
  localStorage.removeItem(ACT_AS_USER_ID_KEY)
  localStorage.removeItem(ACT_AS_USERNAME_KEY)
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(ACT_AS_EVENT))
}

// Set stored token
export const setToken = (token: string | null) => {
  if (token) {
    localStorage.setItem('token', token)
  } else {
    localStorage.removeItem('token')
  }
}

// Get stored user
export const getStoredUser = (): User | null => {
  const userStr = localStorage.getItem('user')
  if (userStr) {
    try {
      return JSON.parse(userStr)
    } catch {
      return null
    }
  }
  return null
}

// Set stored user
export const setStoredUser = (user: User | null) => {
  if (user) {
    localStorage.setItem('user', JSON.stringify(user))
  } else {
    localStorage.removeItem('user')
  }
}

// Clear auth data
export const clearAuth = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  clearActAs()
}

// Auth error handler callback - to be set by router
let authErrorHandler: (() => void) | null = null

export const setAuthErrorHandler = (handler: () => void) => {
  authErrorHandler = handler
}

export type ActAsConfirmInfo = {
  code?: string
  actor_user_id?: string
  target_user_id?: string
  method?: string
  path?: string
}

let actAsConfirmHandler: ((info: ActAsConfirmInfo) => Promise<boolean>) | null = null

export const setActAsConfirmHandler = (handler: ((info: ActAsConfirmInfo) => Promise<boolean>) | null) => {
  actAsConfirmHandler = handler
}

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: API_TIMEOUT_MS,
})

type ReleaseFn = () => void
type SbmInternalConfig = InternalAxiosRequestConfig & { _sbmRelease?: ReleaseFn }

const createConcurrencyLimiter = (max: number) => {
  const queue: Array<() => void> = []
  let active = 0

  const acquire = () =>
    new Promise<() => void>((resolve) => {
      const grant = () => {
        active += 1
        let released = false
        resolve(() => {
          if (released) return
          released = true
          active = Math.max(0, active - 1)
          const next = queue.shift()
          if (next) next()
        })
      }

      if (active < max) {
        grant()
        return
      }

      queue.push(grant)
    })

  return { acquire }
}

const limiter = createConcurrencyLimiter(API_CONCURRENCY)

// Add token to requests
api.interceptors.request.use(async (config) => {
  const cfg = config as SbmInternalConfig
  const token = getToken()
  if (token) {
    cfg.headers.Authorization = `Bearer ${token}`
  }
  const actAsUserId = getActAsUserId()
  if (actAsUserId) {
    cfg.headers['X-Act-As-User'] = actAsUserId
  }

  const release = await limiter.acquire()
  cfg._sbmRelease = release
  return cfg
})

// Handle 401 responses
api.interceptors.response.use(
  (response) => {
    const release = (response.config as SbmInternalConfig)?._sbmRelease
    if (typeof release === 'function') release()
    return response
  },
  async (error) => {
    const release = (error?.config as SbmInternalConfig | undefined)?._sbmRelease
    if (typeof release === 'function') release()

    if (error.response?.status === 400 && error.response?.data?.data?.code === 'ACT_AS_CONFIRM_REQUIRED') {
      const originalConfig = error.config
      const alreadyConfirmed = originalConfig?.headers?.['X-Act-As-Confirmed']
      if (!alreadyConfirmed && actAsConfirmHandler && originalConfig) {
        const ok = await actAsConfirmHandler(error.response.data.data)
        if (ok) {
          originalConfig.headers = originalConfig.headers || {}
          originalConfig.headers['X-Act-As-Confirmed'] = '1'
          return api.request(originalConfig)
        }
      }
    }
    if (error.response?.status === 401) {
      clearAuth()
      // Use callback instead of direct window manipulation
      if (authErrorHandler) {
        authErrorHandler()
      }
    }
    return Promise.reject(error)
  }
)

// Auth APIs
export const authApi = {
  login: (username: string, password: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/login', { username, password }),
  
  register: (username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/register', { username, password, email }),

  inviteRegister: (inviteCode: string, username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/invite/register', { inviteCode, username, password, email }),
  
  verify: () =>
    api.get<ApiResponse<{ userId: string; username: string; role: string }>>('/auth/verify'),
  
  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<ApiResponse<void>>('/auth/change-password', { oldPassword, newPassword }),
  
  getCurrentUser: () =>
    api.get<ApiResponse<User>>('/auth/me'),
  
  checkSetupRequired: () =>
    api.get<ApiResponse<{ setupRequired: boolean }>>('/auth/setup-required'),

  setup: (username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/setup', { username, password, email }),

  adminCreateInvite: (expiresInDays?: number) =>
    api.post<ApiResponse<{ code: string; code_hint: string; expiresAt?: string | null }>>('/admin/invites', { expiresInDays }),

  adminListInvites: (limit = 30, config?: AxiosRequestConfig) =>
    api.get<
      ApiResponse<
        Array<{
          id: string
          code_hint: string
          createdBy: string
          createdAt: string
          expiresAt?: string | null
          usedAt?: string | null
          usedBy?: string | null
          expired: boolean
        }>
      >
    >('/admin/invites', { params: { limit }, ...(config || {}) }),

  adminDeleteInvite: (id: string) => api.delete<ApiResponse<{ deleted: boolean }>>(`/admin/invites/${id}`),
}

export default api
