import { expect, test, type Locator, type Page, type Response } from '@playwright/test'
import { createHash } from 'node:crypto'
import { constants } from 'node:fs'
import { open } from 'node:fs/promises'
import { resolve } from 'node:path'
import process from 'node:process'
import type {
  AllocationAdjustmentRequest,
  AllocationAdjustmentResult,
  AllocationWorkspace,
  ConfirmRequest,
  ConfirmResult,
  FactDetail,
  Invoice,
  JobSummary,
  Payment,
  ProviderConfig,
  Review,
  RevisionRequest,
  Session,
  UploadResult,
} from '../src/data/client'
import type { components } from '../src/data/generated/api'
import { throughflowEnvironment as environment } from './playwright.config'

type SampleID = 'P' | 'A' | 'B'
type Fixture = {
  sample: SampleID
  kind: 'payment' | 'invoice'
  name: string
  date: string
  number: string
  amount: number
}
type Uploaded = { fixture: Fixture; upload: UploadResult; sha256: string; review: Review }
type Confirmed = Uploaded & { confirmation: ConfirmResult }
type Artifact = { file: string; sha256: string }
type Balance = { allocated_minor: number; remaining_minor: number }
type RuntimeState = {
  authenticated: boolean
  initialSession401: number
  pageErrors: number
  externalRequests: number
  failedAPIRequests: number
  expectedConflictPath: string | null
  expectedDuplicateConflicts: number
  duplicateRecovery: {
    sample: 'A'
    before_revision: number
    after_revision: number
    fields_and_evidence_unchanged: true
    no_partial_writes: true
  } | null
  unexpectedAPIResponses: Array<{ route: string; status: number }>
  requests: Record<string, number>
  readbackRequests: number
  mutationKeys: string[]
  screenshots: Artifact[]
}

const merchant = 'Synthetic Throughflow Merchant'
const buyer = 'Synthetic Throughflow Buyer'
const fixtures: readonly Fixture[] = [
  {
    sample: 'P',
    kind: 'payment',
    name: 'throughflow-payment.png',
    date: '2026-09-05',
    number: 'SYNTHETIC-THROUGHFLOW-P-001',
    amount: 10_000,
  },
  {
    sample: 'A',
    kind: 'invoice',
    name: 'throughflow-invoice-a.png',
    date: '2026-09-04',
    number: '90000000000000000001',
    amount: 6_000,
  },
  {
    sample: 'B',
    kind: 'invoice',
    name: 'throughflow-invoice-b.png',
    date: '2026-09-05',
    number: '90000000000000000002',
    amount: 6_000,
  },
]
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const uuidInPath = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/g

