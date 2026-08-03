import api from './client'
import type { ApiResponse } from '@/types'

export type TaskDTO<TResult = unknown> = {
  id: string
  type: string
  status: string
  target_id: string
  error?: string | null
  result?: TResult
  created_at?: string
  updated_at?: string
}

export const tasksApi = {
  getById: <TResult = unknown>(id: string) => api.get<ApiResponse<TaskDTO<TResult>>>(`/tasks/${id}`),
  cancel: (id: string) => api.post<ApiResponse<{ canceled: boolean }>>(`/tasks/${id}/cancel`),
}
