import api from './auth'
import type { EmailConfig, EmailLog, ApiResponse } from '@/types'

export const emailApi = {
  getConfigs: () =>
    api.get<ApiResponse<EmailConfig[]>>('/email/configs'),
  
  createConfig: (config: Omit<EmailConfig, 'id' | 'created_at' | 'last_check'>) =>
    api.post<ApiResponse<EmailConfig>>('/email/configs', config),
  
  updateConfig: (id: string, config: Partial<EmailConfig>) =>
    api.put<ApiResponse<void>>(`/email/configs/${id}`, config),
  
  deleteConfig: (id: string) =>
    api.delete<ApiResponse<void>>(`/email/configs/${id}`),
  
  testConnection: (config: { email: string; imap_host: string; imap_port: number; password: string }) =>
    api.post<ApiResponse<void>>('/email/test', config),
  
  getLogs: (configId?: string, limit?: number) =>
    api.get<ApiResponse<EmailLog[]>>('/email/logs', { params: { configId, limit } }),
  
  startMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/start/${id}`),
  
  stopMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/stop/${id}`),
  
  getMonitoringStatus: () =>
    api.get<ApiResponse<{ configId: string; status: string }[]>>('/email/monitor/status'),
  
  manualCheck: (id: string) =>
    api.post<ApiResponse<{ newEmails: number }>>(`/email/check/${id}`),
}
