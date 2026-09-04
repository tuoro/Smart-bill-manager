import { expect, test, type Page, type Route } from '@playwright/test'
import type {
  Session,
  Trip,
  TripAssignmentRequest,
  TripAttributionCandidate,
} from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const timestamp = '2026-08-31T08:00:00Z'
const tripA = trip('00000000-0000-4000-8000-000000000901', '上海', '北京')
const tripB = trip('00000000-0000-4000-8000-000000000902', '杭州', '深圳')
const paymentID = '00000000-0000-4000-8000-000000000911'
const invoiceID = '00000000-0000-4000-8000-000000000912'
const oldAssignmentID = '00000000-0000-4000-8000-000000000921'
const movedAssignmentID = '00000000-0000-4000-8000-000000000922'

test.describe('M3 行程归属真实组件状态矩阵', () => {
  test('Owner：建议分页、冲突重试、assign/move/unassign 与响应式可达', async ({
    page,
  }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, ownerSession())
    let payment = candidate({
      fact_type: 'payment',
      fact_id: paymentID,
      display_name: '合成交通商户',
      business_date: '2026-08-28',
      amount_minor: 12345,
      currency: 'CNY',
      suggested: true,
      reason_codes: ['date_inside_trip'],
    })
    let invoice = candidate({
      fact_type: 'invoice',
      fact_id: invoiceID,
      display_name: '合成住宿发票',
      business_date: '2026-08-27',
      amount_minor: 88000,
      currency: 'CNY',
      current_assignment_id: oldAssignmentID,
      current_trip_id: tripB.id,
      current_trip_destination: tripB.destination,
      suggested: true,
      reason_codes: ['linked_fact_assigned_to_trip'],
    })
    const outside = candidate({
      fact_type: 'payment',
      fact_id: '00000000-0000-4000-8000-000000000913',
      display_name: '合成范围外记录',
      business_date: '2026-08-01',
      amount_minor: 500,
      currency: 'CNY',
      suggested: false,
      reason_codes: [],
    })
    const submitted: TripAssignmentRequest[] = []
    const idempotencyKeys: string[] = []
    let failNextPage = true
    let conflictFirstAssignment = true

    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [tripA, tripB] }))
    await page.route(tripCandidatesURL(tripA.id), async (route) => {
      const query = new URL(route.request().url()).searchParams
      const view = query.get('view')
      if (view === 'assigned') {
        const assigned = [payment, invoice].filter((item) => item.current_trip_id === tripA.id)
        await fulfillJSON(route, attributionPage(assigned))
        return
      }
      if (query.get('cursor')) {
        if (failNextPage) {
          failNextPage = false
          await fulfillError(route, 503, 'unavailable', '归属分页暂时不可用')
          return
        }
        await fulfillJSON(route, attributionPage([outside]))
        return
      }
      await fulfillJSON(route, {
        ...attributionPage([payment, invoice]),
        next_cursor: 'trip-page-2',
      })
    })
    await page.route(tripCandidatesURL(tripB.id), (route) =>
      fulfillJSON(route, {
        trip: tripB,
        rule_version: 'trip-attribution/1',
        items: [],
      }),
    )
    await page.route(tripAssignmentsURL, async (route) => {
      const body = route.request().postDataJSON() as TripAssignmentRequest
      submitted.push(body)
      idempotencyKeys.push(route.request().headers()['idempotency-key'] ?? '')
      if (body.fact_id === paymentID && conflictFirstAssignment) {
        conflictFirstAssignment = false
        await fulfillError(route, 409, 'assignment_version_conflict', '当前归属已变化')
        return
      }
      if (body.fact_id === paymentID) {
        payment = {
          ...payment,
          current_assignment_id: '00000000-0000-4000-8000-000000000923',
          current_trip_id: tripA.id,
          current_trip_destination: tripA.destination,
          reason_codes: ['currently_assigned', 'date_inside_trip'],
        }
        await fulfillJSON(route, assignmentResult('assign', payment.current_assignment_id))
        return
      }
      if (body.desired_trip_id === tripA.id) {
        invoice = {
          ...invoice,
          current_assignment_id: movedAssignmentID,
          current_trip_id: tripA.id,
          current_trip_destination: tripA.destination,
          reason_codes: ['currently_assigned', 'linked_fact_assigned_to_trip'],
        }
        await fulfillJSON(route, {
          ...assignmentResult('move', movedAssignmentID),
          previous_assignment_id: oldAssignmentID,
        })
        return
      }
      invoice = {
        ...invoice,
        current_assignment_id: undefined,
        current_trip_id: undefined,
        current_trip_destination: undefined,
        reason_codes: ['linked_fact_assigned_to_trip'],
      }
      await fulfillJSON(route, {
        ...assignmentResult('unassign'),
        previous_assignment_id: movedAssignmentID,
      })
    })

    await page.goto('/trips')
    await expect(page.getByRole('heading', { name: '行程归属', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /北京/ })).toHaveAttribute('aria-current', 'true')
    await expect(page.getByText('业务日期在行程区间内')).toBeVisible()
    await expect(page.getByText('关联支付或发票已归属本行程')).toBeVisible()

    const loadMore = page.getByRole('button', { name: '加载更多' })
    await loadMore.click()
    await expect(page.getByRole('alert')).toContainText('归属分页暂时不可用')
    await expect(page.getByText('合成交通商户')).toBeVisible()
    await loadMore.click()
    await expect(page.getByText('合成范围外记录')).toBeVisible()
    pageErrors.length = 0

    await page.evaluate(() => {
      document.cookie = 'sbm_csrf=synthetic-trip-csrf; path=/; SameSite=Strict'
    })
    let paymentRow = page.locator('.trip-candidate').filter({ hasText: '合成交通商户' })
    await paymentRow.getByLabel('归属理由').fill(' 日期命中，人工确认 ')
    await paymentRow.getByRole('button', { name: '归属到当前行程' }).click()
    await expect(paymentRow.getByRole('alert')).toContainText('已保留填写的理由')
    await expect(paymentRow.getByLabel('归属理由')).toHaveValue(' 日期命中，人工确认 ')
    pageErrors.length = 0
    await paymentRow.getByRole('button', { name: '归属到当前行程' }).click()
    await expect(page.locator('.notice-success')).toContainText('合成交通商户 的行程归属已更新')

    let invoiceRow = page.locator('.trip-candidate').filter({ hasText: '合成住宿发票' })
    await invoiceRow.getByLabel('归属理由').fill('调整到北京行程')
    await invoiceRow.getByRole('button', { name: '从原行程移动到当前行程' }).click()
    await expect(page.locator('.notice-success')).toContainText('合成住宿发票 的行程归属已更新')
    invoiceRow = page.locator('.trip-candidate').filter({ hasText: '合成住宿发票' })
    await invoiceRow.getByLabel('归属理由').fill('撤销误归属')
    await invoiceRow.getByRole('button', { name: '撤销当前归属' }).click()
    await expect(page.locator('.notice-success')).toContainText('合成住宿发票 的行程归属已更新')

    expect(submitted).toEqual([
      {
        fact_type: 'payment',
        fact_id: paymentID,
        desired_trip_id: tripA.id,
        expected_assignment_id: null,
        reason: '日期命中，人工确认',
      },
      {
        fact_type: 'payment',
        fact_id: paymentID,
        desired_trip_id: tripA.id,
        expected_assignment_id: null,
        reason: '日期命中，人工确认',
      },
      {
        fact_type: 'invoice',
        fact_id: invoiceID,
        desired_trip_id: tripA.id,
        expected_assignment_id: oldAssignmentID,
        reason: '调整到北京行程',
      },
      {
        fact_type: 'invoice',
        fact_id: invoiceID,
        desired_trip_id: null,
        expected_assignment_id: movedAssignmentID,
        reason: '撤销误归属',
      },
    ])
    expect(idempotencyKeys[0]).toBe(idempotencyKeys[1])
    expect(new Set(idempotencyKeys).size).toBe(3)
    expect(idempotencyKeys.every((key) => /^[0-9a-f-]{36}$/.test(key))).toBe(true)
    await captureResponsiveReview(page, testInfo, 'trips')

    for (const width of [768, 384]) {
      await page.setViewportSize({ width, height: 1000 })
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
        ),
      ).toBe(true)
      paymentRow = page.locator('.trip-candidate').filter({ hasText: '合成交通商户' })
      await paymentRow.getByLabel('归属理由').focus()
      await expect(paymentRow.getByLabel('归属理由')).toBeFocused()
      await expect(paymentRow.getByLabel('归属理由')).toBeVisible()
    }
    expect(pageErrors).toEqual([])
  })

  test('Viewer 只读，Reviewer 直接访问不发起 Fact 请求', async ({ page }) => {
    await mockSession(page, viewerSession())
    let viewerRequests = 0
    await page.route(tripsURL, async (route) => {
      viewerRequests += 1
      await fulfillJSON(route, { items: [tripA] })
    })
    await page.route(tripCandidatesURL(tripA.id), (route) =>
      fulfillJSON(route, attributionPage([readOnlyCandidate()])),
    )
    await page.goto('/trips')
    await expect(page.getByText('当前账号为只读')).toBeVisible()
    await expect(page.getByLabel('归属理由')).toHaveCount(0)
    expect(viewerRequests).toBe(1)

    const reviewerPage = await page.context().newPage()
    await mockSession(reviewerPage, reviewerSession())
    let forbiddenRequests = 0
    await reviewerPage.route(tripsURL, async (route) => {
      forbiddenRequests += 1
      await fulfillJSON(route, { items: [] })
    })
    await reviewerPage.goto('/trips')
    await expect(reviewerPage.getByText('没有查看行程的权限')).toBeVisible()
    await expect(reviewerPage.getByRole('button', { name: '刷新' })).toHaveCount(0)
    expect(forbiddenRequests).toBe(0)
    await reviewerPage.close()
  })

  test('加载、失败、重试空状态与离线状态彼此明确', async ({ context, page }) => {
    await mockSession(page, ownerSession())
    let release = () => {}
    const heldResponse = new Promise<void>((resolve) => {
      release = resolve
    })
    let attempts = 0
    await page.route(tripsURL, async (route) => {
      attempts += 1
      if (attempts === 1) {
        await heldResponse
        await fulfillError(route, 503, 'unavailable', '行程列表暂时不可用')
        return
      }
      await fulfillJSON(route, { items: [] })
    })

    await page.goto('/trips')
    await expect(page.getByRole('status')).toContainText('正在读取行程')
    release()
    await expect(page.getByRole('alert')).toContainText('行程列表暂时不可用')
    await page.getByRole('button', { name: '重试' }).click()
    await expect(page.getByText('还没有正式行程')).toBeVisible()

    await context.setOffline(true)
    await expect(page.getByRole('status')).toContainText('当前离线')
    await expect(page.getByRole('button', { name: '刷新' })).toBeDisabled()
    await context.setOffline(false)
    await expect(page.getByText('当前离线')).toHaveCount(0)
  })
})

