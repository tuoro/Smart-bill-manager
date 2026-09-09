import { expect, test, type Page } from '@playwright/test'
import type { Review } from '../src/data/client'
import {
  mockReductionWorkspace,
  reductionBatch,
  reductionReview,
  reductionSession,
} from './review-reduction-fixtures'
import { captureResponsiveReview } from './visual-review'

const save = '确认保存，不分配并继续'
const later = '稍后处理，不保存'
const reviewPath = (review: Review) => `/reviews/${review.job.id}`
const reviewAPI = (review: Review) => `**/api/v1/reviews/${review.job.id}`
const checks = new WeakMap<Page, { errors: string[]; unexpected: string[] }>()

async function setup(page: Page, reviews = [reductionReview(1), reductionReview(2)]) {
  const state = await mockReductionWorkspace(page, reviews)
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  checks.set(page, { errors, unexpected: state.unexpected })
  return state
}

async function start(page: Page) {
  await page.goto('/inbox')
  await page.getByRole('button', { name: /开始连续审核/ }).click()
  await expect(page.getByRole('heading', { level: 1, name: '审核单据' })).toBeVisible()
}

async function current(page: Page, review: Review) {
  await expect(page).toHaveURL(new RegExp(`${reviewPath(review)}\\?continuous=1$`))
  await expect(page.locator('.review-document-name')).toHaveText(review.job.original_name)
}

function gate() {
  let release = () => {}
  const promise = new Promise<void>((resolve) => (release = resolve))
  return { promise, release }
}

test.afterEach(async ({ page }) => {
  const check = checks.get(page)
  expect(check?.errors).toEqual([])
  expect(check?.unexpected).toEqual([])
})

test('相同 20 份单据：一次开启与逐单明确保存，实际 21 次操作', async ({ page }, testInfo) => {
  const reviews = reductionBatch()
  const state = await setup(page, reviews)
  let navigationClicks = 0
  let finalSaveClicks = 0
  await page.goto('/inbox')
  await page.getByRole('button', { name: /开始连续审核/ }).click()
  navigationClicks++
  for (const [index, review] of reviews.entries()) {
    await current(page, review)
    await expect(page.getByRole('heading', { level: 1 })).toBeFocused()
    expect(state.confirmations).toHaveLength(index)
    await page.getByRole('button', { name: save, exact: true }).click()
    finalSaveClicks++
  }
  await expect(page.getByRole('heading', { name: '本轮审核结束' })).toBeFocused()
  await expect(page.locator('.completion-state')).toContainText(
    '已保存 20 份 · 已驳回 0 份 · 暂缓 0 份',
  )
  expect(state.confirmations.map((request) => request.jobId)).toEqual(reviews.map((r) => r.job.id))
  for (const request of state.confirmations) {
    expect(request.body).toEqual({
      expected_revision: 1,
      association_mode: 'no_candidate',
      allocations: [],
      duplicate_resolutions: [],
    })
    expect(request.key).toBeTruthy()
  }
  expect(new Set(state.confirmations.map((request) => request.key)).size).toBe(20)
  await testInfo.attach('interaction-counts.json', {
    contentType: 'application/json',
    body: JSON.stringify({
      fixture_version: 'review-reduction-fixtures/1',
      documents: 20,
      navigation_clicks: navigationClicks,
      mechanical_no_candidate_selections: 0,
      explicit_save_clicks: finalSaveClicks,
      total_actions: navigationClicks + finalSaveClicks,
      correct_confirm_requests: state.confirmations.length,
      unexpected_requests: state.unexpected.length,
      actual_model_evaluated: false,
      human_time_measured: false,
    }),
  })
})

test('单独审核：不预提交、一次明确不分配、通过项可用键盘展开', async ({ page }) => {
  const review = reductionReview(1)
  const state = await setup(page, [review])
  await page.goto(reviewPath(review))
  const confirm = page.getByRole('button', { name: '确认保存，不分配', exact: true })
  await expect(confirm).toBeEnabled()
  await expect(page.getByRole('radio', { name: /确认当前没有候选/ })).toHaveCount(0)
  await expect(page.locator('.claim-field')).toHaveCount(review.fields.length - 1)
  const rules = page.locator('.passed-validations')
  await expect(rules).not.toHaveAttribute('open', '')
  await rules.locator('summary').focus()
  await page.keyboard.press('Enter')
  await expect(rules.getByText('合成字段和证据齐全')).toBeVisible()
  expect(state.confirmations).toHaveLength(0)
  await confirm.click()
  await expect(page.getByRole('heading', { name: '正式账单已创建' })).toBeVisible()
  expect(state.confirmations).toHaveLength(1)
  expect(state.confirmations[0]!.body.association_mode).toBe('no_candidate')
})

