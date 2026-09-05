import { expect, test, type Page, type Route } from '@playwright/test'
import { captureResponsiveReview } from './visual-review'
import type {
  ExportManifest,
  ExportScope,
  ReimbursementDetail,
  Session,
  Trip,
} from '../src/data/client'

const tripID = '00000000-0000-4000-8000-000000008001'
const reimbursementID = '00000000-0000-4000-8000-000000008002'
const packageID = '00000000-0000-4000-8000-000000008003'
const trip: Trip = {
  bad_debt_locked: false,
  id: tripID,
  name: '合成材料交付行程',
  start_date: '2026-09-01',
  end_date: '2026-09-05',
  timezone: 'Asia/Shanghai',
  version: 1,
  origin_kind: 'manual',
  notes: '',
  created_at: '2026-09-01T00:00:00Z',
  assigned_payment_count: 201,
  assigned_invoice_count: 1,
  material_count: 2,
}
const session: Session = {
  user: { id: 'synthetic-user', email: 'synthetic@example.invalid', display_name: '合成用户' },
  tenant: {
    id: 'synthetic-tenant',
    name: '合成工作区',
    default_currency: 'CNY',
    timezone: 'Asia/Shanghai',
  },
  role: 'owner',
  capabilities: [
    'facts.read',
    'review.source.read',
    'reimbursements.read',
    'reimbursements.manage',
    'trip_assignments.manage',
  ],
  csrf_token: 'synthetic-ui-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}
const detail: ReimbursementDetail = {
  id: reimbursementID,
  trip: {
    id: tripID,
    name: trip.name,
    start_date: trip.start_date,
    end_date: trip.end_date,
    timezone: trip.timezone,
    version: 1,
  },
  trip_deleted: false,
  status: 'submitted',
  version: 1,
  item_count: 1,
  finding_count: 0,
  created_at: trip.created_at,
  updated_at: trip.created_at,
  materials_captured: false,
  material_count: null,
  rule_version: 'reimbursement-policy/1',
  snapshot_hash: 'c'.repeat(64),
  totals_by_currency: [{ currency: 'CNY', amount_minor: 10000 }],
  items: [],
  findings: [],
  decisions: [],
}

function manifest(scope: ExportScope, count = 2): ExportManifest {
  return {
    schema_version: 'material-delivery/1',
    scope,
    name: trip.name,
    version: 1,
    trip: detail.trip,
    snapshot_hash: scope.kind === 'reimbursement' ? detail.snapshot_hash : '',
    materials_captured: scope.kind === 'trip',
    warnings:
      scope.kind === 'trip'
        ? []
        : [
            '报销快照未捕获行程凭证集合，本包不包含 TripEvidence。',
            '此历史快照未捕获辅助材料集合；本包仅包含已知原件，不代表完整历史辅助材料。',
            '部分历史条目未捕获提交时的 Review 身份；清单保留 null，不以当前或初始 Review 回填。',
          ],
    references: Array.from({ length: count + 1 }, (_, index) => ({
      kind: index === count ? 'auxiliary' : 'original',
      relation_id: `synthetic-relation-${index}`,
      fact_type: 'invoice',
      fact_id: `synthetic-invoice-${index}`,
      fact_version: scope.kind === 'trip' ? 1 : null,
      review_decision_id: scope.kind === 'trip' ? 'synthetic-review' : null,
      display_name: '合成单据',
      business_date: '2026-09-01',
      amount_minor: 10000,
      currency: 'CNY',
      document_id: `synthetic-document-${index === count ? 0 : index}`,
    })),
    files: Array.from({ length: count }, (_, index) => ({
      document_id: `synthetic-document-${index}`,
      original_name: `合成辅助说明与票据-${index.toString().padStart(3, '0')}.png`,
      path: `materials/${index.toString().padStart(4, '0')}-synthetic.png`,
      mime: 'image/png',
      size_bytes: 1024,
      sha256: 'a'.repeat(64),
    })),
    source_bytes: count * 1024,
    manifest_hash: 'a'.repeat(64),
  }
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
function prepared(hash = 'a'.repeat(64)) {
  return {
    id: packageID,
    file_name: 'synthetic-materials.zip',
    size_bytes: 22,
    manifest_hash: hash,
    expires_at: '2099-01-01T00:05:00Z',
  }
}
type Hook = (route: Route, path: string, method: string) => Promise<boolean>

async function install(
  page: Page,
  options: {
    hook?: Hook
    count?: number
    identity?: Session
    downloadFailure?: { status: number; message: string }
  } = {},
) {
  // 新窗口的首个原生导航不归原页面路由管理；用同一 BrowserContext 覆盖附件。
  await page.context().route(`**/api/v1/material-exports/${packageID}/content`, async (route) => {
    expect(route.request().method()).toBe('GET')
    if (options.downloadFailure) {
      await json(
        route,
        { error: { code: 'export_unavailable', message: options.downloadFailure.message } },
        options.downloadFailure.status,
      )
      return
    }
    await route.fulfill({
      contentType: 'application/zip',
      headers: { 'Content-Disposition': 'attachment; filename="synthetic-materials.zip"' },
      body: Buffer.from('504b0506000000000000000000000000000000000000', 'hex'),
    })
  })
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const method = route.request().method()
    if (await options.hook?.(route, path, method)) return
    if (path === '/api/v1/session') return json(route, options.identity ?? session)
    if (path === '/api/v1/trips') return json(route, { items: [trip] })
    if (path.endsWith('/attribution-candidates'))
      return json(route, { trip, rule_version: 'trip-attribution/1', items: [] })
    if (path === '/api/v1/trip-evidence') return json(route, { items: [], next_cursor: '' })
    if (path === '/api/v1/reimbursements') return json(route, { items: [detail] })
    if (path === `/api/v1/reimbursements/${reimbursementID}`) return json(route, detail)
    if (path === '/api/v1/material-exports/preview')
      return json(route, manifest(route.request().postDataJSON() as ExportScope, options.count))
    if (path === '/api/v1/material-exports' && method === 'POST')
      return json(route, prepared(), 201)
    if (path === `/api/v1/material-exports/${packageID}` && method === 'DELETE')
      return route.fulfill({ status: 204 })
    if (path === `/api/v1/material-exports/${packageID}/content`) {
      expect(method).toBe('GET')
      return route.fulfill({
        contentType: 'application/zip',
        headers: { 'Content-Disposition': 'attachment; filename="synthetic-materials.zip"' },
        body: Buffer.from('504b0506000000000000000000000000000000000000', 'hex'),
      })
    }
    if (path === '/api/v1/jobs') return json(route, { items: [] })
    throw new Error(`Unexpected synthetic request: ${method} ${path}`)
  })
}

