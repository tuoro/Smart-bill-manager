// API Configuration
export const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'
export const FILE_BASE_URL = import.meta.env.VITE_FILE_URL || ''
export const BACKEND_PORT = import.meta.env.VITE_BACKEND_PORT || '3001'

// Theme Colors
export const THEME_COLORS = {
  primary: '#1890ff',
  success: '#52c41a',
  warning: '#faad14',
  danger: '#f5222d',
  purple: '#722ed1',
  cyan: '#13c2c2'
}

// Chart Colors Palette
export const CHART_COLORS = ['#1890ff', '#52c41a', '#faad14', '#f5222d', '#722ed1', '#13c2c2']

// Gradient Colors
export const GRADIENTS = {
  purple: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
  pink: 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)',
  blue: 'linear-gradient(135deg, #4facfe 0%, #00f2fe 100%)',
  green: 'linear-gradient(135deg, #43e97b 0%, #38f9d7 100%)'
}

// Helper to get backend base URL
export function getBackendBaseUrl(): string {
  const origin = window.location.origin
  // In production, use the same origin (nginx proxies /api)
  // In development, replace the frontend port with backend port
  if (import.meta.env.DEV) {
    return origin.replace(/:\d+$/, `:${BACKEND_PORT}`)
  }
  return origin
}
