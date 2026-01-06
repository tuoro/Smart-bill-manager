import axios from 'axios'

export const isRequestCanceled = (err: any): boolean => {
  if (!err) return false
  if (axios.isCancel?.(err)) return true
  const code = String(err?.code || '')
  const name = String(err?.name || '')
  return code === 'ERR_CANCELED' || name === 'CanceledError' || name === 'AbortError'
}

export const getApiErrorMessage = (err: any, fallback: string): string => {
  const msg =
    err?.response?.data?.message ||
    err?.response?.data?.error ||
    err?.message ||
    ''
  return String(msg || fallback)
}