test('ADR-0036：三张合成单据经真实后端连续审核与分配', async ({ page, context }) => {
  const state: RuntimeState = {
    authenticated: false,
    initialSession401: 0,
    pageErrors: 0,
    externalRequests: 0,
    failedAPIRequests: 0,
    expectedConflictPath: null,
    expectedDuplicateConflicts: 0,
    duplicateRecovery: null,
    unexpectedAPIResponses: [],
    requests: {},
    readbackRequests: 0,
    mutationKeys: [],
    screenshots: [],
  }
  await context.route('**/*', async (route) => {
    if (new URL(route.request().url()).origin !== environment.origin) {
      state.externalRequests++
      await route.abort('blockedbyclient')
      return
    }
    await route.continue()
  })
  observeRequests(page, state)
  const images = await makeFixtureImages(page)
  await login(page, state)
  await requireEmptyWorkspace(page, state)
  await configureProvider(page, state)
  const uploaded = await uploadInSequence(page, state, images)
  for (const path of ['/payments', '/invoices']) {
    expect((await getJSON<{ items: unknown[] }>(page, state, path)).items).toEqual([])
  }
  const confirmed = await continuouslyReview(page, state, uploaded)
  const records = Object.fromEntries(
    confirmed.map((entry) => [entry.fixture.sample, entry]),
  ) as Record<SampleID, Confirmed>
  const sources = []
  for (const record of confirmed) sources.push(await verifySourceChain(page, state, record))
  const initial = await balances(page, state, records)
  expect(initial).toEqual({
    P: { allocated_minor: 0, remaining_minor: 10_000 },
    A: { allocated_minor: 0, remaining_minor: 6_000 },
    B: { allocated_minor: 0, remaining_minor: 6_000 },
  })

  const payment = await showAllocation(page, state, records.P)
  expect(payment.links).toEqual([])
  expect(payment.targets).toHaveLength(2)
  expect(payment.targets.every((target) => target.name_exact && target.currency === 'CNY')).toBe(
    true,
  )
  expect(payment.targets.map((target) => target.remaining_minor)).toEqual([6_000, 6_000])
  await expect(page.locator('.suggestion-panel')).toContainText('存在多种分配可能')
  await expect(page.locator('.suggestion-panel button')).toHaveCount(0)
  for (const invoice of [records.A, records.B]) {
    await expect(
      allocationRow(page, invoice.confirmation.fact_id).getByRole('checkbox'),
    ).not.toBeChecked()
    await expect(amountInput(page, invoice.confirmation.fact_id)).toHaveValue('')
  }
  expect(adjustmentCount(state)).toBe(0)
  await capture(page, state, 'allocation-ambiguous.png')

  const invoiceA = await showAllocation(page, state, records.A)
  expect(invoiceA.links).toEqual([])
  expect(invoiceA.targets).toHaveLength(1)
  expect(invoiceA.targets[0]).toMatchObject({
    id: records.P.confirmation.fact_id,
    remaining_minor: 10_000,
    name_exact: true,
  })
  await expect(page.locator('.suggestion-panel')).toContainText('双方余额不同')
  await page.getByRole('button', { name: '采用目标，金额由我填写', exact: true }).click()
  const partialInput = amountInput(page, records.P.confirmation.fact_id)
  await expect(partialInput).toHaveValue('')
  await expect(partialInput).toBeFocused()
  expect(adjustmentCount(state)).toBe(0)
  expect((await workspace(page, state, records.A)).links).toEqual([])
  await capture(page, state, 'allocation-partial-blank.png')
  await partialInput.fill('4000')
  await page.getByLabel('调整理由', { exact: false }).fill('合成贯通：人工核对部分分配4000分')
  const partialPlan: AllocationAdjustmentRequest = {
    expected_plan_hash: invoiceA.plan_hash,
    desired_allocations: [
      { target_fact_id: records.P.confirmation.fact_id, allocated_minor: 4_000 },
    ],
    reason: '合成贯通：人工核对部分分配4000分',
  }
  const partialResult = await saveAllocation(page, state, records.A, partialPlan)
  const afterPartial = await balances(page, state, records)
  expect(afterPartial).toEqual({
    P: { allocated_minor: 4_000, remaining_minor: 6_000 },
    A: { allocated_minor: 4_000, remaining_minor: 2_000 },
    B: { allocated_minor: 0, remaining_minor: 6_000 },
  })

  const supplemental = await showAllocation(page, state, records.P)
  expect(supplemental.links).toHaveLength(1)
  const originalLink = supplemental.links[0]!
  expect(originalLink).toMatchObject({
    target_fact_id: records.A.confirmation.fact_id,
    allocated_minor: 4_000,
  })
  expect(partialResult.created_link_ids).toEqual([originalLink.id])
  expect(
    supplemental.targets.find((target) => target.id === records.A.confirmation.fact_id),
  ).toMatchObject({
    current_link_id: originalLink.id,
    current_allocated_minor: 4_000,
    remaining_minor: 2_000,
  })
  await expect(page.locator('.suggestion-list li')).toHaveCount(1)
  await expect(page.locator('.suggestion-list li')).toContainText('2026-09-05')
  await expect(page.locator('.suggestion-panel')).toContainText('保留当前 1 条关联及其金额')
  await expect(amountInput(page, records.A.confirmation.fact_id)).toHaveValue('4000')
  await expect(
    allocationRow(page, records.A.confirmation.fact_id).getByRole('checkbox'),
  ).toBeChecked()
  await expect(
    allocationRow(page, records.B.confirmation.fact_id).getByRole('checkbox'),
  ).not.toBeChecked()
  await page.getByRole('button', { name: '采用到编辑区', exact: true }).click()
  await expect(amountInput(page, records.A.confirmation.fact_id)).toHaveValue('4000')
  await expect(amountInput(page, records.B.confirmation.fact_id)).toHaveValue('6000')
  expect(adjustmentCount(state)).toBe(1)
  expect((await workspace(page, state, records.P)).links).toEqual([originalLink])
  await expect(page.getByLabel('调整理由', { exact: false })).toHaveValue('')
  await page.getByLabel('调整理由', { exact: false }).fill('合成贯通：保留旧关联并补充分配6000分')
  await capture(page, state, 'allocation-exact-adopted.png')
  const finalPlan: AllocationAdjustmentRequest = {
    expected_plan_hash: supplemental.plan_hash,
    desired_allocations: [
      { target_fact_id: records.A.confirmation.fact_id, allocated_minor: 4_000 },
      { target_fact_id: records.B.confirmation.fact_id, allocated_minor: 6_000 },
    ].sort((left, right) => left.target_fact_id.localeCompare(right.target_fact_id)),
    reason: '合成贯通：保留旧关联并补充分配6000分',
  }
  const finalResult = await saveAllocation(page, state, records.P, finalPlan)
  const final = await balances(page, state, records)
  const expectedFinal = {
    P: { allocated_minor: 10_000, remaining_minor: 0 },
    A: { allocated_minor: 4_000, remaining_minor: 2_000 },
    B: { allocated_minor: 6_000, remaining_minor: 0 },
  }
  expect(final).toEqual(expectedFinal)
  const finalWorkspace = await workspace(page, state, records.P)
  expect(finalWorkspace.links).toHaveLength(2)
  expect(
    finalWorkspace.links.find((link) => link.target_fact_id === records.A.confirmation.fact_id),
  ).toEqual(originalLink)
  const newLink = finalWorkspace.links.find(
    (link) => link.target_fact_id === records.B.confirmation.fact_id,
  )!
  expect(newLink).toMatchObject({ allocated_minor: 6_000 })
  expect(newLink.id).not.toBe(originalLink.id)
  expect(finalResult.created_link_ids).toEqual([newLink.id])
  await page.reload()
  await expect(page.getByRole('heading', { name: '分配草案（未保存）', exact: true })).toBeVisible()
  await expect(page.locator('.suggestion-panel')).toContainText('当前没有剩余可分配金额')
  expect(await balances(page, state, records)).toEqual(expectedFinal)
  expect((await workspace(page, state, records.P)).links).toEqual(finalWorkspace.links)
  await capture(page, state, 'allocation-persisted-after-reload.png')
  for (const record of confirmed) await verifySourceChain(page, state, record)
  const finalJobs = await getJSON<{ items: JobSummary[] }>(page, state, '/jobs')
  expect(finalJobs.items).toHaveLength(3)
  expect(
    finalJobs.items.every((job) => job.status === 'completed' && job.attempt_count === 1),
  ).toBe(true)
  expect(adjustmentCount(state)).toBe(2)
  const writes = Object.fromEntries(
    Object.entries(state.requests).filter(([route]) => route.startsWith('POST ')),
  )
  expect(writes).toEqual({
    'POST /api/v1/session/login': 1,
    'POST /api/v1/provider-configs': 1,
    'POST /api/v1/provider-configs/:id/detect': 1,
    'POST /api/v1/provider-configs/:id/activate': 1,
    'POST /api/v1/documents': 3,
    'POST /api/v1/reviews/:id/confirm': 4,
    'POST /api/v1/reviews/:id/revisions': 1,
    'POST /api/v1/allocations/invoice/:id/adjustments': 1,
    'POST /api/v1/allocations/payment/:id/adjustments': 1,
  })
  expect(state.mutationKeys).toHaveLength(6)
  expect(new Set(state.mutationKeys).size).toBe(6)
  expect(state.expectedDuplicateConflicts).toBe(1)
  expect(state.duplicateRecovery).toEqual({
    sample: 'A',
    before_revision: 1,
    after_revision: 2,
    fields_and_evidence_unchanged: true,
    no_partial_writes: true,
  })
  assertHealthy(state)
  await writeArtifact(
    'throughflow-summary.json',
    Buffer.from(
      JSON.stringify(
        {
          report_kind: 'review-allocation-throughflow',
          protocol_version: 1,
          synthetic_only: true,
          api_responses_mocked: false,
          passed: true,
          provider_mode: 'review-allocation',
          expected_provider_extractions: 3,
          provider_metrics_verified_by_browser: false,
          fixture_sha256: digest(Buffer.from(JSON.stringify(fixtures))),
          documents: images.map(({ fixture, artifact }) => ({
            sample: fixture.sample,
            ...artifact,
          })),
          source_chains: sources,
          continuous_review: {
            confirmed: confirmed.length,
            review_order: confirmed.map((record) => record.fixture.sample),
            confirmation_requests: state.requests['POST /api/v1/reviews/:id/confirm'],
            expected_duplicate_candidate_set_stale_409: state.expectedDuplicateConflicts,
            explicit_revisions: state.requests['POST /api/v1/reviews/:id/revisions'],
            duplicate_conflict_recovery: state.duplicateRecovery,
            initial_links_created: 0,
            duplicate_decisions: confirmed.reduce(
              (sum, record) => sum + record.review.duplicate_candidates.length,
              0,
            ),
          },
          submitted_plans: [
            {
              anchor: 'A',
              desired_allocations: [{ target: 'P', allocated_minor: 4_000 }],
              expected_plan_hash: partialPlan.expected_plan_hash,
              reason_provided: true,
            },
            {
              anchor: 'P',
              desired_allocations: [
                { target: 'A', allocated_minor: 4_000 },
                { target: 'B', allocated_minor: 6_000 },
              ],
              expected_plan_hash: finalPlan.expected_plan_hash,
              reason_provided: true,
            },
          ],
          balances: { initial, after_partial: afterPartial, final },
          old_link_preserved: finalWorkspace.links.some(
            (link) =>
              link.id === originalLink.id && link.allocated_minor === originalLink.allocated_minor,
          ),
          added_link_count: finalResult.created_link_ids.length,
          ended_link_count: partialResult.ended_link_ids.length + finalResult.ended_link_ids.length,
          distinct_mutation_keys: new Set(state.mutationKeys).size,
          browser_api_requests: state.requests,
          readback_api_requests: state.readbackRequests,
          expected_initial_session_401: state.initialSession401,
          unexpected_api_responses: state.unexpectedAPIResponses.length,
          external_requests: state.externalRequests,
          page_errors: state.pageErrors,
          failed_api_requests: state.failedAPIRequests,
          screenshots: state.screenshots,
          limitations: [
            '固定本地Mock不执行识别',
            '未测量模型正确率、真实用户核对时间或总体操作收益',
            'Provider实际请求计数由外部隔离控制器读取health和metrics核对',
          ],
        },
        null,
        2,
      ) + '\n',
    ),
  )
})

