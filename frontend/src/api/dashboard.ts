import api from './client'
import type { DashboardData, ApiResponse } from '@/types'

export const dashboardApi = {
  getSummary: () =>
    api.get<ApiResponse<DashboardData>>('/dashboard'),
}
