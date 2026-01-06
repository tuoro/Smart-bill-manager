import api from './auth'
import type { AxiosRequestConfig } from 'axios'
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

export type SampleQualityIssue = {
  level: 'error' | 'warn' | string
  code: string
  message: string
}

export type MarkRegressionSampleResult = {
  sample: RegressionSample
  issues?: SampleQualityIssue[]
}

export const regressionSamplesApi = {
  markPayment: (paymentId: string, opts?: { name?: string; force?: boolean }) =>
    api.post<ApiResponse<MarkRegressionSampleResult>>(`/admin/regression-samples/payments/${paymentId}`, {
      name: opts?.name || '',
      force: Boolean(opts?.force),
    }),

  markInvoice: (invoiceId: string, opts?: { name?: string; force?: boolean }) =>
    api.post<ApiResponse<MarkRegressionSampleResult>>(`/admin/regression-samples/invoices/${invoiceId}`, {
      name: opts?.name || '',
      force: Boolean(opts?.force),
    }),

  list: (params?: { kind?: string; origin?: string; search?: string; limit?: number; offset?: number }, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<{ items: RegressionSample[]; total: number }>>('/admin/regression-samples', { params, ...(config || {}) }),

  bulkDelete: (ids: string[]) => api.post<ApiResponse<{ deleted: number }>>('/admin/regression-samples/bulk-delete', { ids }),

  delete: (id: string) => api.delete<ApiResponse<{ deleted: boolean }>>(`/admin/regression-samples/${id}`),

  exportZip: async (params?: { kind?: string; origin?: string; redact?: boolean }) =>
    api.get('/admin/regression-samples/export', {
      params: {
        kind: params?.kind || undefined,
        origin: params?.origin || undefined,
        redact: params?.redact ? 1 : undefined,
      },
      responseType: 'blob',
    }),

  exportSelectedZip: async (input: { ids: string[]; kind?: string; origin?: string; redact?: boolean }) =>
    api.post('/admin/regression-samples/export', input, { responseType: 'blob' }),
}