test('同组件连续切换：页码、证据、字段、焦点与播报不串单', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2), reductionReview(3)]
  reviews[0]!.page_count = 2
  reviews[0]!.pages.push({ page_number: 2, field_paths: [], item_keys: [] })
  await setup(page, reviews)
  await start(page)
  await page.getByRole('button', { name: '查看第 2 页' }).click()
  await page.locator('.passed-validations summary').click()
  await page.getByRole('button', { name: save, exact: true }).click()
  await current(page, reviews[1]!)
  await expect(page.locator('.page-position')).toHaveText('第 1 / 1 页')
  await expect(page.locator('.evidence-focus')).toContainText('合成第 2 单')
  await expect(page.locator('[data-field-path="amount_minor"]')).toContainText('CNY 100.02')
  await expect(
    page.getByAltText(`${reviews[1]!.job.original_name} 的第 1 页规范化审核图`),
  ).toBeVisible()
  await expect(page.locator('.document-stage img')).toHaveAttribute(
    'src',
    `/api/v1/documents/${reviews[1]!.job.document_id}/pages/1/content`,
  )
  await expect(page.getByRole('link', { name: '新窗口查看', exact: true })).toHaveAttribute(
    'href',
    `/api/v1/documents/${reviews[1]!.job.document_id}/content`,
  )
  await expect(page.locator('.passed-validations')).not.toHaveAttribute('open', '')
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused()
  await expect(page.locator('.review-queue-bar [role="status"]')).toContainText('第 1 份已保存')
  await page.getByRole('button', { name: save, exact: true }).click()
  await current(page, reviews[2]!)
  await expect(page.locator('.review-queue-bar [role="status"]')).toContainText('第 2 份已保存')
})

test('重复与关联必须逐单决定；blocked 暂缓不计入保存', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2), reductionReview(3)]
  for (const review of reviews.slice(0, 2)) {
    review.candidates = [
      {
        id: '00000000-0000-4000-8000-000000000021',
        target_type: 'invoice',
        target_id: '00000000-0000-4000-8000-000000000022',
        amount_minor: 20000,
        allocated_minor: 0,
        remaining_minor: 20000,
        currency: 'CNY',
        business_date: '2026-09-05',
        display_name: '合成待关联发票',
        available: true,
        name_exact: true,
        date_distance_days: 0,
        reason_codes: ['currency_exact'],
      },
    ]
    review.duplicate_candidates = [
      {
        id: '00000000-0000-4000-8000-000000000023',
        kind: 'near_file',
        existing_document_id: '00000000-0000-4000-8000-000000000024',
        display_name: '合成近似原件',
        dhash_distance: 1,
        ahash_distance: 1,
        available: true,
        reason_codes: ['ordered_page_visual_match'],
      },
    ]
  }
  reviews[2]!.job.status = 'blocked'
  reviews[2]!.claim_status = 'blocked'
  const state = await setup(page, reviews)
  await start(page)
  const confirm = page.getByRole('button', { name: '确认保存并继续', exact: true })
  await expect(confirm).toBeDisabled()
  await page.getByRole('checkbox', { name: /分配给发票/ }).check()
  await page.getByLabel('本次分配（CNY）').fill('1.00')
  await page.getByRole('checkbox', { name: /合成近似原件/ }).check()
  await confirm.click()
  await current(page, reviews[1]!)
  await expect(confirm).toBeDisabled()
  await expect(page.getByRole('checkbox', { name: /分配给发票/ })).not.toBeChecked()
  await expect(page.getByRole('checkbox', { name: /合成近似原件/ })).not.toBeChecked()
  await page.getByRole('radio', { name: /不关联任何候选/ }).check()
  await page.getByRole('checkbox', { name: /合成近似原件/ }).check()
  await confirm.click()
  await current(page, reviews[2]!)
  await expect(page.getByRole('button', { name: save, exact: true })).toBeDisabled()
  await page.getByRole('button', { name: later, exact: true }).click()
  await expect(page.locator('.completion-state')).toContainText(
    '已保存 2 份 · 已驳回 0 份 · 暂缓 1 份',
  )
  expect(state.confirmations.map((request) => request.body.association_mode)).toEqual([
    'allocate_candidates',
    'reject_all',
  ])
  expect(state.reviews.get(reviews[2]!.job.id)!.job.status).toBe('blocked')
})

