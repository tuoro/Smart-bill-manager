import api from './auth'
import type { DingtalkConfig, DingtalkLog, ApiResponse } from '@/types'

export const dingtalkApi = {
  getConfigs: () =>
    api.get<ApiResponse<DingtalkConfig[]>>('/dingtalk/configs'),
  
  createConfig: (config: Omit<DingtalkConfig, 'id' | 'created_at'>) =>
    api.post<ApiResponse<DingtalkConfig>>('/dingtalk/configs', config),
  
  updateConfig: (id: string, config: Partial<DingtalkConfig>) =>
    api.put<ApiResponse<void>>(`/dingtalk/configs/${id}`, config),
  
  deleteConfig: (id: string) =>
    api.delete<ApiResponse<void>>(`/dingtalk/configs/${id}`),
  
  getLogs: (configId?: string, limit?: number) =>
    api.get<ApiResponse<DingtalkLog[]>>('/dingtalk/logs', { params: { configId, limit } }),
}
