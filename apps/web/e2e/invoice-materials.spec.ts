import { test, expect, type Page } from '@playwright/test'
import type { FactDetail, InvoiceMaterial, Session } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const uuid = (index: number) => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
const invoiceID = uuid(50),
  timestamp = '2026-09-04T08:00:00Z'
const syntheticPNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
  'base64',
)
const session: Session = {
  user: { id: uuid(1), email: 'material@example.invalid', display_name: '合成材料用户' },
  tenant: {
    id: uuid(2),
    name: '合成材料工作区',
    default_currency: 'CNY',
    timezone: 'Asia/Shanghai',
  },
  role: 'owner',
  capabilities: ['facts.read', 'review.source.read', 'documents.process'],
  csrf_token: 'synthetic-browser-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}
function fact(version: number): FactDetail {
  return {
    fact_type: 'invoice',
    version,
    links: [],
    invoice: {
      bad_debt: false,
      id: invoiceID,
      total_minor: 10000,
      currency: 'CNY',
      allocated_minor: 0,
      remaining_minor: 10000,
      allocation_status: 'unallocated',
      invoice_number: 'SYN-MATERIAL-001',
      invoice_date: '2026-09-04',
      seller_name: version === 1 ? '合成原销售方' : '合成已核对销售方',
      buyer_name: '合成购买方',
      item_count: 0,
      items: [],
      created_at: timestamp,
    },
  }
}
const material = (index: number): InvoiceMaterial => ({
  id: uuid(index + 1000),
  document_id: uuid(index),
  original_name: `synthetic-material-${index}.png`,
  mime: 'image/png',
  size_bytes: 1024,
  page_count: 1,
  created_at: timestamp,
})
const path = `/api/v1/invoices/${invoiceID}`

async function fixture(
  page: Page,
  options: { role?: 'owner' | 'viewer' | 'reviewer'; conflict?: boolean; uncertain?: boolean } = {},
) {
  let version = 1,
    failed = false
  let items: InvoiceMaterial[] = []
  const writes: { path: string; body: string }[] = [],
    unexpected: string[] = []
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      current = url.pathname
    const reply = (value: unknown) => route.fulfill({ json: value })
    if (current === '/api/v1/session')
      return reply({
        ...session,
        role: options.role ?? 'owner',
        capabilities:
          options.role === 'viewer'
            ? ['facts.read']
            : options.role === 'reviewer'
              ? ['documents.process', 'review.source.read']
              : session.capabilities,
      })
    if (current === path) return reply(fact(version))
    if (current === `${path}/materials` && request.method() === 'GET')
      return reply({ invoice_id: invoiceID, version, items })
    if (current === `${path}/material-candidates`) {
      const start = Number(url.searchParams.get('cursor') || 0),
        query = url.searchParams.get('q') || ''
      const all = Array.from({ length: 201 }, (_, i) => material(i + 100)).filter((item) =>
        item.original_name.includes(query),
      )
      return reply({
        items: all.slice(start, start + 20),
        next_cursor: start + 20 < all.length ? String(start + 20) : '',
      })
    }
    if (current.startsWith(`${path}/materials`) && request.method() === 'POST') {
      writes.push({ path: current, body: request.postDataBuffer()?.toString('utf8') ?? '' })
      if (options.conflict && !failed) {
        failed = true
        version++
        return route.fulfill({
          status: 409,
          json: { error: { code: 'conflict', message: '发票版本已变化' } },
        })
      }
      if (options.uncertain && !failed) {
        failed = true
        return route.abort('failed')
      }
      const input = current.endsWith('/upload') ? undefined : request.postDataJSON()
      const item = current.endsWith('/remove')
        ? items[0]!
        : current.endsWith('/upload')
          ? material(81)
          : { ...material(101), document_id: input.document_id }
      items = current.endsWith('/remove') ? [] : [...items, item]
      version++
      return reply({
        invoice_id: invoiceID,
        link_id: item.id,
        document_id: item.document_id,
        version,
        replayed: false,
      })
    }
    if (/^\/api\/v1\/documents\/[^/]+\/content$/.test(current))
      return route.fulfill({ contentType: 'image/png', body: syntheticPNG })
    unexpected.push(current)
    return route.fulfill({
      status: 404,
      json: { error: { code: 'not_found', message: '合成请求不存在' } },
    })
  })
  await page.goto(`/invoices/${invoiceID}`)
  return { writes, unexpected }
}
async function chooseUpload(page: Page) {
  await page
    .getByLabel('选择图片或 PDF')
    .setInputFiles({ name: 'synthetic-selected.png', mimeType: 'image/png', buffer: syntheticPNG })
  await page.getByLabel('操作理由').fill('合成追加材料理由')
}
function formField(body: string, name: string): string {
  return body.match(new RegExp(`name="${name}"\\r\\n\\r\\n([^\\r]+)`))?.[1] ?? ''
}