test('异常定位和草稿保护，浅深主题与四档宽度', async ({ page }, testInfo) => {
  const review = reductionReview(1)
  review.page_count = 2
  const amount = review.fields[1]!
  amount.evidence[0]!.page = 2
  review.pages[0]!.field_paths = review.pages[0]!.field_paths.filter((path) => path !== amount.path)
  review.pages.push({ page_number: 2, field_paths: [amount.path], item_keys: [] })
  review.validations.push(
    {
      id: 'synthetic-warning',
      field_claim_id: amount.id,
      rule_code: 'synthetic.warning',
      severity: 'warning',
      status: 'warning',
      safe_message: '合成金额需要核对原件',
    },
    {
      id: 'synthetic-type',
      field_claim_id: review.fields[0]!.id,
      rule_code: 'synthetic.type',
      severity: 'error',
      status: 'blocked',
      safe_message: '合成类型需要人工核对',
    },
  )
  review.job.status = 'blocked'
  review.claim_status = 'blocked'
  const state = await setup(page, [review, reductionReview(2)])
  await start(page)
  const rules = page.locator('[aria-labelledby="validation-title"]')
  await expect(rules.getByText('合成金额需要核对原件')).toBeVisible()
  await expect(rules.getByText('合成类型需要人工核对')).toBeVisible()
  await rules.locator('li').filter({ hasText: 'synthetic.warning' }).getByRole('button').click()
  await expect(page.locator('.page-position')).toHaveText('第 2 / 2 页')
  await expect(page.locator('[data-field-path="amount_minor"] button')).toBeFocused()
  await page.getByRole('button', { name: '修订字段', exact: true }).click()
  await page.getByLabel('支付金额', { exact: true }).fill('123.45')
  await page.getByRole('button', { name: '支付金额 清空证据选择', exact: true }).click()
  await rules.locator('li').filter({ hasText: 'synthetic.type' }).getByRole('button').click()
  await expect(page.getByLabel('文档类型', { exact: true })).toBeFocused()
  await expect(page.getByLabel('支付金额', { exact: true })).toHaveValue('123.45')
  await expect(page.locator('[data-field-path="amount_minor"]')).toContainText('已选择 0 条证据')
  await expect(page.getByRole('button', { name: later, exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '驳回识别结果', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '确认驳回并继续，不保存正式记录', exact: true }),
  ).toBeDisabled()
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('link', { name: '返回收件箱', exact: true }).click()
  await expect(page.getByLabel('支付金额', { exact: true })).toHaveValue('123.45')
  await captureResponsiveReview(page, testInfo, 'continuous-review-exceptions')
  await page.getByRole('button', { name: '放弃修订', exact: true }).click()
  await page.getByRole('button', { name: later, exact: true }).click()
  await current(page, reductionReview(2))
  expect(state.confirmations).toHaveLength(0)
})

test('保存修订期间证据不可变，成功后仍须明确决定才继续', async ({ page }) => {
  const review = reductionReview(1)
  const state = await setup(page)
  const pending = gate()
  let submitted = false
  await page.route(`${reviewAPI(review)}/revisions`, async (route) => {
    submitted = true
    const body = route.request().postDataJSON()
    expect(
      body.fields.find((field: { path: string }) => field.path === 'amount_minor'),
    ).toMatchObject({ value: 12345, evidence_ids: [review.fields[1]!.evidence[0]!.id] })
    await pending.promise
    const latest = state.reviews.get(review.job.id)!
    latest.revision = 2
    latest.fields[1]!.value = 12345
    await route.fulfill({ json: latest })
  })
  await start(page)
  await page.getByRole('button', { name: '修订字段', exact: true }).click()
  await page.getByLabel('支付金额', { exact: true }).fill('123.45')
  await page.getByRole('button', { name: '保存修订版本', exact: true }).click()
  await expect.poll(() => submitted).toBe(true)
  for (const checkbox of await page.locator('.evidence-options input').all())
    await expect(checkbox).toBeDisabled()
  await expect(page.locator('.fields-panel')).toHaveAttribute('inert', '')
  await expect(page.getByRole('button', { name: later, exact: true })).toBeDisabled()
  pending.release()
  await expect(page.getByRole('button', { name: save, exact: true })).toBeEnabled()
  expect(state.confirmations).toHaveLength(0)
  await page.getByRole('button', { name: save, exact: true }).click()
  await current(page, reductionReview(2))
  expect(state.confirmations[0]!.body.expected_revision).toBe(2)
  await page.getByRole('button', { name: '驳回识别结果', exact: true }).click()
  await page.getByRole('button', { name: '确认驳回并继续，不保存正式记录', exact: true }).click()
  await expect(page.locator('.completion-state')).toContainText(
    '已保存 1 份 · 已驳回 1 份 · 暂缓 0 份',
  )
})

