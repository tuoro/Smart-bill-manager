import type { components } from './generated/api'

export type Session = components['schemas']['Session']
export type JobSummary = components['schemas']['JobSummary']
export type Review = components['schemas']['Review']
export type RevisionRequest = components['schemas']['RevisionRequest']
export type ConfirmRequest = components['schemas']['ConfirmRequest']
export type ConfirmResult = components['schemas']['ConfirmResult']
export type Payment = components['schemas']['Payment']
export type Invoice = components['schemas']['Invoice']
export type ProviderConfig = components['schemas']['ProviderConfig']

type ErrorEnvelope = {
  error?: { code?: string; message?: string; resource_id?: string }
  request_id?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly resourceId?: string

  constructor(status: number, body: ErrorEnvelope) {
    super(body.error?.message ?? '服务暂时无法完成请求')
    this.name = 'ApiError'
    this.status = status
    this.code = body.error?.code ?? 'unknown_error'
    this.requestId = body.request_id ?? ''
    this.resourceId = body.error?.resource_id
  }
}

const apiBase = '/api/v1'

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const method = (init.method ?? 'GET').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    const csrf = readCookie('sbm_csrf')
    if (csrf) headers.set('X-CSRF-Token', csrf)
  }
  const response = await fetch(apiBase + path, {
    ...init,
    headers,
    credentials: 'same-origin',
  })
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as ErrorEnvelope
    throw new ApiError(response.status, body)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

function readCookie(name: string): string {
  const prefix = `${encodeURIComponent(name)}=`
  for (const entry of document.cookie.split(';')) {
    const value = entry.trim()
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length))
  }
  return ''
}

export const api = {
  login(email: string, password: string): Promise<Session> {
    return request('/session/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
  },
  session(): Promise<Session> {
    return request('/session')
  },
  logout(): Promise<void> {
    return request('/session', { method: 'DELETE' })
  },
  listJobs(status?: JobSummary['status']): Promise<{ items: JobSummary[] }> {
    const query = status ? `?status=${encodeURIComponent(status)}` : ''
    return request(`/jobs${query}`)
  },
  getJob(jobId: string): Promise<JobSummary> {
    return request(`/jobs/${encodeURIComponent(jobId)}`)
  },
  upload(file: File): Promise<components['schemas']['UploadResult']> {
    const form = new FormData()
    form.append('file', file)
    return request('/documents', { method: 'POST', body: form })
  },
  cancelJob(jobId: string): Promise<JobSummary> {
    return request(`/jobs/${encodeURIComponent(jobId)}/cancel`, { method: 'POST' })
  },
  retryJob(jobId: string): Promise<JobSummary> {
    return request(`/jobs/${encodeURIComponent(jobId)}/retry`, { method: 'POST' })
  },
  getReview(jobId: string): Promise<Review> {
    return request(`/reviews/${encodeURIComponent(jobId)}`)
  },
  revise(jobId: string, body: RevisionRequest): Promise<Review> {
    return request(`/reviews/${encodeURIComponent(jobId)}/revisions`, {
      method: 'POST',
      body: JSON.stringify(body),
    })
  },
  confirm(jobId: string, body: ConfirmRequest, idempotencyKey: string): Promise<ConfirmResult> {
    return request(`/reviews/${encodeURIComponent(jobId)}/confirm`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    })
  },
  reject(
    jobId: string,
    expectedRevision: number,
    reason: string,
    idempotencyKey: string,
  ): Promise<void> {
    return request(`/reviews/${encodeURIComponent(jobId)}/reject`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify({ expected_revision: expectedRevision, reason }),
    })
  },
  payments(): Promise<{ items: Payment[] }> {
    return request('/payments')
  },
  invoices(): Promise<{ items: Invoice[] }> {
    return request('/invoices')
  },
  providerConfigs(): Promise<{ items: ProviderConfig[] }> {
    return request('/provider-configs')
  },
  createProvider(
    baseUrl: string,
    apiKey: string,
    model: string,
    outputMode: ProviderConfig['output_mode'],
  ): Promise<ProviderConfig> {
    return request('/provider-configs', {
      method: 'POST',
      body: JSON.stringify({ base_url: baseUrl, api_key: apiKey, model, output_mode: outputMode }),
    })
  },
  detectProvider(id: string): Promise<ProviderConfig> {
    return request(`/provider-configs/${encodeURIComponent(id)}/detect`, { method: 'POST' })
  },
  activateProvider(id: string): Promise<ProviderConfig> {
    return request(`/provider-configs/${encodeURIComponent(id)}/activate`, { method: 'POST' })
  },
}
