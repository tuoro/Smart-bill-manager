import { test, expect, type Page } from '@playwright/test'
import type { Invitation, Member, Session } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`
const timestamp = '2026-09-05T00:00:00Z'
const mockCode = 'A'.repeat(43) // 仅 Mock API 占位，不对应任何数据库邀请。
const session: Session = {
  user: { id: id(1), email: 'owner@example.invalid', display_name: '合成管理员' },
  tenant: { id: id(900), name: '合成团队', default_currency: 'CNY', timezone: 'UTC' },
  role: 'owner',
  capabilities: ['members.manage', 'facts.read'],
  csrf_token: 'synthetic-browser-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}
const member = (n: number): Member => ({
  user_id: id(n),
  email: `member-${n}@example.invalid`,
  display_name: `合成成员 ${n}`,
  role: n === 1 ? 'owner' : 'viewer',
  status: 'active',
  version: 1,
  created_at: timestamp,
})
const invitation = (n: number): Invitation => ({
  id: id(n + 1000),
  email: `invite-${n}@example.invalid`,
  role: 'viewer',
  status: 'pending',
  version: 1,
  created_at: timestamp,
  expires_at: '2099-01-01T00:00:00Z',
})

async function fixture(
  page: Page,
  options: {
    role?: Session['role']
    total?: number
    conflict?: boolean
    uncertain?: boolean
    public?: boolean
    existing?: boolean
  } = {},
) {
  const state = {
    authenticated: !options.public,
    memberConflict: !!options.conflict,
    failNextPage: false,
    inviteUncertain: !!options.uncertain,
    existing: !!options.existing,
    members: Array.from({ length: options.total ?? 202 }, (_, n) => member(n + 1)),
    invitations: Array.from({ length: options.total ?? 201 }, (_, n) => invitation(n + 1)),
    writes: [] as { path: string; body: Record<string, unknown> }[],
    unexpected: [] as string[],
    errors: [] as string[],
    memberReads: 0,
    sessionReads: 0,
  }
  page.on('pageerror', (error) => state.errors.push(error.name))
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname,
      method = req.method()
    const reply = (json: unknown) => route.fulfill({ json })
    const failure = (status: number, code: string, message: string) =>
      route.fulfill({ status, json: { error: { code, message } } })
    if (path === '/api/v1/session') {
      if (method === 'DELETE') {
        state.authenticated = false
        return failure(401, 'unauthenticated', '会话已失效')
      }
      state.sessionReads++
      return state.authenticated
        ? reply({
            ...session,
            role: options.role ?? 'owner',
            capabilities:
              options.role && options.role !== 'owner' ? ['facts.read'] : session.capabilities,
          })
        : failure(401, 'unauthenticated', '会话已失效')
    }
    if (method !== 'GET')
      state.writes.push({ path, body: req.postDataJSON() as Record<string, unknown> })
    if (path === '/api/v1/session/login') {
      if (!req.postDataJSON().tenant_id) return failure(409, 'tenant_required', '请选择工作区')
      state.authenticated = true
      return reply(session)
    }
    if (path === '/api/v1/session/workspaces')
      return reply({
        items: [
          { id: id(900), name: '合成团队', role: 'owner' },
          { id: id(901), name: '合成第二团队', role: 'viewer' },
        ],
      })
    if (path === '/api/v1/invitations/check')
      return reply({
        email: 'joined@example.invalid',
        tenant_name: '合成团队',
        role: 'reviewer',
        expires_at: '2099-01-01T00:00:00Z',
        existing_account: state.existing,
      })
    if (path === '/api/v1/invitations/accept') {
      if (req.postDataJSON().password === 'synthetic-wrong-password')
        return failure(401, 'invalid_credentials', '邮箱或密码不正确')
      return route.fulfill({ status: 204 })
    }
    if (path === '/api/v1/account/password') {
      if (req.postDataJSON().current_password === 'synthetic-wrong-password')
        return failure(401, 'invalid_credentials', '当前密码不正确')
      state.authenticated = false
      return route.fulfill({ status: 204 })
    }
    if (path === '/api/v1/members' && method === 'GET') {
      state.memberReads++
      if (!state.authenticated) return failure(401, 'unauthenticated', '会话已失效')
      const start = Number(url.searchParams.get('cursor') || 0)
      if (start && state.failNextPage) {
        state.failNextPage = false
        return failure(503, 'unavailable', '合成分页失败')
      }
      return reply({
        items: state.members.slice(start, start + 20),
        next_cursor: start + 20 < state.members.length ? String(start + 20) : '',
      })
    }
    if (path.startsWith('/api/v1/members/')) {
      const value = state.members.find((item) => item.user_id === path.split('/').at(-1))
      if (!value) return failure(404, 'not_found', '成员不存在')
      if (method === 'GET') return reply(value)
      if (state.memberConflict) {
        state.memberConflict = false
        value.version++
        state.members = [...Array.from({ length: 20 }, (_, n) => member(500 + n)), ...state.members]
        return failure(409, 'version_conflict', '成员版本已变化')
      }
      Object.assign(value, {
        role: req.postDataJSON().role,
        status: req.postDataJSON().status,
        version: value.version + 1,
      })
      return reply(value)
    }
    if (path === '/api/v1/member-invitations') {
      if (method === 'GET') {
        const start = Number(url.searchParams.get('cursor') || 0)
        return reply({
          items: state.invitations.slice(start, start + 20),
          next_cursor: start + 20 < state.invitations.length ? String(start + 20) : '',
        })
      }
      if (state.inviteUncertain) {
        state.inviteUncertain = false
        return route.abort('failed')
      }
      const value = {
        ...invitation(3000 + state.writes.length),
        email: req.postDataJSON().email,
        role: req.postDataJSON().role,
      }
      state.invitations.unshift(value)
      return reply({
        invitation: value,
        code: options.uncertain ? '' : mockCode,
        replayed: !!options.uncertain,
      })
    }
    if (path.startsWith('/api/v1/member-invitations/')) {
      const value = state.invitations.find((item) => item.id === path.split('/')[4])
      if (!value) return failure(404, 'not_found', '邀请不存在')
      if (method === 'GET') return reply(value)
      value.version = 2
      value.status = 'revoked'
      return reply(value)
    }
    state.unexpected.push(`${method} ${path}`)
    return failure(418, 'unexpected_mock_request', '未声明的合成请求')
  })
  return state
}

async function fillInvite(page: Page) {
  await page.getByLabel('受邀邮箱').fill('new@example.invalid')
  await page.getByLabel('邀请理由').fill('合成团队加入')
  await page.getByRole('button', { name: '创建邀请', exact: true }).click()
}

test('邀请只显示一次；新建和无关撤销不丢失当前代码，复制失败可手工保存', async ({ page }) => {
  const state = await fixture(page, { total: 2 })
  await page.goto('/settings/members')
  await fillInvite(page)
  await expect(page.getByLabel('一次性邀请代码')).toHaveValue(mockCode)
  expect((await page.locator('.invitation-code > p').boundingBox())!.width).toBeGreaterThan(200)
  await expect(page.getByRole('button', { name: '创建邀请', exact: true })).toBeDisabled()
  await page.evaluate(() =>
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async () => {
          throw new Error('synthetic clipboard denied')
        },
      },
      configurable: true,
    }),
  )
  await page.getByRole('button', { name: '复制邀请代码' }).click()
  await expect(page.getByText('无法自动复制，请手动选择上方代码复制')).toBeVisible()
  await page.getByRole('button', { name: '撤销 invite-1@example.invalid 的邀请' }).click()
  await page.getByLabel('撤销理由').fill('合成撤销其他邀请')
  await page.getByRole('button', { name: '确认撤销邀请' }).click()
  await expect(page.getByLabel('一次性邀请代码')).toHaveValue(mockCode)
  expect(
    await page.evaluate(() =>
      JSON.stringify({
        local: { ...localStorage },
        session: { ...sessionStorage },
        url: location.href,
      }),
    ),
  ).not.toContain(mockCode)
  await page.getByRole('button', { name: '已保存，关闭代码' }).click()
  await expect(page.getByLabel('一次性邀请代码')).toHaveCount(0)
  expect(state.unexpected).toEqual([])
  expect(state.errors).toEqual([])
})

test('邀请创建网络结果不明时复用同一请求，不显示伪造代码', async ({ page }) => {
  const state = await fixture(page, { uncertain: true, total: 2 })
  await page.goto('/settings/members')
  await fillInvite(page)
  await expect(page.getByLabel('受邀邮箱')).toBeDisabled()
  await page.getByRole('button', { name: '核对上次邀请请求' }).click()
  await expect(
    page.getByText('邀请已创建，但代码只在首次响应中返回。请在列表撤销该邀请后重新创建。'),
  ).toBeVisible()
  const writes = state.writes.filter((item) => item.path === '/api/v1/member-invitations')
  expect(writes).toHaveLength(2)
  expect(writes[0]?.body).toEqual(writes[1]?.body)
  await expect(page.getByLabel('一次性邀请代码')).toHaveCount(0)
  expect(state.errors).toEqual([])
})

test('成员和邀请完整翻页；失败不破坏分页历史，冲突后可精确恢复页外目标草稿', async ({ page }) => {
  const state = await fixture(page, { conflict: true })
  await page.goto('/settings/members')
  const members = page.getByRole('list', { name: '工作区成员' })
  await expect(members.locator('li')).toHaveCount(20)
  state.failNextPage = true
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('合成分页失败')
  await page.getByRole('button', { name: '刷新成员' }).click()
  const seen = new Set<string>()
  for (let index = 0; index < 11; index++) {
    await expect(members.locator('li').first()).toContainText(
      `member-${index * 20 + 1}@example.invalid`,
    )
    await expect(members.locator('li')).toHaveCount(index === 10 ? 2 : 20)
    for (const text of await members.locator('li p').allTextContents()) seen.add(text)
    if (index < 10) await page.getByRole('button', { name: '下一页', exact: true }).click()
  }
  expect(seen.size).toBe(202)
  for (let index = 0; index < 10; index++) {
    await page.getByRole('button', { name: '上一页', exact: true }).click()
    await expect(members.locator('li').first()).toContainText(
      `member-${(9 - index) * 20 + 1}@example.invalid`,
    )
  }
  await expect(page.getByRole('button', { name: '上一页', exact: true })).toBeDisabled()
  const invites = page.getByRole('list', { name: '邀请记录' })
  const invitationSeen = new Set<string>()
  for (let index = 0; index < 11; index++) {
    await expect(invites.locator('li').first()).toContainText(
      `invite-${index * 20 + 1}@example.invalid`,
    )
    await expect(invites.locator('li')).toHaveCount(index === 10 ? 1 : 20)
    for (const text of await invites.locator('li strong').allTextContents())
      invitationSeen.add(text)
    if (index < 10) await page.getByRole('button', { name: '下一页邀请' }).click()
  }
  expect(invitationSeen.size).toBe(201)
  await page.getByRole('button', { name: '管理 member-20@example.invalid', exact: true }).click()
  await page.getByRole('combobox', { name: '角色', exact: true }).selectOption('finance')
  await page.getByLabel('变更理由').fill('合成保留草稿')
  await page.getByRole('button', { name: '核对变更', exact: true }).click()
  await page.getByRole('button', { name: '确认保存成员变更' }).click()
  await expect(page.getByLabel('变更理由')).toHaveValue('合成保留草稿')
  await expect(page.getByRole('button', { name: '我已核对最新成员状态' })).toBeEnabled()
  expect(
    (await page.locator('.member-editor .notice-stack p').first().boundingBox())!.width,
  ).toBeGreaterThan(200)
  await expect(page.getByRole('button', { name: '核对变更', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '我已核对最新成员状态' }).click()
  await page.getByRole('button', { name: '核对变更', exact: true }).click()
  await page.getByRole('button', { name: '确认保存成员变更' }).click()
  await expect(page.getByLabel('变更理由')).toHaveCount(0)
  const writes = state.writes.filter((item) => item.path.includes('/members/'))
  expect(writes.map((item) => item.body.expected_version)).toEqual([1, 2])
  expect(state.errors).toEqual([])
  expect(state.unexpected).toEqual([])
})

test('非管理员不能直达成员数据；改密错误保留身份，成功后要求重新登录', async ({
  browser,
  baseURL,
}) => {
  for (const role of ['finance', 'reviewer', 'viewer'] as const) {
    const page = await browser.newPage({ baseURL })
    try {
      const state = await fixture(page, { role })
      await page.goto('/settings/members')
      await expect(page.getByText('仅管理员可以查看和管理工作区成员。')).toBeVisible()
      expect(state.memberReads).toBe(0)
      await page.goto('/settings/account')
      await page.getByLabel('当前密码', { exact: true }).fill('synthetic-wrong-password')
      await page.getByLabel('新密码', { exact: true }).fill('synthetic-next-password')
      await page.getByLabel('确认新密码').fill('synthetic-next-password')
      await page.getByRole('button', { name: '修改密码并退出所有会话' }).click()
      await expect(page.getByRole('alert')).toContainText('当前密码不正确')
      await expect(page).toHaveURL(/settings\/account/)
      await page.getByLabel('当前密码', { exact: true }).fill('synthetic-current-password')
      await page.getByRole('button', { name: '修改密码并退出所有会话' }).click()
      await expect(page).toHaveURL(/login.*reason=password_changed/)
      expect(state.errors).toEqual([])
      expect(state.unexpected).toEqual([])
    } finally {
      await page.close()
    }
  }
})

test('公开加入不依赖 Session；已有账号不会被替换，成功不自动登录', async ({ page }) => {
  const state = await fixture(page, { public: true, existing: true })
  await page.goto('/join')
  await page.getByLabel('邀请代码', { exact: true }).fill(mockCode)
  await page.getByRole('button', { name: '检查邀请', exact: true }).click()
  await expect(page.getByLabel('姓名', { exact: true })).toHaveCount(0)
  await page.getByLabel('现有账号密码').fill('synthetic-wrong-password')
  await page.getByRole('button', { name: '确认加入工作区' }).click()
  await expect(page.getByRole('alert')).toContainText('邮箱或密码不正确')
  await page.getByLabel('现有账号密码').fill('synthetic-current-password')
  await page.getByRole('button', { name: '确认加入工作区' }).click()
  await expect(page.getByRole('heading', { name: '已加入工作区' })).toBeVisible()
  expect(state.sessionReads).toBe(0)
  expect(
    state.writes
      .filter((item) => item.path.endsWith('/accept'))
      .every((item) => item.body.display_name === ''),
  ).toBe(true)
  expect(await page.locator('input').count()).toBe(0)
  expect(state.errors).toEqual([])
})

test('验证密码后显式选择工作区；失效会话退出没有未处理异常', async ({ page }) => {
  const state = await fixture(page, { public: true })
  await page.goto('/login?redirect=/settings/account')
  await page.getByLabel('邮箱', { exact: true }).fill('owner@example.invalid')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-current-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByLabel('选择工作区')).toHaveValue('')
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeDisabled()
  await page.getByLabel('选择工作区').selectOption(id(900))
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/settings\/account/)
  await page.getByRole('button', { name: '退出', exact: true }).click()
  await expect(page).toHaveURL(/login/)
  expect(
    state.writes.filter((item) => item.path.endsWith('/login')).map((item) => item.body.tenant_id),
  ).toEqual([undefined, id(900)])
  expect(state.errors).toEqual([])
  expect(state.unexpected).toEqual([])
})

test('新账号加入与成员、账号页面四尺寸双主题检查', async ({ page }, testInfo) => {
  const state = await fixture(page, { total: 2 })
  await page.goto('/settings/members')
  await expect(page.getByLabel('受邀邮箱')).toBeVisible()
  await captureResponsiveReview(page, testInfo, 'members')
  await page.goto('/settings/account')
  await expect(page.getByLabel('当前密码', { exact: true })).toBeVisible()
  await captureResponsiveReview(page, testInfo, 'account-password')
  await page.goto('/join')
  await page.getByLabel('邀请代码', { exact: true }).fill(mockCode)
  await page.getByRole('button', { name: '检查邀请', exact: true }).click()
  await expect(page.getByLabel('姓名', { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 384, height: 960 })
  expect((await page.locator('.join-form .notice > strong').boundingBox())!.width).toBeGreaterThan(
    200,
  )
  await captureResponsiveReview(page, testInfo, 'join-account')
  await page.getByLabel('姓名', { exact: true }).fill('合成新成员')
  await page.getByLabel('设置密码').fill('synthetic-next-password')
  await page.getByLabel('确认密码', { exact: true }).fill('synthetic-next-password')
  await page.getByRole('button', { name: '确认加入工作区' }).click()
  await expect(page.getByRole('heading', { name: '已加入工作区' })).toBeVisible()
  expect(state.errors).toEqual([])
  expect(state.unexpected).toEqual([])
})
