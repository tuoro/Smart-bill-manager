import { test, expect, type Page } from '@playwright/test'
import type { AllocationWorkspace, FactDetail, FactKind, Session, Trip } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`
const time = '2026-09-05T00:00:00Z'
const session: Session = {
  user: { id: id(1), email: 'synthetic@example.invalid', display_name: '合成用户' },
  tenant: { id: id(2), name: '合成业务工作区', default_currency: 'CNY', timezone: 'Asia/Shanghai' },
  role: 'owner',
  capabilities: ['facts.read', 'allocations.manage', 'trip_assignments.manage', 'resources.delete'],
  csrf_token: 'synthetic-ui-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}
async function authenticate(page: Page, viewer = false) {
  await page.route('**/api/v1/session', (route) =>
    route.fulfill({
      json: viewer ? { ...session, role: 'viewer', capabilities: ['facts.read'] } : session,
    }),
  )
}
function target(n: number, current = false): AllocationWorkspace['targets'][number] {
  return {
    fact_type: 'invoice',
    id: id(n),
    amount_minor: 5000,
    allocated_minor: current ? 1000 : 0,
    remaining_minor: current ? 4000 : 5000,
    currency: 'CNY',
    business_date: '2026-01-01',
    display_name: `合成跨期发票 ${n}`,
    name_exact: false,
    date_distance_days: 200,
    current_allocated_minor: current ? 1000 : 0,
    maximum_allocatable_minor: 5000,
    ...(current ? { current_link_id: id(90) } : {}),
  }
}
test('跨期搜索、翻页保留草稿、理由与失败重试幂等', async ({ page }, info) => {
  await authenticate(page)
  const workspace: AllocationWorkspace = {
    anchor: {
      fact_type: 'payment',
      id: id(10),
      amount_minor: 10000,
      allocated_minor: 1000,
      remaining_minor: 9000,
      currency: 'CNY',
      business_date: '2026-08-27',
      display_name: '合成支付',
    },
    links: [
      {
        id: id(90),
        target_fact_type: 'invoice',
        target_fact_id: id(20),
        allocated_minor: 1000,
        currency: 'CNY',
        created_at: time,
      },
    ],
    targets: [target(20, true)],
    plan_hash: 'a'.repeat(64),
  }
  const bodies: {
    desired_allocations: { target_fact_id: string; allocated_minor: number }[]
    reason: string
  }[] = []
  const keys: string[] = []
  await page.route('**/api/v1/allocations/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/targets')) {
      expect(url.searchParams.get('view')).toBe('all_dates')
      return route.fulfill({
        json: url.searchParams.get('cursor')
          ? { items: [target(22)] }
          : { items: [target(21)], next_cursor: 'synthetic-next' },
      })
    }
    if (url.pathname.endsWith('/adjustments')) {
      bodies.push(route.request().postDataJSON())
      keys.push(route.request().headers()['idempotency-key']!)
      if (bodies.length === 1)
        return route.fulfill({
          status: 503,
          json: { error: { code: 'synthetic_failure', message: '合成失败，请重试' } },
        })
      return route.fulfill({
        json: {
          adjustment_id: id(91),
          mode: 'supplement',
          ended_link_ids: [],
          created_link_ids: [id(92)],
          plan_hash: 'b'.repeat(64),
          replayed: false,
        },
      })
    }
    return route.fulfill({ json: workspace })
  })
  await page.goto(`/allocations/payment/${id(10)}`)
  await expect(page.getByText('合成跨期发票 20', { exact: true })).toBeVisible()
  await page.getByLabel('日期范围').selectOption('all_dates')
  await page.getByRole('button', { name: '查询单据' }).click()
  const chosen = page.locator('.allocation-target-row').filter({ hasText: '合成跨期发票 21' })
  await chosen.getByRole('checkbox').check()
  await chosen.getByLabel('分配金额（最小单位）').fill('2000')
  await page.getByRole('button', { name: '下一页候选' }).click()
  await expect(chosen.getByRole('checkbox')).toBeChecked()
  await expect(
    page
      .locator('.allocation-target-row')
      .filter({ hasText: '合成跨期发票 20' })
      .getByRole('checkbox'),
  ).toBeChecked()
  await expect(page.getByText('已选择超过 30 天的跨期单据', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: '确认补充分配' }).click()
  await expect(page.getByText('请填写本次调整理由')).toBeVisible()
  expect(bodies).toHaveLength(0)
  await page.getByLabel(/调整理由/).fill('合成跨期人工核对理由')
  await captureResponsiveReview(page, info, 'b7-cross-period-allocation')
  await page.getByRole('button', { name: '确认补充分配' }).click()
  await expect(page.getByText('合成失败，请重试')).toBeVisible()
  await expect(chosen.getByLabel('分配金额（最小单位）')).toHaveValue('2000')
  await page.getByRole('button', { name: '确认补充分配' }).click()
  await expect(page.getByText('补充分配已保存，余额已刷新')).toBeVisible()
  expect(bodies).toHaveLength(2)
  expect(bodies[1]?.desired_allocations).toEqual([
    { target_fact_id: id(20), allocated_minor: 1000 },
    { target_fact_id: id(21), allocated_minor: 2000 },
  ])
  expect(keys[0]).toBe(keys[1])
})

function detail(kind: FactKind, marked = false, version = 1): FactDetail {
  const common = {
    id: id(10),
    bad_debt: marked,
    currency: 'CNY' as const,
    allocated_minor: 0,
    remaining_minor: 10000,
    allocation_status: 'unallocated' as const,
    created_at: time,
  }
  return {
    fact_type: kind,
    version,
    links: [],
    ...(kind === 'payment'
      ? {
          payment: {
            ...common,
            amount_minor: 10000,
            merchant: '合成坏账支付',
            transaction_time: time,
            business_date: '2026-09-05',
            source_timezone: 'Asia/Shanghai',
          },
        }
      : {
          invoice: {
            ...common,
            total_minor: 10000,
            invoice_number: 'SYN-BAD-DEBT',
            invoice_date: '2026-09-05',
            seller_name: '合成销售方',
            buyer_name: '合成购买方',
            item_count: 0,
          },
        }),
  }
}
for (const kind of ['payment', 'invoice'] as const) {
  test(`${kind} 标记/取消坏账、版本冲突保留理由和只读可见`, async ({ page }, info) => {
    await authenticate(page)
    let marked = false,
      version = 1,
      writes = 0
    await page.route(
      `**/api/v1/${kind === 'payment' ? 'payments' : 'invoices'}/${id(10)}`,
      (route) => route.fulfill({ json: detail(kind, marked, version) }),
    )
    await page.route('**/api/v1/facts/**/bad-debt', (route) => {
      const body = route.request().postDataJSON() as {
        marked: boolean
        expected_version: number
        reason: string
      }
      writes++
      if (writes === 1) {
        version++
        return route.fulfill({
          status: 409,
          json: { error: { code: 'version_conflict', message: '单据版本已变化' } },
        })
      }
      expect(body.expected_version).toBe(version)
      expect(body.reason).not.toBe('')
      marked = body.marked
      version++
      return route.fulfill({
        json: { decision_id: id(100 + writes), version, marked, replayed: false },
      })
    })
    await page.goto(`/${kind === 'payment' ? 'payments' : 'invoices'}/${id(10)}`)
    await page.getByRole('button', { name: '标记坏账', exact: true }).click()
    await expect(page.getByLabel('标记坏账理由')).toBeFocused()
    await page.getByLabel('标记坏账理由').fill('合成异常核对说明')
    await page.getByRole('button', { name: '确认标记坏账', exact: true }).click()
    await expect(page.getByText('单据版本已变化')).toBeVisible()
    await expect(page.getByLabel('标记坏账理由')).toHaveValue('合成异常核对说明')
    await page.getByRole('button', { name: '刷新当前状态' }).click()
    await expect(page.getByText('已刷新，请核对当前坏账状态再提交；理由已保留。')).toBeVisible()
    await page.getByRole('button', { name: '确认标记坏账', exact: true }).click()
    await expect(page.getByText('已标记坏账', { exact: true })).toBeVisible()
    await captureResponsiveReview(page, info, `b8-${kind}-bad-debt`)
    await page.getByRole('button', { name: '取消坏账标记' }).click()
    await page.getByLabel('取消坏账理由').fill('合成已解决异常')
    await page.getByRole('button', { name: '确认取消标记' }).click()
    await expect(page.getByText('未标记坏账', { exact: true })).toBeVisible()
    await authenticate(page, true)
    marked = true
    await page.reload()
    await expect(page.getByText('已标记坏账', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '取消坏账标记' })).toHaveCount(0)
  })
}
test('坏账关联行程显示保护并禁用删除', async ({ page }, info) => {
  await authenticate(page)
  const trip: Trip = {
    id: id(30),
    name: '合成受保护行程',
    start_date: '2026-09-01',
    end_date: '2026-09-05',
    timezone: 'Asia/Shanghai',
    notes: '',
    version: 1,
    origin_kind: 'manual',
    material_count: 0,
    assigned_payment_count: 1,
    assigned_invoice_count: 0,
    created_at: time,
    bad_debt_locked: true,
  }
  await page.route('**/api/v1/trips', (route) => route.fulfill({ json: { items: [trip] } }))
  await page.route('**/api/v1/trips/*/attribution-candidates?*', (route) =>
    route.fulfill({ json: { trip, rule_version: 'trip-attribution/1', items: [] } }),
  )
  await page.route('**/api/v1/trip-evidence?*', (route) => route.fulfill({ json: { items: [] } }))
  await page.goto('/trips')
  await expect(page.getByText('坏账删除保护', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '编辑行程', exact: true }).click()
  await expect(page.getByRole('button', { name: '删除行程…', exact: true })).toBeDisabled()
  await captureResponsiveReview(page, info, 'b8-trip-delete-protection')
})
