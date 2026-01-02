import api from './auth'
import type { ApiResponse } from '@/types'

export type RegressionSample = {
  id: string
  kind: 'payment_screenshot' | 'invoice' | string
  name: string
  origin?: 'ui' | 'repo' | string
  source_type: 'payment' | 'invoice' | string
  source_id: string
  created_by: string
  created_at: string
  updated_at?: string
}

export const regressionSamplesApi = {
  markPayment: (paymentId: string, name?: string) =>
    api.post<ApiResponse<RegressionSample>>(`/admin/regression-samples/payments/${paymentId}`, { name: name || '' }),

  markInvoice: (invoiceId: string, name?: string) =>
    api.post<ApiResponse<RegressionSample>>(`/admin/regression-samples/invoices/${invoiceId}`, { name: name || '' }),

  list: (params?: { kind?: string; search?: string; limit?: number; offset?: number }) =>
    api.get<ApiResponse<{ items: RegressionSample[]; total: number }>>('/admin/regression-samples', { params }),

  bulkDelete: (ids: string[]) => api.post<ApiResponse<{ deleted: number }>>('/admin/regression-samples/bulk-delete', { ids }),

  delete: (id: string) => api.delete<ApiResponse<{ deleted: boolean }>>(`/admin/regression-samples/${id}`),

  exportZip: async (kind?: string) =>
    api.get('/admin/regression-samples/export', {
      params: { kind: kind || undefined },
      responseType: 'blob',
    }),

  syncFromRepo: (mode?: 'repo_only' | 'overwrite') =>
    api.post<ApiResponse<{ files: number; inserted: number; updated: number; skipped: number; errors: number; error_list?: string[] }>>(
      '/admin/regression-samples/sync',
      {},
      { params: { mode: mode || 'repo_only' } }
    ),
}