test('材料上传、预览下载、明确解除与四尺寸双主题', async ({ page }, testInfo) => {
  const state = await fixture(page)
  await expect(page.getByRole('heading', { name: '辅助材料', exact: true })).toBeVisible()
  await chooseUpload(page)
  await page.getByRole('button', { name: '上传并关联', exact: true }).click()
  const panel = page.locator('.invoice-materials')
  await expect(panel.getByText('synthetic-material-81.png', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0]!.path).toBe(`${path}/materials/upload`)
  await expect(panel.getByRole('link', { name: '下载 synthetic-material-81.png' })).toHaveAttribute(
    'download',
    'synthetic-material-81.png',
  )
  await expect(panel.getByRole('link', { name: '打开 synthetic-material-81.png' })).toHaveAttribute(
    'href',
    `/api/v1/documents/${uuid(81)}/content`,
  )
  await page.getByLabel('操作理由').fill('合成解除理由')
  await panel.getByRole('button', { name: '解除关联 synthetic-material-81.png' }).click()
  await expect(panel.getByRole('button', { name: '确认解除', exact: true })).toBeDisabled()
  await panel.getByRole('button', { name: '取消解除' }).click()
  await expect(
    panel.getByRole('button', { name: '解除关联 synthetic-material-81.png' }),
  ).toBeFocused()
  await captureResponsiveReview(page, testInfo, 'invoice-materials')
  await panel.getByRole('button', { name: '解除关联 synthetic-material-81.png' }).click()
  await page.getByLabel('确认解除此辅助材料关联').check()
  await panel.getByRole('button', { name: '确认解除', exact: true }).click()
  await expect(panel.getByText('还没有辅助材料。票面原件仍在下方的来源区。')).toBeVisible()
  expect(state.writes).toHaveLength(2)
  expect(state.unexpected).toEqual([])
})

test('已有材料全部 201 条可翻页访问，并能按文件名筛选关联', async ({ page }) => {
  const state = await fixture(page)
  await page.getByRole('button', { name: '关联已有材料', exact: true }).click()
  await expect(page.getByRole('radio')).toHaveCount(20)
  for (let pageIndex = 1; pageIndex <= 10; pageIndex++) {
    await page.getByRole('button', { name: '加载更多材料' }).click()
    await expect(page.getByRole('radio')).toHaveCount(Math.min(201, (pageIndex + 1) * 20))
  }
  await expect(page.getByRole('button', { name: '加载更多材料' })).toHaveCount(0)
  await page.getByLabel('查找已有材料').fill('material-101.png')
  await page.getByRole('button', { name: '查找材料', exact: true }).click()
  await expect(page.getByRole('radio')).toHaveCount(1)
  await page.getByRole('radio', { name: 'synthetic-material-101.png' }).check()
  await page.getByLabel('操作理由').fill('合成关联理由')
  await page.getByRole('button', { name: '确认关联', exact: true }).click()
  await expect(
    page.locator('.material-list').getByText('synthetic-material-101.png', { exact: true }),
  ).toBeVisible()
  expect(JSON.parse(state.writes[0]!.body).document_id).toBe(uuid(101))
})

test('409 保留文件和理由，同时刷新正式字段后才允许重新核对', async ({ page }) => {
  const state = await fixture(page, { conflict: true })
  await chooseUpload(page)
  await page.getByRole('button', { name: '上传并关联', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('发票版本已变化')
  await expect(page.getByRole('button', { name: '上传并关联', exact: true })).toBeDisabled()
  const recheck = page.getByLabel('我已核对刷新后的材料和发票版本')
  await expect(recheck).toBeDisabled()
  await page.getByRole('button', { name: '刷新材料', exact: true }).click()
  await expect(page.getByText('合成已核对销售方', { exact: true })).toBeVisible()
  await expect(page.getByLabel('操作理由')).toHaveValue('合成追加材料理由')
  expect(
    await page
      .getByLabel('选择图片或 PDF')
      .evaluate((el) => (el as HTMLInputElement).files?.[0]?.name),
  ).toBe('synthetic-selected.png')
  await recheck.check()
  await page.getByRole('button', { name: '上传并关联', exact: true }).click()
  await expect(
    page.locator('.material-list').getByText('synthetic-material-81.png', { exact: true }),
  ).toBeVisible()
  expect(state.writes).toHaveLength(2)
  expect(formField(state.writes[0]!.body, 'expected_version')).toBe('1')
  expect(formField(state.writes[1]!.body, 'expected_version')).toBe('2')
})

test('未确认的网络结果保留同文件同请求幂等键', async ({ page }) => {
  const state = await fixture(page, { uncertain: true })
  await chooseUpload(page)
  await page.getByRole('button', { name: '上传并关联', exact: true }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByLabel('操作理由')).toHaveValue('合成追加材料理由')
  await page.getByRole('button', { name: '上传并关联', exact: true }).click()
  await expect(
    page.locator('.material-list').getByText('synthetic-material-81.png', { exact: true }),
  ).toBeVisible()
  const keys = state.writes.map((write) => formField(write.body, 'idempotency_key'))
  expect(keys).toHaveLength(2)
  expect(keys[0]).toMatch(/^[a-f0-9-]{36}$/)
  expect(keys[0]).toBe(keys[1])
})

test('Viewer 和 Reviewer 不读取或操作已确认发票辅助材料', async ({ page }) => {
  for (const role of ['viewer', 'reviewer'] as const) {
    const isolated = await page.context().newPage()
    let materialRequests = 0
    const listener = (request: { url(): string }) => {
      if (/\/materials|\/material-candidates/.test(request.url())) materialRequests++
    }
    isolated.on('request', listener)
    const state = await fixture(isolated, { role })
    await expect(isolated.getByRole('heading', { name: '当前正式字段', exact: true })).toBeVisible()
    await expect(isolated.locator('.invoice-materials')).toHaveCount(0)
    expect(materialRequests).toBe(0)
    expect(state.writes).toEqual([])
    isolated.off('request', listener)
    await isolated.close()
  }
})
