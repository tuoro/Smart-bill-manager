import axios from 'axios'

type UnknownRecord = Record<string, unknown>

const asRecord = (value: unknown): UnknownRecord | null =>
  typeof value === 'object' && value !== null ? (value as UnknownRecord) : null

const asMessage = (value: unknown) => {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

export const isRequestCanceled = (error: unknown): boolean => {
  if (!error) return false
  if (axios.isCancel(error)) return true
  const record = asRecord(error)
  const code = asMessage(record?.code)
  const name = asMessage(record?.name)
  return code === 'ERR_CANCELED' || name === 'CanceledError' || name === 'AbortError'
}

export const getApiErrorMessage = (error: unknown, fallback: string): string => {
  const record = asRecord(error)
  const response = asRecord(record?.response)
  const data = asRecord(response?.data)
  return asMessage(data?.message) || asMessage(data?.error) || asMessage(record?.message) || fallback
}
