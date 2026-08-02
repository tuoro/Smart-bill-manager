import api from './client'
import type { ApiResponse } from '@/types'

export type TaskDTO = {
  id: string
  type: string
  status: string
  target_id: string
  error?: string | null
  result?: unknown
  created_at?: string
  updated_at?: string
}

export const tasksApi = {
  getById: (id: string) => api.get<ApiResponse<TaskDTO>>(`/tasks/${id}`),
  cancel: (id: string) => api.post<ApiResponse<{ canceled: boolean }>>(`/tasks/${id}/cancel`),
}
