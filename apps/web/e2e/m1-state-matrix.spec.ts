import { expect, test, type Page, type Route } from '@playwright/test'
import type { JobSummary, Payment, Review, Session } from '../src/data/client'

const timestamp = '2026-08-28T08:00:00Z'
const transparentPNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAF/gL+X9jXAAAAAElFTkSuQmCC',
  'base64',
)

test.describe('M1/M2 真实组件状态矩阵', () => {
  test('登录：默认、凭据错误、提交中和权限不足', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await page.route(sessionURL, (route) => fulfillError(route, 401, 'unauthenticated', '请先登录'))

    let loginAttempt = 0
    let releasePermissionResponse = () => {}
    const permissionResponse = new Promise<void>((resolve) => {
      releasePermissionResponse = resolve
    })
    await page.route(loginURL, async (route) => {
      loginAttempt += 1
      if (loginAttempt === 1) {
        await fulfillError(route, 401, 'invalid_credentials', '邮箱或密码错误')
        return
      }
      await permissionResponse
      await fulfillError(route, 403, 'forbidden', '当前账号已停用，无法进入工作区')
    })

    await page.goto('/login')
    await expect(page.locator('.login-layout')).toBeVisible()
    await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)

    await page.getByLabel('邮箱').fill('reviewer@example.test')
    await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('邮箱或密码错误')
    await expect(page.getByLabel('邮箱')).toHaveAttribute('aria-describedby', 'login-error')

    await page.getByRole('button', { name: '登录', exact: true }).click()
    const pendingButton = page.getByRole('button', { name: '正在登录…' })
    await expect(pendingButton).toBeDisabled()
    releasePermissionResponse()
    await expect(page.getByRole('alert')).toContainText('当前账号已停用，无法进入工作区')
    await expect(page.getByRole('button', { name: '登录', exact: true })).toBeEnabled()
    expect(pageErrors).toEqual([])
  })

  test('AI 收件箱：混合队列覆盖处理、部分结果、待确认、失败和取消', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const jobs: JobSummary[] = [
      job('queued', 'queued-receipt.png'),
      job('processing', 'processing-receipt.png'),
      job('needs_review', 'review-receipt.png'),
      job('blocked', 'partial-invoice.pdf', '缺少价税合计，需要人工补充证据'),
      job('failed', 'failed-receipt.webp', 'Provider 临时不可用，可显式重试'),
      job('cancel_requested', 'cancelling-receipt.jpg'),
      job('cancelled', 'cancelled-receipt.png'),
      job('completed', 'completed-receipt.png'),
    ]
    await mockJobs(page, () => jobs)

    await page.goto('/inbox')
    const queue = page.locator('section.queue-panel')
    await expect(queue).toBeVisible()
    await expect(queue.locator('tbody tr')).toHaveCount(jobs.length)
    for (const label of [
      '等待处理',
      'AI 提取中',
      '待人工确认',
      '部分结果',
      '处理失败',
      '正在取消',
      '已取消',
      '已生成事实',
    ]) {
      await expect(queue.locator('.status').filter({ hasText: label })).toBeVisible()
    }
    await expect(
      rowFor(queue, 'processing-receipt.png').getByRole('button', { name: '取消' }),
    ).toBeVisible()
    await expect(
      rowFor(queue, 'failed-receipt.webp').getByRole('button', { name: '重试' }),
    ).toBeVisible()
    await expect(
      rowFor(queue, 'partial-invoice.pdf').getByRole('link', { name: '审核' }),
    ).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('AI 收件箱：空状态', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    await mockJobs(page, () => [])

    await page.goto('/inbox')
    await expect(page.locator('section.queue-panel .state-layout')).toContainText('收件箱还是空的')
    await expect(page.getByText('0 个任务')).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('AI 收件箱：离线后自动重连并恢复真实队列布局', async ({ context, page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    let jobs: JobSummary[] = []
    await mockJobs(page, () => jobs)

    await page.goto('/inbox')
    await expect(page.getByText('收件箱还是空的')).toBeVisible()

    await context.setOffline(true)
    await expect(page.getByRole('status').filter({ hasText: '当前离线' })).toBeVisible()
    jobs = [job('queued', 'reconnected-receipt.png')]
    await context.setOffline(false)

    await expect(page.getByText('当前离线')).toHaveCount(0)
    await expect(
      page.locator('tbody tr').filter({ hasText: 'reconnected-receipt.png' }),
    ).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：加载、待审核并完成', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-ready')
    let releaseReview = () => {}
    const reviewResponse = new Promise<void>((resolve) => {
      releaseReview = resolve
    })
    await mockDocumentContent(page, review.job.document_id)
    await page.route(reviewURL(review.job.id), async (route) => {
      if (route.request().method() !== 'GET') {
        await route.fallback()
        return
      }
      await reviewResponse
      await fulfillJSON(route, review)
    })
    await page.route(confirmURL(review.job.id), (route) =>
      fulfillJSON(route, {
        review_decision_id: '00000000-0000-4000-8000-000000000701',
        fact_type: 'payment',
        fact_id: '00000000-0000-4000-8000-000000000702',
        link_ids: [],
        replayed: false,
      }),
    )

    await page.goto(`/reviews/${review.job.id}`)
    await expect(page.getByRole('status')).toContainText('正在加载 Claim 与证据')
    releaseReview()

    await expect(page.locator('.review-grid')).toBeVisible()
    await expect(
      page.getByRole('heading', { name: `审核 ${review.job.original_name}` }),
    ).toBeVisible()
    await expect(page.getByText('校验通过，待确认')).toBeVisible()
    await expect(page.getByRole('radio', { name: /确认当前没有候选/ })).toBeVisible()

    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    await page.getByRole('button', { name: '确认并生成事实' }).click()
    const completion = page.locator('section.completion-state')
    await expect(completion).toBeVisible()
    await expect(completion.getByRole('heading', { name: '正式账单已创建' })).toBeVisible()
    await expect(completion).toContainText('00000000-0000-4000-8000-000000000702')
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：一笔 Fact 可提交多候选金额分配', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-allocation')
    review.candidates = [
      {
        id: '00000000-0000-4000-8000-000000000711',
        target_type: 'invoice',
        target_id: '00000000-0000-4000-8000-000000000721',
        amount_minor: 20000,
        allocated_minor: 5000,
        remaining_minor: 15000,
        currency: 'CNY',
        business_date: '2026-08-27',
        display_name: '第一发票',
        available: true,
        name_exact: true,
        date_distance_days: 0,
        reason_codes: ['currency_exact', 'remaining_available'],
      },
      {
        id: '00000000-0000-4000-8000-000000000712',
        target_type: 'invoice',
        target_id: '00000000-0000-4000-8000-000000000722',
        amount_minor: 18000,
        allocated_minor: 6000,
        remaining_minor: 12000,
        currency: 'CNY',
        business_date: '2026-08-28',
        display_name: '第二发票',
        available: true,
        name_exact: false,
        date_distance_days: 1,
        reason_codes: ['currency_exact', 'partial_allocation'],
      },
    ]
    await mockDocumentContent(page, review.job.document_id)
    await page.route(reviewURL(review.job.id), (route) => fulfillJSON(route, review))
    let submitted: unknown
    await page.route(confirmURL(review.job.id), async (route) => {
      submitted = route.request().postDataJSON()
      await fulfillJSON(route, {
        review_decision_id: '00000000-0000-4000-8000-000000000731',
        fact_type: 'payment',
        fact_id: '00000000-0000-4000-8000-000000000732',
        link_ids: ['00000000-0000-4000-8000-000000000741', '00000000-0000-4000-8000-000000000742'],
        replayed: false,
      })
    })

    await page.goto(`/reviews/${review.job.id}`)
    await page.getByRole('checkbox', { name: /第一发票/ }).check()
    await page.getByRole('checkbox', { name: /第二发票/ }).check()
    await page.getByLabel('本次分配（最小单位）').nth(0).fill('10000')
    await page.getByLabel('本次分配（最小单位）').nth(1).fill('12000')
    await expect(page.getByText('本次合计 22000')).toBeVisible()
    await page.getByRole('button', { name: '确认并生成事实' }).click()

    expect(submitted).toMatchObject({
      expected_revision: 1,
      association_mode: 'allocate_candidates',
      allocations: [
        { candidate_id: review.candidates[0].id, allocated_minor: 10000 },
        { candidate_id: review.candidates[1].id, allocated_minor: 12000 },
      ],
    })
    await expect(page.locator('section.completion-state')).toContainText('2 条')
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：多页单据可直接分页且保留审核上下文', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-paged-review')
    review.page_count = 3
    review.pages = [
      { page_number: 1, field_paths: review.fields.map((field) => field.path), item_keys: [] },
      { page_number: 2, field_paths: [], item_keys: [] },
      { page_number: 3, field_paths: [], item_keys: [] },
    ]
    await mockReview(page, review)

    await page.goto(`/reviews/${review.job.id}`)
    await expect(page.getByText('第 1 / 3 页')).toBeVisible()
    await expect(page.getByAltText(/第 1 页规范化审核图/)).toBeVisible()
    await page.getByRole('button', { name: '查看第 3 页' }).click()
    await expect(page.getByText('第 3 / 3 页')).toBeVisible()
    await expect(page.getByAltText(/第 3 页规范化审核图/)).toBeVisible()
    await expect(page.getByRole('button', { name: '下一页' })).toBeDisabled()
    await expect(
      page.getByRole('button', { name: /支付金额（最小单位） amount_minor/ }),
    ).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：分页在 768px 与等效 200% 回流保持可达', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-paged-reflow')
    review.page_count = 3
    review.pages = [
      { page_number: 1, field_paths: review.fields.map((field) => field.path), item_keys: [] },
      { page_number: 2, field_paths: [], item_keys: [] },
      { page_number: 3, field_paths: [], item_keys: [] },
    ]
    await mockReview(page, review)

    await page.setViewportSize({ width: 768, height: 1000 })
    await page.goto(`/reviews/${review.job.id}`)
    for (const width of [768, 384]) {
      await page.setViewportSize({ width, height: 1000 })
      const nextPage = page.getByRole('button', { name: '下一页' })
      await expect(nextPage).toBeVisible()
      await nextPage.focus()
      await expect(nextPage).toBeFocused()
      const layout = await page.evaluate(() => {
        const root = document.documentElement
        const controls = [...document.querySelectorAll<HTMLElement>('.page-toolbar button')]
        return {
          horizontalOverflow: root.scrollWidth > root.clientWidth + 1,
          clippedControls: controls.filter((control) => {
            const bounds = control.getBoundingClientRect()
            return bounds.left < -0.5 || bounds.right > root.clientWidth + 0.5
          }).length,
        }
      })
      expect(layout).toEqual({ horizontalOverflow: false, clippedControls: 0 })
      await nextPage.click()
    }
    await expect(page.getByText('第 3 / 3 页')).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：疑似重复必须逐项明确保留独立记录', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-duplicate')
    review.duplicate_candidates = [
      {
        id: '00000000-0000-4000-8000-000000000751',
        kind: 'near_file',
        existing_document_id: '00000000-0000-4000-8000-000000000752',
        display_name: '近似支付截图.png',
        dhash_distance: 1,
        ahash_distance: 1,
        available: true,
        reason_codes: ['same_page_count', 'ordered_page_visual_match'],
      },
    ]
    await mockDocumentContent(page, review.job.document_id)
    await page.route(reviewURL(review.job.id), (route) => fulfillJSON(route, review))
    let submitted: unknown
    await page.route(confirmURL(review.job.id), async (route) => {
      submitted = route.request().postDataJSON()
      await fulfillJSON(route, {
        review_decision_id: '00000000-0000-4000-8000-000000000753',
        fact_type: 'payment',
        fact_id: '00000000-0000-4000-8000-000000000754',
        link_ids: [],
        replayed: false,
      })
    })

    await page.goto(`/reviews/${review.job.id}`)
    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    await expect(page.getByRole('button', { name: '确认并生成事实' })).toBeDisabled()
    await expect(page.locator('#duplicate-resolution-error')).toContainText(
      '请逐项确认全部疑似重复候选',
    )
    await expect(page.locator('fieldset[aria-labelledby="duplicate-title"]')).toHaveAttribute(
      'aria-describedby',
      'duplicate-resolution-error',
    )
    await page.getByRole('checkbox', { name: /近似文件.*近似支付截图/ }).check()
    await expect(page.getByRole('button', { name: '确认并生成事实' })).toBeEnabled()
    await page.getByRole('button', { name: '确认并生成事实' }).click()

    expect(submitted).toMatchObject({
      duplicate_resolutions: [
        { candidate_id: review.duplicate_candidates[0].id, action: 'keep_distinct' },
      ],
    })
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：阻断状态禁止确认', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = blockedReview('job-blocked')
    await mockReview(page, review)

    await page.goto(`/reviews/${review.job.id}`)
    await expect(page.locator('.review-grid')).toBeVisible()
    await expect(page.getByText('阻断，需修订')).toBeVisible()
    await expect(page.getByText('Claim 被服务端校验阻断')).toBeVisible()
    await expect(
      page
        .locator('[aria-labelledby="validation-title"]')
        .getByText('金额字段缺失，必须人工修订并绑定证据'),
    ).toBeVisible()
    await expect(page.getByRole('button', { name: '确认并生成事实' })).toBeDisabled()
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：版本冲突显式要求刷新 revision', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const review = readyReview('job-conflict')
    await mockReview(page, review)
    await page.route(confirmURL(review.job.id), (route) =>
      fulfillError(route, 409, 'version_conflict', 'revision 已被其他审核人更新'),
    )

    await page.goto(`/reviews/${review.job.id}`)
    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    await page.getByRole('button', { name: '确认并生成事实' }).click()

    const conflict = page.getByRole('alert')
    await expect(conflict).toContainText('审核版本已变化')
    await expect(conflict.getByRole('button', { name: '刷新 revision' })).toBeVisible()
    await expect(page.locator('.review-grid')).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('账单列表：加载、有数据并切换到空状态', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const payment = syntheticPayment()
    let releasePayments = () => {}
    const paymentResponse = new Promise<void>((resolve) => {
      releasePayments = resolve
    })
    await page.route(paymentsURL, async (route) => {
      await paymentResponse
      await fulfillJSON(route, { items: [payment] })
    })
    await page.route(invoicesURL, (route) => fulfillJSON(route, { items: [] }))

    await page.goto('/payments')
    const facts = page.locator('section.facts-panel')
    await expect(facts.getByRole('status')).toContainText('正在加载支付')
    releasePayments()

    const paymentRow = facts.locator('tbody tr').filter({ hasText: payment.merchant })
    await expect(paymentRow).toContainText('Synthetic State Merchant')
    await expect(paymentRow).toContainText('321.09')
    await expect(paymentRow).toContainText('SYN-STATE-001')
    await expect(facts.getByText('1 条未删除记录')).toBeVisible()

    await page.getByRole('tab', { name: '发票' }).click()
    await expect(page.locator('section.facts-panel .state-layout')).toContainText('还没有正式发票')
    expect(pageErrors).toEqual([])
  })

  test('账单列表：权限不足状态不泄露数据', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, reviewerSession())
    await page.route(paymentsURL, (route) =>
      fulfillError(route, 403, 'forbidden', '当前账号没有执行此操作的权限'),
    )

    await page.goto('/payments')
    const facts = page.locator('section.facts-panel')
    await expect(facts).toContainText('没有查看账单的权限')
    await expect(facts).toContainText('Reviewer 只能处理审核资料')
    await expect(facts.locator('table')).toHaveCount(0)
    expect(pageErrors).toEqual([])
  })
})

