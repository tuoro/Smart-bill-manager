import axios from 'axios'
import type { ApiResponse, User } from '@/types'

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'
export const FILE_BASE_URL = import.meta.env.VITE_FILE_URL || ''

// Get stored token
const getToken = (): string | null => {
  return localStorage.getItem('token')
}

// Set stored token
export const setToken = (token: string | null) => {
  if (token) {
    localStorage.setItem('token', token)
  } else {
    localStorage.removeItem('token')
  }
}

// Get stored user
export const getStoredUser = (): User | null => {
  const userStr = localStorage.getItem('user')
  if (userStr) {
    try {
      return JSON.parse(userStr)
    } catch {
      return null
    }
  }
  return null
}

// Set stored user
export const setStoredUser = (user: User | null) => {
  if (user) {
    localStorage.setItem('user', JSON.stringify(user))
  } else {
    localStorage.removeItem('user')
  }
}

// Clear auth data
export const clearAuth = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
}

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Add token to requests
api.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Handle 401 responses
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      clearAuth()
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

// Auth APIs
export const authApi = {
  login: (username: string, password: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/login', { username, password }),
  
  register: (username: string, password: string, email?: string) =>
    api.post<{ success: boolean; message: string; user?: User; token?: string }>('/auth/register', { username, password, email }),
  
  verify: () =>
    api.get<ApiResponse<{ userId: string; username: string; role: string }>>('/auth/verify'),
  
  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<ApiResponse<void>>('/auth/change-password', { oldPassword, newPassword }),
  
  getCurrentUser: () =>
    api.get<ApiResponse<User>>('/auth/me'),
  
  checkSetupRequired: () =>
    api.get<ApiResponse<{ setupRequired: boolean }>>('/auth/setup-required'),
}

export default api
