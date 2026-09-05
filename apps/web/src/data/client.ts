import type { components } from './generated/api'

export type Session = components['schemas']['Session']
export type ExportScope = components['schemas']['ExportScope']
export type ExportManifest = components['schemas']['ExportManifest']
export type ExportPrepared = components['schemas']['ExportPrepared']
export type Member = components['schemas']['Member']
export type MemberPage = components['schemas']['MemberPage']
export type MemberChange = components['schemas']['MemberChange']
export type Invitation = components['schemas']['Invitation']
export type InvitationPage = components['schemas']['InvitationPage']
export type InvitationRequest = components['schemas']['InvitationRequest']
export type InvitationCreated = components['schemas']['InvitationCreated']
export type InvitationView = components['schemas']['InvitationView']
export type WorkspaceChoices = components['schemas']['WorkspaceChoices']
export type JobSummary = components['schemas']['JobSummary']
export type Review = components['schemas']['Review']
export type RevisionRequest = components['schemas']['RevisionRequest']
export type ManualReviewRequest = components['schemas']['ManualReviewRequest']
export type ManualReviewResult = components['schemas']['ManualReviewResult']
export type ConfirmRequest = components['schemas']['ConfirmRequest']
export type ConfirmResult = components['schemas']['ConfirmResult']
export type CorrectionWorkspace = components['schemas']['CorrectionWorkspace']
export type CorrectionRequest = components['schemas']['CorrectionRequest']
export type CorrectionConfirmRequest = components['schemas']['CorrectionConfirmRequest']
export type CorrectionPreview = components['schemas']['CorrectionPreview']
export type CorrectionResult = components['schemas']['CorrectionResult']
export type CorrectionHistoryPage = components['schemas']['CorrectionHistoryPage']
export type CorrectionFactType = CorrectionResult['fact_type']
export type Payment = components['schemas']['Payment']
export type Invoice = components['schemas']['Invoice']
export type InvoiceMaterial = components['schemas']['InvoiceMaterial']
export type InvoiceMaterialWorkspace = components['schemas']['InvoiceMaterialWorkspace']
export type InvoiceMaterialPage = components['schemas']['InvoiceMaterialPage']
export type InvoiceMaterialRequest = components['schemas']['InvoiceMaterialRequest']
export type InvoiceMaterialResult = components['schemas']['InvoiceMaterialResult']
export type FactDetail = components['schemas']['FactDetail']
export type FactKind = FactDetail['fact_type']
export type FactListQuery = {
  cursor?: string
  limit?: string
  date_from?: string
  date_to?: string
  q?: string
  allocation_status?: string
}
export type FactListPage<T> = { items: T[]; next_cursor: string }