function sessionURL(url: URL): boolean {
  return url.pathname === '/api/v1/session'
}

function loginURL(url: URL): boolean {
  return url.pathname === '/api/v1/session/login'
}

function paymentsURL(url: URL): boolean {
  return url.pathname === '/api/v1/payments'
}

function invoicesURL(url: URL): boolean {
  return url.pathname === '/api/v1/invoices'
}

function reviewURL(jobID: string): (url: URL) => boolean {
  return (url) => url.pathname === `/api/v1/reviews/${jobID}`
}

function confirmURL(jobID: string): (url: URL) => boolean {
  return (url) => url.pathname === `/api/v1/reviews/${jobID}/confirm`
}

async function mockSession(page: Page, session: Session = ownerSession()) {
  await page.route(sessionURL, (route) => fulfillJSON(route, session))
}

async function mockJobs(page: Page, current: () => JobSummary[]) {
  await page.route(
    (url) => url.pathname === '/api/v1/jobs',
    (route) => fulfillJSON(route, { items: current() }),
  )
}

async function mockReview(page: Page, review: Review) {
  await mockDocumentContent(page, review.job.document_id)
  await page.route(reviewURL(review.job.id), (route) => fulfillJSON(route, review))
}

async function mockDocumentContent(page: Page, documentID: string) {
  await page.route(
    (url) =>
      url.pathname === `/api/v1/documents/${documentID}/content` ||
      url.pathname.startsWith(`/api/v1/documents/${documentID}/pages/`),
    (route) => route.fulfill({ status: 200, contentType: 'image/png', body: transparentPNG }),
  )
}

