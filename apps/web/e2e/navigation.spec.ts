import { expect, test, type Page } from '@playwright/test'
import type { JobSummary, Session } from '../src/data/client'

const entries = [
  ['AI 收件箱', '/inbox'],
  ['支付管理', '/payments'],
  ['发票管理', '/invoices'],
  ['行程归属', '/trips'],
  ['报销管理', '/reimbursements'],
  ['数据洞察', '/insights'],
  ['邮箱来源', '/email-sources'],
  ['AI 配置', '/settings/ai'],
  ['成员管理', '/settings/members'],
  ['账号与密码', '/settings/account'],
] as const
const capabilities = [
  'documents.process',
  'facts.read',
  'reimbursements.read',
  'insights.read',
  'email_archive.read',
  'providers.manage',
  'members.manage',
  'allocations.manage',
]

test.use({ serviceWorkers: 'block' })

test.describe('全站导航：纯合成布局、权限与键盘验收', () => {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [384, 768, 1024, 1440]) {
      test(`${theme} ${width}px：顺序、对齐与文字导航`, async ({ page }, testInfo) => {
        const state = await mockNavigation(page)
        await page.setViewportSize({ width, height: 960 })
        await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
        await page.goto('/inbox')
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
        await expect(page.getByRole('heading', { level: 1, name: 'AI 收件箱' })).toBeVisible()
        const navigation = page.locator('#primary-navigation')
        if (width < 768) {
          const mainBefore = await page.locator('#main-content').boundingBox()
          const expand = page.getByRole('button', { name: '展开导航', exact: true })
          await expect(expand).toHaveAttribute('aria-controls', 'primary-navigation')
          await expect(expand).toHaveAttribute('aria-expanded', 'false')
          await expect(navigation).not.toBeVisible()
          await expand.focus()
          await page.keyboard.press('Enter')
          await expect(page.locator('.navigation-toggle')).toHaveAttribute('aria-expanded', 'true')
          const navigationBox = await navigation.boundingBox()
          const mainAfter = await page.locator('#main-content').boundingBox()
          expect(mainBefore).not.toBeNull()
          expect(mainAfter).not.toBeNull()
          expect(navigationBox).not.toBeNull()
          for (const dimension of ['x', 'y', 'width', 'height'] as const) {
            expect(Math.abs(mainAfter![dimension] - mainBefore![dimension])).toBeLessThanOrEqual(1)
          }
          expect(navigationBox!.x).toBe(0)
          expect(navigationBox!.width).toBe(Math.min(280, width - 48))
          expect(navigationBox!.width).toBeLessThan(width)
          const topbar = await page.locator('.topbar').boundingBox()
          expect(topbar).not.toBeNull()
          expect(navigationBox!.y).toBeGreaterThanOrEqual(topbar!.y + topbar!.height)
          expect(await navigation.evaluate((element) => getComputedStyle(element).position)).toBe(
            'fixed',
          )
          await expect(page.locator('dialog:modal')).toHaveCount(1)
          await expect(
            navigation.getByRole('button', { name: '关闭导航', exact: true }),
          ).toBeVisible()
          const first = navigation.getByRole('link', { name: 'AI 收件箱', exact: true })
          await first.focus()
          await page
            .locator('#main-content')
            .evaluate((element) => (element as HTMLElement).focus())
          await expect(first).toBeFocused()
          await page
            .locator('.navigation-toggle')
            .evaluate((element) => (element as HTMLElement).focus())
          await expect(first).toBeFocused()
        } else {
          await expect(page.getByRole('button', { name: '展开导航' })).not.toBeVisible()
          expect((await page.locator('.sidebar').boundingBox())?.width).toBe(
            width < 1024 ? 196 : 216,
          )
          expect(await navigation.evaluate((element) => getComputedStyle(element).position)).toBe(
            'fixed',
          )
          await expect(page.locator('dialog:modal')).toHaveCount(0)
        }
        await expect(navigation).toBeVisible()
        await expect(navigation.locator('.nav-group h2')).toHaveText([
          '工作台',
          '财务数据',
          '来源',
          '系统',
        ])
        await expect(navigation.locator('.nav-label')).toHaveText(entries.map(([label]) => label))
        for (const [label, path] of entries) {
          const link = navigation.getByRole('link', { name: label, exact: true })
          await expect(link).toBeVisible()
          await expect(link).toHaveAttribute('href', path)
          await expect(link.locator('.nav-label')).toBeVisible()
        }
        await assertAlignedRows(page)
        await assertNoOverflow(page)
        await page.screenshot({
          path: testInfo.outputPath(`navigation-${theme}-${width}.png`),
          fullPage: true,
          animations: 'disabled',
        })

        if (width < 768) {
          const first = navigation.getByRole('link', { name: 'AI 收件箱', exact: true })
          await first.focus()
          await page.keyboard.press('Tab')
          await expect(
            navigation.getByRole('link', { name: '支付管理', exact: true }),
          ).toBeFocused()
          await page.keyboard.press('Escape')
          const expand = page.getByRole('button', { name: '展开导航', exact: true })
          await expect(expand).toHaveAttribute('aria-expanded', 'false')
          await expect(expand).toBeFocused()
          await expect(navigation).not.toBeVisible()
          await expect(page.locator('dialog:modal')).toHaveCount(0)
          await page.keyboard.press('Enter')
          const backdropX = await navigation.evaluate((element) => {
            const right = element.getBoundingClientRect().right
            const contentRight = document.documentElement.getBoundingClientRect().right
            return right + (contentRight - right) / 2
          })
          await page.mouse.click(backdropX, 200)
          await expect(expand).toHaveAttribute('aria-expanded', 'false')
          await expect(expand).toBeFocused()
          await expect(navigation).not.toBeVisible()
          await expect(page.locator('dialog:modal')).toHaveCount(0)
          await page.keyboard.press('Enter')
          await navigation.getByRole('button', { name: '关闭导航', exact: true }).click()
          await expect(expand).toBeFocused()
          await expect(navigation).not.toBeVisible()
          await expect(page.locator('dialog:modal')).toHaveCount(0)
          await page.keyboard.press('Enter')
          const payments = navigation.getByRole('link', { name: '支付管理', exact: true })
          await payments.focus()
          await page.keyboard.press('Enter')
          await expect(page).toHaveURL(/\/payments$/)
          await expect(expand).toHaveAttribute('aria-expanded', 'false')
          await expect(navigation).not.toBeVisible()
          await expect(page.locator('dialog:modal')).toHaveCount(0)
          await expect(page.locator('#main-content')).toBeFocused()
          await assertNoOverflow(page)
        } else {
          const originalNavigationBox = await navigation.boundingBox()
          await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight))
          await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(0)
          await expect(navigation).toBeVisible()
          await expect(
            navigation.getByRole('link', { name: 'AI 收件箱', exact: true }),
          ).toBeInViewport()
          expect((await navigation.boundingBox())?.y).toBe(originalNavigationBox?.y)
          await navigation.getByRole('link', { name: '支付管理', exact: true }).click()
          await expect(page).toHaveURL(/\/payments$/)
          await expect(navigation).toBeVisible()
          await expect(
            navigation.getByRole('link', { name: '支付管理', exact: true }),
          ).toHaveAttribute('aria-current', 'page')
          expect((await navigation.boundingBox())?.width).toBe(width < 1024 ? 196 : 216)
          await expect(page.getByRole('button', { name: '展开导航' })).not.toBeVisible()
          await expect(page.locator('dialog:modal')).toHaveCount(0)
        }

        const themeToggle = page.getByRole('button', { name: /切换到[浅深]色模式/ })
        await themeToggle.focus()
        await expect(themeToggle).toBeFocused()
        await expect(themeToggle).toBeVisible()
        const logout = page.getByRole('button', { name: '退出', exact: true })
        await logout.focus()
        await expect(logout).toBeFocused()
        await expect(logout).toBeVisible()
        expect(state.unexpectedRequests).toEqual([])
        expect(state.pageErrors).toEqual([])
      })
    }
  }

  for (const width of [768, 1024, 1440]) {
    test(`${width}px：底部折叠按钮固定、图标导航可达并独立持久化`, async ({ page }, testInfo) => {
      const state = await mockNavigation(page)
      const height = 480
      const expandedWidth = width < 1024 ? 196 : 216
      await page.setViewportSize({ width, height })
      await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' })
      await page.goto('/inbox')
      await expect(page.getByRole('heading', { level: 1, name: 'AI 收件箱' })).toBeVisible()
      const navigation = page.locator('#primary-navigation')
      const links = navigation.locator('#sidebar-links')
      const footer = navigation.locator('.sidebar-footer')
      const collapse = footer.getByRole('button', { name: '收起侧栏', exact: true })
      await expect(page.locator('.app-frame')).toHaveAttribute('data-sidebar-collapsed', 'false')
      expect((await navigation.boundingBox())?.width).toBe(expandedWidth)
      await expect(collapse).toHaveAttribute('aria-controls', 'sidebar-links')
      await expect(collapse).toHaveAttribute('aria-expanded', 'true')
      await expect(collapse).toBeInViewport({ ratio: 1 })
      const buttonBox = await collapse.boundingBox()
      expect(buttonBox).not.toBeNull()
      expect(height - buttonBox!.y - buttonBox!.height).toBeGreaterThanOrEqual(0)
      expect(height - buttonBox!.y - buttonBox!.height).toBeLessThanOrEqual(24)
      const savedTheme = await page.evaluate(() => localStorage.getItem('sbm_theme'))

      await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight))
      await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(0)
      expect(await links.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(
        true,
      )
      await links.evaluate((element) => element.scrollTo(0, element.scrollHeight))
      await expect.poll(() => links.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
      await expect(collapse).toBeInViewport({ ratio: 1 })
      expect((await collapse.boundingBox())?.y).toBe(buttonBox!.y)

      await collapse.focus()
      await page.keyboard.press('Enter')
      const expand = footer.getByRole('button', { name: '展开侧栏', exact: true })
      await expect(expand).toHaveAttribute('aria-expanded', 'false')
      await expect(expand).toBeFocused()
      await expect(page.locator('.app-frame')).toHaveAttribute('data-sidebar-collapsed', 'true')
      expect((await navigation.boundingBox())?.width).toBe(64)
      await expect(navigation).toBeVisible()
      await expect(expand).toBeInViewport({ ratio: 1 })
      expect((await expand.boundingBox())?.y).toBe(buttonBox!.y)
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
      expect(await page.evaluate(() => localStorage.getItem('sbm_theme'))).toBe(savedTheme)
      expect(await page.evaluate(() => localStorage.getItem('sbm_sidebar_collapsed'))).toBe('true')
      for (const [label, path] of entries) {
        const link = navigation.getByRole('link', { name: label, exact: true })
        await expect(link).toHaveAttribute('href', path)
        await expect(link).toHaveAttribute('aria-label', label)
        await expect(link).toHaveAttribute('title', label)
        await expect(link.locator('svg')).toBeVisible()
        await expect(link.locator('.nav-label')).not.toBeVisible()
        await link.focus()
        await expect(link).toBeFocused()
      }
      await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
      expect((await navigation.boundingBox())?.width).toBe(64)
      expect(await page.evaluate(() => localStorage.getItem('sbm_sidebar_collapsed'))).toBe('true')
      await navigation.getByRole('link', { name: '支付管理', exact: true }).click()
      await expect(page).toHaveURL(/\/payments$/)
      await expect(navigation.getByRole('link', { name: '支付管理', exact: true })).toHaveAttribute(
        'aria-current',
        'page',
      )
      expect((await navigation.boundingBox())?.width).toBe(64)
      await page.reload()
      await expect(page.getByRole('heading', { level: 1, name: '支付管理' })).toBeVisible()
      await expect(expand).toHaveAttribute('aria-expanded', 'false')
      expect((await navigation.boundingBox())?.width).toBe(64)
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
      await assertNoOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`sidebar-collapsed-${width}.png`),
        fullPage: true,
        animations: 'disabled',
      })

      await expand.focus()
      await page.keyboard.press('Enter')
      await expect(collapse).toHaveAttribute('aria-expanded', 'true')
      await expect(collapse).toBeFocused()
      await expect(navigation).toHaveCSS('width', `${expandedWidth}px`)
      expect((await navigation.boundingBox())?.width).toBe(expandedWidth)
      expect((await collapse.boundingBox())?.y).toBe(buttonBox!.y)
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
      expect(await page.evaluate(() => localStorage.getItem('sbm_sidebar_collapsed'))).toBe('false')
      for (const label of await navigation.locator('.nav-label').all())
        await expect(label).toBeVisible()
      await page.reload()
      await expect(collapse).toHaveAttribute('aria-expanded', 'true')
      expect((await navigation.boundingBox())?.width).toBe(expandedWidth)
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
      await assertNoOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath(`sidebar-expanded-${width}.png`),
        fullPage: true,
        animations: 'disabled',
      })
      await page.getByRole('button', { name: '切换到浅色模式', exact: true }).click()
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
      expect((await navigation.boundingBox())?.width).toBe(expandedWidth)
      expect(await page.evaluate(() => localStorage.getItem('sbm_sidebar_collapsed'))).toBe('false')
      expect(state.unexpectedRequests).toEqual([])
      expect(state.pageErrors).toEqual([])
    })
  }

  test('手机抽屉保持窄幅，跨常驻断点后清除遮罩和背景禁用', async ({ page }) => {
    const state = await mockNavigation(page)
    await page.setViewportSize({ width: 320, height: 960 })
    await page.goto('/inbox')
    const navigation = page.locator('#primary-navigation')
    const main = page.locator('#main-content')
    await page.getByRole('button', { name: '展开导航', exact: true }).click()
    expect((await navigation.boundingBox())?.width).toBe(272)
    await expect(page.locator('dialog:modal')).toHaveCount(1)
    await assertNoOverflow(page)

    await page.setViewportSize({ width: 768, height: 960 })
    await expect(page.locator('aside#primary-navigation')).toBeVisible()
    expect((await navigation.boundingBox())?.width).toBe(196)
    await expect(page.locator('#primary-navigation[open]')).toHaveCount(0)
    await expect(page.locator('dialog:modal')).toHaveCount(0)
    await main.focus()
    await expect(main).toBeFocused()
    await expect(page.getByRole('button', { name: '展开导航' })).not.toBeVisible()

    await page.setViewportSize({ width: 767, height: 960 })
    await expect(page.locator('dialog#primary-navigation')).toBeAttached()
    const expand = page.getByRole('button', { name: '展开导航', exact: true })
    await expect(expand).toHaveAttribute('aria-expanded', 'false')
    await expect(navigation).not.toBeVisible()
    await expect(page.locator('dialog:modal')).toHaveCount(0)
    await main.focus()
    await expect(main).toBeFocused()
    await expand.click()
    expect((await navigation.boundingBox())?.width).toBe(280)
    await page.setViewportSize({ width: 1024, height: 960 })
    await expect(page.locator('aside#primary-navigation')).toBeVisible()
    expect((await navigation.boundingBox())?.width).toBe(216)
    await expect(page.locator('dialog:modal')).toHaveCount(0)
    await main.focus()
    await expect(main).toBeFocused()
    await assertNoOverflow(page)
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  const permissionCases = [
    { capability: 'documents.process', groups: ['工作台'], paths: ['/inbox'] },
    { capability: 'facts.read', groups: ['财务数据'], paths: ['/payments', '/invoices', '/trips'] },
    { capability: 'reimbursements.read', groups: ['财务数据'], paths: ['/reimbursements'] },
    { capability: 'insights.read', groups: ['财务数据'], paths: ['/insights'] },
    { capability: 'email_archive.read', groups: ['来源'], paths: ['/email-sources'] },
    { capability: 'providers.manage', groups: ['系统'], paths: ['/settings/ai'] },
    { capability: 'members.manage', groups: ['系统'], paths: ['/settings/members'] },
    { capability: '', groups: [], paths: [] },
  ]
  for (const { capability, groups, paths } of permissionCases) {
    test(`项目权限独立且无空组：${capability || '无导航权限'}`, async ({ page }) => {
      const state = await mockNavigation(page, capability ? [capability] : [])
      await page.goto('/inbox')
      await expect(page.getByRole('heading', { level: 1, name: 'AI 收件箱' })).toBeVisible()
      const navigation = page.locator('#primary-navigation')
      const expectedGroups = [...new Set([...groups, '系统'])]
      const expectedPaths = [...paths, '/settings/account']
      await expect(navigation.locator('.nav-group h2')).toHaveText(expectedGroups)
      await expect(navigation.locator('.nav-item')).toHaveCount(expectedPaths.length)
      expect(
        await navigation
          .locator('.nav-item')
          .evaluateAll((links) => links.map((link) => link.getAttribute('href'))),
      ).toEqual(expectedPaths)
      for (const group of await navigation.locator('.nav-group').all()) {
        expect(await group.locator('.nav-item').count()).toBeGreaterThan(0)
      }
      expect(state.unexpectedRequests).toEqual([])
      expect(state.pageErrors).toEqual([])
    })
  }

  for (const [path, selected] of [
    ['/reviews/synthetic-navigation-review', '/inbox'],
    ['/allocations/payment/00000000-0000-4000-8000-000000000001', '/payments'],
    ['/allocations/invoice/00000000-0000-4000-8000-000000000002', '/invoices'],
  ]) {
    test(`详情保留所属入口高亮：${path}`, async ({ page }) => {
      const state = await mockNavigation(page)
      await page.goto(path!)
      await expect(page.getByRole('alert')).toContainText(
        path!.startsWith('/reviews/')
          ? '该审核已结束或不存在，请返回收件箱查看最新状态。'
          : '合成导航验收记录不存在',
      )
      for (const width of [1440, 768, 384]) {
        await page.setViewportSize({ width, height: 960 })
        if (width < 768) await page.getByRole('button', { name: '展开导航', exact: true }).click()
        const current = page.locator('#primary-navigation .nav-item[aria-current="page"]')
        await expect(current).toHaveCount(1)
        await expect(current).toHaveAttribute('href', selected!)
        await expect(current).toBeVisible()
        await assertNoOverflow(page)
      }
      expect(state.unexpectedRequests).toEqual([])
      expect(state.pageErrors).toEqual([])
    })
  }
})

