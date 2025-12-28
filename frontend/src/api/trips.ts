import api from './auth'
import type {
  ApiResponse,
  Trip,
  TripSummary,
  TripAssignPreview,
  TripCascadePreview,
  TripPaymentWithInvoices,
} from '@/types'

export const tripsApi = {
  list: () => api.get<ApiResponse<Trip[]>>('/trips'),

  create: (trip: Omit<Trip, 'id' | 'created_at' | 'updated_at'>) => api.post<ApiResponse<Trip>>('/trips', trip),

  update: (id: string, trip: Partial<Pick<Trip, 'name' | 'start_time' | 'end_time' | 'note'>>) =>
    api.put<ApiResponse<void>>(`/trips/${id}`, trip),

  getSummary: (id: string) => api.get<ApiResponse<TripSummary>>(`/trips/${id}/summary`),

  getPayments: (id: string, includeInvoices = true) =>
    api.get<ApiResponse<TripPaymentWithInvoices[]>>(`/trips/${id}/payments`, {
      params: { includeInvoices: includeInvoices ? 1 : 0 },
    }),

  assignByTimePreview: (id: string) => api.post<ApiResponse<TripAssignPreview>>(`/trips/${id}/assign-by-time`, { dry_run: true }),

  assignByTime: (id: string) => api.post<ApiResponse<TripAssignPreview>>(`/trips/${id}/assign-by-time`, { dry_run: false }),

  cascadePreview: (id: string) => api.get<ApiResponse<TripCascadePreview>>(`/trips/${id}/cascade-preview`),

  deleteCascade: (id: string) => api.delete<ApiResponse<TripCascadePreview>>(`/trips/${id}`),
}

