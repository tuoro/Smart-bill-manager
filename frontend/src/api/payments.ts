import api from './auth'
import type { Payment, Invoice, ApiResponse, DedupHint } from '@/types'

type UploadScreenshotResult = {
  payment: Payment | null
  extracted: unknown
  screenshot_path: string
  ocr_error?: string
  dedup?: DedupHint | null
}

type UploadScreenshotAsyncResult = {
  taskId: string
  payment: Payment | null
  screenshot_path: string
}

export const paymentApi = {
  getAll: (params?: { limit?: number; offset?: number; startDate?: string; endDate?: string; category?: string }) =>
    api.get<ApiResponse<Payment[]>>('/payments', { params }),
  
  getById: (id: string) =>
    api.get<ApiResponse<Payment>>(`/payments/${id}`),
  
  getStats: (startDate?: string, endDate?: string) =>
    api.get<ApiResponse<{ totalAmount: number; totalCount: number; categoryStats: Record<string, number>; merchantStats: Record<string, number>; dailyStats: Record<string, number> }>>('/payments/stats', { params: { startDate, endDate } }),
  
  create: (payment: Omit<Payment, 'id' | 'created_at'>) =>
    api.post<ApiResponse<Payment>>('/payments', payment),
  
  update: (id: string, payment: (Partial<Payment> & { confirm?: boolean; force_duplicate_save?: boolean })) =>
    api.put<ApiResponse<void>>(`/payments/${id}`, payment),
  
  delete: (id: string) =>
    api.delete<ApiResponse<void>>(`/payments/${id}`),
  
  // Upload screenshot and OCR recognition
  uploadScreenshot: (file: File) => {
    const formData = new FormData()
    formData.append('file', file)
    return api.post<ApiResponse<UploadScreenshotResult>>('/payments/upload-screenshot', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  // Upload screenshot and OCR recognition (async task)
  uploadScreenshotAsync: (file: File) => {
    const formData = new FormData()
    formData.append('file', file)
    return api.post<ApiResponse<UploadScreenshotAsyncResult>>('/payments/upload-screenshot-async', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  cancelUploadScreenshot: (screenshot_path: string) =>
    api.post<ApiResponse<void>>('/payments/upload-screenshot/cancel', { screenshot_path }),
  
  // Reparse screenshot with OCR
  reparseScreenshot: (id: string) => 
    api.post<ApiResponse<unknown>>(`/payments/${id}/reparse`),
  
  // Get invoices linked to a payment
  getPaymentInvoices: (paymentId: string) =>
    api.get<ApiResponse<Invoice[]>>(`/payments/${paymentId}/invoices`),

  // Get suggested invoices for a payment (smart matching)
  getSuggestedInvoices: (paymentId: string, params?: { limit?: number; debug?: boolean }) =>
    api.get<ApiResponse<Invoice[]>>(`/payments/${paymentId}/suggest-invoices`, {
      params: {
        ...(params || {}),
        debug: params?.debug ? 1 : undefined,
      },
    }),

  getScreenshotBlob: (paymentId: string) =>
    api.get(`/payments/${paymentId}/screenshot`, { responseType: 'blob' }),
}