test('人工来源与行程凭证共用连续审核，行程不发送金额分配字段', async ({ page }) => {
  const manual: Review = { ...reductionReview(1), entry_mode: 'manual' }
  for (const field of manual.fields) field.source = 'user'
  const trip = reductionReview(2)
  trip.document_type = 'trip'
  trip.fields = trip.fields.slice(0, 4).map((field, index) => ({
    ...field,
    path: ['document_type', 'destination', 'start_date', 'end_date'][index]!,
    value_type: index < 2 ? 'string' : 'date',
    value: ['trip', '合成目的地', '2026-09-05', '2026-09-06'][index],
    evidence: field.evidence.map((evidence) => ({ ...evidence, quote: '合成行程票面摘录' })),
  }))
  trip.pages[0]!.field_paths = trip.fields.map((field) => field.path)
  const state = await setup(page, [manual, trip])
  await start(page)
  await expect(page.locator('.manual-source-notice')).toContainText('不代表 AI 识别成功')
  await page.getByRole('button', { name: save, exact: true }).click()
  await current(page, trip)
  await expect(page.getByRole('heading', { name: '金额分配', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '确认保存并继续', exact: true }).click()
  await expect(page.getByRole('heading', { name: '本轮审核结束' })).toBeVisible()
  expect(state.confirmations[1]!.body).toEqual({ expected_revision: 1, duplicate_resolutions: [] })
})

test('409 停留并刷新：新 revision 经再次明确确认才推进', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2)]
  const state = await setup(page, reviews)
  const attempts: Array<{ body: unknown; key: string | undefined }> = []
  await page.route(`${reviewAPI(reviews[0]!)}/confirm`, async (route) => {
    attempts.push({
      body: route.request().postDataJSON(),
      key: route.request().headers()['idempotency-key'],
    })
    if (attempts.length > 1) return route.fallback()
    state.reviews.get(reviews[0]!.job.id)!.revision = 2
    await route.fulfill({ status: 409, json: { error: { code: 'version_conflict' } } })
  })
  await start(page)
  await page.getByRole('button', { name: save, exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('审核版本已变化')
  await current(page, reviews[0]!)
  await expect(page.getByRole('button', { name: save, exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '刷新最新版本', exact: true }).click()
  await page.getByRole('button', { name: save, exact: true }).click()
  await current(page, reviews[1]!)
  expect(attempts.map((attempt) => attempt.body)).toEqual(
    [1, 2].map((revision) => ({
      expected_revision: revision,
      association_mode: 'no_candidate',
      allocations: [],
      duplicate_resolutions: [],
    })),
  )
  expect(attempts[0]!.key).not.toBe(attempts[1]!.key)
})

for (const action of ['confirm', 'reject'] as const) {
  test(`未知 ${action} 结果保留原请求与键，禁止提交中导航和静默推进`, async ({ page }) => {
    const reviews = [reductionReview(1), reductionReview(2)]
    await setup(page, reviews)
    const pending = gate()
    const attempts: Array<{ body: unknown; key: string | undefined }> = []
    const commits = new Set<string>()
    await page.route(`${reviewAPI(reviews[0]!)}/${action}`, async (route) => {
      const key = route.request().headers()['idempotency-key']!
      attempts.push({ body: route.request().postDataJSON(), key })
      commits.add(key)
      if (attempts.length === 1) {
        await pending.promise
        await route.abort('failed')
      } else if (action === 'reject') await route.fulfill({ status: 204 })
      else
        await route.fulfill({
          status: 201,
          json: {
            review_decision_id: 'synthetic-decision',
            fact_id: 'synthetic-fact',
            fact_type: 'payment',
            link_ids: [],
            replayed: true,
          },
        })
    })
    await start(page)
    if (action === 'reject') {
      await page.getByRole('button', { name: '驳回识别结果', exact: true }).click()
      await page.getByLabel('驳回原因（可选）').fill('合成驳回理由')
      await page
        .getByRole('button', { name: '确认驳回并继续，不保存正式记录', exact: true })
        .click()
    } else await page.getByRole('button', { name: save, exact: true }).click()
    await expect.poll(() => attempts.length).toBe(1)
    await expect(page.getByRole('button', { name: later, exact: true })).toBeDisabled()
    await page.getByRole('link', { name: '返回收件箱', exact: true }).click()
    await current(page, reviews[0]!)
    pending.release()
    const retry = page.getByRole('button', {
      name: action === 'confirm' ? '重试原确认' : '重试原驳回',
      exact: true,
    })
    await expect(retry).toBeEnabled()
    await expect(page.getByRole('button', { name: '修订字段', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: '刷新最新版本', exact: true })).toBeDisabled()
    await current(page, reviews[0]!)
    expect(attempts).toHaveLength(1)
    await retry.click()
    await current(page, reviews[1]!)
    expect(attempts).toHaveLength(2)
    expect(attempts[1]).toEqual(attempts[0])
    expect(commits.size).toBe(1)
    await expect(page.getByLabel('驳回原因（可选）')).toHaveCount(0)
  })
}

test('下一项读取失败：不残留旧字段，显式重试当前项', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2)]
  const state = await setup(page, reviews)
  let reads = 0
  await page.route(reviewAPI(reviews[1]!), async (route) => {
    if (++reads === 1)
      await route.fulfill({
        status: 503,
        json: { error: { code: 'unavailable', message: '合成读取失败' } },
      })
    else await route.fallback()
  })
  await start(page)
  await page.getByRole('button', { name: save, exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('无法打开审核')
  await expect(page.locator('.review-document-name')).toHaveCount(0)
  await expect(page.locator('.completion-state')).toHaveCount(0)
  await expect(page.locator('.review-queue-bar')).toContainText('第 2 / 2 份')
  expect(state.confirmations).toHaveLength(1)
  await page.getByRole('button', { name: '重试读取当前单据', exact: true }).click()
  await current(page, reviews[1]!)
  expect(state.confirmations).toHaveLength(1)
})