function observeRequests(page: Page, state: RuntimeState) {
  page.on('pageerror', () => state.pageErrors++)
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.origin !== environment.origin || !url.pathname.startsWith('/api/v1/')) return
    const key = `${request.method()} ${url.pathname.replace(uuidInPath, ':id')}`
    state.requests[key] = (state.requests[key] ?? 0) + 1
    if (request.method() === 'POST' && /\/(confirm|adjustments)$/.test(url.pathname)) {
      state.mutationKeys.push(request.headers()['idempotency-key'] ?? '')
    }
  })
  page.on('response', (response) => {
    const url = new URL(response.url())
    if (
      url.origin !== environment.origin ||
      !url.pathname.startsWith('/api/v1/') ||
      response.status() < 400
    )
      return
    if (
      response.request().method() === 'POST' &&
      url.pathname === state.expectedConflictPath &&
      response.status() === 409 &&
      state.expectedDuplicateConflicts === 0
    ) {
      state.expectedDuplicateConflicts++
      return
    }
    if (
      !state.authenticated &&
      url.pathname === '/api/v1/session' &&
      response.status() === 401 &&
      state.initialSession401 === 0
    ) {
      state.initialSession401++
      return
    }
    state.unexpectedAPIResponses.push({
      route: url.pathname.replace(uuidInPath, ':id'),
      status: response.status(),
    })
  })
  page.on('requestfailed', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/')) state.failedAPIRequests++
  })
}

