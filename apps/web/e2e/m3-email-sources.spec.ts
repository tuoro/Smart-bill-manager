import { expect, test, type Page, type Route } from '@playwright/test'
import type { EmailMessage, EmailSource, Session } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const timestamp = '2026-08-31T08:00:00Z'
const sourcePending = emailSource(
  '00000000-0000-4000-8000-000000000801',
  '待连接邮箱',
  'pending_connection',
)
const sourceActive = {
  ...emailSource('00000000-0000-4000-8000-000000000802', '财务归档邮箱', 'active'),
  last_archived_at: timestamp,
  message_count: 2,
  attachment_count: 3,
  blocked_count: 1,
} satisfies EmailSource

test.describe('M3 邮箱来源真实组件状态矩阵', () => {
  test('Owner：带密码登记、混合附件、blocked、分页失败恢复与响应式可达', async ({
    page,
  }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, ownerSession())
    let sources = [sourcePending, sourceActive]
    let submitted: Record<string, unknown> | undefined
    let idempotencyKey = ''
    let failNextPage = true
    await page.route(emailSourcesURL, async (route) => {
      if (route.request().method() === 'GET') {
        await fulfillJSON(route, { items: sources })
        return
      }
      submitted = route.request().postDataJSON() as Record<string, unknown>
      idempotencyKey = route.request().headers()['idempotency-key'] ?? ''
      const created = emailSource(
        '00000000-0000-4000-8000-000000000803',
        String(submitted.display_name),
        'pending_connection',
      )
      sources = [...sources, created]
      await fulfillJSON(route, created, 201)
    })
    await page.route(emailMessagesURL(sourcePending.id), (route) =>
      fulfillJSON(route, { items: [] }),
    )
    await page.route(emailMessagesURL(sourceActive.id), async (route) => {
      const cursor = new URL(route.request().url()).searchParams.get('cursor')
      if (!cursor) {
        await fulfillJSON(route, { items: [archivedMessage()], next_cursor: 'next-email-page' })
        return
      }
      if (failNextPage) {
        failNextPage = false
        await fulfillError(route, 503, 'unavailable', '分页暂时不可用')
        return
      }
      await fulfillJSON(route, { items: [blockedMessage()] })
    })
    await page.route(emailMessagesURL('00000000-0000-4000-8000-000000000803'), (route) =>
      fulfillJSON(route, { items: [] }),
    )

    await page.goto('/email-sources')
    await expect(page.getByRole('heading', { name: '邮箱来源', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /待连接邮箱 待检测/ })).toBeVisible()
    await expect(page.getByText('这个邮箱还没有同步到邮件')).toBeVisible()
    await expect(page.getByRole('button', { name: /财务归档邮箱/ })).toBeVisible()
    await page.getByRole('button', { name: /财务归档邮箱/ }).click()

    const messageCard = page.locator('.email-message-card').filter({ hasText: '合成 <script>' })
    await expect(messageCard).toBeVisible()
    await expect(messageCard.locator('.status').filter({ hasText: '已归档' })).toBeVisible()
    await expect(messageCard.locator('.status').filter({ hasText: '已入队' })).toBeVisible()
    await expect(messageCard.locator('.status').filter({ hasText: '已存在' })).toBeVisible()
    await expect(messageCard.locator('.status').filter({ hasText: '仅归档' })).toBeVisible()
    await expect(messageCard.getByText('文件类型不支持处理，仅保留归档')).toBeVisible()
    await expect(messageCard.getByRole('link', { name: '下载原始邮件' })).toHaveAttribute(
      'download',
      '',
    )
    await expect(messageCard.getByRole('link', { name: '下载附件' }).first()).toHaveAttribute(
      'download',
      '',
    )
    await expect(page.getByText('private body marker')).toHaveCount(0)
    await expect(page.locator('script').filter({ hasText: 'unsafe()' })).toHaveCount(0)

    const more = page.getByRole('button', { name: '加载更多邮件' })
    await more.click()
    await expect(page.getByRole('alert')).toContainText('分页暂时不可用')
    await expect(messageCard).toBeVisible()
    pageErrors.length = 0
    await more.click()
    await expect(page.locator('.status').filter({ hasText: '已阻断' })).toBeVisible()
    await expect(page.getByText('邮件 MIME 嵌套超过 10 层')).toBeVisible()
    await expect(more).toHaveCount(0)
    await captureResponsiveReview(page, testInfo, 'email-sources')

    for (const width of [768, 384]) {
      await page.setViewportSize({ width, height: 1000 })
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
        ),
      ).toBe(true)
      const rawDownload = page.getByRole('link', { name: '下载原始邮件' }).first()
      await rawDownload.focus()
      await expect(rawDownload).toBeFocused()
      await expect(rawDownload).toBeVisible()
    }

    await page.evaluate(() => {
      document.cookie = 'sbm_csrf=synthetic-email-csrf; path=/; SameSite=Strict'
    })
    await page.getByRole('button', { name: '登记邮箱来源' }).click()
    await page.getByLabel('显示名称').fill('新增邮箱')
    await page.getByLabel('邮箱地址').fill('new@example.invalid')
    await page.getByLabel('邮箱服务器（IMAP）').fill('imap.example.invalid')
    await page.getByLabel('IMAP 端口').fill('993')
    await page.getByLabel('加密方式').selectOption('implicit_tls')
    await page.getByLabel('密码或授权码').fill('synthetic-app-password')
    await page.getByRole('button', { name: '保存邮箱来源' }).click()
    await expect(page.getByText('新增邮箱').first()).toBeVisible()
    expect(submitted).toEqual({
      display_name: '新增邮箱',
      mailbox_address: 'new@example.invalid',
      imap_host: 'imap.example.invalid',
      imap_port: 993,
      transport_security: 'implicit_tls',
      imap_username: '',
      imap_password: 'synthetic-app-password',
    })
    expect(idempotencyKey).toMatch(/^[0-9a-f-]{36}$/)
    // 密码只上送一次：提交后输入框清空，页面任何地方不出现明文。
    await expect(page.locator('body')).not.toContainText('synthetic-app-password')
    expect(pageErrors).toEqual([])
  })

  // 连接动作：密码错 → 检测失败且不能开同步；换密码回到待检测；通过后开同步、立即同步；删除需确认。
  test('Owner：检测、换密码、开启同步、立即同步与删除', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, ownerSession())
    let current: EmailSource = { ...sourcePending, has_password: true }
    const actions: string[] = []
    let credentials: Record<string, unknown> | undefined
    await page.route(emailSourcesURL, (route) =>
      fulfillJSON(route, { items: current ? [current] : [] }),
    )
    await page.route(emailMessagesURL(sourcePending.id), (route) =>
      fulfillJSON(route, { items: [] }),
    )
    await page.route(
      (url) => url.pathname.startsWith(`/api/v1/email-sources/${sourcePending.id}`),
      async (route) => {
        const url = new URL(route.request().url())
        const method = route.request().method()
        if (url.pathname.endsWith('/messages')) return route.fallback()
        if (method === 'DELETE') {
          actions.push('delete')
          await route.fulfill({ status: 204, body: '' })
          return
        }
        if (url.pathname.endsWith('/credentials')) {
          credentials = route.request().postDataJSON() as Record<string, unknown>
          current = {
            ...current,
            connection_status: 'pending',
            connection_message: '',
            sync_enabled: false,
          }
          await fulfillJSON(route, current)
          return
        }
        const action = url.pathname.split('/').pop() ?? ''
        actions.push(action)
        if (action === 'detect')
          current =
            actions.filter((item) => item === 'detect').length === 1
              ? {
                  ...current,
                  connection_status: 'failed',
                  connection_message: '邮箱拒绝了登录，请检查用户名与密码',
                }
              : {
                  ...current,
                  connection_status: 'passed',
                  connection_message: '连接成功，收件箱可读',
                }
        if (action === 'activate') current = { ...current, sync_enabled: true }
        if (action === 'deactivate') current = { ...current, sync_enabled: false }
        if (action === 'sync')
          current = {
            ...current,
            last_sync_at: timestamp,
            last_sync_message: '已同步 2 封新邮件',
            message_count: 2,
          }
        await fulfillJSON(route, current)
      },
    )
    await page.goto('/email-sources')
    await page.evaluate(() => {
      document.cookie = 'sbm_csrf=synthetic-email-csrf; path=/; SameSite=Strict'
    })
    const activate = page.getByRole('button', { name: '开启同步', exact: true })
    await expect(activate).toBeDisabled()
    await page.getByRole('button', { name: '检测连接', exact: true }).click()
    await expect(page.getByText('连接失败 · 邮箱拒绝了登录')).toBeVisible()
    await expect(activate).toBeDisabled()

    await page.getByRole('button', { name: '更换密码', exact: true }).click()
    await page.getByLabel('新密码或授权码').fill('new-app-password')
    await page.getByRole('button', { name: '保存密码', exact: true }).click()
    expect(credentials).toEqual({ imap_username: '', imap_password: 'new-app-password' })
    await expect(page.locator('body')).not.toContainText('new-app-password')
    await expect(page.getByText('待检测', { exact: true }).first()).toBeVisible()

    await page.getByRole('button', { name: '检测连接', exact: true }).click()
    await expect(page.getByText('连接正常 · 连接成功')).toBeVisible()
    await expect(activate).toBeEnabled()
    await activate.click()
    await expect(page.getByText('每 5 分钟自动同步')).toBeVisible()
    await page.getByRole('button', { name: '立即同步', exact: true }).click()
    await expect(page.getByText('已同步 2 封新邮件')).toBeVisible()
    await page.getByRole('button', { name: '停止同步', exact: true }).click()
    await expect(activate).toBeVisible()
    expect(actions).toEqual(['detect', 'detect', 'activate', 'sync', 'deactivate'])

    page.once('dialog', (dialog) => void dialog.dismiss())
    await page.getByRole('button', { name: '删除邮箱', exact: true }).click()
    expect(actions).not.toContain('delete')
    page.once('dialog', (dialog) => void dialog.accept())
    await page.getByRole('button', { name: '删除邮箱', exact: true }).click()
    await expect(page.getByText('还没有邮箱来源')).toBeVisible()
    expect(actions).toContain('delete')
    expect(pageErrors).toEqual([])
  })

  test('缺少管理能力的会话可读不可登记，缺少读取能力的会话不发起归档请求', async ({ page }) => {
    let sourceRequests = 0
    await mockSession(page, financeSession())
    await page.route(emailSourcesURL, async (route) => {
      sourceRequests += 1
      await fulfillJSON(route, { items: [sourcePending] })
    })
    await page.route(emailMessagesURL(sourcePending.id), (route) =>
      fulfillJSON(route, { items: [] }),
    )
    await page.goto('/email-sources')
    await expect(page.getByRole('button', { name: /待连接邮箱 待检测/ })).toBeVisible()
    await expect(page.getByRole('button', { name: '登记邮箱来源' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '检测连接' })).toHaveCount(0)
    expect(sourceRequests).toBe(1)

    const reviewerPage = await page.context().newPage()
    await mockSession(reviewerPage, reviewerSession())
    let forbiddenRequests = 0
    await reviewerPage.route(emailSourcesURL, async (route) => {
      forbiddenRequests += 1
      await fulfillJSON(route, { items: [] })
    })
    await reviewerPage.goto('/email-sources')
    await expect(reviewerPage.getByRole('alert')).toContainText('没有读取邮箱归档的权限')
    await expect(reviewerPage.getByText('来源列表')).toHaveCount(0)
    expect(forbiddenRequests).toBe(0)
    await reviewerPage.close()
  })

  test('来源加载、失败、重试空状态与离线状态彼此明确', async ({ context, page }) => {
    await mockSession(page, ownerSession())
    let release = () => {}
    const heldResponse = new Promise<void>((resolve) => {
      release = resolve
    })
    let attempts = 0
    await page.route(emailSourcesURL, async (route) => {
      attempts += 1
      if (attempts === 1) {
        await heldResponse
        await fulfillError(route, 503, 'unavailable', '邮箱来源暂时不可用')
        return
      }
      await fulfillJSON(route, { items: [] })
    })
    await page.goto('/email-sources')
    await expect(page.getByRole('status')).toContainText('正在读取邮箱来源')
    release()
    await expect(page.getByRole('alert')).toContainText('邮箱来源暂时不可用')
    await page.getByRole('button', { name: '重试' }).click()
    await expect(page.getByText('还没有邮箱来源')).toBeVisible()

    await context.setOffline(true)
    await expect(page.getByRole('status')).toContainText('当前离线')
    await expect(page.getByRole('button', { name: '登记邮箱来源' })).toBeDisabled()
    await context.setOffline(false)
    await expect(page.getByText('当前离线')).toHaveCount(0)
  })
})

