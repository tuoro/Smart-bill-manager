import axios from 'axios'
import type { InternalAxiosRequestConfig } from 'axios'

import { clearAuth, getActAsUserId, getToken } from './storage'

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'
export const FILE_BASE_URL = import.meta.env.VITE_FILE_URL || ''

const positiveNumber = (value: unknown, fallback: number): number => {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

const API_TIMEOUT_MS = positiveNumber(import.meta.env.VITE_API_TIMEOUT_MS, 15000)
const API_CONCURRENCY = Math.floor(positiveNumber(import.meta.env.VITE_API_CONCURRENCY, 6))

export type ActAsConfirmInfo = {
  code?: string
  actor_user_id?: string
  target_user_id?: string
  method?: string
  path?: string
}

let authErrorHandler: (() => void) | null = null
let actAsConfirmHandler: ((info: ActAsConfirmInfo) => Promise<boolean>) | null = null

export const setAuthErrorHandler = (handler: (() => void) | null) => {
  authErrorHandler = handler
}

export const setActAsConfirmHandler = (handler: ((info: ActAsConfirmInfo) => Promise<boolean>) | null) => {
  actAsConfirmHandler = handler
}

type ReleaseFn = () => void
type SbmInternalConfig = InternalAxiosRequestConfig & { _sbmRelease?: ReleaseFn }

export const createConcurrencyLimiter = (max: number) => {
  const limit = Math.max(1, Math.floor(positiveNumber(max, 1)))
  const queue: Array<() => void> = []
  let active = 0

  const acquire = () =>
    new Promise<ReleaseFn>((resolve) => {
      const grant = () => {
        active += 1
        let released = false
        resolve(() => {
          if (released) return
          released = true
          active = Math.max(0, active - 1)
          queue.shift()?.()
        })
      }

      if (active < limit) {
        grant()
        return
      }
      queue.push(grant)
    })

  return { acquire }
}

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: API_TIMEOUT_MS,
})

const limiter = createConcurrencyLimiter(API_CONCURRENCY)

api.interceptors.request.use(async (config) => {
  const cfg = config as SbmInternalConfig
  const token = getToken()
  if (token) cfg.headers.Authorization = `Bearer ${token}`

  const actAsUserId = getActAsUserId()
  if (actAsUserId) cfg.headers['X-Act-As-User'] = actAsUserId

  cfg._sbmRelease = await limiter.acquire()
  return cfg
})

api.interceptors.response.use(
  (response) => {
    const release = (response.config as SbmInternalConfig)?._sbmRelease
    release?.()
    return response
  },
  async (error) => {
    const release = (error?.config as SbmInternalConfig | undefined)?._sbmRelease
    release?.()

    if (error.response?.status === 400 && error.response?.data?.data?.code === 'ACT_AS_CONFIRM_REQUIRED') {
      const originalConfig = error.config
      const alreadyConfirmed = originalConfig?.headers?.['X-Act-As-Confirmed']
      if (!alreadyConfirmed && actAsConfirmHandler && originalConfig) {
        const confirmed = await actAsConfirmHandler(error.response.data.data)
        if (confirmed) {
          originalConfig.headers = originalConfig.headers || {}
          originalConfig.headers['X-Act-As-Confirmed'] = '1'
          return api.request(originalConfig)
        }
      }
    }

    if (error.response?.status === 401) {
      clearAuth()
      authErrorHandler?.()
    }
    return Promise.reject(error)
  },
)

export default api
