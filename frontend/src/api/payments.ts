import api from './auth'
import type { Payment, Invoice, ApiResponse } from '@/types'

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
  
  // Upload screenshot and OCR recognition
  uploadScreenshot: (file: File) => {
    const formData = new FormData()
    formData.append('file', file)
    return api.post<ApiResponse<{ payment: Payment; extracted: any }>>('/payments/upload-screenshot', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  
  // Reparse screenshot with OCR
  reparseScreenshot: (id: string) => 
    api.post<ApiResponse<any>>(`/payments/${id}/reparse`),
  
  // Get invoices linked to a payment
  getPaymentInvoices: (paymentId: string) =>
    api.get<ApiResponse<Invoice[]>>(`/payments/${paymentId}/invoices`),

  // Get suggested invoices for a payment (smart matching)
  getSuggestedInvoices: (paymentId: string) =>
    api.get<ApiResponse<Invoice[]>>(`/payments/${paymentId}/suggest-invoices`),
}
