import api from './auth'
import type { AxiosRequestConfig } from 'axios'
import type {
  ApiResponse,
  AssignmentChangeSummary,
  PendingPayment,
  Trip,
  TripSummary,
  TripCascadePreview,
  TripPaymentWithInvoices,
} from '@/types'

export const tripsApi = {
  list: (config?: AxiosRequestConfig) => api.get<ApiResponse<Trip[]>>('/trips', config),

  getSummaries: (config?: AxiosRequestConfig) => api.get<ApiResponse<TripSummary[]>>('/trips/summaries', config),

  create: (trip: Omit<Trip, 'id' | 'created_at' | 'updated_at'>) =>
    api.post<ApiResponse<{ trip: Trip; changes?: AssignmentChangeSummary }>>('/trips', trip),

  update: (id: string, trip: Partial<Pick<Trip, 'name' | 'start_time' | 'end_time' | 'note' | 'reimburse_status' | 'timezone'>>) =>
    api.put<ApiResponse<{ changes?: AssignmentChangeSummary }>>(`/trips/${id}`, trip),

  getSummary: (id: string, config?: AxiosRequestConfig) => api.get<ApiResponse<TripSummary>>(`/trips/${id}/summary`, config),

  getPayments: (id: string, includeInvoices = true, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<TripPaymentWithInvoices[]>>(`/trips/${id}/payments`, {
      params: { includeInvoices: includeInvoices ? 1 : 0 },
      ...(config || {}),
    }),

  exportZip: async (id: string, config?: AxiosRequestConfig) =>
    api.get(`/trips/${id}/export`, {
      responseType: 'blob',
      ...(config || {}),
    }),

  cascadePreview: (id: string, config?: AxiosRequestConfig) =>
    api.get<ApiResponse<TripCascadePreview>>(`/trips/${id}/cascade-preview`, config),

  deleteCascade: (
    id: string,
    opts?: { deletePayments?: boolean },
  ) =>
    api.delete<ApiResponse<TripCascadePreview>>(`/trips/${id}`, {
      params: opts,
    }),

  pendingPayments: (config?: AxiosRequestConfig) => api.get<ApiResponse<PendingPayment[]>>('/trips/pending-payments', config),

  assignPendingPayment: (paymentId: string, tripId: string) =>
    api.post<ApiResponse<void>>(`/trips/pending-payments/${paymentId}/assign`, { trip_id: tripId }),

  blockPendingPayment: (paymentId: string) => api.post<ApiResponse<void>>(`/trips/pending-payments/${paymentId}/block`),
}
