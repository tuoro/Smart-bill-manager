import { expect, test, type Page, type Route } from '@playwright/test'
import type { InsightAggregate, InsightFact, InsightPage, Session, Trip } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const trip: Trip = {
  bad_debt_locked: false,
  id: '00000000-0000-4000-8000-000000001001',
  name: '北京',
  timezone: 'Asia/Shanghai',
  version: 1,
  notes: '',
  origin_kind: 'manual',
  material_count: 0,
  start_date: '2026-08-01',
  end_date: '2026-08-03',
  assigned_payment_count: 1,
  assigned_invoice_count: 0,
  created_at: '2026-08-31T08:00:00Z',
}
const payment = insightFact(
  'payment',
  '00000000-0000-4000-8000-000000001011',
  '合成交通支付',
  'CNY',
  12_345,
  6_000,
  trip,
)
const invoice = insightFact(
  'invoice',
  '00000000-0000-4000-8000-000000001012',
  '合成住宿发票',
  'CNY',
  6_000,
  6_000,
)
const usdInvoice = insightFact(
  'invoice',
  '00000000-0000-4000-8000-000000001013',
  '合成美元发票',
  'USD',
  8_800,
  0,
)

test.describe('M4 数据洞察真实组件状态矩阵', () => {
  test('Owner：分币种/类型汇总、稳定分页失败恢复和响应式可达', async ({ page }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, session('owner', ['facts.read', 'insights.read']))
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
    let failNextPage = true
    await page.route(insightsURL, async (route) => {
      const query = new URL(route.request().url()).searchParams
      if (query.get('cursor')) {
        if (failNextPage) {
          failNextPage = false
          await fulfillError(route, 503, 'unavailable', '洞察分页暂时不可用')
          return
        }
        await fulfillJSON(route, insightPage([usdInvoice], summaryGroups(), undefined))
        return
      }
      await fulfillJSON(route, insightPage([payment, invoice], summaryGroups(), 'insight-page-2'))
    })

    await page.goto('/insights')
    await expect(page.getByRole('heading', { name: '数据洞察', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'CNY' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'USD' })).toBeVisible()
    await expect(page.getByText('合成交通支付')).toBeVisible()
    await expect(page.getByText('合成住宿发票')).toBeVisible()
    await expect(page.getByText(/未分配 0 · 部分 1 · 已分配 0/)).toBeVisible()
    await expect(page.getByText('不同币种及支付、发票分别统计，不合并计算。')).toBeVisible()

    const more = page.getByRole('button', { name: '加载更多' })
    await more.click()
    await expect(page.getByRole('alert')).toContainText('洞察分页暂时不可用')
    await expect(page.getByText('合成交通支付')).toBeVisible()
    pageErrors.length = 0
    await more.click()
    await expect(page.getByText('合成美元发票')).toBeVisible()
    await expect(page.getByText('已到达当前筛选结果末尾')).toBeVisible()
    await captureResponsiveReview(page, testInfo, 'insights')

    for (const width of [768, 384]) {
      await page.setViewportSize({ width, height: 1100 })
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
        ),
      ).toBe(true)
      const factType = page.getByLabel('单据类型', { exact: true })
      await factType.focus()
      await expect(factType).toBeFocused()
      await expect(page.getByText('合成交通支付')).toBeVisible()
    }
    expect(pageErrors).toEqual([])
  })

  test('Finance：日期组合门禁、具体 Trip 查询与清除筛选', async ({ page }) => {
    await mockSession(page, session('finance', ['facts.read', 'insights.read']))
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
    const requests: URLSearchParams[] = []
    await page.route(insightsURL, async (route) => {
      const query = new URL(route.request().url()).searchParams
      requests.push(query)
      const filtered = query.get('trip_id') ? [payment] : []
      await fulfillJSON(
        route,
        insightPage(
          filtered,
          filtered.length ? [aggregate('CNY', 'payment', 1, 12_345, 6_000)] : [],
        ),
      )
    })

    await page.goto('/insights')
    await expect(page.getByText('当前筛选没有单据')).toBeVisible()
    expect(requests).toHaveLength(1)

    await page.getByLabel('起始日期').fill('2026-08-01')
    await page.getByRole('button', { name: '应用筛选' }).click()
    await expect(page.getByRole('alert')).toContainText('起止日期必须同时填写')
    expect(requests).toHaveLength(1)

    await page.getByLabel('结束日期').fill('2026-08-31')
    await page.getByLabel('单据类型', { exact: true }).selectOption('payment')
    await page.getByLabel('币种', { exact: true }).selectOption('CNY')
    await page.getByLabel('分配状态', { exact: true }).selectOption('partial')
    await page.getByLabel('行程范围', { exact: true }).selectOption('assigned')
    await page.getByLabel('具体行程（可选）', { exact: true }).selectOption(trip.id)
    await page.getByRole('button', { name: '应用筛选' }).click()
    await expect(page.getByText('合成交通支付')).toBeVisible()
    const query = requests.at(-1)
    expect(Object.fromEntries(query?.entries() ?? [])).toMatchObject({
      fact_type: 'payment',
      date_from: '2026-08-01',
      date_to: '2026-08-31',
      currency: 'CNY',
      allocation_status: 'partial',
      trip_scope: 'assigned',
      trip_id: trip.id,
      limit: '50',
    })

    await page.getByRole('button', { name: '清除筛选' }).click()
    await expect(page.getByLabel('单据类型', { exact: true })).toHaveValue('all')
    await expect(page.getByLabel('起始日期')).toHaveValue('')
    await expect(page.getByLabel('行程范围', { exact: true })).toHaveValue('all')
    expect(Object.fromEntries(requests.at(-1)?.entries() ?? [])).toMatchObject({
      fact_type: 'all',
      allocation_status: 'all',
      trip_scope: 'all',
      limit: '50',
    })
  })

  test('Viewer 可读，Reviewer 直接访问不发起 Fact 或 Trip 请求', async ({ page }) => {
    await mockSession(page, session('viewer', ['facts.read', 'insights.read']))
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
    let viewerRequests = 0
    await page.route(insightsURL, async (route) => {
      viewerRequests += 1
      await fulfillJSON(
        route,
        insightPage([payment], [aggregate('CNY', 'payment', 1, 12_345, 6_000)]),
      )
    })
    await page.goto('/insights')
    await expect(page.getByText('合成交通支付')).toBeVisible()
    await expect(page.getByRole('link', { name: '数据洞察' })).toBeVisible()
    expect(viewerRequests).toBe(1)

    const reviewerPage = await page.context().newPage()
    await mockSession(reviewerPage, session('reviewer', ['documents.process', 'claims.review']))
    let forbiddenRequests = 0
    await reviewerPage.route(insightsURL, async (route) => {
      forbiddenRequests += 1
      await fulfillJSON(route, insightPage([]))
    })
    await reviewerPage.route(tripsURL, async (route) => {
      forbiddenRequests += 1
      await fulfillJSON(route, { items: [] })
    })
    await reviewerPage.goto('/insights')
    await expect(reviewerPage.getByText('没有查看数据洞察的权限')).toBeVisible()
    expect(forbiddenRequests).toBe(0)
    await reviewerPage.close()
  })

  test('加载、失败重试、空结果与离线状态分别呈现', async ({ context, page }) => {
    await mockSession(page, session('owner', ['facts.read', 'insights.read']))
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [] }))
    let release = () => {}
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    let attempts = 0
    await page.route(insightsURL, async (route) => {
      attempts += 1
      if (attempts === 1) {
        await held
        await fulfillError(route, 503, 'unavailable', '洞察服务暂时不可用')
        return
      }
      await fulfillJSON(route, insightPage([]))
    })
    await page.goto('/insights')
    await expect(page.getByRole('status')).toContainText('正在汇总单据')
    release()
    await expect(page.getByRole('alert')).toContainText('洞察服务暂时不可用')
    await page.getByRole('button', { name: '重试' }).click()
    await expect(page.getByText('当前筛选没有汇总')).toBeVisible()
    await expect(page.getByText('当前筛选没有单据')).toBeVisible()

    await context.setOffline(true)
    await page.evaluate(() => window.dispatchEvent(new Event('offline')))
    await expect(page.getByText('当前离线。已加载结果会保留')).toBeVisible()
    await expect(page.getByRole('button', { name: '应用筛选' })).toBeDisabled()
    await context.setOffline(false)
  })
})

