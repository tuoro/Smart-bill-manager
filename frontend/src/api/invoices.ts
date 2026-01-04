import api from './auth'
import type { Invoice, Payment, ApiResponse, DedupHint } from '@/types'

type UploadInvoiceResult = {
  invoice: Invoice
  dedup?: DedupHint | null
}

type UploadInvoiceAsyncResult = {
  taskId: string
  invoice: Invoice
}

export const invoiceApi = {
  getAll: (params?: { limit?: number; offset?: number }) =>
    api.get<ApiResponse<Invoice[]>>('/invoices', { params }),

  getUnlinked: (params?: { limit?: number; offset?: number }) =>
    api.get<ApiResponse<{ items: Invoice[]; total: number }>>('/invoices/unlinked', { params }),
  
  getById: (id: string) =>
    api.get<ApiResponse<Invoice>>(`/invoices/${id}`),
  
  getStats: () =>
    api.get<ApiResponse<{ totalCount: number; totalAmount: number; bySource: Record<string, number>; byMonth: Record<string, number> }>>('/invoices/stats'),
  
  upload: (file: File, paymentId?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    if (paymentId) formData.append('payment_id', paymentId)
    return api.post<ApiResponse<UploadInvoiceResult>>('/invoices/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  uploadAsync: (file: File, paymentId?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    if (paymentId) formData.append('payment_id', paymentId)
    return api.post<ApiResponse<UploadInvoiceAsyncResult>>('/invoices/upload-async', formData, {
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

  uploadMultipleAsync: (files: File[], paymentId?: string) => {
    const formData = new FormData()
    files.forEach(file => formData.append('files', file))
    if (paymentId) formData.append('payment_id', paymentId)
    return api.post<ApiResponse<Array<{ taskId: string; invoice: Invoice }>>>('/invoices/upload-multiple-async', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  
  update: (id: string, invoice: (Partial<Invoice> & { confirm?: boolean; force_duplicate_save?: boolean })) =>
    api.put<ApiResponse<void>>(`/invoices/${id}`, invoice),
  
  delete: (id: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${id}`),
  
  parse: (id: string) =>
    api.post<ApiResponse<Invoice>>(`/invoices/${id}/parse`),
  
  // Get payments linked to an invoice
  getLinkedPayments: (invoiceId: string) =>
    api.get<ApiResponse<Payment[]>>(`/invoices/${invoiceId}/linked-payments`),
  
  // Get suggested payments for an invoice (smart matching)
  getSuggestedPayments: (invoiceId: string, params?: { limit?: number; debug?: boolean }) =>
    api.get<ApiResponse<Payment[]>>(`/invoices/${invoiceId}/suggest-payments`, {
      params: {
        ...(params || {}),
        debug: params?.debug ? 1 : undefined,
      },
    }),
  
  // Link a payment to an invoice
  linkPayment: (invoiceId: string, paymentId: string) =>
    api.post<ApiResponse<void>>(`/invoices/${invoiceId}/link-payment`, { payment_id: paymentId }),
  
  // Unlink a payment from an invoice
  unlinkPayment: (invoiceId: string, paymentId: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${invoiceId}/unlink-payment?payment_id=${paymentId}`),

  getFileBlob: (invoiceId: string) =>
    api.get(`/invoices/${invoiceId}/file`, { responseType: 'blob' }),
}
