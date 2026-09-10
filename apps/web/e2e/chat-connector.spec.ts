import { expect, test, type Page } from '@playwright/test'
import type { ChatConnector, Session } from '../src/data/client'

const session: Session = {
  user: {
    id: '00000000-0000-4000-8000-000000000101',
    email: 'owner@example.test',
    display_name: '合成管理员',
  },
  tenant: {
    id: '00000000-0000-4000-8000-000000000102',
    name: '合成团队',
    default_currency: 'CNY',
    timezone: 'Asia/Shanghai',
  },
  role: 'owner',
  capabilities: ['providers.manage', 'documents.process'],
  csrf_token: 'csrf-token',
  expires_at: '2099-01-01T00:00:00Z',
}

function connector(overrides: Partial<ChatConnector> = {}): ChatConnector {
  return {
    platform: 'dingtalk',
    app_key: 'dingxxxx',
    has_secret: true,
    detection_status: 'pending',
    detection_checked_at: null,
    detection_message: '',
    active: false,
    version: 1,
    updated_at: '2026-09-11T00:00:00Z',
    ...overrides,
  }
}

async function setup(page: Page) {
  const state = {
    current: undefined as ChatConnector | undefined,
    saves: [] as Record<string, unknown>[],
    errors: [] as string[],
  }
  page.on('pageerror', (error) => state.errors.push(error.message))
  await page.route('**/api/v1/session', (route) => route.fulfill({ json: session }))
  await page.route('**/api/v1/chat-connectors/dingtalk', async (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      if (!state.current)
        return route.fulfill({
          status: 404,
          json: { error: { code: 'not_found', message: '尚未配置' } },
        })
      return route.fulfill({ json: state.current })
    }
    if (method === 'PUT') {
      const body = route.request().postDataJSON()
      state.saves.push(body)
      state.current = connector({
        app_key: body.app_key,
        version: (state.current?.version ?? 0) + 1,
      })
      return route.fulfill({ json: state.current })
    }
    return route.fallback()
  })
  await page.route('**/api/v1/chat-connectors/dingtalk/detect', (route) => {
    state.current = connector({
      ...state.current,
      detection_status: 'passed',
      detection_message: '令牌换取成功',
      detection_checked_at: '2026-09-11T01:00:00Z',
    })
    return route.fulfill({ json: state.current })
  })
  await page.route('**/api/v1/chat-connectors/dingtalk/activate', (route) => {
    state.current = connector({ ...state.current, active: true })
    return route.fulfill({ json: state.current })
  })
  await page.route('**/api/v1/chat-connectors/dingtalk/deactivate', (route) => {
    state.current = connector({ ...state.current, active: false })
    return route.fulfill({ json: state.current })
  })
  return state
}

// 保存 → 检测 → 启用是唯一路径；密钥只上送一次，页面上永远看不到它。
test('钉钉收单：保存、检测、启用、停用，密钥不回显', async ({ page }) => {
  const state = await setup(page)
  await page.goto('/settings/dingtalk')
  await expect(page.getByRole('heading', { level: 1, name: '钉钉收单' })).toBeVisible()
  await expect(page.getByText('尚未配置')).toBeVisible()

  await page.getByLabel('AppKey').fill('dingabc')
  await page.getByLabel('AppSecret').fill('super-secret')
  await page.getByRole('button', { name: '保存凭据', exact: true }).click()
  await expect(page.getByText('已设置（不回显）')).toBeVisible()
  expect(state.saves).toEqual([{ app_key: 'dingabc', app_secret: 'super-secret' }])
  // 密钥输入框清空、页面任何地方都不出现明文。
  await expect(page.getByLabel('AppSecret')).toHaveValue('')
  await expect(page.locator('body')).not.toContainText('super-secret')

  // 没检测通过前启用按钮禁用。
  const activate = page.getByRole('button', { name: '启用', exact: true })
  await expect(activate).toBeDisabled()
  await page.getByRole('button', { name: '检测凭据', exact: true }).click()
  await expect(page.getByText('检测通过')).toBeVisible()
  await expect(page.getByText('令牌换取成功')).toBeVisible()
  await expect(activate).toBeEnabled()
  await activate.click()
  await expect(page.getByText('已启用')).toBeVisible()
  await expect(
    page.locator('.connector-note').getByRole('link', { name: '账号与密码' }),
  ).toBeVisible()
  await page.getByRole('button', { name: '停用', exact: true }).click()
  await expect(page.getByText('未启用')).toBeVisible()
  expect(state.errors).toEqual([])
})