test('当前行程完整预览不受一页限制，共享材料只列一次并原生下载', async ({ page }, info) => {
  const scopes: ExportScope[] = []
  await install(page, {
    count: 201,
    hook: async (route, path) => {
      if (path === '/api/v1/material-exports/preview')
        scopes.push(route.request().postDataJSON() as ExportScope)
      return false
    },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料', exact: true }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await expect(panel).toContainText('202 个业务引用 · 201 份去重文件')
  await panel.locator('summary').click()
  await expect(panel.locator('.export-files li')).toHaveCount(201)
  await expect(panel.locator('.export-files li').last()).toContainText('200.png')
  expect(scopes).toEqual([{ kind: 'trip', id: tripID }])
  await captureResponsiveReview(page, info, 'current-trip-export')
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).focus()
  await page.keyboard.press('Enter')
  const link = panel.getByRole('link', { name: /^下载 ZIP/ })
  await expect(link).toBeFocused()
  const downloadPromise = page.waitForEvent('download')
  await page.keyboard.press('Enter')
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('synthetic-materials.zip')
  await download.saveAs(info.outputPath('synthetic-materials.zip'))
  await expect(panel.getByRole('button', { name: '重新预览材料' })).toBeFocused()
  await expect(panel).toContainText('是否保存成功以浏览器下载列表为准')
})

test('原生下载过期在独立错误页披露，原页不伪称保存成功', async ({ page }) => {
  await install(page, {
    downloadFailure: { status: 404, message: '材料包已过期，请重新预览准备' },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).click()
  const popupPromise = page.waitForEvent('popup')
  await panel.getByRole('link', { name: /^下载 ZIP/ }).click()
  const popup = await popupPromise
  await expect(popup.locator('body')).toContainText('材料包已过期')
  await expect(panel).toContainText('下载请求已交给浏览器')
  await expect(panel).toContainText('是否保存成功以浏览器下载列表为准')
  await popup.close()
})

test('历史报销显式披露未捕获集合并要求确认，不能替换为当前行程', async ({ page }, info) => {
  let selected: ExportScope | undefined
  await install(page, {
    hook: async (route, path) => {
      if (path === '/api/v1/material-exports') {
        const body = route.request().postDataJSON() as ExportScope & {
          acknowledged_warnings: boolean
        }
        expect(body.acknowledged_warnings).toBe(true)
        selected = { kind: body.kind, id: body.id }
      }
      return false
    },
  })
  await page.goto('/reimbursements')
  await page.getByRole('button', { name: '导出此报销快照' }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await expect(panel).toContainText('不代表完整历史辅助材料')
  await expect(panel).toContainText('清单保留 null')
  const prepare = panel.getByRole('button', { name: '确认清单并准备 ZIP' })
  await expect(prepare).toBeDisabled()
  await panel.getByRole('checkbox').check()
  await expect(prepare).toBeEnabled()
  await captureResponsiveReview(page, info, 'fixed-reimbursement-export')
  await prepare.click()
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toBeVisible()
  expect(selected).toEqual({ kind: 'reimbursement', id: reimbursementID })
  await panel.getByRole('button', { name: '关闭导出' }).click()
  await expect(page.getByRole('button', { name: '导出此报销快照' })).toBeFocused()
})

test('清单变化 409 和缺件不提供下载，保留范围重新核对', async ({ page }) => {
  let previews = 0,
    attempts = 0
  await install(page, {
    hook: async (route, path) => {
      if (path === '/api/v1/material-exports/preview') {
        previews++
        const value = manifest(route.request().postDataJSON() as ExportScope)
        value.manifest_hash = (previews > 1 ? 'b' : 'a').repeat(64)
        await json(route, value)
        return true
      }
      if (path === '/api/v1/material-exports') {
        attempts++
        const body = route.request().postDataJSON() as { expected_manifest_hash: string }
        expect(body.expected_manifest_hash).toBe((attempts === 1 ? 'a' : 'b').repeat(64))
        await json(
          route,
          {
            error: {
              code: attempts === 1 ? 'export_preview_stale' : 'export_object_unavailable',
              message:
                attempts === 1
                  ? '材料范围已变化，请重新预览并核对'
                  : '无法完整读取，请检查单据 synthetic-document-1',
            },
          },
          409,
        )
        return true
      }
      return false
    },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).click()
  await expect(panel.getByRole('alert')).toContainText('材料范围已变化')
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toHaveCount(0)
  await panel.getByRole('button', { name: '重新预览材料' }).click()
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).click()
  await expect(panel.getByRole('alert')).toContainText('synthetic-document-1')
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toHaveCount(0)
})

test('取消准备使迟到响应失效并恢复焦点', async ({ page }) => {
  let release!: () => void
  const waiting = new Promise<void>((resolve) => {
    release = resolve
  })
  let prepares = 0
  await install(page, {
    hook: async (route, path) => {
      if (path === '/api/v1/material-exports') {
        prepares++
        await waiting
        await json(route, prepared(), 201)
        return true
      }
      return false
    },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).click()
  await expect.poll(() => prepares).toBe(1)
  await panel.getByRole('button', { name: '取消准备' }).click()
  await expect(panel.getByRole('heading', { name: '导出当前行程材料' })).toBeFocused()
  release()
  await expect(panel).toContainText('已取消本次等待')
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toHaveCount(0)
})

test('取消已准备包时禁止并发领取，取消失败不伪成功', async ({ page }) => {
  let release!: () => void,
    cancellations = 0
  const waiting = new Promise<void>((resolve) => {
    release = resolve
  })
  await install(page, {
    hook: async (route, path, method) => {
      if (path === `/api/v1/material-exports/${packageID}` && method === 'DELETE') {
        cancellations++
        if (cancellations === 1) {
          await waiting
          await json(route, { error: { code: 'unavailable', message: '合成取消失败' } }, 503)
        } else await route.fulfill({ status: 204 })
        return true
      }
      return false
    },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  const panel = page.getByRole('region', { name: '材料包导出' })
  await panel.getByRole('button', { name: '确认清单并准备 ZIP' }).click()
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toBeVisible()
  await panel.getByRole('button', { name: '关闭导出' }).click()
  await expect.poll(() => cancellations).toBe(1)
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toHaveCount(0)
  await expect(panel.getByRole('button', { name: /^下载 ZIP/ })).toBeDisabled()
  release()
  await expect(panel.getByRole('alert')).toContainText('取消结果未确认')
  await expect(panel.getByRole('link', { name: /^下载 ZIP/ })).toBeVisible()
  await panel.getByRole('button', { name: '关闭导出' }).click()
  await expect(page.getByRole('button', { name: '导出当前行程材料' })).toBeFocused()
})

test('来源权限不完整时不展示或请求材料导出', async ({ browser }) => {
  for (const role of ['viewer', 'reviewer'] as const) {
    const page = await browser.newPage()
    let exports = 0
    const capabilities: Session['capabilities'] =
      role === 'viewer'
        ? ['facts.read', 'reimbursements.read']
        : ['review.source.read', 'claims.review']
    await install(page, {
      identity: { ...session, role, capabilities },
      hook: async (_route, path) => {
        if (path.includes('material-exports')) exports++
        return false
      },
    })
    await page.goto(role === 'viewer' ? '/trips' : '/reimbursements')
    await expect(page.getByRole('button', { name: /^导出(当前行程材料|此报销快照)$/ })).toHaveCount(
      0,
    )
    expect(exports).toBe(0)
    await page.close()
  }
})

test('切换行程取消旧清单，迟到结果不能覆盖新范围', async ({ page }) => {
  const otherID = '00000000-0000-4000-8000-000000008004'
  let release!: () => void,
    firstStarted = false
  const waiting = new Promise<void>((resolve) => {
    release = resolve
  })
  await install(page, {
    hook: async (route, path) => {
      if (path === '/api/v1/trips') {
        await json(route, { items: [trip, { ...trip, id: otherID, name: '合成第二行程' }] })
        return true
      }
      if (path === '/api/v1/material-exports/preview') {
        const scope = route.request().postDataJSON() as ExportScope
        if (scope.id === tripID) {
          firstStarted = true
          await waiting
        }
        const value = manifest(scope)
        value.name = scope.id === otherID ? '合成第二行程材料' : '迟到旧行程材料'
        await json(route, value)
        return true
      }
      return false
    },
  })
  await page.goto('/trips')
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  await expect.poll(() => firstStarted).toBe(true)
  await page.getByRole('button', { name: /合成第二行程/ }).click()
  await page.getByRole('button', { name: '导出当前行程材料' }).click()
  await expect(page.getByRole('region', { name: '材料包导出' })).toContainText('合成第二行程材料')
  release()
  await expect(page.getByText('迟到旧行程材料')).toHaveCount(0)
})
