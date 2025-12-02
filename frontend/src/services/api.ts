import axios from 'axios';
import type { Payment, Invoice, EmailConfig, DashboardData, ApiResponse, EmailLog, DingtalkConfig, DingtalkLog } from '../types';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:3001/api';
export const FILE_BASE_URL = import.meta.env.VITE_API_URL?.replace('/api', '') || 'http://localhost:3001';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Payment APIs
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
};

// Invoice APIs
export const invoiceApi = {
  getAll: (params?: { limit?: number; offset?: number }) =>
    api.get<ApiResponse<Invoice[]>>('/invoices', { params }),
  
  getById: (id: string) =>
    api.get<ApiResponse<Invoice>>(`/invoices/${id}`),
  
  getStats: () =>
    api.get<ApiResponse<{ totalCount: number; totalAmount: number; bySource: Record<string, number>; byMonth: Record<string, number> }>>('/invoices/stats'),
  
  upload: (file: File, paymentId?: string) => {
    const formData = new FormData();
    formData.append('file', file);
    if (paymentId) formData.append('payment_id', paymentId);
    return api.post<ApiResponse<Invoice>>('/invoices/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  
  uploadMultiple: (files: File[], paymentId?: string) => {
    const formData = new FormData();
    files.forEach(file => formData.append('files', file));
    if (paymentId) formData.append('payment_id', paymentId);
    return api.post<ApiResponse<Invoice[]>>('/invoices/upload-multiple', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  
  update: (id: string, invoice: Partial<Invoice>) =>
    api.put<ApiResponse<void>>(`/invoices/${id}`, invoice),
  
  delete: (id: string) =>
    api.delete<ApiResponse<void>>(`/invoices/${id}`),
};

// Email APIs
export const emailApi = {
  getConfigs: () =>
    api.get<ApiResponse<EmailConfig[]>>('/email/configs'),
  
  createConfig: (config: Omit<EmailConfig, 'id' | 'created_at' | 'last_check'>) =>
    api.post<ApiResponse<EmailConfig>>('/email/configs', config),
  
  updateConfig: (id: string, config: Partial<EmailConfig>) =>
    api.put<ApiResponse<void>>(`/email/configs/${id}`, config),
  
  deleteConfig: (id: string) =>
    api.delete<ApiResponse<void>>(`/email/configs/${id}`),
  
  testConnection: (config: { email: string; imap_host: string; imap_port: number; password: string }) =>
    api.post<ApiResponse<void>>('/email/test', config),
  
  getLogs: (configId?: string, limit?: number) =>
    api.get<ApiResponse<EmailLog[]>>('/email/logs', { params: { configId, limit } }),
  
  startMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/start/${id}`),
  
  stopMonitoring: (id: string) =>
    api.post<ApiResponse<void>>(`/email/monitor/stop/${id}`),
  
  getMonitoringStatus: () =>
    api.get<ApiResponse<{ configId: string; status: string }[]>>('/email/monitor/status'),
  
  manualCheck: (id: string) =>
    api.post<ApiResponse<{ newEmails: number }>>(`/email/check/${id}`),
};

// DingTalk APIs
export const dingtalkApi = {
  getConfigs: () =>
    api.get<ApiResponse<DingtalkConfig[]>>('/dingtalk/configs'),
  
  createConfig: (config: Omit<DingtalkConfig, 'id' | 'created_at'>) =>
    api.post<ApiResponse<DingtalkConfig>>('/dingtalk/configs', config),
  
  updateConfig: (id: string, config: Partial<DingtalkConfig>) =>
    api.put<ApiResponse<void>>(`/dingtalk/configs/${id}`, config),
  
  deleteConfig: (id: string) =>
    api.delete<ApiResponse<void>>(`/dingtalk/configs/${id}`),
  
  getLogs: (configId?: string, limit?: number) =>
    api.get<ApiResponse<DingtalkLog[]>>('/dingtalk/logs', { params: { configId, limit } }),
};

// Dashboard API
export const dashboardApi = {
  getSummary: () =>
    api.get<ApiResponse<DashboardData>>('/dashboard'),
};

export default api;