test('离开加载中的旧任务：迟到响应不能覆盖新单据', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2)]
  await setup(page, reviews)
  const pending = gate()
  const settled = gate()
  let requested = false
  const cancelled = page.waitForEvent(
    'requestfailed',
    (request) => new URL(request.url()).pathname === `/api/v1/reviews/${reviews[0]!.job.id}`,
  )
  await page.route(reviewAPI(reviews[0]!), async (route) => {
    requested = true
    await pending.promise
    await route.fulfill({ json: reviews[0] })
    settled.release()
  })
  await page.goto('/inbox')
  await page.getByRole('button', { name: /开始连续审核/ }).click()
  await expect.poll(() => requested).toBe(true)
  await page.getByRole('link', { name: '返回收件箱', exact: true }).click()
  await page
    .locator('tbody tr')
    .filter({ hasText: reviews[1]!.job.original_name })
    .getByRole('link', { name: '审核', exact: true })
    .click()
  await expect(page.locator('.review-document-name')).toHaveText(reviews[1]!.job.original_name)
  pending.release()
  await cancelled
  await settled.promise
  await expect(page.getByRole('button', { name: '确认保存，不分配', exact: true })).toBeEnabled()
  await expect(page.locator('[data-field-path="amount_minor"]')).toContainText('CNY 100.02')
})

test('队列只取开启时筛选内待审核项：新任务不追加，可继续或明确结束', async ({ page }) => {
  const reviews = [reductionReview(1), reductionReview(2), reductionReview(3)]
  reviews[1]!.job.status = 'blocked'
  reviews[1]!.claim_status = 'blocked'
  reviews[2]!.job.status = 'processing'
  const state = await setup(page, reviews)
  await page.goto('/inbox')
  await page.getByRole('button', { name: '处理中', exact: true }).click()
  await expect(page.getByRole('button', { name: /开始连续审核/ })).toBeDisabled()
  await page.getByRole('button', { name: '需处理', exact: true }).click()
  await page.getByRole('button', { name: /开始连续审核.*2/ }).click()
  await current(page, reviews[0]!)
  const late = reductionReview(4)
  state.reviews.set(late.job.id, late)
  await page.getByRole('button', { name: later, exact: true }).click()
  await current(page, reviews[1]!)
  await page.getByRole('link', { name: '返回收件箱', exact: true }).click()
  await page.getByRole('button', { name: /继续本轮审核.*1/ }).click()
  await current(page, reviews[1]!)
  await expect(page.locator('.review-queue-bar')).toContainText('第 2 / 2 份')
  await page.getByRole('link', { name: '返回收件箱', exact: true }).click()
  await page.getByRole('button', { name: '结束本轮', exact: true }).click()
  await expect(page.getByRole('button', { name: /继续本轮审核/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /开始连续审核.*3/ })).toBeEnabled()
  expect(state.confirmations).toHaveLength(0)
})

