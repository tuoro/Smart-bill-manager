import api from './auth'
import type { DashboardData, ApiResponse } from '@/types'

export const dashboardApi = {
  getSummary: () =>
    api.get<ApiResponse<DashboardData>>('/dashboard'),
}