function factQueryString(query: FactListQuery): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query))
    if (value !== undefined && value !== '') params.set(key, value)
  return params.size ? `?${params.toString()}` : ''
}
export type Trip = components['schemas']['Trip']
export type TripManagementRequest = components['schemas']['TripManagementRequest']
export type TripManagementResult = components['schemas']['TripManagementResult']
export type TripEvidence = components['schemas']['TripEvidence']
export type TripEvidencePage = components['schemas']['TripEvidencePage']
export type TripMaterialRequest = components['schemas']['TripMaterialRequest']
export type TripAttributionView = components['schemas']['TripAttributionView']
export type TripAttributionCandidate = components['schemas']['TripAttributionCandidate']
export type TripAttributionPage = components['schemas']['TripAttributionPage']
export type TripAssignmentRequest = components['schemas']['TripAssignmentRequest']
export type TripAssignmentResult = components['schemas']['TripAssignmentResult']
export type ReimbursementStatus = components['schemas']['ReimbursementStatus']
export type ReimbursementPolicyItem = components['schemas']['ReimbursementPolicyItem']
export type ReimbursementPolicyFinding = components['schemas']['ReimbursementPolicyFinding']
export type ReimbursementPolicySnapshot = components['schemas']['ReimbursementPolicySnapshot']
export type ReimbursementPreviewRequest = components['schemas']['ReimbursementPreviewRequest']
export type ReimbursementSubmissionRequest = components['schemas']['ReimbursementSubmissionRequest']
export type ReimbursementSummary = components['schemas']['ReimbursementSummary']
export type ReimbursementPage = components['schemas']['ReimbursementPage']
export type ReimbursementDetail = components['schemas']['ReimbursementDetail']
export type ReimbursementStatusRequest = components['schemas']['ReimbursementStatusRequest']
export type ReimbursementMutationResult = components['schemas']['ReimbursementMutationResult']
export type InsightFactTypeFilter = components['schemas']['InsightFactTypeFilter']
export type InsightAllocationStatusFilter = components['schemas']['InsightAllocationStatusFilter']
export type InsightTripScope = components['schemas']['InsightTripScope']
export type InsightFilter = components['schemas']['InsightFilter']
export type InsightFact = components['schemas']['InsightFact']
export type InsightAggregate = components['schemas']['InsightAggregate']
export type InsightPage = components['schemas']['InsightPage']
export type AllocationFactType = components['schemas']['AllocationFactType']
export type AllocationWorkspace = components['schemas']['AllocationWorkspace']
export type AllocationTargetPage = components['schemas']['AllocationTargetPage']
export type AllocationAdjustmentRequest = components['schemas']['AllocationAdjustmentRequest']
export type AllocationAdjustmentResult = components['schemas']['AllocationAdjustmentResult']
export type ProviderConfig = components['schemas']['ProviderConfig']
export type UploadResult = components['schemas']['UploadResult']
export type EmailSourceRegistration = components['schemas']['EmailSourceRegistration']
export type EmailSource = components['schemas']['EmailSource']
export type EmailMessagePage = components['schemas']['EmailMessagePage']
export type EmailMessage = components['schemas']['EmailMessage']
export type EmailAttachment = components['schemas']['EmailAttachment']

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
let authenticationLifecycle: {
  generation: () => number
  expired: (generation: number) => void
} | null = null