function emailSource(id: string, displayName: string, status: EmailSource['status']): EmailSource {
  return {
    id,
    display_name: displayName,
    mailbox_address: `${id.endsWith('1') ? 'pending' : 'finance'}@example.invalid`,
    imap_host: 'imap.example.invalid',
    imap_port: 993,
    transport_security: 'implicit_tls',
    status,
    created_by_user_id: '00000000-0000-4000-8000-000000000101',
    created_at: timestamp,
    version: status === 'active' ? 2 : 1,
    message_count: 0,
    attachment_count: 0,
    blocked_count: 0,
    imap_username: '',
    has_password: true,
    connection_status: status === 'active' ? 'passed' : 'pending',
    connection_message: status === 'active' ? '连接成功，收件箱可读' : '',
    sync_enabled: status === 'active',
    last_sync_message: '',
  }
}

function archivedMessage(): EmailMessage {
  return {
    id: '00000000-0000-4000-8000-000000000811',
    email_source_id: sourceActive.id,
    subject: '合成 <script>unsafe()</script> 邮件',
    sender_address: 'sender@example.invalid',
    sent_at: timestamp,
    received_at: timestamp,
    status: 'archived',
    created_at: timestamp,
    attachments: [
      {
        id: '00000000-0000-4000-8000-000000000821',
        part_index: 1,
        original_name: 'invoice.png',
        declared_mime: 'image/png',
        disposition: 'attachment',
        size_bytes: 1024,
        processing_status: 'queued',
        document_id: '00000000-0000-4000-8000-000000000831',
        job_id: '00000000-0000-4000-8000-000000000841',
      },
      {
        id: '00000000-0000-4000-8000-000000000822',
        part_index: 2,
        original_name: 'existing.pdf',
        declared_mime: 'application/pdf',
        disposition: 'inline',
        size_bytes: 2048,
        processing_status: 'existing_document',
        document_id: '00000000-0000-4000-8000-000000000832',
        job_id: '00000000-0000-4000-8000-000000000842',
      },
      {
        id: '00000000-0000-4000-8000-000000000823',
        part_index: 3,
        original_name: 'notes.txt',
        declared_mime: 'text/plain',
        disposition: 'attachment',
        size_bytes: 128,
        processing_status: 'archived_only',
        safe_reason_code: 'unsupported_attachment_type',
      },
    ],
  }
}

function blockedMessage(): EmailMessage {
  return {
    id: '00000000-0000-4000-8000-000000000812',
    email_source_id: sourceActive.id,
    subject: '结构超限邮件',
    sender_address: 'sender@example.invalid',
    received_at: '2026-08-31T09:00:00Z',
    status: 'blocked',
    safe_error_code: 'email_mime_depth_exceeded',
    safe_error_text: '邮件 MIME 嵌套超过 10 层',
    created_at: '2026-08-31T09:00:00Z',
    attachments: [],
  }
}

function ownerSession(): Session {
  return session('owner', [
    'documents.process',
    'facts.read',
    'email_archive.read',
    'email_sources.manage',
  ])
}

function financeSession(): Session {
  return session('member', ['documents.process', 'facts.read', 'email_archive.read'])
}

function reviewerSession(): Session {
  return session('member', ['documents.process', 'claims.review'])
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

const emailSourcesURL = (url: URL) => url.pathname === '/api/v1/email-sources'

function emailMessagesURL(sourceID: string) {
  return (url: URL) => url.pathname === `/api/v1/email-sources/${sourceID}/messages`
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