function assertHealthy(state: RuntimeState) {
  expect(state.expectedConflictPath).toBeNull()
  expect(state.initialSession401).toBe(1)
  expect(state.pageErrors).toBe(0)
  expect(state.externalRequests).toBe(0)
  expect(state.failedAPIRequests).toBe(0)
  expect(state.unexpectedAPIResponses).toEqual([])
  expect(state.mutationKeys.every(Boolean)).toBe(true)
}

async function login(page: Page, state: RuntimeState) {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
  await page.getByLabel('用户名或邮箱', { exact: true }).fill(environment.ownerEmail)
  await fillSecret(page.getByLabel('密码', { exact: true }), environment.ownerPasswordFile, 1_024)
  const responsePromise = matchingResponse(page, '/api/v1/session/login', 'POST')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  state.authenticated = true
  await expect(page).toHaveURL(`${environment.origin}/inbox`)
  const session = await getJSON<Session>(page, state, '/session')
  expect(session.role).toBe('owner')
  expect(session.user.email === environment.ownerEmail).toBe(true)
  for (const capability of [
    'documents.process',
    'claims.review',
    'facts.read',
    'review.source.read',
    'allocations.manage',
  ]) {
    expect(session.capabilities).toContain(capability)
  }
}

async function requireEmptyWorkspace(page: Page, state: RuntimeState) {
  for (const path of ['/jobs', '/provider-configs', '/payments', '/invoices']) {
    expect((await getJSON<{ items: unknown[] }>(page, state, path)).items).toEqual([])
  }
}

async function configureProvider(page: Page, state: RuntimeState) {
  await page.getByRole('link', { name: 'AI 配置', exact: true }).click()
  await page.getByLabel('Base URL').fill(environment.providerBaseURL)
  await page.getByLabel('Model').fill(environment.model)
  await page.getByLabel('Output Mode').selectOption('json_schema')
  await fillSecret(page.getByLabel('API Key'), environment.providerKeyFile, 4_096)
  const createdPromise = matchingResponse(page, '/api/v1/provider-configs', 'POST')
  await page.getByRole('button', { name: '创建待检测配置', exact: true }).click()
  expect((await createdPromise).status()).toBe(201)
  expect((await page.getByLabel('API Key').inputValue()).length === 0).toBe(true)
  const item = page.locator('.provider-list li').filter({ hasText: environment.model })
  await item.getByRole('button', { name: '能力检测', exact: true }).click()
  await expect(item).toContainText('检测通过')
  await item.getByRole('button', { name: '激活', exact: true }).click()
  await expect(item).toContainText('使用中')
  const configs = await getJSON<{ items: ProviderConfig[] }>(page, state, '/provider-configs')
  expect(configs.items).toHaveLength(1)
  expect(configs.items[0]).toMatchObject({
    base_url: environment.providerBaseURL,
    model: environment.model,
    output_mode: 'json_schema',
    capability_status: 'passed',
    active: true,
  })
  assertHealthy(state)
}