function trip(id: string, origin: string, destination: string): Trip {
  return {
    id,
    origin,
    destination,
    start_date: '2026-08-26',
    end_date: '2026-08-30',
    traveler_name: '合成用户',
    transport_type: 'train',
    booking_reference: `SYN-${id.slice(-3)}`,
    assigned_payment_count: 0,
    assigned_invoice_count: 0,
    created_at: timestamp,
  }
}

function candidate(value: TripAttributionCandidate): TripAttributionCandidate {
  return value
}

function readOnlyCandidate(): TripAttributionCandidate {
  return candidate({
    fact_type: 'payment',
    fact_id: paymentID,
    display_name: '合成交通商户',
    business_date: '2026-08-28',
    amount_minor: 12345,
    currency: 'CNY',
    suggested: true,
    reason_codes: ['date_inside_trip'],
  })
}

function attributionPage(items: TripAttributionCandidate[]) {
  return { trip: tripA, rule_version: 'trip-attribution/1' as const, items }
}

function assignmentResult(action: 'assign' | 'move' | 'unassign', assignmentID?: string) {
  return {
    decision_id: crypto.randomUUID(),
    action,
    ...(assignmentID ? { assignment_id: assignmentID } : {}),
    replayed: false,
  }
}

function ownerSession(): Session {
  return session('owner', ['documents.process', 'facts.read', 'trip_assignments.manage'])
}

function viewerSession(): Session {
  return session('viewer', ['facts.read'])
}

function reviewerSession(): Session {
  return session('reviewer', ['documents.process', 'claims.review'])
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
  await page.route(
    (url) => url.pathname === '/api/v1/session',
    (route) => fulfillJSON(route, value),
  )
}

const tripsURL = (url: URL) => url.pathname === '/api/v1/trips'
const tripAssignmentsURL = (url: URL) => url.pathname === '/api/v1/trip-assignments'

function tripCandidatesURL(tripID: string) {
  return (url: URL) => url.pathname === `/api/v1/trips/${tripID}/attribution-candidates`
}

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
