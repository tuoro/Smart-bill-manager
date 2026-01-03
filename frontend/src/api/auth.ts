import axios from 'axios'
import type { ApiResponse, User } from '@/types'

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'
export const FILE_BASE_URL = import.meta.env.VITE_FILE_URL || ''

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
  localStorage.removeItem('user')
}

// Auth error handler callback - to be set by router
let authErrorHandler: (() => void) | null = null

export const setAuthErrorHandler = (handler: () => void) => {
  authErrorHandler = handler
}

const api = axios.create({
  baseURL: API_BASE_URL,
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Handle 401 responses
api.interceptors.response.use(
  (response) => response,
  (error) => {
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
    api.post<{ success: boolean; message: string; user?: User }>('/auth/login', { username, password }),

  logout: () => api.post<ApiResponse<void>>('/auth/logout'),
  
  register: (username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User }>('/auth/register', { username, password, email }),

  inviteRegister: (inviteCode: string, username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User }>('/auth/invite/register', { inviteCode, username, password, email }),
  
  verify: () =>
    api.get<ApiResponse<{ userId: string; username: string; role: string }>>('/auth/verify'),
  
  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<ApiResponse<void>>('/auth/change-password', { oldPassword, newPassword }),
  
  getCurrentUser: () =>
    api.get<ApiResponse<User>>('/auth/me'),
  
  checkSetupRequired: () =>
    api.get<ApiResponse<{ setupRequired: boolean }>>('/auth/setup-required'),

  setup: (username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User }>('/auth/setup', { username, password, email }),

  adminCreateInvite: (expiresInDays?: number) =>
    api.post<ApiResponse<{ code: string; code_hint: string; expiresAt?: string | null }>>('/admin/invites', { expiresInDays }),

  adminListInvites: (limit = 30) =>
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
    >('/admin/invites', { params: { limit } }),

  adminDeleteInvite: (id: string) => api.delete<ApiResponse<{ deleted: boolean }>>(`/admin/invites/${id}`),

  adminCreateApiToken: (name?: string, expiresInDays?: number) =>
    api.post<ApiResponse<{ token: string; token_hint: string; id: string; expires_at?: string | null }>>('/admin/api-tokens', {
      name,
      expiresInDays,
    }),

  adminListApiTokens: (limit = 50) =>
    api.get<
      ApiResponse<
        Array<{
          id: string
          name: string
          token_hint: string
          expires_at?: string | null
          last_used_at?: string | null
          revoked_at?: string | null
          created_at: string
        }>
      >
    >('/admin/api-tokens', { params: { limit } }),

  adminRevokeApiToken: (id: string) => api.post<ApiResponse<{ revoked: boolean }>>(`/admin/api-tokens/${id}/revoke`),
}

export default api
