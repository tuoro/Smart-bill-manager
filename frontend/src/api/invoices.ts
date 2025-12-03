import api from './auth'
import type { Invoice, ApiResponse } from '@/types'

export const invoiceApi = {
  getAll: (params?: { limit?: number; offset?: number }) =>
    api.get<ApiResponse<Invoice[]>>('/invoices', { params }),
  
  getById: (id: string) =>
    api.get<ApiResponse<Invoice>>(`/invoices/${id}`),
  
  getStats: () =>
    api.get<ApiResponse<{ totalCount: number; totalAmount: number; bySource: Record<string, number>; byMonth: Record<string, number> }>>('/invoices/stats'),
  
  upload: (file: File, paymentId?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    if (paymentId) formData.append('payment_id', paymentId)
    return api.post<ApiResponse<Invoice>>('/invoices/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  
  uploadMultiple: (files: File[], paymentId?: string) => {
    const formData = new FormData()
    files.forEach(file => formData.append('files', file))
    if (paymentId) formData.append('payment_id', paymentId)
    return api.post<ApiResponse<Invoice[]>>('/invoices/upload-multiple', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  
  update: (id: string, invoice: Partial<Invoice>) =>
    api.put<ApiResponse<void>>(`/invoices/${id}`, invoice),
  
  delete: (id: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${id}`),
  
  parse: (id: string) =>
    api.post<ApiResponse<Invoice>>(`/invoices/${id}/parse`),
}
