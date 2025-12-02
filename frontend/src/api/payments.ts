import api from './auth'
import type { Payment, ApiResponse } from '@/types'

export const paymentApi = {
  getAll: (params?: { limit?: number; offset?: number; startDate?: string; endDate?: string; category?: string }) =>
    api.get<ApiResponse<Payment[]>>('/payments', { params }),
  
  getById: (id: string) =>
    api.get<ApiResponse<Payment>>(`/payments/${id}`),
  
  getStats: (startDate?: string, endDate?: string) =>
    api.get<ApiResponse<{ totalAmount: number; totalCount: number; categoryStats: Record<string, number>; merchantStats: Record<string, number>; dailyStats: Record<string, number> }>>('/payments/stats', { params: { startDate, endDate } }),
  
  create: (payment: Omit<Payment, 'id' | 'created_at'>) =>
    api.post<ApiResponse<Payment>>('/payments', payment),
  
  update: (id: string, payment: Partial<Payment>) =>
    api.put<ApiResponse<void>>(`/payments/${id}`, payment),
  
  delete: (id: string) =>
    api.delete<ApiResponse<void>>(`/payments/${id}`),
}
