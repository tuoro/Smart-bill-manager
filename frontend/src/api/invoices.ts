import api from './auth'
import type { AxiosRequestConfig } from 'axios'
import type { Invoice, InvoiceAttachment, Payment, ApiResponse, DedupHint } from '@/types'

type UploadInvoiceResult = {
  invoice: Invoice
  dedup?: DedupHint | null
}

type UploadInvoiceAsyncResult = {
  taskId: string
  invoice: Invoice
}

export const invoiceApi = {
  getAll: (
    params?: { limit?: number; offset?: number; startDate?: string; endDate?: string; includeDraft?: boolean },
    config?: AxiosRequestConfig,
  ) =>
    api.get<ApiResponse<{ items: Invoice[]; total: number }>>('/invoices', { params, ...(config || {}) }),

  getUnlinked: (params?: { limit?: number; offset?: number }, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<{ items: Invoice[]; total: number }>>('/invoices/unlinked', { params, ...(config || {}) }),
  
  getById: (id: string, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<Invoice>>(`/invoices/${id}`, config),
  
  getStats: (params?: { startDate?: string; endDate?: string }, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<{ totalCount: number; totalAmount: number; bySource: Record<string, number>; byMonth: Record<string, number> }>>(
      '/invoices/stats',
      { params, ...(config || {}) },
    ),
  
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
  getLinkedPayments: (invoiceId: string, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<Payment[]>>(`/invoices/${invoiceId}/linked-payments`, config),
  
  // Get suggested payments for an invoice (smart matching)
  getSuggestedPayments: (invoiceId: string, params?: { limit?: number; debug?: boolean }, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<Payment[]>>(`/invoices/${invoiceId}/suggest-payments`, {
      params: {
        ...(params || {}),
        debug: params?.debug ? 1 : undefined,
      },
      ...(config || {}),
    }),
  
  // Link a payment to an invoice
  linkPayment: (invoiceId: string, paymentId: string) =>
    api.post<ApiResponse<void>>(`/invoices/${invoiceId}/link-payment`, { payment_id: paymentId }),
  
  // Unlink a payment from an invoice
  unlinkPayment: (invoiceId: string, paymentId: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${invoiceId}/unlink-payment?payment_id=${paymentId}`),

  getFileBlob: (invoiceId: string, config?: AxiosRequestConfig) =>
    api.get(`/invoices/${invoiceId}/file`, { responseType: 'blob', ...(config || {}) }),

  getAttachmentBlob: (invoiceId: string, attachmentId: string, config?: AxiosRequestConfig) =>
    api.get<Blob>(`/invoices/${invoiceId}/attachments/${attachmentId}/download`, { responseType: 'blob', ...(config || {}) }),

  uploadAttachment: (invoiceId: string, file: File, kind?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    if (kind) formData.append('kind', kind)
    return api.post<ApiResponse<{ attachment: InvoiceAttachment }>>(`/invoices/${invoiceId}/attachments`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },

  deleteAttachment: (invoiceId: string, attachmentId: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${invoiceId}/attachments/${attachmentId}`),
}