async function makeFixtureImages(page: Page) {
  const images = []
  for (const fixture of fixtures) {
    const rows =
      fixture.kind === 'payment'
        ? [
            ['商户 / Merchant', merchant],
            ['支付金额 / Amount', 'CNY 100.00'],
            ['交易时间 / Transaction time', `${fixture.date} 09:00`],
            ['订单号 / Order number', fixture.number],
          ]
        : [
            ['发票号码 / Invoice number', fixture.number],
            ['开票日期 / Invoice date', fixture.date],
            ['销售方 / Seller', merchant],
            ['购买方 / Buyer', buyer],
            ['价税合计 / Total including tax', 'CNY 60.00'],
          ]
    await page.setContent(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><style>
      *{box-sizing:border-box}body{margin:0;background:#fff;font-family:Arial,"Noto Sans CJK SC",sans-serif;color:#172b40}main{width:1050px;padding:50px;border:12px solid ${fixture.sample === 'P' ? '#1a635b' : fixture.sample === 'A' ? '#254a79' : '#7c4126'}}h1{font-size:30px;margin:0 0 14px}p{font-size:19px;line-height:1.5}.banner{background:#fff0a8;padding:18px;font-weight:bold}.row{padding:18px 0;border-bottom:1px solid #9faab5;display:grid;grid-template-columns:350px 1fr;gap:18px;font-size:20px}.value{overflow-wrap:anywhere;font-weight:bold}footer{font-size:17px;margin-top:24px}
      </style></head><body><main><h1>合成测试 · ${fixture.kind === 'payment' ? '支付凭证' : '发票'} ${fixture.sample}</h1><p class="banner">SYNTHETIC TEST ONLY — NO REAL TRANSACTION</p><p>用于隔离环境的固定响应贯通验收；不代表真实交易或模型识别。</p>${rows.map(([label, value]) => `<div class="row"><span>${label}</span><span class="value">${value}</span></div>`).join('')}<footer>币种 / Currency: CNY · 仅供测试<br>未列出的可选字段留空；本凭证不用于报销。</footer></main></body></html>`)
    const content = await page.locator('main').screenshot({ animations: 'disabled' })
    const artifact = await writeArtifact(fixture.name, content)
    images.push({ fixture, content, artifact })
  }
  return images
}

async function uploadInSequence(
  page: Page,
  state: RuntimeState,
  images: Awaited<ReturnType<typeof makeFixtureImages>>,
) {
  await page.getByRole('link', { name: 'AI 收件箱', exact: true }).click()
  const uploaded: Uploaded[] = []
  for (const image of images) {
    const responsePromise = matchingResponse(page, '/api/v1/documents', 'POST')
    await page
      .locator('input[type="file"]')
      .setInputFiles({ name: image.fixture.name, mimeType: 'image/png', buffer: image.content })
    const response = await responsePromise
    expect(response.status()).toBe(201)
    const upload = (await response.json()) as UploadResult
    expect(upload.job_id).toMatch(uuid)
    expect(upload.document_id).toMatch(uuid)
    expect(upload.sha256).toBe(image.artifact.sha256)
    await expect
      .poll(
        async () => {
          const job = await getJSON<JobSummary>(page, state, `/jobs/${upload.job_id}`)
          expect(['queued', 'processing', 'needs_review']).toContain(job.status)
          expect(job.attempt_count).toBeLessThanOrEqual(1)
          return job.status
        },
        { timeout: 60_000, intervals: [250, 500, 1_000] },
      )
      .toBe('needs_review')
    const job = await getJSON<JobSummary>(page, state, `/jobs/${upload.job_id}`)
    expect(job.attempt_count).toBe(1)
    const review = await getJSON<Review>(page, state, `/reviews/${upload.job_id}`)
    verifyReviewFields(review, image.fixture, 'ready_for_review')
    await expect(page.locator('tbody tr').filter({ hasText: image.fixture.name })).toContainText(
      '待人工确认',
    )
    uploaded.push({ fixture: image.fixture, upload, sha256: image.artifact.sha256, review })
  }
  expect(state.requests['POST /api/v1/documents']).toBe(3)
  return uploaded
}

async function continuouslyReview(page: Page, state: RuntimeState, uploaded: Uploaded[]) {
  const results: Confirmed[] = []
  const expectedOrder: SampleID[] = ['B', 'A', 'P']
  await page.getByRole('button', { name: /开始连续审核.*3/ }).click()
  for (let index = 0; index < uploaded.length; index++) {
    await expect(page.getByRole('heading', { name: '审核单据', exact: true })).toBeVisible()
    await expect(page.getByLabel('连续审核进度')).toContainText(`第 ${index + 1} / 3 份`)
    const name = (await page.locator('.review-document-name').textContent())?.trim()
    const current = uploaded.find((entry) => entry.fixture.name === name)
    if (!current || results.some((entry) => entry.fixture.sample === current.fixture.sample))
      throw new Error('continuous review selected an unexpected synthetic document')
    expect(current.fixture.sample).toBe(expectedOrder[index])
    expect(new URL(page.url()).pathname).toBe(`/reviews/${current.upload.job_id}`)
    let review = await getJSON<Review>(page, state, `/reviews/${current.upload.job_id}`)
    verifyReviewFields(review, current.fixture, 'ready_for_review')
    await expect(page.getByAltText(`${current.fixture.name} 的第 1 页规范化审核图`)).toBeVisible()
    if (index === 1) {
      review = await recoverExpectedDuplicateConflict(page, state, current, review, results[0]!)
    }
    await selectExplicitReviewDecisions(page, review, uploaded, results)
    if (index === 0) await capture(page, state, 'continuous-review.png')
    const response = await submitReviewConfirmation(page, current, review)
    expect(response.status()).toBe(200)
    const confirmation = (await response.json()) as ConfirmResult
    expect(confirmation.fact_type).toBe(current.fixture.kind)
    expect(confirmation.fact_id).toMatch(uuid)
    expect(confirmation.review_decision_id).toMatch(uuid)
    expect(confirmation.link_ids).toEqual([])
    expect(confirmation.replayed).toBe(false)
    results.push({ ...current, review, confirmation })
    if (index < uploaded.length - 1)
      await expect(page.locator('.review-document-name')).not.toHaveText(current.fixture.name)
  }
  await expect(page.getByRole('heading', { name: '本轮审核结束', exact: true })).toBeVisible()
  await expect(page.locator('.completion-state')).toContainText('已保存 3 份')
  expect(state.requests['POST /api/v1/reviews/:id/confirm']).toBe(4)
  expect(state.requests['POST /api/v1/reviews/:id/revisions']).toBe(1)
  expect(state.expectedDuplicateConflicts).toBe(1)
  await capture(page, state, 'continuous-review-completed.png')
  return results
}

async function selectExplicitReviewDecisions(
  page: Page,
  review: Review,
  uploaded: Uploaded[],
  confirmed: Confirmed[],
) {
  const duplicateInputs = page.locator('.duplicate-options input[type="checkbox"]')
  await expect(duplicateInputs).toHaveCount(review.duplicate_candidates.length)
  for (const [index, candidate] of review.duplicate_candidates.entries()) {
    expect(candidate.available).toBe(true)
    const belongsToFixture =
      uploaded.some((entry) => entry.upload.document_id === candidate.existing_document_id) ||
      confirmed.some((entry) =>
        [candidate.existing_payment_id, candidate.existing_invoice_id].includes(
          entry.confirmation.fact_id,
        ),
      )
    expect(belongsToFixture).toBe(true)
    await duplicateInputs.nth(index).check()
  }
  if (review.candidates.length) await page.getByRole('radio', { name: /不关联任何候选/ }).check()
}

async function submitReviewConfirmation(page: Page, current: Uploaded, review: Review) {
  const responsePromise = matchingResponse(
    page,
    `/api/v1/reviews/${current.upload.job_id}/confirm`,
    'POST',
  )
  const confirmButton = page.getByRole('button', { name: /^确认保存(?:，不分配)?并继续$/ })
  await expect(confirmButton).toBeEnabled()
  await confirmButton.click()
  const response = await responsePromise
  const body = response.request().postDataJSON() as ConfirmRequest
  expect(body).toEqual({
    expected_revision: review.revision,
    association_mode: review.candidates.length ? 'reject_all' : 'no_candidate',
    allocations: [],
    duplicate_resolutions: review.duplicate_candidates.map((candidate) => ({
      candidate_id: candidate.id,
      action: 'keep_distinct',
    })),
  })
  return response
}

async function recoverExpectedDuplicateConflict(
  page: Page,
  state: RuntimeState,
  current: Uploaded,
  previous: Review,
  confirmedB: Confirmed,
) {
  expect(current.fixture.sample).toBe('A')
  expect(confirmedB.fixture.sample).toBe('B')
  expect(previous.revision).toBe(1)
  expect(previous.claim_set_id).toBe(current.review.claim_set_id)
  expect(previous.duplicate_candidates).toEqual([])
  expect(previous.candidates).toEqual([])
  expect(state.expectedDuplicateConflicts).toBe(0)
  expect(state.requests['POST /api/v1/reviews/:id/revisions'] ?? 0).toBe(0)
  await expect(page.locator('.duplicate-options input[type="checkbox"]')).toHaveCount(0)

  // 固定第二份的第一次确认必须失败；这不是遇错重试或预先刷新候选。
  state.expectedConflictPath = `/api/v1/reviews/${current.upload.job_id}/confirm`
  const conflict = await submitReviewConfirmation(page, current, previous)
  expect(conflict.status()).toBe(409)
  expect(await conflict.json()).toMatchObject({ error: { code: 'duplicate_candidate_set_stale' } })
  expect(state.expectedDuplicateConflicts).toBe(1)
  state.expectedConflictPath = null
  await expect(
    page.getByText('疑似重复候选已变化。请保存当前字段为新版本后重新核对。', { exact: true }),
  ).toBeVisible()
  await assertConflictHasNoPartialWrites(page, state, current, confirmedB)
  const unchanged = await getJSON<Review>(page, state, `/reviews/${current.upload.job_id}`)
  expect(unchanged).toEqual(previous)
  await capture(page, state, 'continuous-review-duplicate-conflict.png')
  assertHealthy(state)

  await page.getByRole('button', { name: '修订字段', exact: true }).click()
  const revisionPromise = matchingResponse(
    page,
    `/api/v1/reviews/${current.upload.job_id}/revisions`,
    'POST',
  )
  await page.getByRole('button', { name: '保存修订版本', exact: true }).click()
  const response = await revisionPromise
  expect(response.status()).toBe(201)
  const request = response.request().postDataJSON() as RevisionRequest
  expect(request).toEqual({
    expected_revision: previous.revision,
    expected_optimistic_version: previous.optimistic_version,
    document_type: previous.document_type,
    fields: expect.any(Array),
  })
  const expectedFields = previous.fields
    .filter((field) => field.path !== 'document_type')
    .map((field) => ({
      path: field.path,
      value_type: field.value_type,
      presence: field.presence,
      ...(field.presence === 'present'
        ? { value: field.value, evidence_ids: field.evidence.map((entry) => entry.id) }
        : {}),
    }))
  expect([...request.fields].sort(byFieldPath)).toEqual(expectedFields.sort(byFieldPath))
  const revised = (await response.json()) as Review
  expect(revised.revision).toBe(2)
  expect(revised.claim_set_id).toMatch(uuid)
  expect(revised.claim_set_id).not.toBe(previous.claim_set_id)
  expect(revised.job.id).toBe(current.upload.job_id)
  expect(revised.job.document_id).toBe(current.upload.document_id)
  expect(reviewFieldContents(revised)).toEqual(reviewFieldContents(previous))
  verifyReviewFields(revised, current.fixture, 'ready_for_review')
  expect(revised.duplicate_candidates).toHaveLength(1)
  expect(revised.duplicate_candidates[0]).toMatchObject({
    kind: 'near_file',
    existing_document_id: confirmedB.upload.document_id,
    available: true,
  })
  expect(await getJSON<Review>(page, state, `/reviews/${current.upload.job_id}`)).toEqual(revised)
  await expect(page.getByRole('button', { name: '修订字段', exact: true })).toBeVisible()
  await assertConflictHasNoPartialWrites(page, state, current, confirmedB)
  expect(state.requests['POST /api/v1/reviews/:id/confirm']).toBe(2)
  expect(state.requests['POST /api/v1/reviews/:id/revisions']).toBe(1)
  state.duplicateRecovery = {
    sample: 'A',
    before_revision: previous.revision,
    after_revision: revised.revision,
    fields_and_evidence_unchanged: true,
    no_partial_writes: true,
  }
  return revised
}

async function assertConflictHasNoPartialWrites(
  page: Page,
  state: RuntimeState,
  current: Uploaded,
  confirmedB: Confirmed,
) {
  await expect(page.getByLabel('连续审核进度')).toContainText('第 2 / 3 份')
  await expect(page.locator('.review-document-name')).toHaveText(current.fixture.name)
  expect(new URL(page.url()).pathname).toBe(`/reviews/${current.upload.job_id}`)
  expect((await getJSON<{ items: Payment[] }>(page, state, '/payments')).items).toEqual([])
  const invoices = await getJSON<{ items: Invoice[] }>(page, state, '/invoices')
  expect(invoices.items).toHaveLength(1)
  expect(invoices.items[0]).toMatchObject({
    id: confirmedB.confirmation.fact_id,
    allocated_minor: 0,
    remaining_minor: 6_000,
  })
  expect((await workspace(page, state, confirmedB)).links).toEqual([])
  const job = await getJSON<JobSummary>(page, state, `/jobs/${current.upload.job_id}`)
  expect(job.status).toBe('needs_review')
  expect(job.attempt_count).toBe(1)
  expect(adjustmentCount(state)).toBe(0)
}

function byFieldPath(left: { path: string }, right: { path: string }) {
  return left.path.localeCompare(right.path)
}

function reviewFieldContents(review: Review) {
  // 新快照的 ID 必须变化；字段及原件上的证据绑定按路径与页码/摘录/区域核对。
  return review.fields
    .map((field) => ({
      path: field.path,
      value_type: field.value_type,
      presence: field.presence,
      value: field.value,
      normalized_value: field.normalized_value,
      source: field.source,
      evidence: field.evidence
        .map((entry) => ({ page: entry.page, quote: entry.quote, region: entry.region }))
        .sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right))),
    }))
    .sort(byFieldPath)
}

function verifyReviewFields(review: Review, fixture: Fixture, status: Review['claim_status']) {
  expect(review.entry_mode).toBe('ai')
  expect(review.document_type).toBe(fixture.kind)
  expect(review.claim_status).toBe(status)
  expect(review.page_count).toBe(1)
  expect(review.validations.filter((entry) => ['blocked', 'error'].includes(entry.status))).toEqual(
    [],
  )
  const values: Record<string, string | number> =
    fixture.kind === 'payment'
      ? { amount_minor: fixture.amount, currency: 'CNY', merchant, order_number: fixture.number }
      : {
          total_minor: fixture.amount,
          currency: 'CNY',
          invoice_number: fixture.number,
          invoice_date: fixture.date,
          seller_name: merchant,
          buyer_name: buyer,
        }
  for (const [path, expected] of Object.entries(values)) {
    const field = review.fields.find((entry) => entry.path === path)
    expect(field?.presence).toBe('present')
    expect(field?.source).toBe('ai')
    expect(field?.value).toBe(expected)
    if (
      ['merchant', 'order_number', 'invoice_number', 'seller_name', 'buyer_name'].includes(path)
    ) {
      expect(field?.normalized_value).toBe(String(expected).toLowerCase())
    }
    expect(field?.evidence.length).toBeGreaterThan(0)
    expect(field?.evidence.every((entry) => entry.page === 1)).toBe(true)
  }
}

async function verifySourceChain(page: Page, state: RuntimeState, record: Confirmed) {
  const detail = await factDetail(page, state, record)
  expect(detail.source).toMatchObject({
    document_id: record.upload.document_id,
    claim_set_id: record.review.claim_set_id,
    review_decision_id: record.confirmation.review_decision_id,
    origin_kind: 'ai',
    original_name: record.fixture.name,
    page_count: 1,
    revision: record.review.revision,
  })
  const claim = await getJSON<Review>(page, state, `/claim-sets/${record.review.claim_set_id}`)
  verifyReviewFields(claim, record.fixture, 'confirmed')
  expect(claim.job.document_id).toBe(record.upload.document_id)
  const document = await getJSON<components['schemas']['Document']>(
    page,
    state,
    `/documents/${record.upload.document_id}`,
  )
  expect(document).toMatchObject({
    id: record.upload.document_id,
    original_name: record.fixture.name,
    sha256: record.sha256,
    detected_mime: 'image/png',
    page_count: 1,
    status: 'completed',
    ingestion_kind: 'upload',
  })
  const original = await getBytes(page, state, `/documents/${record.upload.document_id}/content`)
  expect(digest(original)).toBe(record.sha256)
  const normalized = await getBytes(
    page,
    state,
    `/documents/${record.upload.document_id}/pages/1/content`,
  )
  expect(normalized.length).toBeGreaterThan(0)
  return {
    sample: record.fixture.sample,
    origin_kind: detail.source!.origin_kind,
    revision: claim.revision,
    original_sha256: digest(original),
    normalized_page_sha256: digest(normalized),
    linked_document_claim_review: true,
  }
}

async function factDetail(page: Page, state: RuntimeState, record: Confirmed) {
  const detail = await getJSON<FactDetail>(
    page,
    state,
    `/${record.fixture.kind === 'payment' ? 'payments' : 'invoices'}/${record.confirmation.fact_id}`,
  )
  expect(detail.fact_type).toBe(record.fixture.kind)
  if (record.fixture.kind === 'payment') {
    expect(detail.payment).toMatchObject({
      id: record.confirmation.fact_id,
      amount_minor: 10_000,
      currency: 'CNY',
      merchant,
      business_date: record.fixture.date,
      source_timezone: 'Asia/Shanghai',
      order_number: record.fixture.number,
    })
    expect(new Date(detail.payment!.transaction_time).toISOString()).toBe(
      '2026-09-05T01:00:00.000Z',
    )
  } else {
    expect(detail.invoice).toMatchObject({
      id: record.confirmation.fact_id,
      total_minor: 6_000,
      currency: 'CNY',
      invoice_number: record.fixture.number,
      invoice_date: record.fixture.date,
      seller_name: merchant,
      buyer_name: buyer,
      item_count: 0,
    })
    expect(detail.invoice!.tax_minor).toBeUndefined()
  }
  return detail
}

async function balances(page: Page, state: RuntimeState, records: Record<SampleID, Confirmed>) {
  const result = {} as Record<SampleID, Balance>
  for (const sample of ['P', 'A', 'B'] as const) {
    const detail = await factDetail(page, state, records[sample])
    const fact = (detail.payment ?? detail.invoice) as Payment | Invoice
    result[sample] = {
      allocated_minor: fact.allocated_minor,
      remaining_minor: fact.remaining_minor,
    }
  }
  return result
}

function allocationPath(record: Confirmed) {
  return `/allocations/${record.fixture.kind}/${record.confirmation.fact_id}`
}

async function workspace(page: Page, state: RuntimeState, record: Confirmed) {
  const result = await getJSON<AllocationWorkspace>(page, state, allocationPath(record))
  expect(result.anchor.id).toBe(record.confirmation.fact_id)
  expect(result.anchor.fact_type).toBe(record.fixture.kind)
  expect(result.next_cursor ?? '').toBe('')
  expect(result.plan_hash).toMatch(/^[0-9a-f]{64}$/)
  return result
}

async function showAllocation(page: Page, state: RuntimeState, record: Confirmed) {
  await page.goto(allocationPath(record))
  await expect(page.getByRole('heading', { name: '分配草案（未保存）', exact: true })).toBeVisible()
  return workspace(page, state, record)
}

function amountInput(page: Page, id: string) {
  return page.locator(`#allocation-amount-${id}`)
}

function allocationRow(page: Page, id: string) {
  return page.locator('.allocation-target-row').filter({ has: amountInput(page, id) })
}

function adjustmentCount(state: RuntimeState) {
  return (
    (state.requests['POST /api/v1/allocations/payment/:id/adjustments'] ?? 0) +
    (state.requests['POST /api/v1/allocations/invoice/:id/adjustments'] ?? 0)
  )
}

async function saveAllocation(
  page: Page,
  state: RuntimeState,
  record: Confirmed,
  expected: AllocationAdjustmentRequest,
) {
  const responsePromise = matchingResponse(
    page,
    `/api/v1${allocationPath(record)}/adjustments`,
    'POST',
  )
  await page.getByRole('button', { name: '确认补充分配', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  expect(response.request().postDataJSON()).toEqual(expected)
  const result = (await response.json()) as AllocationAdjustmentResult
  expect(result).toMatchObject({ mode: 'supplement', ended_link_ids: [], replayed: false })
  expect(result.created_link_ids).toHaveLength(1)
  await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
  assertHealthy(state)
  return result
}

function matchingResponse(page: Page, path: string, method: string): Promise<Response> {
  return page.waitForResponse(
    (response) =>
      response.url() === `${environment.origin}${path}` && response.request().method() === method,
  )
}

async function getJSON<T>(page: Page, state: RuntimeState, path: string): Promise<T> {
  const response = await page.request.get(`${environment.origin}/api/v1${path}`, {
    maxRedirects: 0,
  })
  state.readbackRequests++
  expect(response.status(), `readback ${path.replace(uuidInPath, ':id')}`).toBe(200)
  return response.json() as Promise<T>
}

async function getBytes(page: Page, state: RuntimeState, path: string) {
  const response = await page.request.get(`${environment.origin}/api/v1${path}`, {
    maxRedirects: 0,
  })
  state.readbackRequests++
  expect(response.status(), 'source image readback').toBe(200)
  expect(response.headers()['content-type']).toMatch(/^image\//)
  return response.body()
}

async function fillSecret(locator: Locator, path: string, maximumBytes: number) {
  await expect(locator).toBeVisible()
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW)
  try {
    const information = await handle.stat()
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      information.uid !== process.getuid?.() ||
      (information.mode & 0o077) !== 0 ||
      information.size < 1 ||
      information.size > maximumBytes + 2
    )
      throw new Error('credential input must be a protected owner-only regular file')
    const content = await handle.readFile()
    try {
      const value = content.toString('utf8').replace(/\r?\n$/, '')
      if (!value || Buffer.byteLength(value) > maximumBytes)
        throw new Error('credential input has an invalid size')
      // 参数不进入 fill() 的调用日志；仍只填写当前真实页面的密码输入框。
      await locator.evaluate((element, secret) => {
        if (!(element instanceof HTMLInputElement) || element.type !== 'password')
          throw new Error('protected input is not a password field')
        element.value = secret
        element.dispatchEvent(new Event('input', { bubbles: true }))
        element.dispatchEvent(new Event('change', { bubbles: true }))
      }, value)
    } finally {
      content.fill(0)
    }
  } finally {
    await handle.close()
  }
}

function digest(content: Buffer) {
  return createHash('sha256').update(content).digest('hex')
}

async function writeArtifact(file: string, content: Buffer): Promise<Artifact> {
  if (!/^[a-z0-9][a-z0-9.-]*$/.test(file)) throw new Error('invalid private artifact filename')
  const handle = await open(
    resolve(environment.outputDirectory, file),
    constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW,
    0o600,
  )
  try {
    await handle.writeFile(content)
  } finally {
    await handle.close()
  }
  return { file, sha256: digest(content) }
}

async function capture(page: Page, state: RuntimeState, file: string) {
  expect(state.authenticated).toBe(true)
  expect(new URL(page.url()).pathname).not.toMatch(/^\/(login|settings)/)
  state.screenshots.push(
    await writeArtifact(file, await page.screenshot({ fullPage: true, animations: 'disabled' })),
  )
}
