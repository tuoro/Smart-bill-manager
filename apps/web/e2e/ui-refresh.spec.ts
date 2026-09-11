import { expect, test, type Locator, type Page, type TestInfo } from '@playwright/test'
import { resolve, sep } from 'node:path'
import type { JobSummary, ProviderConfig, Session } from '../src/data/client'
import { assertActionButtonContrast } from './visual-review'

const timestamp = '2026-09-04T08:00:00Z'
const syntheticKey = 'synthetic-ui-fixture-not-a-provider-key'
const widths = [384, 768, 1024, 1440]
const themes = ['light', 'dark'] as const

test.use({ serviceWorkers: 'block' })

test.describe('全站视觉：收件箱与 AI 配置纯合成隔离验收', () => {
  for (const theme of themes) {
    for (const width of widths) {
      test(`${theme} ${width}px：收件箱与 AI 配置空态和有数据布局`, async ({ page }, testInfo) => {
        const state = await mockWorkspace(page)
        await page.setViewportSize({ width, height: 960 })
        await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
        await page.goto('/inbox')
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
        await expect(page.getByRole('heading', { level: 1, name: 'AI 收件箱' })).toBeVisible()
        await expect(page.getByText('收件箱还是空的')).toBeVisible()
        await expect(page.getByRole('button', { name: '上传第一张单据' })).toBeVisible()
        await expect(page.locator('input[type="file"]')).toHaveCount(1)
        await assertNavigationIcons(page)
        await assertNoHorizontalOverflow(page)
        await assertActionButtonContrast(page, true)
        await screenshot(page, testInfo, 'inbox-empty')

        state.jobs = [job('needs_review', 1), job('processing', 2), job('failed', 3)]
        await page.reload()
        await expect(page.locator('.queue-table tbody tr')).toHaveCount(3)
        await expect(page.getByRole('link', { name: '审核', exact: true })).toBeVisible()
        await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
        await page.getByRole('button', { name: '已结束', exact: true }).click()
        await expect(page.getByText('当前筛选没有任务')).toBeVisible()
        await expect(page.getByRole('button', { name: '上传第一张单据' })).toHaveCount(0)
        await page.getByRole('button', { name: '全部', exact: true }).click()
        await expect(page.locator('.queue-table tbody tr')).toHaveCount(3)
        await assertNoHorizontalOverflow(page)
        await screenshot(page, testInfo, 'inbox-queue')

        await page.goto('/settings/ai')
        await expect(page.getByRole('heading', { level: 1, name: 'AI 配置' })).toBeVisible()
        await expect(page.getByText('连接你的第一个模型', { exact: true })).toBeVisible()
        await assertProviderForm(page)
        await assertNoHorizontalOverflow(page)
        await assertActionButtonContrast(page, true)
        await screenshot(page, testInfo, 'providers-empty')

        state.providers = [
          provider('pending', 1),
          provider('failed', 2),
          provider('passed', 3),
          provider('passed', 4, true),
        ]
        await page.reload()
        await expect(page.locator('.provider-list li')).toHaveCount(4)
        for (const [suffix, status] of [
          [1, '待检测'],
          [2, '检测失败'],
          [3, '检测通过'],
          [4, '使用中'],
        ] as const) {
          await expect(
            providerCard(page, suffix).locator('.status').filter({ hasText: status }),
          ).toBeVisible()
        }
        for (const suffix of [1, 2, 4]) {
          await expect(
            providerCard(page, suffix).getByRole('button', { name: '激活', exact: true }),
          ).toBeDisabled()
        }
        await expect(
          providerCard(page, 3).getByRole('button', { name: '激活', exact: true }),
        ).toBeEnabled()
        await assertProviderForm(page)
        await assertNoHorizontalOverflow(page)
        await assertActionButtonContrast(page, true)
        await screenshot(page, testInfo, 'providers-list')
        expect(state.unexpectedRequests).toEqual([])
        expect(state.pageErrors).toEqual([])
      })
    }
  }

  test('空态上传和筛选支持键盘并复用唯一文件输入', async ({ page }) => {
    const state = await mockWorkspace(page)
    await page.goto('/inbox')
    const upload = page.getByRole('button', { name: '上传第一张单据', exact: true })
    await upload.focus()
    await expect(upload).toBeFocused()
    await expect(upload).toBeEnabled()
    const [chooser] = await Promise.all([page.waitForEvent('filechooser'), upload.press('Enter')])
    expect(chooser.isMultiple()).toBe(true)
    const input = page.locator('input[type="file"]')
    expect(
      await chooser
        .element()
        .evaluate((element) => element === document.querySelector('input[type="file"]')),
    ).toBe(true)
    await expect(input).toHaveCount(1)
    const filter = page.getByRole('button', { name: '需处理', exact: true })
    await filter.focus()
    await page.keyboard.press('Enter')
    await expect(filter).toHaveAttribute('aria-pressed', 'true')
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  // 每 2.5 秒一次的静默轮询十几毫秒就结束；「正在同步」若每次都闪出来，
  // 会把旁边的「开始连续审核」按钮推得左右跳。只有刷新真的慢才显示。
  test('收件箱静默轮询不闪「正在同步」，慢刷新才显示', async ({ page }) => {
    const state = await mockWorkspace(page)
    state.jobs = [job('needs_review', 1)]
    await page.goto('/inbox')
    // 计数标签是被撑宽的那个元素，它一变宽旁边的按钮就会跳。
    const count = page.locator('.queue-count')
    await expect(count).toHaveText('1 个任务')
    const box = await count.boundingBox()
    // 覆盖两次轮询周期，期间提示不能出现、标签宽度不能变。
    const flashes = await page.evaluate(
      () =>
        new Promise<number>((resolve) => {
          let seen = 0
          const target = document.querySelector('.queue-count')!
          const observer = new MutationObserver(() => {
            if (target.textContent?.includes('正在同步')) seen += 1
          })
          observer.observe(target, { subtree: true, childList: true, characterData: true })
          setTimeout(() => {
            observer.disconnect()
            resolve(seen)
          }, 5600)
        }),
    )
    expect(flashes).toBe(0)
    expect(await count.boundingBox()).toEqual(box)

    state.jobsDelay = 1500
    await expect(page.getByText('正在同步')).toBeVisible({ timeout: 6000 })
    state.jobsDelay = 0
    await expect(page.getByText('正在同步')).toHaveCount(0)
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  test('AI 配置保存、检测、激活保持显式顺序并清空输入密钥', async ({ page }) => {
    const state = await mockWorkspace(page)
    await page.goto('/settings/ai')
    await assertProviderForm(page)
    await fillProviderForm(page)
    await page.getByRole('button', { name: '创建待检测配置' }).click()
    const card = providerCard(page, 1)
    await expect(card).toContainText('待检测')
    await expect(page.getByLabel('API Key')).toHaveValue('')
    await expect(card.getByRole('button', { name: '激活', exact: true })).toBeDisabled()
    expect(state.operations).toEqual(['create'])
    await card.getByRole('button', { name: '能力检测', exact: true }).click()
    await expect(card).toContainText('检测通过')
    await expect(card.getByRole('button', { name: '激活', exact: true })).toBeEnabled()
    expect(state.operations).toEqual(['create', 'detect'])
    await card.getByRole('button', { name: '激活', exact: true }).click()
    await expect(card).toContainText('使用中')
    await expect(card.getByRole('button', { name: '激活', exact: true })).toBeDisabled()
    expect(state.operations).toEqual(['create', 'detect', 'activate'])
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  test('AI 配置保存、检测和激活失败不伪装成功', async ({ page }) => {
    const state = await mockWorkspace(page)
    state.failOperation = 'create'
    await page.goto('/settings/ai')
    await fillProviderForm(page)
    await page.getByRole('button', { name: '创建待检测配置' }).click()
    await expect(page.getByRole('alert')).toContainText('合成保存失败')
    await expect(page.getByLabel('API Key')).toHaveValue('')
    await expect(page.locator('.provider-list li')).toHaveCount(0)

    state.failOperation = 'detect'
    state.providers = [provider('pending', 1)]
    await page.reload()
    const card = providerCard(page, 1)
    await card.getByRole('button', { name: '能力检测', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('合成检测失败')
    await expect(card).toContainText('待检测')
    await expect(card.getByRole('button', { name: '激活', exact: true })).toBeDisabled()

    state.failOperation = ''
    await card.getByRole('button', { name: '能力检测', exact: true }).click()
    await expect(card).toContainText('检测通过')
    state.failOperation = 'activate'
    await card.getByRole('button', { name: '激活', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('合成激活失败')
    await expect(card).not.toContainText('使用中')
    await expect(card.getByRole('button', { name: '激活', exact: true })).toBeEnabled()
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  // 编辑与新建共用一张表单；密钥留空表示沿用。编辑过的配置必须回到待检测、
  // 失去「使用中」——旧检测证明的是另一组参数。
  test('AI 配置编辑回填、留空密钥沿用、保存后回到待检测；删除需确认', async ({ page }) => {
    const state = await mockWorkspace(page)
    state.providers = [provider('passed', 1, true), provider('failed', 2)]
    await page.goto('/settings/ai')
    const first = providerCard(page, 1)
    await expect(first).toContainText('使用中')
    // 指纹只展示前缀，完整值在 title 里；长指纹不能把卡片撑破。
    await expect(first.locator('.provider-meta span[title]')).toHaveText('指纹 synthetic-fi')

    await first.getByRole('button', { name: '编辑', exact: true }).click()
    await expect(page.getByRole('heading', { name: '编辑配置' })).toBeVisible()
    await expect(page.getByLabel('Base URL')).toHaveValue(
      'https://synthetic-provider.example.test/v1',
    )
    await expect(page.getByLabel('Model')).toHaveValue('synthetic-model-1')
    await expect(page.getByLabel('API Key')).toHaveValue('')
    await expect(page.getByLabel('API Key')).not.toHaveAttribute('required', '')
    await page.getByLabel('Model').fill('synthetic-model-1-edited')
    await page.getByRole('button', { name: '保存修改', exact: true }).click()
    expect(state.updates).toEqual([
      {
        base_url: 'https://synthetic-provider.example.test/v1',
        api_key: '',
        model: 'synthetic-model-1-edited',
        output_mode: 'json_schema',
      },
    ])
    const edited = page.locator('.provider-list li').filter({ hasText: 'synthetic-model-1-edited' })
    await expect(edited).toContainText('待检测')
    await expect(edited).not.toContainText('使用中')
    await expect(edited).toContainText('版本 2')
    await expect(edited.getByRole('button', { name: '激活', exact: true })).toBeDisabled()
    // 表单回到新建态。
    await expect(page.getByRole('heading', { name: '添加配置' })).toBeVisible()
    await expect(page.getByLabel('Model')).toHaveValue('')

    // 取消编辑不发请求。
    const second = providerCard(page, 2)
    await second.getByRole('button', { name: '编辑', exact: true }).click()
    await expect(page.getByLabel('Model')).toHaveValue('synthetic-model-2')
    await page.getByRole('button', { name: '取消编辑', exact: true }).click()
    await expect(page.getByLabel('Model')).toHaveValue('')

    // 删除：取消确认则什么都不发；确认后从列表移除。
    page.once('dialog', (dialog) => void dialog.dismiss())
    await second.getByRole('button', { name: '删除', exact: true }).click()
    await expect(second).toBeVisible()
    expect(state.operations).toEqual(['update'])
    page.once('dialog', (dialog) => void dialog.accept())
    await second.getByRole('button', { name: '删除', exact: true }).click()
    await expect(second).toHaveCount(0)
    await expect(page.locator('.provider-list li')).toHaveCount(1)
    expect(state.operations).toEqual(['update', 'delete'])
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })

  test('配置列表加载失败不会被创建成功掩盖，显式重试成功后才清除', async ({ page }) => {
    const state = await mockWorkspace(page)
    state.failOperation = 'list'
    await page.goto('/settings/ai')
    const listError = page.getByRole('alert').filter({ hasText: '合成列表加载失败' })
    await expect(listError).toBeVisible()
    await expect(page.getByRole('button', { name: '重新加载', exact: true })).toBeVisible()

    await fillProviderForm(page)
    await page.getByRole('button', { name: '创建待检测配置' }).click()
    await expect(providerCard(page, 1)).toContainText('待检测')
    await expect(page.getByLabel('API Key')).toHaveValue('')
    await expect(listError).toBeVisible()
    await page.getByRole('button', { name: '重新加载', exact: true }).click()
    await expect(listError).toBeVisible()
    await expect(providerCard(page, 1)).toBeVisible()

    state.failOperation = ''
    await page.getByRole('button', { name: '重新加载', exact: true }).click()
    await expect(listError).toHaveCount(0)
    await expect(page.getByRole('button', { name: '重新加载', exact: true })).toHaveCount(0)
    await expect(providerCard(page, 1)).toBeVisible()
    expect(state.operations).toEqual(['create'])
    expect(state.unexpectedRequests).toEqual([])
    expect(state.pageErrors).toEqual([])
  })
})

async function assertNoHorizontalOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true)
}

async function assertNavigationIcons(page: Page) {
  const expand = page.getByRole('button', { name: '展开导航', exact: true })
  const collapsed = (page.viewportSize()?.width ?? 1440) < 768
  if (collapsed) await expect(expand).toBeVisible()
  else await expect(expand).not.toBeVisible()
  if (collapsed) await expand.click()
  await expect(page.locator('#primary-navigation')).toBeVisible()
  const links = page.locator('.sidebar .nav-item')
  expect(await links.count()).toBeGreaterThan(0)
  for (const link of await links.all()) {
    const icon = link.locator('svg')
    await expect(icon).toHaveCount(1)
    await expect(icon).toHaveText('')
    await expect(icon).toHaveAttribute('aria-hidden', 'true')
  }
  if (collapsed) {
    await expect(page.locator('dialog:modal')).toHaveCount(1)
    await page.keyboard.press('Escape')
    await expect(expand).toBeFocused()
    await expect(page.locator('#primary-navigation')).not.toBeVisible()
    await expect(page.locator('dialog:modal')).toHaveCount(0)
  }
}

async function assertProviderForm(page: Page) {
  const controls: Locator[] = [
    page.getByLabel('Base URL'),
    page.getByLabel('Model'),
    page.getByLabel('Output Mode'),
    page.getByLabel('API Key'),
  ]
  for (const control of controls) await expect(control).toBeVisible()
  await expect(controls[3]!).toHaveAttribute('type', 'password')
  const fields = page.locator('.provider-form-panel form').locator('input, select')
  await expect(fields).toHaveCount(4)
  expect(
    await fields.evaluateAll((elements) => elements.map((element) => element.tagName)),
  ).toEqual(['INPUT', 'INPUT', 'SELECT', 'INPUT'])
  await controls[0]!.focus()
  for (const control of controls.slice(1)) {
    await page.keyboard.press('Tab')
    await expect(control).toBeFocused()
  }
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: '创建待检测配置' })).toBeFocused()
}

async function fillProviderForm(page: Page) {
  await page.getByLabel('Base URL').fill('https://synthetic-provider.example.test/v1')
  await page.getByLabel('Model').fill('synthetic-model-1')
  await page.getByLabel('Output Mode').selectOption('json_object')
  await page.getByLabel('API Key').fill(syntheticKey)
}

async function screenshot(page: Page, testInfo: TestInfo, name: string) {
  const path = testInfo.outputPath(`${name}.png`)
  if (!resolve(path).startsWith(`${resolve(testInfo.project.outputDir)}${sep}`)) {
    throw new Error('UI screenshots must remain within the configured Playwright outputDir')
  }
  await page.screenshot({ path, fullPage: true, animations: 'disabled' })
}

function providerCard(page: Page, suffix: number) {
  return page.locator('.provider-list li').filter({ hasText: `synthetic-model-${suffix}` })
}

function provider(
  status: ProviderConfig['capability_status'],
  suffix: number,
  active = false,
): ProviderConfig {
  return {
    id: `00000000-0000-4000-8000-${String(suffix).padStart(12, '0')}`,
    base_url: 'https://synthetic-provider.example.test/v1',
    model: `synthetic-model-${suffix}`,
    output_mode: 'json_schema',
    capability_status: status,
    active,
    version: suffix,
    safe_fingerprint: 'synthetic-fingerprint',
    ...(status === 'failed'
      ? { capability_safe_message: '合成检测未通过，请核对服务地址与模型能力。' }
      : {}),
  }
}

function job(status: JobSummary['status'], suffix: number): JobSummary {
  return {
    id: `10000000-0000-4000-8000-${String(suffix).padStart(12, '0')}`,
    document_id: `20000000-0000-4000-8000-${String(suffix).padStart(12, '0')}`,
    original_name: `合成单据-${suffix}.png`,
    ingestion_kind: 'upload',
    detected_mime: 'image/png',
    status,
    attempt_count: 1,
    created_at: timestamp,
    version: 1,
    ...(status === 'failed' ? { safe_error_message: '合成提取失败，可重新处理。' } : {}),
  }
}

async function mockWorkspace(page: Page) {
  const state = {
    jobs: [] as JobSummary[],
    providers: [] as ProviderConfig[],
    operations: [] as string[],
    updates: [] as Record<string, unknown>[],
    jobsDelay: 0,
    failOperation: '',
    unexpectedRequests: [] as string[],
    pageErrors: [] as string[],
  }
  const session: Session = {
    user: {
      id: '30000000-0000-4000-8000-000000000001',
      email: 'owner@example.test',
      display_name: '合成用户',
    },
    tenant: {
      id: '30000000-0000-4000-8000-000000000002',
      name: '合成验收工作区',
      default_currency: 'CNY',
      timezone: 'Asia/Shanghai',
    },
    role: 'owner',
    capabilities: [
      'documents.process',
      'facts.read',
      'providers.manage',
      'email_archive.read',
      'reimbursements.read',
      'insights.read',
    ],
    csrf_token: 'synthetic-ui-csrf-token',
    expires_at: '2099-01-01T00:00:00Z',
  }
  page.on('pageerror', (error) => state.pageErrors.push(error.message))
  // 拦截全部 API 路径；未知请求不得落到用户正在运行的后端。
  await page.route(
    (url) => url.pathname.startsWith('/api/v1/'),
    async (route) => {
      const { pathname } = new URL(route.request().url())
      const method = route.request().method()
      if (pathname === '/api/v1/session' && method === 'GET') {
        await route.fulfill({
          json: session,
          headers: { 'Set-Cookie': `sbm_csrf=${session.csrf_token}; Path=/; SameSite=Strict` },
        })
        return
      }
      if (pathname === '/api/v1/jobs' && method === 'GET') {
        if (state.jobsDelay) await new Promise((resolve) => setTimeout(resolve, state.jobsDelay))
        await route.fulfill({ json: { items: state.jobs } })
        return
      }
      if (pathname === '/api/v1/provider-configs' && method === 'GET') {
        if (state.failOperation === 'list') {
          await route.fulfill({
            status: 503,
            json: {
              error: { code: 'synthetic_list_failure', message: '合成列表加载失败' },
              request_id: 'synthetic-ui-request',
            },
          })
          return
        }
        await route.fulfill({ json: { items: state.providers } })
        return
      }
      const single = /^\/api\/v1\/provider-configs\/([^/]+)$/.exec(pathname)
      if (single && (method === 'PUT' || method === 'DELETE')) {
        const target = state.providers.find((item) => item.id === single[1])
        if (!target) {
          await route.fulfill({
            status: 404,
            json: { error: { code: 'not_found', message: '不存在' } },
          })
          return
        }
        if (method === 'DELETE') {
          state.operations.push('delete')
          state.providers = state.providers.filter((item) => item !== target)
          await route.fulfill({ status: 204, body: '' })
          return
        }
        state.operations.push('update')
        state.updates.push(route.request().postDataJSON())
        const body = route.request().postDataJSON() as {
          base_url: string
          model: string
          output_mode: ProviderConfig['output_mode']
        }
        Object.assign(target, {
          base_url: body.base_url,
          model: body.model,
          output_mode: body.output_mode,
          capability_status: 'pending',
          active: false,
          version: target.version + 1,
          safe_fingerprint: 'synthetic-fingerprint-after-edit',
        })
        delete target.capability_safe_message
        await route.fulfill({ json: target })
        return
      }
      const action = /^\/api\/v1\/provider-configs\/([^/]+)\/(detect|activate)$/.exec(pathname)
      const operation = pathname === '/api/v1/provider-configs' ? 'create' : action?.[2]
      if (method === 'POST' && operation) {
        state.operations.push(operation)
        if (state.failOperation === operation) {
          const labels: Record<string, string> = {
            create: '保存',
            detect: '检测',
            activate: '激活',
          }
          await route.fulfill({
            status: 422,
            json: {
              error: { code: 'synthetic_failure', message: `合成${labels[operation]}失败` },
              request_id: 'synthetic-ui-request',
            },
          })
          return
        }
        if (operation === 'create') {
          expect(route.request().postDataJSON()).toEqual({
            base_url: 'https://synthetic-provider.example.test/v1',
            api_key: syntheticKey,
            model: 'synthetic-model-1',
            output_mode: 'json_object',
          })
          const created = { ...provider('pending', 1), output_mode: 'json_object' as const }
          state.providers.unshift(created)
          await route.fulfill({ status: 201, json: created })
          return
        }
        const current = state.providers.find((item) => item.id === action?.[1])
        if (current) {
          if (operation === 'detect') current.capability_status = 'passed'
          else {
            expect(current.capability_status).toBe('passed')
            state.providers.forEach((item) => {
              item.active = item.id === current.id
            })
          }
          await route.fulfill({ json: current })
          return
        }
      }
      state.unexpectedRequests.push(`${method} ${pathname}`)
      await route.abort('blockedbyclient')
    },
  )
  return state
}