export function setAuthenticationLifecycle(
  value: NonNullable<typeof authenticationLifecycle>,
): void {
  authenticationLifecycle = value
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const generation = authenticationLifecycle?.generation()
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
    const publicAuthentication = [
      '/session/login',
      '/session/workspaces',
      '/invitations/check',
      '/invitations/accept',
    ].includes(path)
    if (
      response.status === 401 &&
      body.error?.code === 'unauthenticated' &&
      !publicAuthentication &&
      path !== '/session' &&
      generation !== undefined
    ) {
      authenticationLifecycle?.expired(generation)
    }
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
  previewMaterialExport(scope: ExportScope, signal?: AbortSignal): Promise<ExportManifest> {
    return request('/material-exports/preview', {
      method: 'POST',
      body: JSON.stringify(scope),
      signal,
    })
  },
  prepareMaterialExport(
    scope: ExportScope,
    hash: string,
    acknowledged: boolean,
    signal?: AbortSignal,
  ): Promise<ExportPrepared> {
    return request('/material-exports', {
      method: 'POST',
      body: JSON.stringify({
        ...scope,
        expected_manifest_hash: hash,
        acknowledged_warnings: acknowledged,
      }),
      signal,
    })
  },
  cancelMaterialExport(id: string): Promise<void> {
    return request(`/material-exports/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },
  materialExportURL(id: string): string {
    return `${apiBase}/material-exports/${encodeURIComponent(id)}/content`
  },
  member(id: string): Promise<Member> {
    return request(`/members/${encodeURIComponent(id)}`)
  },
  invitation(id: string): Promise<Invitation> {
    return request(`/member-invitations/${encodeURIComponent(id)}`)
  },
  members(cursor = ''): Promise<MemberPage> {
    return request(`/members${factQueryString({ cursor, limit: '20' })}`)
  },
  changeMember(id: string, body: MemberChange): Promise<Member> {
    return request(`/members/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    })
  },
  invitations(cursor = ''): Promise<InvitationPage> {
    return request(`/member-invitations${factQueryString({ cursor, limit: '20' })}`)
  },
  createInvitation(body: InvitationRequest): Promise<InvitationCreated> {
    return request('/member-invitations', { method: 'POST', body: JSON.stringify(body) })
  },
  revokeInvitation(id: string, expected_version: number, reason: string): Promise<Invitation> {
    return request(`/member-invitations/${encodeURIComponent(id)}/revoke`, {
      method: 'POST',
      body: JSON.stringify({ expected_version, reason }),
    })
  },
  checkInvitation(code: string): Promise<InvitationView> {
    return request('/invitations/check', { method: 'POST', body: JSON.stringify({ code }) })
  },
  acceptInvitation(code: string, display_name: string, password: string): Promise<void> {
    return request('/invitations/accept', {
      method: 'POST',
      body: JSON.stringify({ code, display_name, password }),
    })
  },
  workspaces(email: string, password: string): Promise<WorkspaceChoices> {
    return request('/session/workspaces', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
  },
  changePassword(current_password: string, new_password: string): Promise<void> {
    return request('/account/password', {
      method: 'POST',
      body: JSON.stringify({ current_password, new_password }),
    })
  },
  invoiceMaterials(id: string): Promise<InvoiceMaterialWorkspace> {
    return request(`/invoices/${encodeURIComponent(id)}/materials`)
  },
  invoiceMaterialCandidates(id: string, query = '', cursor = ''): Promise<InvoiceMaterialPage> {
    return request(
      `/invoices/${encodeURIComponent(id)}/material-candidates${factQueryString({ q: query, cursor, limit: '20' })}`,
    )
  },
  addInvoiceMaterial(
    id: string,
    documentId: string,
    body: InvoiceMaterialRequest,
  ): Promise<InvoiceMaterialResult> {
    return request(`/invoices/${encodeURIComponent(id)}/materials`, {
      method: 'POST',
      body: JSON.stringify({ ...body, document_id: documentId }),
    })
  },
  removeInvoiceMaterial(
    id: string,
    linkId: string,
    body: InvoiceMaterialRequest,
  ): Promise<InvoiceMaterialResult> {
    return request(
      `/invoices/${encodeURIComponent(id)}/materials/${encodeURIComponent(linkId)}/remove`,
      { method: 'POST', body: JSON.stringify(body) },
    )
  },
  uploadInvoiceMaterial(
    id: string,
    file: File,
    body: InvoiceMaterialRequest,
  ): Promise<InvoiceMaterialResult> {
    const form = new FormData()
    form.set('file', file)
    for (const [key, value] of Object.entries(body)) form.set(key, String(value))
    return request(`/invoices/${encodeURIComponent(id)}/materials/upload`, {
      method: 'POST',
      body: form,
    })
  },
  claimSet(id: string): Promise<Review> {
    return request(`/claim-sets/${encodeURIComponent(id)}`)
  },
  correction(kind: CorrectionFactType, id: string): Promise<CorrectionWorkspace> {
    return request(`/facts/${kind}/${encodeURIComponent(id)}/correction`)
  },
  correctionHistory(
    kind: CorrectionFactType,
    id: string,
    before = 0,
  ): Promise<CorrectionHistoryPage> {
    return request(
      `/facts/${kind}/${encodeURIComponent(id)}/correction/history?before_revision=${before}&limit=20`,
    )
  },
  previewCorrection(
    kind: CorrectionFactType,
    id: string,
    body: CorrectionRequest,
  ): Promise<CorrectionPreview> {
    return request(`/facts/${kind}/${encodeURIComponent(id)}/correction/preview`, {
      method: 'POST',
      body: JSON.stringify(body),
    })
  },
  confirmCorrection(
    kind: CorrectionFactType,
    id: string,
    body: CorrectionConfirmRequest,
    key: string,
  ): Promise<CorrectionResult> {
    return request(`/facts/${kind}/${encodeURIComponent(id)}/correction`, {
      method: 'POST',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    })
  },
  login(email: string, password: string, tenantId = ''): Promise<Session> {
    return request('/session/login', {
      method: 'POST',
      body: JSON.stringify({ email, password, ...(tenantId ? { tenant_id: tenantId } : {}) }),
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
  upload(file: File): Promise<UploadResult> {
    const form = new FormData()
    form.append('file', file)
    return request('/documents', { method: 'POST', body: form })
  },
  documentContentURL(documentId: string): string {
    return `${apiBase}/documents/${encodeURIComponent(documentId)}/content`
  },
  documentPageURL(documentId: string, page: number): string {
    return `${apiBase}/documents/${encodeURIComponent(documentId)}/pages/${page}/content`
  },
  emailSources(): Promise<{ items: EmailSource[] }> {
    return request('/email-sources')
  },
  registerEmailSource(
    registration: EmailSourceRegistration,
    idempotencyKey: string,
  ): Promise<EmailSource> {
    return request('/email-sources', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(registration),
    })
  },
  emailMessages(sourceId: string, cursor = '', limit = 50): Promise<EmailMessagePage> {
    const query = new URLSearchParams({ limit: String(limit) })
    if (cursor) query.set('cursor', cursor)
    return request(`/email-sources/${encodeURIComponent(sourceId)}/messages?${query}`)
  },
  emailMessageDownloadURL(messageId: string): string {
    return `${apiBase}/email-messages/${encodeURIComponent(messageId)}/raw`
  },
  emailAttachmentDownloadURL(attachmentId: string): string {
    return `${apiBase}/email-attachments/${encodeURIComponent(attachmentId)}/content`
  },
  cancelJob(jobId: string): Promise<JobSummary> {
    return request(`/jobs/${encodeURIComponent(jobId)}/cancel`, { method: 'POST' })
  },
  retryJob(jobId: string): Promise<JobSummary> {
    return request(`/jobs/${encodeURIComponent(jobId)}/retry`, { method: 'POST' })
  },
  startManualReview(
    jobId: string,
    body: ManualReviewRequest,
    key: string,
  ): Promise<ManualReviewResult> {
    return request(`/jobs/${encodeURIComponent(jobId)}/manual-review`, {
      method: 'POST',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    })
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
  payments(query: FactListQuery = {}): Promise<FactListPage<Payment>> {
    return request('/payments' + factQueryString(query))
  },
  invoices(query: FactListQuery = {}): Promise<FactListPage<Invoice>> {
    return request('/invoices' + factQueryString(query))
  },
  factDetail(kind: FactKind, id: string): Promise<FactDetail> {
    return request(`/${kind === 'payment' ? 'payments' : 'invoices'}/${encodeURIComponent(id)}`)
  },
  deleteFact(kind: FactKind, id: string): Promise<void> {
    return request(`/${kind === 'payment' ? 'payments' : 'invoices'}/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  },
  trips(): Promise<{ items: Trip[] }> {
    return request('/trips')
  },
  createTrip(body: TripManagementRequest, key: string): Promise<TripManagementResult> {
    return request('/trips', {
      method: 'POST',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    })
  },
  editTrip(id: string, body: TripManagementRequest, key: string): Promise<TripManagementResult> {
    return request(`/trips/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    })
  },
  deleteTrip(
    id: string,
    expectedVersion: number,
    reason: string,
    key: string,
  ): Promise<TripManagementResult> {
    return request(`/trips/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify({ expected_version: expectedVersion, reason }),
    })
  },
  tripEvidence(tripId = '', cursor = ''): Promise<TripEvidencePage> {
    const query = new URLSearchParams({ limit: '20' })
    if (tripId) query.set('trip_id', tripId)
    if (cursor) query.set('cursor', cursor)
    return request(`/trip-evidence?${query}`)
  },
  assignTripMaterial(
    body: TripMaterialRequest,
    key: string,
  ): Promise<components['schemas']['TripMaterialResult']> {
    return request('/trip-material-assignments', {
      method: 'POST',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    })
  },
  tripPreference(
    paymentId: string,
    mode: 'auto' | 'blocked',
    expectedVersion: number,
  ): Promise<void> {
    return request(`/payments/${encodeURIComponent(paymentId)}/trip-preference`, {
      method: 'POST',
      body: JSON.stringify({ mode, expected_version: expectedVersion }),
    })
  },
  tripAttributionCandidates(
    tripId: string,
    view: TripAttributionView,
    cursor = '',
    limit = 50,
  ): Promise<TripAttributionPage> {
    const query = new URLSearchParams({ view, limit: String(limit) })
    if (cursor) query.set('cursor', cursor)
    return request(
      `/trips/${encodeURIComponent(tripId)}/attribution-candidates?${query.toString()}`,
    )
  },
  changeTripAssignment(
    body: TripAssignmentRequest,
    idempotencyKey: string,
  ): Promise<TripAssignmentResult> {
    return request('/trip-assignments', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    })
  },
  reimbursementPreview(body: ReimbursementPreviewRequest): Promise<ReimbursementPolicySnapshot> {
    return request('/reimbursement-previews', {
      method: 'POST',
      body: JSON.stringify(body),
    })
  },
  reimbursements(cursor = '', limit = 50): Promise<ReimbursementPage> {
    const query = new URLSearchParams({ limit: String(limit) })
    if (cursor) query.set('cursor', cursor)
    return request(`/reimbursements?${query.toString()}`)
  },
  submitReimbursement(
    body: ReimbursementSubmissionRequest,
    idempotencyKey: string,
  ): Promise<ReimbursementMutationResult> {
    return request('/reimbursements', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    })
  },
  reimbursement(reimbursementId: string): Promise<ReimbursementDetail> {
    return request(`/reimbursements/${encodeURIComponent(reimbursementId)}`)
  },
  insights(filter: InsightFilter, cursor = '', limit = 50): Promise<InsightPage> {
    const query = new URLSearchParams({
      fact_type: filter.fact_type,
      allocation_status: filter.allocation_status,
      trip_scope: filter.trip_scope,
      limit: String(limit),
    })
    if (filter.date_from) query.set('date_from', filter.date_from)
    if (filter.date_to) query.set('date_to', filter.date_to)
    if (filter.currency) query.set('currency', filter.currency)
    if (filter.trip_id) query.set('trip_id', filter.trip_id)
    if (cursor) query.set('cursor', cursor)
    return request(`/insights?${query.toString()}`)
  },
  changeReimbursementStatus(
    reimbursementId: string,
    body: ReimbursementStatusRequest,
    idempotencyKey: string,
  ): Promise<ReimbursementMutationResult> {
    return request(`/reimbursements/${encodeURIComponent(reimbursementId)}/status-decisions`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    })
  },
  allocationWorkspace(factType: AllocationFactType, factId: string): Promise<AllocationWorkspace> {
    return request(`/allocations/${encodeURIComponent(factType)}/${encodeURIComponent(factId)}`)
  },
  allocationTargets(
    factType: AllocationFactType,
    factId: string,
    q: string,
    view: string,
    cursor = '',
  ): Promise<AllocationTargetPage> {
    const query = new URLSearchParams({ q, view, cursor })
    return request(
      `/allocations/${encodeURIComponent(factType)}/${encodeURIComponent(factId)}/targets?${query}`,
    )
  },
  setBadDebt(
    factType: AllocationFactType,
    factId: string,
    body: components['schemas']['BadDebtRequest'],
    key: string,
  ): Promise<components['schemas']['BadDebtResult']> {
    return request(
      `/facts/${encodeURIComponent(factType)}/${encodeURIComponent(factId)}/bad-debt`,
      { method: 'POST', body: JSON.stringify(body), headers: { 'Idempotency-Key': key } },
    )
  },
  adjustAllocation(
    factType: AllocationFactType,
    factId: string,
    body: AllocationAdjustmentRequest,
    idempotencyKey: string,
  ): Promise<AllocationAdjustmentResult> {
    return request(
      `/allocations/${encodeURIComponent(factType)}/${encodeURIComponent(factId)}/adjustments`,
      {
        method: 'POST',
        headers: { 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(body),
      },
    )
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
