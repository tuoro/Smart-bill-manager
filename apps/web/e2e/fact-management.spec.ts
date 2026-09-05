import { test, expect, type Page, type Route } from '@playwright/test'
import type { FactDetail, FactKind, Invoice, Payment, Review, Session } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const timestamp = '2026-09-04T08:00:00Z'
const uuid = (index: number) => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
const sourceID = uuid(8001)
const session: Session = {
  user: { id: uuid(1), email: 'synthetic@example.invalid', display_name: '合成用户' },
  tenant: {
    id: uuid(2),
    name: '合成单据工作区',
    default_currency: 'CNY',
    timezone: 'Asia/Shanghai',
  },
  role: 'owner',
  capabilities: [
    'facts.read',
    'claims.review',
    'review.source.read',
    'allocations.manage',
    'resources.delete',
  ],
  csrf_token: 'synthetic-ui-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}
const payment = (index: number): Payment => ({
  bad_debt: false,
  id: uuid(index),
  amount_minor: 12345,
  allocated_minor: 0,
  remaining_minor: 12345,
  allocation_status: 'unallocated',
  currency: 'CNY',
  merchant: `合成商户 ${index}`,
  transaction_time: timestamp,
  business_date: '2026-09-04',
  source_timezone: 'Asia/Shanghai',
  order_number: `SYN-${index}`,
  created_at: timestamp,
})
const invoice = (index: number): Invoice => ({
  bad_debt: false,
  id: uuid(index),
  total_minor: 12345,
  allocated_minor: 0,
  remaining_minor: 12345,
  allocation_status: 'unallocated',
  currency: 'CNY',
  invoice_number: `SYN-INV-${index}`,
  invoice_date: '2026-09-04',
  seller_name: `合成销售方 ${index}`,
  buyer_name: '合成购买方',
  item_count: 2,
  created_at: timestamp,
})
function detail(kind: FactKind, index: number, viewer = false): FactDetail {
  const value: FactDetail = {
    fact_type: kind,
    version: 2,
    links: [],
    ...(kind === 'payment'
      ? { payment: payment(index) }
      : {
          invoice: {
            ...invoice(index),
            items: [
              { item_key: uuid(9001), name: '合成服务', amount_minor: 5000, sort_order: 0 },
              { item_key: uuid(9002), name: '合成材料', amount_minor: 7345, sort_order: 1 },
            ],
          },
        }),
  }
  if (!viewer)
    value.source = {
      document_id: sourceID,
      claim_set_id: uuid(8002),
      review_decision_id: uuid(8003),
      revision: 2,
      origin_kind: 'ai',
      original_name: 'synthetic-original.png',
      page_count: 1,
    }
  return value
}
async function base(page: Page, viewer = false) {
  await page.route('**/api/v1/session', (route) =>
    route.fulfill({
      json: viewer ? { ...session, role: 'viewer', capabilities: ['facts.read'] } : session,
    }),
  )
  await page.route('**/api/v1/documents/**/pages/*/content', (route) =>
    route.fulfill({
      contentType: 'image/svg+xml',
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="360" height="240"><rect width="360" height="240" fill="white"/><text x="20" y="50">Synthetic original</text></svg>',
    }),
  )
}
function failed(route: Route, status = 503) {
  return route.fulfill({
    status,
    json: {
      error: { code: 'synthetic_failure', message: '合成请求失败，请重试' },
      request_id: 'synthetic-request',
    },
  })
}

for (const kind of ['payment', 'invoice'] as const) {
  test(`${kind} 完整分页 201 条、详情与返回范围`, async ({ page }, testInfo) => {
    await base(page)
    const collection = kind === 'payment' ? 'payments' : 'invoices'
    await page.route(
      (url) => url.pathname === `/api/v1/${collection}`,
      (route) => {
        const cursor = new URL(route.request().url()).searchParams.get('cursor')
        const start = cursor ? Number(cursor) : 0
        return route.fulfill({
          json: {
            items: Array.from({ length: Math.min(20, 201 - start) }, (_, i) =>
              kind === 'payment' ? payment(start + i + 1) : invoice(start + i + 1),
            ),
            next_cursor: start + 20 < 201 ? String(start + 20) : '',
          },
        })
      },
    )
    await page.route(
      (url) => url.pathname.startsWith(`/api/v1/${collection}/`),
      (route) =>
        route.fulfill({
          json: detail(
            kind,
            Number(new URL(route.request().url()).pathname.split('/').at(-1)!.split('-').at(-1)),
          ),
        }),
    )
    await page.goto(`/${collection}`)
    const table = page.locator('section.facts-panel table')
    await expect(table.locator('tbody tr')).toHaveCount(20)
    if (kind === 'payment') await captureResponsiveReview(page, testInfo, 'fact-management-list')
    const seen = new Set<string>()
    for (let index = 0; index < 11; index++) {
      const links = await table
        .getByRole('link', { name: '查看详情', exact: true })
        .evaluateAll((nodes) => nodes.map((node) => (node as HTMLAnchorElement).pathname))
      for (const href of links) {
        expect(seen.has(href)).toBe(false)
        seen.add(href)
      }
      if (index < 10) {
        await page.getByRole('button', { name: '下一页', exact: true }).click()
        await expect(table.locator('tbody tr')).toHaveCount(index === 9 ? 1 : 20)
        await expect(page).toHaveURL(new RegExp(`cursor=${(index + 1) * 20}$`))
        await expect(
          table.getByRole('link', { name: '查看详情', exact: true }).first(),
        ).toHaveAttribute('href', new RegExp(uuid((index + 1) * 20 + 1)))
      }
    }
    expect(seen.size).toBe(201)
    await expect(page.getByRole('button', { name: '下一页', exact: true })).toBeDisabled()
    await page.getByRole('link', { name: '查看详情', exact: true }).click()
    await expect(
      page.getByRole('heading', {
        name: kind === 'payment' ? '支付详情' : '发票详情',
        exact: true,
      }),
    ).toBeVisible()
    await expect(page.getByRole('link', { name: '打开原件', exact: true })).toHaveAttribute(
      'href',
      `/api/v1/documents/${sourceID}/content`,
    )
    await expect(
      page.getByRole('link', { name: '纠正字段 / 查看历史', exact: true }),
    ).toHaveAttribute('href', `/facts/${kind}/${uuid(201)}/correction`)
    await expect(page.getByRole('link', { name: '调整分配', exact: true })).toHaveAttribute(
      'href',
      `/allocations/${kind}/${uuid(201)}`,
    )
    if (kind === 'invoice') {
      await expect(page.getByRole('heading', { name: '当前发票明细' })).toBeVisible()
      await expect(page.getByText('合成服务', { exact: true })).toBeVisible()
      await captureResponsiveReview(page, testInfo, 'fact-management-detail')
    }
    await page.getByRole('link', { name: '返回列表', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/${collection}\\?cursor=200$`))
    await expect(table.locator('tbody tr')).toHaveCount(1)
  })
}

test('切换筛选忽略迟到响应，失败清空旧范围并按相同查询重试', async ({ page }) => {
  await base(page)
  let release = () => {}
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  let failure = true,
    oldRequested = false,
    oldFinished = false
  const attempts: string[] = []
  await page.route(
    (url) => url.pathname === '/api/v1/payments',
    async (route) => {
      const q = new URL(route.request().url()).searchParams.get('q') ?? ''
      attempts.push(q)
      if (q === 'old') {
        oldRequested = true
        await pending
      }
      if (q === 'failure' && failure) return failed(route)
      await route.fulfill({
        json: { items: [{ ...payment(1), merchant: q || 'initial' }], next_cursor: '' },
      })
      if (q === 'old') oldFinished = true
    },
  )
  await page.goto('/payments')
  await expect(page.getByText('initial', { exact: true })).toBeVisible()
  await page.getByLabel('商户 / 订单号').fill('old')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect.poll(() => oldRequested).toBe(true)
  await page.getByLabel('商户 / 订单号').fill('new')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByText('new', { exact: true })).toBeVisible()
  release()
  await expect.poll(() => oldFinished).toBe(true)
  await expect(page.getByText('new', { exact: true })).toBeVisible()
  await expect(page.getByText('old', { exact: true })).toHaveCount(0)
  await page.getByLabel('商户 / 订单号').fill('failure')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('合成请求失败')
  await expect(page.locator('tbody tr')).toHaveCount(0)
  failure = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('failure', { exact: true })).toBeVisible()
  expect(attempts.filter((q) => q === 'failure')).toHaveLength(2)
})

test('Viewer 查看完整详情但不接收或请求来源，路由 404 不保留旧详情', async ({ page }) => {
  await base(page, true)
  const sources: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/documents/') || request.url().includes('/claim-sets/'))
      sources.push(request.url())
  })
  const current = detail('invoice', 1, true)
  current.links = [
    {
      id: uuid(91),
      target_id: uuid(2),
      allocated_minor: 1000,
      currency: 'CNY',
      target_currency: 'CNY',
      target_business_date: '2026-09-04',
      target_amount_minor: 12345,
      target_allocated_minor: 1000,
      target_version: 1,
      target_available: true,
    },
  ]
  await page.route(`**/api/v1/invoices/${uuid(1)}`, (route) => route.fulfill({ json: current }))
  await page.route(`**/api/v1/payments/${uuid(2)}`, (route) => failed(route, 404))
  await page.goto(`/invoices/${uuid(1)}`)
  await expect(page.getByText('合成服务', { exact: true })).toBeVisible()
  for (const name of ['打开原件', '纠正字段 / 查看历史', '调整分配'])
    await expect(page.getByRole('link', { name, exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除单据', exact: true })).toHaveCount(0)
  expect(sources).toEqual([])
  await page.getByRole('link', { name: '查看关联支付', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('单据不存在')
  await expect(page.getByText('合成服务', { exact: true })).toHaveCount(0)
})

test('删除必须明确确认，失败保留详情，成功返回刷新后的首屏', async ({ page }) => {
  await base(page)
  let attempts = 0
  await page.route(`**/api/v1/payments/${uuid(1)}`, (route) => {
    if (route.request().method() === 'DELETE') {
      attempts++
      return attempts === 1 ? failed(route, 409) : route.fulfill({ status: 204 })
    }
    return route.fulfill({ json: detail('payment', 1) })
  })
  await page.route(
    (url) => url.pathname === '/api/v1/payments',
    (route) => route.fulfill({ json: { items: [], next_cursor: '' } }),
  )
  await page.goto(`/payments/${uuid(1)}`)
  await page.getByRole('button', { name: '删除单据', exact: true }).click()
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('button', { name: '删除单据', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  const confirm = page.getByRole('button', { name: '确认删除', exact: true })
  await expect(confirm).toBeDisabled()
  expect(attempts).toBe(0)
  await page.getByRole('checkbox', { name: '我已核对单据并理解影响' }).check()
  await confirm.click()
  await expect(page.getByRole('alert')).toContainText('合成请求失败')
  await expect(page.getByRole('heading', { name: '当前正式字段' })).toBeVisible()
  await confirm.click()
  await expect(page).toHaveURL(/\/payments$/)
  await expect(page.getByText('当前范围没有支付记录')).toBeVisible()
  expect(attempts).toBe(2)
})

test('审核来源失败可重试，跨详情后迟到来源不覆盖新单据', async ({ page }) => {
  await base(page)
  const first = detail('payment', 1),
    second = detail('invoice', 2)
  first.links = [
    {
      id: uuid(3),
      target_id: uuid(2),
      allocated_minor: 100,
      currency: 'CNY',
      target_currency: 'CNY',
      target_amount_minor: 12345,
      target_allocated_minor: 100,
      target_version: 2,
      target_available: true,
      target_business_date: '2026-09-04',
    },
  ]
  second.source!.claim_set_id = uuid(8004)
  await page.route(`**/api/v1/payments/${uuid(1)}`, (route) => route.fulfill({ json: first }))
  await page.route(`**/api/v1/invoices/${uuid(2)}`, (route) => route.fulfill({ json: second }))
  const sourceReview = (marker: string): Review => ({
    entry_mode: 'ai',
    job: {
      id: uuid(5),
      document_id: sourceID,
      original_name: 'synthetic.png',
      status: 'completed',
      ingestion_kind: 'upload',
      detected_mime: 'image/png',
      attempt_count: 1,
      created_at: timestamp,
      version: 1,
    },
    claim_set_id: uuid(8004),
    document_type: 'invoice',
    revision: 2,
    optimistic_version: 1,
    claim_status: 'confirmed',
    page_count: 1,
    pages: [],
    invoice_item_spans: [],
    fields: [
      {
        id: uuid(6),
        path: 'seller_name',
        value_type: 'string',
        presence: 'present',
        value: marker,
        source: 'ai',
        evidence: [{ id: uuid(7), page: 1, quote: marker }],
      },
    ],
    validations: [],
    candidates: [],
    duplicate_candidates: [],
  })
  let release = () => {},
    attempts = 0,
    pendingRequested = false,
    oldFinished = false
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route(`**/api/v1/claim-sets/${uuid(8002)}`, async (route) => {
    if (++attempts === 1) return failed(route)
    pendingRequested = true
    await pending
    await route.fulfill({ json: sourceReview('迟到旧来源') })
    oldFinished = true
  })
  await page.route(`**/api/v1/claim-sets/${uuid(8004)}`, (route) =>
    route.fulfill({ json: sourceReview('当前合成来源') }),
  )
  await page.goto(`/payments/${uuid(1)}`)
  await page.getByRole('button', { name: '展开审核来源' }).click()
  await expect(page.getByRole('alert')).toContainText('合成请求失败')
  await page.getByRole('button', { name: '展开审核来源' }).click()
  await expect.poll(() => pendingRequested).toBe(true)
  await page.getByRole('link', { name: '查看关联发票' }).click()
  await expect(page.getByRole('heading', { name: '发票详情', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '展开审核来源' }).click()
  await expect(page.locator('.source-fields')).toContainText('当前合成来源')
  await expect(page.locator('.source-fields')).toContainText('第 1 页：当前合成来源')
  release()
  await expect.poll(() => oldFinished).toBe(true)
  await expect(page.locator('.source-fields')).not.toContainText('迟到旧来源')
  await expect(page.locator('.source-fields')).toContainText('当前合成来源')
})