async function assertAlignedRows(page: Page) {
  const rows = await page.locator('#primary-navigation .nav-item').evaluateAll((links) =>
    links.map((link) => {
      const row = link.getBoundingClientRect()
      const icon = link.querySelector('.nav-icon')!.getBoundingClientRect()
      const label = link.querySelector('.nav-label')!.getBoundingClientRect()
      return {
        height: row.height,
        iconX: icon.x,
        iconWidth: icon.width,
        iconHeight: icon.height,
        labelX: label.x,
        centerDelta: Math.abs(icon.y + icon.height / 2 - label.y - label.height / 2),
      }
    }),
  )
  expect(rows).toHaveLength(entries.length)
  expect(rows[0]!.height).toBeGreaterThanOrEqual(40)
  for (const row of rows) {
    expect(Math.abs(row.height - rows[0]!.height)).toBeLessThanOrEqual(1)
    expect(Math.abs(row.iconX - rows[0]!.iconX)).toBeLessThanOrEqual(1)
    expect(Math.abs(row.labelX - rows[0]!.labelX)).toBeLessThanOrEqual(1)
    expect(row.iconWidth).toBe(rows[0]!.iconWidth)
    expect(row.iconHeight).toBe(rows[0]!.iconHeight)
    expect(row.centerDelta).toBeLessThanOrEqual(1)
  }
}