function ownerSession(): Session {
  return {
    user: {
      id: '00000000-0000-4000-8000-000000000101',
      email: 'owner@example.test',
      display_name: 'Synthetic Owner',
    },
    tenant: {
      id: '00000000-0000-4000-8000-000000000102',
      name: '合成验收工作区',
      default_currency: 'CNY',
      timezone: 'Asia/Shanghai',
    },
    role: 'owner',
    capabilities: ['documents.process', 'facts.read', 'providers.manage'],
    csrf_token: 'synthetic-csrf-token',
    expires_at: '2026-08-29T08:00:00Z',
  }
}

function reviewerSession(): Session {
  return {
    ...ownerSession(),
    role: 'reviewer',
    capabilities: ['documents.process'],
  }
}

function job(
  status: JobSummary['status'],
  originalName: string,
  safeErrorMessage?: string,
): JobSummary {
  const suffix = String(jobSequence++).padStart(12, '0')
  return {
    id: `00000000-0000-4000-8000-${suffix}`,
    document_id: `10000000-0000-4000-8000-${suffix}`,
    original_name: originalName,
    detected_mime: originalName.endsWith('.pdf') ? 'application/pdf' : 'image/png',
    status,
    attempt_count: status === 'failed' ? 2 : 1,
    ...(safeErrorMessage ? { safe_error_message: safeErrorMessage } : {}),
    created_at: timestamp,
    version: 1,
  }
}

