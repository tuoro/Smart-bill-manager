import api from './auth'
import type { FeishuConfig, FeishuLog, ApiResponse } from '@/types'

export const feishuApi = {
  getConfigs: () => api.get<ApiResponse<FeishuConfig[]>>('/feishu/configs'),

  createConfig: (config: Omit<FeishuConfig, 'id' | 'created_at'>) =>
    api.post<ApiResponse<FeishuConfig>>('/feishu/configs', config),

  updateConfig: (id: string, config: Partial<FeishuConfig>) =>
    api.put<ApiResponse<void>>(`/feishu/configs/${id}`, config),

  deleteConfig: (id: string) => api.delete<ApiResponse<void>>(`/feishu/configs/${id}`),

  getLogs: (configId?: string, limit?: number) =>
    api.get<ApiResponse<FeishuLog[]>>('/feishu/logs', { params: { configId, limit } }),
}

