import type { AxiosRequestConfig } from 'axios'
import type { ApiResponse, User } from '@/types'

import api from './client'

export { FILE_BASE_URL, setActAsConfirmHandler, setAuthErrorHandler } from './client'
export type { ActAsConfirmInfo } from './client'
export {
  clearActAs,
  clearAuth,
  getActAsUserId,
  getActAsUsername,
  getStoredUser,
  setActAsUser,
  setStoredUser,
  setToken,
} from './storage'

export type AdminDeleteUserResult = {
  userId: string
  paymentsDeleted: number
  invoicesDeleted: number
  tripsDeleted: number
  emailConfigsDeleted: number
  emailLogsDeleted: number
  tasksDeleted: number
  regressionSamplesDeleted: number
  paymentOCRDeleted: number
  invoiceOCRDeleted: number
  linksDeleted: number
  invitesCreatedByUser: number
  invitesUsedByUser: number
}

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

  adminListUsers: (config?: AxiosRequestConfig) => api.get<ApiResponse<User[]>>('/admin/users', config),

  adminCreateInvite: (expiresInDays?: number) =>
    api.post<ApiResponse<{ code: string; code_hint: string; expiresAt?: string | null }>>('/admin/invites', { expiresInDays }),

  adminListInvites: (limit = 30, config?: AxiosRequestConfig) =>
    api.get<
      ApiResponse<
        Array<{
          id: string
          code_hint: string
          createdBy: string
          createdByUsername?: string
          createdByDeleted?: boolean
          createdAt: string
          expiresAt?: string | null
          usedAt?: string | null
          usedBy?: string | null
          usedByUsername?: string
          usedByDeleted?: boolean
          expired: boolean
        }>
      >
    >('/admin/invites', { params: { limit }, ...(config || {}) }),

  adminDeleteInvite: (id: string) => api.delete<ApiResponse<{ deleted: boolean }>>(`/admin/invites/${id}`),

  adminSetUserActive: (id: string, active: boolean) =>
    api.patch<ApiResponse<User>>(`/admin/users/${id}/active`, { is_active: active }),

  adminSetUserPassword: (id: string, password: string) =>
    api.patch<ApiResponse<{ updated: boolean; userId: string }>>(`/admin/users/${id}/password`, { password }),

  adminDeleteUser: (id: string) => api.delete<ApiResponse<AdminDeleteUserResult>>(`/admin/users/${id}`),
}

export default api
