import { expect, test, type Route } from '@playwright/test'
import { captureResponsiveReview } from './visual-review'
import type {
  Session,
  Trip,
  TripEvidence,
  TripManagementRequest,
  TripMaterialRequest,
} from '../src/data/client'

const tripID = '00000000-0000-4000-8000-000000007001'
const evidence = (id: string, destination: string): TripEvidence => ({
  id,
  document_id: id,
  destination,
  origin: '合成出发地',
  start_date: '2026-09-01',
  end_date: '2026-09-05',
  version: 1,
})
const session: Session = {
  user: {
    id: '00000000-0000-4000-8000-000000007002',
    email: 'synthetic@example.invalid',
    display_name: '合成用户',
  },
  tenant: {
    id: '00000000-0000-4000-8000-000000007003',
    name: '合成工作区',
    default_currency: 'CNY',
    timezone: 'Asia/Shanghai',
  },
  role: 'owner',
  capabilities: ['facts.read', 'documents.process', 'trip_assignments.manage', 'resources.delete'],
  csrf_token: 'synthetic-ui-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}

test('手动创建一趟行程、关联往返两张票、保留编辑草稿处理版本冲突', async ({ page }, testInfo) => {
  let trip: Trip | undefined
  const tickets = [
    evidence('00000000-0000-4000-8000-000000007011', '合成去程'),
    evidence('00000000-0000-4000-8000-000000007012', '合成返程'),
  ]
  let createCalls = 0
  let conflict = true
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const method = route.request().method()
    if (path === '/api/v1/session') return json(route, session)
    if (path === '/api/v1/trips' && method === 'GET')
      return json(route, { items: trip ? [trip] : [] })
    if (path === '/api/v1/trips' && method === 'POST') {
      const body = route.request().postDataJSON() as TripManagementRequest
      expect(body.expected_version).toBe(0)
      expect(body.timezone).toBe('Asia/Shanghai')
      expect(route.request().headers()['idempotency-key']).toBeTruthy()
      createCalls++
      trip = {
        bad_debt_locked: false,
        id: tripID,
        name: body.name,
        start_date: body.start_date,
        end_date: body.end_date,
        timezone: body.timezone,
        notes: body.notes,
        version: 1,
        origin_kind: 'manual',
        material_count: 0,
        assigned_payment_count: 0,
        assigned_invoice_count: 0,
        created_at: '2026-09-01T00:00:00Z',
      }
      return json(route, { trip_id: tripID, version: 1, replayed: false }, 201)
    }
    if (path === `/api/v1/trips/${tripID}` && method === 'PATCH') {
      const body = route.request().postDataJSON() as TripManagementRequest
      if (conflict) {
        conflict = false
        trip = { ...trip!, version: 2, notes: '另一位用户刚更新的备注' }
        return json(route, { error: { code: 'version_conflict', message: '行程版本已变化' } }, 409)
      }
      expect(body.expected_version).toBe(2)
      trip = { ...trip!, name: body.name, notes: body.notes, version: 3 }
      return json(route, { trip_id: tripID, version: 3, replayed: false })
    }
    if (path === `/api/v1/trips/${tripID}/attribution-candidates`)
      return json(route, { trip, rule_version: 'trip-attribution/1', items: [] })
    if (path === '/api/v1/trip-evidence') {
      const filter = new URL(route.request().url()).searchParams.get('trip_id')
      return json(route, {
        items: filter ? tickets.filter((item) => item.current_trip_id === filter) : tickets,
      })
    }
    if (path === '/api/v1/trip-material-assignments') {
      const body = route.request().postDataJSON() as TripMaterialRequest
      const item = tickets.find((ticket) => ticket.id === body.evidence_id)!
      expect(body.expected_version).toBe(item.version)
      expect(body.expected_link_id).toBe(item.current_link_id ?? null)
      item.version++
      item.current_link_id = body.desired_trip_id ? `${item.id}-link` : undefined
      item.current_trip_id = body.desired_trip_id ?? undefined
      item.current_trip_name = body.desired_trip_id ? trip!.name : undefined
      trip!.material_count = tickets.filter((ticket) => ticket.current_trip_id === tripID).length
      return json(route, { version: item.version, link_id: item.current_link_id, replayed: false })
    }
    throw new Error(`Unexpected synthetic UI request: ${method} ${path}`)
  })
  await page.goto('/trips')
  await expect(page.getByText('还没有行程', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '新建行程', exact: true }).click()
  await expect(page.getByLabel('行程名称')).toBeFocused()
  await page.getByLabel('行程名称').fill('合成客户拜访')
  await page.getByLabel('开始日期').fill('2026-09-01')
  await page.getByLabel('结束日期').fill('2026-09-05')
  await page.getByLabel('操作理由').fill('先创建一趟行程')
  await captureResponsiveReview(page, testInfo, 'manual-trip-editor')
  await page.getByRole('button', { name: '保存行程', exact: true }).click()
  await expect(page.getByRole('heading', { name: '合成客户拜访', exact: true })).toBeVisible()
  expect(createCalls).toBe(1)
  const materials = page.getByRole('region', { name: '行程凭证', exact: true })
  for (let index = 0; index < 2; index++) {
    await materials.getByLabel('材料归属理由').nth(index).fill('同一趟出差的往返机票')
    await materials.getByRole('button', { name: '加入当前行程', exact: true }).first().click()
    await expect(materials.getByRole('button', { name: '移出当前行程', exact: true })).toHaveCount(
      index + 1,
    )
  }
  await expect(page.getByText('支付 0 · 发票 0 · 凭证 2')).toBeVisible()
  await captureResponsiveReview(page, testInfo, 'manual-trip-materials')
  await page.getByRole('button', { name: '编辑行程', exact: true }).click()
  await page.getByLabel('行程名称').fill('合成客户拜访（修订）')
  await page.getByLabel('操作理由').fill('补充名称')
  await page.getByRole('button', { name: '保存行程', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('行程版本已变化')
  await expect(page.getByLabel('行程名称')).toHaveValue('合成客户拜访（修订）')
  await page.getByRole('button', { name: '读取最新版本' }).click()
  await expect(page.getByText('最新备注：另一位用户刚更新的备注')).toBeVisible()
  await page.getByRole('button', { name: '已核对，使用当前草稿继续' }).click()
  await page.getByRole('button', { name: '保存行程', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: '合成客户拜访（修订）', exact: true }),
  ).toBeVisible()
})

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
