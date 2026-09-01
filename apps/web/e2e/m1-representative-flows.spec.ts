import { expect, test, type Page } from '@playwright/test'
import { constants } from 'node:fs'
import { open, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const email = requiredEnvironment('SBM_E2E_EMAIL')
const passwordFile = requiredEnvironment('SBM_E2E_PASSWORD_FILE')
const providerBaseURL = requiredEnvironment('SBM_E2E_PROVIDER_BASE_URL')
const providerAPIKeyFile = requiredEnvironment('SBM_E2E_PROVIDER_API_KEY_FILE')
const providerModel = requiredEnvironment('SBM_E2E_PROVIDER_MODEL')
const uploadAsset = resolve(
  process.cwd(),
  '../../tests/evaluation/assets/m1-synthetic-v1/pay-001.png',
)

test.describe.serial('M1 四个代表页面流程', () => {
  let reviewPath = ''
  let confirmedFactID = ''
  let password = ''
  let providerAPIKey = ''

  test.beforeAll(async () => {
    password = await readProtectedSecret(passwordFile, 1024)
    providerAPIKey = await readProtectedSecret(providerAPIKeyFile, 4096)
  })

  test.afterAll(() => {
    password = ''
    providerAPIKey = ''
  })

  test('登录：错误提示后用 owner 进入工作区', async ({ page }) => {
    const browserErrors = collectBrowserErrors(page)
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()

    await page.getByLabel('邮箱').fill(email)
    await page.getByLabel('密码', { exact: true }).fill('definitely-wrong-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('alert')).toBeVisible()
    browserErrors.length = 0

    await page.getByLabel('密码', { exact: true }).fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(/\/inbox$/)
    await expect(page.getByRole('heading', { name: 'AI 收件箱' })).toBeVisible()
    expect(browserErrors).toEqual([])
  })

  test('AI 收件箱：配置 Provider、上传并进入待人工确认', async ({ page }) => {
    await login(page, password)
    const browserErrors = collectBrowserErrors(page)
    await page.getByRole('link', { name: 'AI 配置' }).click()
    await expect(page.getByRole('heading', { name: 'AI Provider' })).toBeVisible()

    await page.getByLabel('Base URL').fill(providerBaseURL)
    await page.getByLabel('Model').fill(providerModel)
    await page.getByLabel('API Key').fill(providerAPIKey)
    await page.getByRole('button', { name: '创建待检测配置' }).click()

    const provider = page.locator('.provider-list li').filter({ hasText: providerModel })
    await expect(provider).toBeVisible()
    await provider.getByRole('button', { name: '能力检测' }).click()
    await expect(provider).toContainText('passed')
    await provider.getByRole('button', { name: '激活' }).click()
    await expect(provider).toContainText('活动配置')

    const secretRetention = await page.evaluate(
      ({ apiKey, loginPassword }) => {
        const serializedStores = JSON.stringify({
          local: Object.fromEntries(Object.entries(localStorage)),
          session: Object.fromEntries(Object.entries(sessionStorage)),
          cookie: document.cookie,
          markup: document.documentElement.outerHTML,
        })
        return {
          api_key_present: serializedStores.includes(apiKey),
          login_password_present: serializedStores.includes(loginPassword),
        }
      },
      { apiKey: providerAPIKey, loginPassword: password },
    )
    expect(secretRetention).toEqual({ api_key_present: false, login_password_present: false })
    expect((await page.getByLabel('API Key').inputValue()).includes(providerAPIKey)).toBe(false)

    await page.getByRole('link', { name: 'AI 收件箱' }).click()
    await page.locator('input[type="file"]').setInputFiles({
      name: 'playwright-payment.png',
      mimeType: 'image/png',
      buffer: await readFile(uploadAsset),
    })
    const row = page.locator('tbody tr').filter({ hasText: 'playwright-payment.png' })
    await expect(row).toContainText('待人工确认', { timeout: 30_000 })
    const href = await row.getByRole('link', { name: '审核' }).getAttribute('href')
    if (!href) throw new Error('review link did not contain a path')
    reviewPath = href
    expect(browserErrors).toEqual([])
  })

  test('审核工作台：核验证据、显式无候选并确认 Fact', async ({ page }) => {
    if (!reviewPath) throw new Error('inbox flow did not produce a review path')
    await login(page, password)
    const browserErrors = collectBrowserErrors(page)
    await page.goto(reviewPath)
    await expect(page.getByRole('heading', { name: /审核 playwright-payment\.png/ })).toBeVisible()
    await expect(page.getByRole('heading', { name: '规则校验' })).toBeVisible()
    await expect(page.getByAltText('playwright-payment.png 的第 1 页规范化审核图')).toBeVisible()

    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    const confirm = page.getByRole('button', { name: '确认并生成事实' })
    await expect(confirm).toBeEnabled()
    await confirm.click()
    await expect(page.getByRole('heading', { name: '正式账单已创建' })).toBeVisible()
    confirmedFactID =
      (
        await page
          .locator('.completion-details div')
          .filter({ hasText: 'Fact ID' })
          .locator('dd')
          .textContent()
      )?.trim() ?? ''
    expect(confirmedFactID).not.toBe('')
    expect(browserErrors).toEqual([])
  })

  test('账单列表：查询已确认 Payment 并切换发票空状态', async ({ page }) => {
    if (!confirmedFactID) throw new Error('review flow did not produce a Fact ID')
    await login(page, password)
    const browserErrors = collectBrowserErrors(page)
    await page.getByRole('link', { name: '支付管理' }).click()
    await expect(page.getByRole('heading', { name: '账单列表' })).toBeVisible()
    const paymentRow = page.locator('tbody tr').filter({ hasText: confirmedFactID })
    await expect(paymentRow).toContainText('Synthetic Memory Merchant')
    await expect(paymentRow).toContainText('123.45')
    await expect(page.getByText('1 条未删除记录')).toBeVisible()

    await page.getByRole('tab', { name: '发票' }).click()
    await expect(page.getByText('还没有正式发票')).toBeVisible()
    expect(browserErrors).toEqual([])
  })
})

async function login(page: Page, password: string) {
  await page.goto('/login')
  await page.getByLabel('邮箱').fill(email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/inbox$/)
}

function collectBrowserErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
  return errors
}

function requiredEnvironment(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required`)
  return value
}

async function readProtectedSecret(path: string, maximumBytes: number): Promise<string> {
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW)
  try {
    const information = await handle.stat()
    if (
      !information.isFile() ||
      information.nlink !== 1 ||
      (information.mode & 0o077) !== 0 ||
      information.uid !== process.getuid?.() ||
      information.size < 1 ||
      information.size > maximumBytes + 2
    ) {
      throw new Error('secret input must be a singly linked owner-only regular file')
    }
    const content = await handle.readFile()
    try {
      const end =
        content.at(-1) === 0x0a
          ? content.length - (content.at(-2) === 0x0d ? 2 : 1)
          : content.length
      if (end < 1 || end > maximumBytes) throw new Error('secret input has an invalid size')
      return content.subarray(0, end).toString('utf8')
    } finally {
      content.fill(0)
    }
  } finally {
    await handle.close()
  }
}