let jobSequence = 1

function readyReview(label: string): Review {
  const review = baseReview(label)
  return {
    ...review,
    claim_status: 'ready_for_review',
    validations: [
      {
        id: '00000000-0000-4000-8000-000000000601',
        field_claim_id: review.fields[1]?.id,
        rule_code: 'payment.required_fields',
        severity: 'info',
        status: 'passed',
        safe_message: '关键字段和证据完整',
      },
    ],
  }
}

function blockedReview(label: string): Review {
  const review = baseReview(label)
  return {
    ...review,
    job: {
      ...review.job,
      status: 'blocked',
      safe_error_message: '金额字段缺失，必须人工修订并绑定证据',
    },
    claim_status: 'blocked',
    fields: review.fields.map((field) =>
      field.path === 'amount_minor'
        ? { ...field, presence: 'absent', value: undefined, evidence: [] }
        : field,
    ),
    validations: [
      {
        id: '00000000-0000-4000-8000-000000000602',
        field_claim_id: review.fields[1]?.id,
        rule_code: 'payment.amount.required',
        severity: 'blocked',
        status: 'blocked',
        safe_message: '金额字段缺失，必须人工修订并绑定证据',
      },
    ],
  }
}

function baseReview(label: string): Review {
  const idSuffix = label === 'job-ready' ? '401' : label === 'job-blocked' ? '402' : '403'
  const documentID = `00000000-0000-4000-8000-000000000${idSuffix}`
  const evidence = {
    id: `00000000-0000-4000-8000-0000000005${idSuffix.slice(-2)}`,
    page: 1,
    quote: 'Synthetic State Merchant CNY 321.09',
  }
  const values: Array<[string, string, unknown]> = [
    ['document_type', 'string', 'payment'],
    ['amount_minor', 'money_minor', 32109],
    ['currency', 'string', 'CNY'],
    ['merchant', 'string', 'Synthetic State Merchant'],
    ['transaction_time', 'instant', '2026-08-28T08:00:00Z'],
    ['source_timezone', 'string', 'Asia/Shanghai'],
  ]
  return {
    job: {
      id: label,
      document_id: documentID,
      original_name: `${label}.png`,
      detected_mime: 'image/png',
      status: 'needs_review',
      attempt_count: 1,
      created_at: timestamp,
      version: 1,
    },
    claim_set_id: `00000000-0000-4000-8000-000000000${Number(idSuffix) + 100}`,
    document_type: 'payment',
    revision: 1,
    optimistic_version: 1,
    claim_status: 'ready_for_review',
    page_count: 1,
    pages: [{ page_number: 1, field_paths: values.map(([path]) => path), item_keys: [] }],
    invoice_item_spans: [],
    fields: values.map(([path, valueType, value], index) => ({
      id: `00000000-0000-4000-8000-${String(800 + Number(idSuffix) + index).padStart(12, '0')}`,
      path,
      value_type: valueType,
      presence: 'present',
      value,
      source: 'ai',
      evidence: path === 'document_type' ? [] : [evidence],
    })),
    validations: [],
    candidates: [],
    duplicate_candidates: [],
  }
}

function syntheticPayment(): Payment {
  return {
    id: '00000000-0000-4000-8000-000000000901',
    amount_minor: 32109,
    allocated_minor: 12345,
    remaining_minor: 19764,
    allocation_status: 'partial',
    currency: 'CNY',
    merchant: 'Synthetic State Merchant',
    transaction_time: timestamp,
    source_timezone: 'Asia/Shanghai',
    payment_method: 'Synthetic Card',
    order_number: 'SYN-STATE-001',
    category: '合成测试',
    created_at: timestamp,
  }
}

function rowFor(queue: ReturnType<Page['locator']>, originalName: string) {
  return queue.locator('tbody tr').filter({ hasText: originalName })
}

function trackPageErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, json: body })
}

async function fulfillError(route: Route, status: number, code: string, message: string) {
  await fulfillJSON(route, { error: { code, message }, request_id: 'synthetic-request-id' }, status)
}