function insightPage(
  items: InsightFact[],
  groups: InsightAggregate[] = [],
  nextCursor?: string,
): InsightPage {
  return {
    rule_version: 'fact-insights/1',
    filter: { fact_type: 'all', allocation_status: 'all', trip_scope: 'all' },
    groups,
    items,
    ...(nextCursor ? { next_cursor: nextCursor } : {}),
  }
}

function summaryGroups(): InsightAggregate[] {
  return [
    aggregate('CNY', 'payment', 1, 12_345, 6_000),
    aggregate('CNY', 'invoice', 1, 6_000, 6_000),
    aggregate('USD', 'invoice', 1, 8_800, 0),
  ]
}

function aggregate(
  currency: InsightAggregate['currency'],
  factType: InsightAggregate['fact_type'],
  count: number,
  total: number,
  allocated: number,
): InsightAggregate {
  const status = allocated === 0 ? 'unallocated' : allocated === total ? 'allocated' : 'partial'
  return {
    currency,
    fact_type: factType,
    count,
    total_minor: total,
    allocated_minor: allocated,
    remaining_minor: total - allocated,
    unallocated_count: status === 'unallocated' ? count : 0,
    partial_count: status === 'partial' ? count : 0,
    allocated_count: status === 'allocated' ? count : 0,
  }
}