test('整页刷新明确退为单独审核，不恢复或提交内存队列', async ({ page }) => {
  const state = await setup(page)
  await start(page)
  await page.reload()
  await expect(page.getByText(/连续审核队列未保留/)).toBeVisible()
  await expect(page.getByRole('button', { name: '确认保存，不分配', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: later, exact: true })).toHaveCount(0)
  expect(state.confirmations).toHaveLength(0)
})

test('退出与切换身份清空队列，无审核能力不提供连续入口', async ({ page }) => {
  const state = await setup(page)
  await start(page)
  await page.getByRole('button', { name: '退出', exact: true }).click()
  await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
  state.session = {
    ...structuredClone(reductionSession),
    user: { ...reductionSession.user, id: 'synthetic-other-user' },
    capabilities: ['facts.read', 'documents.process'],
  }
  await page.route('**/api/v1/session/login', (route) => route.fulfill({ json: state.session }))
  await page.getByLabel('邮箱').fill('other@example.test')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'AI 收件箱', exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: /开始连续审核|继续本轮审核|结束本轮/ }),
  ).toHaveCount(0)
  expect(state.confirmations).toHaveLength(0)
})

for (const action of ['confirm', 'reject'] as const) {
  test(`提交 ${action} 中退出并原身份重登：旧成功响应不能恢复队列或跳转`, async ({ page }) => {
    const review = reductionReview(1)
    const state = await setup(page)
    const pending = gate()
    let requested = false
    await page.route(`${reviewAPI(review)}/${action}`, async (route) => {
      requested = true
      await pending.promise
      if (action === 'reject') await route.fulfill({ status: 204 })
      else
        await route.fulfill({
          status: 201,
          json: {
            review_decision_id: 'synthetic-old-decision',
            fact_id: 'synthetic-old-fact',
            fact_type: 'payment',
            link_ids: [],
            replayed: false,
          },
        })
    })
    await start(page)
    if (action === 'reject') {
      await page.getByRole('button', { name: '驳回识别结果', exact: true }).click()
      await page
        .getByRole('button', { name: '确认驳回并继续，不保存正式记录', exact: true })
        .click()
    } else await page.getByRole('button', { name: save, exact: true }).click()
    await expect.poll(() => requested).toBe(true)
    await page.getByRole('button', { name: '退出', exact: true }).click()
    await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
    state.session = structuredClone(reductionSession)
    await page.route('**/api/v1/session/login', (route) => route.fulfill({ json: state.session }))
    await page.getByLabel('邮箱').fill('reviewer@example.test')
    await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'AI 收件箱', exact: true })).toBeVisible()
    const oldResponse = page.waitForResponse((response) =>
      response.url().endsWith(`/${review.job.id}/${action}`),
    )
    pending.release()
    await oldResponse
    await expect(page.getByRole('button', { name: /开始连续审核/ })).toBeEnabled()
    await expect(page.getByRole('button', { name: /继续本轮审核|结束本轮/ })).toHaveCount(0)
    await expect(page).toHaveURL(/\/inbox$/)
    await expect(page.locator('.completion-state')).toHaveCount(0)
  })
}

for (const status of [403, 404, 401]) {
  test(`当前读取 ${status} 不自动推进；失效会话可离开审核`, async ({ page }) => {
    const reviews = [reductionReview(1), reductionReview(2)]
    const state = await setup(page, reviews)
    await page.route(reviewAPI(reviews[0]!), (route) =>
      route.fulfill({
        status,
        json: {
          error: {
            code: status === 401 ? 'unauthenticated' : status === 403 ? 'forbidden' : 'not_found',
          },
        },
      }),
    )
    await page.goto('/inbox')
    await page.getByRole('button', { name: /开始连续审核/ }).click()
    if (status === 401)
      await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
    else {
      await expect(page.getByRole('alert')).toContainText('无法打开审核')
      await expect(page.locator('.review-queue-bar')).toContainText('第 1 / 2 份')
      await page.getByRole('button', { name: later, exact: true }).click()
      await current(page, reviews[1]!)
    }
    expect(state.confirmations).toHaveLength(0)
    expect(state.rejections).toHaveLength(0)
  })
}
