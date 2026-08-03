import api from './client'
import type { AxiosRequestConfig } from 'axios'
import type { EmailConfig, EmailLog, ApiResponse, Invoice } from '@/types'

export const emailApi = {
  getConfigs: (config?: AxiosRequestConfig) =>
    api.get<ApiResponse<EmailConfig[]>>('/email/configs', config),
  
  createConfig: (config: Omit<EmailConfig, 'id' | 'created_at' | 'last_check'>) =>
    api.post<ApiResponse<EmailConfig>>('/email/configs', config),
  
  updateConfig: (id: string, config: Partial<EmailConfig>) =>
    api.put<ApiResponse<void>>(`/email/configs/${id}`, config),
  
  deleteConfig: (id: string) =>
    api.delete<ApiResponse<void>>(`/email/configs/${id}`),
  
  testConnection: (config: { email: string; imap_host: string; imap_port: number; password: string }) =>
    api.post<ApiResponse<void>>('/email/test', config),
  
  getLogs: (configId?: string, limit?: number, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<EmailLog[]>>('/email/logs', { params: { configId, limit }, ...(config || {}) }),

  clearLogs: (configId: string) =>
    api.delete<ApiResponse<{ deleted: number }>>('/email/logs', { params: { configId } }),

  parseLog: (id: string) =>
    api.post<ApiResponse<Invoice>>(`/email/logs/${id}/parse`),

  exportLogEML: (id: string, format?: 'eml' | 'text') =>
    api.get<Blob>(`/email/logs/${id}/export`, {
      params: format === 'text' ? { format: 'text' } : undefined,
      responseType: 'blob',
    }),
  
  startMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/start/${id}`),
  
  stopMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/stop/${id}`),
  
  getMonitoringStatus: (config?: AxiosRequestConfig) =>
    api.get<ApiResponse<{ configId: string; status: string }[]>>('/email/monitor/status', config),
  
  manualCheck: (id: string) =>
    api.post<ApiResponse<{ newEmails: number }>>(`/email/check/${id}`),

  manualFullSync: (id: string, params?: { limit?: number; beforeUid?: number; mode?: 'latest' | 'all' | 'backfill' }) =>
    api.post<ApiResponse<{ newEmails: number }>>(`/email/check/${id}`, null, {
      params: { full: 1, ...(params || {}) },
      // Full sync is intentionally rate-limited to avoid IMAP risk control and can exceed the default 15s timeout.
      timeout: 600_000,
    }),
}
