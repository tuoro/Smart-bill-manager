import api from './auth'
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
  list: () => api.get<ApiResponse<Trip[]>>('/trips'),

  create: (trip: Omit<Trip, 'id' | 'created_at' | 'updated_at'>) =>
    api.post<ApiResponse<{ trip: Trip; changes?: AssignmentChangeSummary }>>('/trips', trip),

  update: (id: string, trip: Partial<Pick<Trip, 'name' | 'start_time' | 'end_time' | 'note' | 'reimburse_status' | 'timezone'>>) =>
    api.put<ApiResponse<{ changes?: AssignmentChangeSummary }>>(`/trips/${id}`, trip),

  getSummary: (id: string) => api.get<ApiResponse<TripSummary>>(`/trips/${id}/summary`),

  getPayments: (id: string, includeInvoices = true) =>
    api.get<ApiResponse<TripPaymentWithInvoices[]>>(`/trips/${id}/payments`, {
      params: { includeInvoices: includeInvoices ? 1 : 0 },
    }),

  cascadePreview: (id: string) => api.get<ApiResponse<TripCascadePreview>>(`/trips/${id}/cascade-preview`),

  deleteCascade: (id: string) => api.delete<ApiResponse<TripCascadePreview>>(`/trips/${id}`),

  pendingPayments: () => api.get<ApiResponse<PendingPayment[]>>('/trips/pending-payments'),

  assignPendingPayment: (paymentId: string, tripId: string) =>
    api.post<ApiResponse<void>>(`/trips/pending-payments/${paymentId}/assign`, { trip_id: tripId }),

  blockPendingPayment: (paymentId: string) => api.post<ApiResponse<void>>(`/trips/pending-payments/${paymentId}/block`),
}
