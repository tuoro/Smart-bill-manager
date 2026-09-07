import type { Page } from '@playwright/test'
import type { ConfirmRequest, ConfirmResult, Review, Session } from '../src/data/client'

const image = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAF/gL+X9jXAAAAAElFTkSuQmCC',
  'base64',
)
const timestamp = '2026-09-05T08:00:00Z'
const id = (kind: number, index: number) =>
  `${kind}0000000-0000-4000-8000-${String(index).padStart(12, '0')}`

export const reductionSession: Session = {
  user: { id: id(1, 1), email: 'reviewer@example.test', display_name: '合成审核人' },
  tenant: { id: id(2, 1), name: '合成工作区', default_currency: 'CNY', timezone: 'Asia/Shanghai' },
  role: 'owner',
  capabilities: ['claims.review', 'documents.process', 'facts.read'],
  csrf_token: 'synthetic-review-csrf',
  expires_at: '2030-01-01T00:00:00Z',
}

export function reductionReview(index: number, type: 'payment' | 'invoice' = 'payment'): Review {
  const values: Array<[string, string, unknown]> = [
    ['document_type', 'string', type],
    [type === 'payment' ? 'amount_minor' : 'total_minor', 'money_minor', 10000 + index],
    ['currency', 'string', 'CNY'],
    ...(type === 'payment'
      ? [
          ['merchant', 'string', `合成商户 ${index}`],
          ['transaction_time', 'instant', timestamp],
          ['source_timezone', 'string', 'Asia/Shanghai'],
        ]
      : [
          ['invoice_number', 'string', `SYN-REVIEW-${index}`],
          ['invoice_date', 'date', '2026-09-05'],
          ['seller_name', 'string', `合成销售方 ${index}`],
          ['buyer_name', 'string', '合成购买方'],
          ['tax_minor', 'money_minor', 0],
        ]),
  ] as Array<[string, string, unknown]>
  const fields: Review['fields'] = values.map(([path, valueType, value], fieldIndex) => ({
    id: id(5, index * 100 + fieldIndex),
    path,
    value_type: valueType,
    presence: 'present',
    value,
    source: 'ai',
    evidence:
      path === 'document_type'
        ? []
        : [{ id: id(6, index * 100 + fieldIndex), page: 1, quote: `合成第 ${index} 单：${value}` }],
  }))
  return {
    job: {
      id: id(3, index),
      document_id: id(4, index),
      original_name: `合成单据-${String(index).padStart(2, '0')}.png`,
      ingestion_kind: 'upload',
      detected_mime: 'image/png',
      status: 'needs_review',
      attempt_count: 1,
      created_at: timestamp,
      version: 1,
    },
    entry_mode: 'ai',
    claim_set_id: id(7, index),
    document_type: type,
    revision: 1,
    optimistic_version: 1,
    claim_status: 'ready_for_review',
    page_count: 1,
    pages: [{ page_number: 1, field_paths: fields.map((field) => field.path), item_keys: [] }],
    invoice_item_spans: [],
    fields,
    validations: [
      {
        id: id(8, index),
        field_claim_id: fields[1]!.id,
        rule_code: `${type}.required_fields`,
        severity: 'info',
        status: 'passed',
        safe_message: '合成字段和证据齐全',
      },
    ],
    candidates: [],
    duplicate_candidates: [],
  }
}

export function reductionBatch(): Review[] {
  return Array.from({ length: 20 }, (_, index) =>
    reductionReview(index + 1, index % 2 ? 'invoice' : 'payment'),
  )
}

export async function mockReductionWorkspace(page: Page, initial: Review[]) {
  const state = {
    session: structuredClone(reductionSession) as Session | null,
    reviews: new Map(initial.map((review) => [review.job.id, structuredClone(review)])),
    confirmations: [] as Array<{ jobId: string; body: ConfirmRequest; key: string | undefined }>,
    rejections: [] as string[],
    reads: [] as string[],
    unexpected: [] as string[],
  }
  const confirmed = new Map<string, ConfirmResult>()
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/api/v1/session') {
      if (method === 'DELETE') {
        state.session = null
        await route.fulfill({ status: 204 })
      } else if (state.session) {
        await route.fulfill({
          json: state.session,
          headers: { 'Set-Cookie': 'sbm_csrf=synthetic-review-csrf; Path=/; SameSite=Strict' },
        })
      } else {
        await route.fulfill({ status: 401, json: { error: { code: 'unauthenticated' } } })
      }
      return
    }
    if (path === '/api/v1/jobs' && method === 'GET') {
      await route.fulfill({ json: { items: [...state.reviews.values()].map((r) => r.job) } })
      return
    }
    if (/^\/api\/v1\/documents\/[^/]+\/(content|pages\/\d+\/content)$/.test(path)) {
      await route.fulfill({ status: 200, contentType: 'image/png', body: image })
      return
    }
    const match = /^\/api\/v1\/reviews\/([^/]+)(?:\/(confirm|reject))?$/.exec(path)
    const review = match && state.reviews.get(match[1]!)
    if (match && review) {
      const jobId = review.job.id
      if (!match[2] && method === 'GET') {
        state.reads.push(jobId)
        if (!['needs_review', 'blocked'].includes(review.job.status)) {
          await route.fulfill({ status: 404, json: { error: { code: 'not_found' } } })
        } else await route.fulfill({ json: review })
        return
      }
      if (match[2] === 'confirm' && method === 'POST') {
        const body = request.postDataJSON() as ConfirmRequest
        const key = request.headers()['idempotency-key']
        state.confirmations.push({ jobId, body, key })
        if (!key || body.expected_revision !== review.revision) {
          await route.fulfill({ status: 409, json: { error: { code: 'version_conflict' } } })
          return
        }
        const result = confirmed.get(key) ?? {
          review_decision_id: id(9, state.confirmations.length),
          fact_type: review.document_type as ConfirmResult['fact_type'],
          fact_id: id(9, state.confirmations.length + 100),
          link_ids: [],
          replayed: false,
        }
        const replayed = confirmed.has(key)
        confirmed.set(key, result)
        review.job.status = 'completed'
        await route.fulfill({ status: 201, json: { ...result, replayed } })
        return
      }
      if (match[2] === 'reject' && method === 'POST') {
        state.rejections.push(jobId)
        review.job.status = 'rejected'
        await route.fulfill({ status: 204 })
        return
      }
    }
    state.unexpected.push(`${method} ${path}`)
    await route.fulfill({ status: 501, json: { error: { code: 'undeclared_synthetic_request' } } })
  })
  return state
}