function insightFact(
  factType: InsightFact['fact_type'],
  factID: string,
  displayName: string,
  currency: InsightFact['currency'],
  amount: number,
  allocated: number,
  assignedTrip?: Trip,
): InsightFact {
  return {
    fact_type: factType,
    fact_id: factID,
    business_date: '2026-08-02',
    display_name: displayName,
    amount_minor: amount,
    allocated_minor: allocated,
    remaining_minor: amount - allocated,
    currency,
    allocation_status:
      allocated === 0 ? 'unallocated' : allocated === amount ? 'allocated' : 'partial',
    ...(assignedTrip
      ? {
          trip: {
            id: assignedTrip.id,
            name: assignedTrip.name,
            start_date: assignedTrip.start_date,
            end_date: assignedTrip.end_date,
          },
        }
      : {}),
  }
}

function session(role: Session['role'], capabilities: string[]): Session {
  return {
    user: {
      id: '00000000-0000-4000-8000-000000000101',
      email: `${role}@example.invalid`,
      display_name: `Synthetic ${role}`,
    },
    tenant: {
      id: '00000000-0000-4000-8000-000000000102',
      name: '合成验收工作区',
      default_currency: 'CNY',
      timezone: 'Asia/Shanghai',
    },
    role,
    capabilities,
    csrf_token: 'synthetic-csrf-token',
    expires_at: '2026-09-01T08:00:00Z',
  }
}

async function mockSession(page: Page, value: Session) {
  await page.route(sessionURL, (route) => fulfillJSON(route, value))
}

const sessionURL = (url: URL) => url.pathname === '/api/v1/session'
const tripsURL = (url: URL) => url.pathname === '/api/v1/trips'
const insightsURL = (url: URL) => url.pathname === '/api/v1/insights'

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

async function fulfillError(route: Route, status: number, code: string, message: string) {
  await fulfillJSON(route, { error: { code, message }, request_id: 'synthetic-request-id' }, status)
}

function trackPageErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
  return errors
}