async function assertNoOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true)
}

async function mockNavigation(page: Page, allowed = capabilities) {
  const state = { unexpectedRequests: [] as string[], pageErrors: [] as string[] }
  const session: Session = {
    user: {
      id: '10000000-0000-4000-8000-000000000001',
      email: 'navigation@example.test',
      display_name: '合成导航用户',
    },
    tenant: {
      id: '10000000-0000-4000-8000-000000000002',
      name: '合成验收工作区',
      default_currency: 'CNY',
      timezone: 'Asia/Shanghai',
    },
    role: 'owner',
    capabilities: allowed,
    csrf_token: 'synthetic-navigation-csrf',
    expires_at: '2099-01-01T00:00:00Z',
  }
  page.on('pageerror', (error) => state.pageErrors.push(error.message))
  await page.route(
    (url) => url.pathname.startsWith('/api/v1/'),
    async (route) => {
      const { pathname } = new URL(route.request().url())
      const method = route.request().method()
      if (pathname === '/api/v1/session' && method === 'GET') {
        await route.fulfill({ json: session })
        return
      }
      if (pathname === '/api/v1/jobs' && method === 'GET') {
        const items: JobSummary[] = Array.from({ length: 20 }, (_, index) => ({
          id: `20000000-0000-4000-8000-${String(index + 1).padStart(12, '0')}`,
          document_id: `30000000-0000-4000-8000-${String(index + 1).padStart(12, '0')}`,
          original_name: `合成导航滚动单据-${index + 1}.png`,
          ingestion_kind: 'upload',
          detected_mime: 'image/png',
          status: 'completed',
          attempt_count: 1,
          created_at: '2026-09-04T08:00:00Z',
          version: 1,
        }))
        await route.fulfill({ json: { items } })
        return
      }
      if (pathname === '/api/v1/payments' && method === 'GET') {
        await route.fulfill({ json: { items: [], next_cursor: '' } })
        return
      }
      if (
        (pathname.startsWith('/api/v1/reviews/') || pathname.startsWith('/api/v1/allocations/')) &&
        method === 'GET'
      ) {
        await route.fulfill({
          status: 404,
          json: {
            error: { code: 'not_found', message: '合成导航验收记录不存在' },
            request_id: 'synthetic-navigation-request',
          },
        })
        return
      }
      state.unexpectedRequests.push(`${method} ${pathname}`)
      await route.abort('blockedbyclient')
    },
  )
  return state
}
