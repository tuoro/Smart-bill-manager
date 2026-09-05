import { expect, test, type Page, type Route } from '@playwright/test'
import type { AllocationWorkspace, JobSummary, Payment, Review, Session } from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const timestamp = '2026-08-28T08:00:00Z'
const transparentPNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAF/gL+X9jXAAAAAElFTkSuQmCC',
  'base64',
)

test.describe('M1/M2 真实组件状态矩阵', () => {
  test('登录：默认遵循系统深色，手动切换后刷新保留选择', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await page.route(sessionURL, (route) => fulfillError(route, 401, 'unauthenticated', '请先登录'))
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await page.getByRole('button', { name: '切换到浅色模式', exact: true }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    await page.reload()
    await expect(page.getByRole('heading', { name: '登录工作区' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    await expect(page.getByRole('button', { name: '切换到深色模式', exact: true })).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('登录：默认、凭据错误、提交中和权限不足', async ({ page }, testInfo) => {
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
    await captureResponsiveReview(page, testInfo, 'login')

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

  test('AI 收件箱：混合队列覆盖处理、部分结果、待确认、失败和取消', async ({ page }, testInfo) => {
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
      '已生成账单',
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
    await captureResponsiveReview(page, testInfo, 'inbox')
    expect(pageErrors).toEqual([])
  })

  test('AI 收件箱：空状态', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    await mockJobs(page, () => [])

    await page.goto('/inbox')
    await expect(page.locator('section.queue-panel .state-layout')).toContainText('收件箱还是空的')
    await expect(page.getByText('0 个任务')).toBeVisible()
    await expect(page.locator('input[type="file"]')).toHaveCount(1)
    const fileChooser = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '上传第一张单据', exact: true }).click()
    expect((await fileChooser).isMultiple()).toBe(true)
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

  test('AI 收件箱：批量上传保持顺序、逐项反馈并隔离失败', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    await mockJobs(page, () => [])
    const requestNames: string[] = []
    let releaseFirst = () => {}
    const firstResponse = new Promise<void>((resolve) => {
      releaseFirst = resolve
    })

    await page.route(
      (url) => url.pathname === '/api/v1/documents',
      async (route) => {
        const body = route.request().postDataBuffer()?.toString('utf8') ?? ''
        const filename = /filename="([^"]+)"/.exec(body)?.[1] ?? 'missing-filename'
        requestNames.push(filename)
        if (requestNames.length === 1) {
          await firstResponse
          await fulfillJSON(
            route,
            {
              document_id: '20000000-0000-4000-8000-000000000001',
              job_id: '30000000-0000-4000-8000-000000000001',
              status: 'queued',
              sha256: 'a'.repeat(64),
            },
            201,
          )
          return
        }
        if (requestNames.length === 2) {
          await fulfillJSON(
            route,
            {
              error: {
                code: 'duplicate_document',
                message: '该文件已上传',
                resource_id: '20000000-0000-4000-8000-000000000001',
              },
              request_id: 'synthetic-request-id',
            },
            409,
          )
          return
        }
        await fulfillError(
          route,
          400,
          'document_signature_mismatch',
          '文件扩展名、声明类型和文件签名不一致',
        )
      },
    )

    await page.goto('/inbox')
    const input = page.locator('input[type="file"]')
    await input.setInputFiles([
      { name: 'first.png', mimeType: 'image/png', buffer: transparentPNG },
      { name: 'same.png', mimeType: 'image/png', buffer: transparentPNG },
      { name: 'invalid.png', mimeType: 'image/png', buffer: Buffer.from('invalid') },
    ])

    const batch = page.locator('section.batch-panel')
    await expect(batch).toBeVisible()
    await expect(batch.locator('.batch-item')).toHaveCount(3)
    await expect(batch.locator('.batch-item').filter({ hasText: 'first.png' })).toContainText(
      '正在上传',
    )
    await expect(batch.locator('.batch-item').filter({ hasText: 'same.png' })).toContainText(
      '等待上传',
    )
    await expect(input).toBeDisabled()

    releaseFirst()
    await expect(
      batch.getByText('3 个文件处理完成：已入队 1 个，已存在 1 个，已拒绝 1 个'),
    ).toBeVisible()
    await expect(batch.locator('.batch-item').filter({ hasText: 'first.png' })).toContainText(
      '已入队',
    )
    await expect(batch.locator('.batch-item').filter({ hasText: 'same.png' })).toContainText(
      '已存在',
    )
    await expect(batch.locator('.batch-item').filter({ hasText: 'invalid.png' })).toContainText(
      '文件扩展名、声明类型和文件签名不一致',
    )
    await expect(input).toBeEnabled()
    expect(requestNames).toEqual(['first.png', 'same.png', 'invalid.png'])

    for (const viewport of [
      { width: 768, height: 900 },
      { width: 384, height: 900 },
    ]) {
      await page.setViewportSize(viewport)
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      ).toBe(true)
      await expect(batch.locator('.batch-item')).toHaveCount(3)
    }
    await input.focus()
    expect(await input.evaluate((element) => document.activeElement === element)).toBe(true)
    expect(pageErrors).toEqual([])
  })

  test('审核工作台：加载、待审核并完成', async ({ page }, testInfo) => {
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
    await expect(page.getByRole('status')).toContainText('正在加载识别结果与原件')
    releaseReview()

    await expect(page.locator('.review-grid')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: '审核单据' })).toBeVisible()
    await expect(page.locator('.review-document-name')).toHaveText(review.job.original_name)
    await expect(page.getByText('校验通过，待确认')).toBeVisible()
    await expect(page.locator('.inline-validations li')).toHaveAttribute('data-status', 'passed')
    await expect(page.locator('.inline-validations li')).toHaveCSS(
      'color',
      await page
        .locator('.validation-list li[data-status="passed"]')
        .evaluate((element) => getComputedStyle(element).color),
    )
    await expect(page.getByRole('radio', { name: /确认当前没有候选/ })).toBeVisible()

    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    await captureResponsiveReview(page, testInfo, 'review')
    await page.getByRole('button', { name: '确认并保存记录' }).click()
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
    await page.getByRole('button', { name: '确认并保存记录' }).click()

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
    await expect(page.getByRole('button', { name: '确认并保存记录' })).toBeDisabled()
    await expect(page.locator('#duplicate-resolution-error')).toContainText(
      '请逐项确认全部疑似重复候选',
    )
    await expect(page.locator('fieldset[aria-labelledby="duplicate-title"]')).toHaveAttribute(
      'aria-describedby',
      'duplicate-resolution-error',
    )
    await page.getByRole('checkbox', { name: /近似文件.*近似支付截图/ }).check()
    await expect(page.getByRole('button', { name: '确认并保存记录' })).toBeEnabled()
    await page.getByRole('button', { name: '确认并保存记录' }).click()

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
    await expect(page.getByText('当前识别结果未通过校验')).toBeVisible()
    await expect(page.locator('.inline-validations li')).toHaveAttribute('data-status', 'blocked')
    await expect(page.locator('.inline-validations li')).toHaveCSS(
      'color',
      await page
        .locator('.validation-list li[data-status="blocked"]')
        .evaluate((element) => getComputedStyle(element).color),
    )
    await expect(
      page
        .locator('[aria-labelledby="validation-title"]')
        .getByText('金额字段缺失，必须人工修订并绑定证据'),
    ).toBeVisible()
    await expect(page.getByRole('button', { name: '确认并保存记录' })).toBeDisabled()
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
    await page.getByRole('button', { name: '确认并保存记录' }).click()

    const conflict = page.getByRole('alert')
    await expect(conflict).toContainText('审核版本已变化')
    await expect(conflict.getByRole('button', { name: '刷新最新版本' })).toBeVisible()
    await expect(page.locator('.review-grid')).toBeVisible()
    expect(pageErrors).toEqual([])
  })

  test('账单列表：加载、有数据并切换到空状态', async ({ page }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const payment = syntheticPayment()
    let releasePayments = () => {}
    const paymentResponse = new Promise<void>((resolve) => {
      releasePayments = resolve
    })
    await page.route(paymentsURL, async (route) => {
      await paymentResponse
      await fulfillJSON(route, { items: [payment], next_cursor: '' })
    })
    await page.route(invoicesURL, (route) => fulfillJSON(route, { items: [], next_cursor: '' }))

    await page.goto('/payments')
    const facts = page.locator('section.facts-panel')
    await expect(facts.getByRole('status')).toContainText('正在加载支付')
    releasePayments()

    const paymentRow = facts.locator('tbody tr').filter({ hasText: payment.merchant })
    await expect(paymentRow).toContainText('Synthetic State Merchant')
    await expect(paymentRow).toContainText('321.09')
    await expect(paymentRow).toContainText('SYN-STATE-001')
    await expect(facts.getByText('本页 1 条')).toBeVisible()
    await captureResponsiveReview(page, testInfo, 'payments')

    await page.getByRole('tab', { name: '发票' }).click()
    await expect(page.locator('section.facts-panel .state-layout')).toContainText(
      '当前范围没有发票记录',
    )
    await captureResponsiveReview(page, testInfo, 'invoices')
    expect(pageErrors).toEqual([])
  })

  test('分配调整：列表入口提交完整替换计划并刷新权威余额', async ({ page }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const payment = syntheticPayment()
    await page.route(paymentsURL, (route) =>
      fulfillJSON(route, { items: [payment], next_cursor: '' }),
    )
    let workspace = allocationWorkspaceFixture()
    let submitted: unknown
    await page.route(allocationURL(payment.id), async (route) => {
      if (route.request().method() === 'GET') {
        await fulfillJSON(route, workspace)
        return
      }
      submitted = route.request().postDataJSON()
      expect(route.request().headers()['idempotency-key']).toMatch(/^allocation-/)
      expect(route.request().headers()['x-csrf-token']).toBe('synthetic-csrf-token')
      workspace = adjustedAllocationWorkspaceFixture()
      await fulfillJSON(route, {
        adjustment_id: '50000000-0000-4000-8000-000000000001',
        mode: 'replace',
        ended_link_ids: ['40000000-0000-4000-8000-000000000001'],
        created_link_ids: [
          '40000000-0000-4000-8000-000000000002',
          '40000000-0000-4000-8000-000000000003',
        ],
        plan_hash: 'b'.repeat(64),
        replayed: false,
      })
    })

    await page.goto('/payments')
    await page.getByRole('link', { name: '调整分配' }).click()
    await expect(page.getByRole('heading', { name: '调整金额分配' })).toBeVisible()
    await expect(page.getByText('当前已分配').locator('..')).toContainText('4.00')

    const invoiceARow = page.locator('.allocation-target-row').filter({ hasText: '合成发票 A' })
    const invoiceBRow = page.locator('.allocation-target-row').filter({ hasText: '合成发票 B' })
    await invoiceARow.getByLabel('分配金额（最小单位）').fill('500')
    await invoiceBRow.getByRole('checkbox').check()
    await invoiceBRow.getByLabel('分配金额（最小单位）').fill('300')
    await page.getByLabel('调整理由').fill('  人工核对后替换计划  ')
    await page.getByRole('button', { name: '确认替换分配' }).click()

    await expect(page.getByRole('status').filter({ hasText: '替换分配已保存' })).toBeVisible()
    expect(submitted).toEqual({
      expected_plan_hash: 'a'.repeat(64),
      desired_allocations: [
        { target_fact_id: allocationInvoiceA, allocated_minor: 500 },
        { target_fact_id: allocationInvoiceB, allocated_minor: 300 },
      ],
      reason: '人工核对后替换计划',
    })
    await expect(page.getByText('当前已分配').locator('..')).toContainText('8.00')
    await captureResponsiveReview(page, testInfo, 'allocations')

    for (const viewport of [
      { width: 768, height: 900 },
      { width: 384, height: 900 },
    ]) {
      await page.setViewportSize(viewport)
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      ).toBe(true)
      await expect(page.getByRole('button', { name: /确认/ })).toBeVisible()
    }
    await page.getByLabel('调整理由').focus()
    expect(
      await page.getByLabel('调整理由').evaluate((element) => document.activeElement === element),
    ).toBe(true)
    expect(pageErrors).toEqual([])
  })

  test('分配调整：加载后明确展示无合格目标状态', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const payment = syntheticPayment()
    let releaseWorkspace = () => {}
    const workspaceResponse = new Promise<void>((resolve) => {
      releaseWorkspace = resolve
    })
    await page.route(allocationURL(payment.id), async (route) => {
      await workspaceResponse
      await fulfillJSON(route, emptyAllocationWorkspaceFixture())
    })

    await page.goto(`/allocations/payment/${payment.id}`)
    await expect(page.getByRole('status')).toContainText('正在加载当前分配')
    releaseWorkspace()

    await expect(page.getByText('没有可分配的单据')).toBeVisible()
    await expect(page.getByText('可切换全部日期搜索同币种单据；跨期分配须填写理由。')).toBeVisible()
    await expect(page.getByLabel('日期范围')).toHaveValue('recommended')
    await expect(page.getByRole('option', { name: '全部日期（可跨期）' })).toBeAttached()
    await expect(page.getByRole('button', { name: '确认没有变化' })).toBeDisabled()
    expect(pageErrors).toEqual([])
  })

  test('分配调整：撤销全部需二次确认，陈旧冲突保留草稿', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page)
    const payment = syntheticPayment()
    await page.route(allocationURL(payment.id), async (route) => {
      if (route.request().method() === 'GET') {
        await fulfillJSON(route, allocationWorkspaceFixture())
        return
      }
      await fulfillError(route, 409, 'allocation_plan_stale', '分配计划已变化，请刷新后重试')
    })

    await page.goto(`/allocations/payment/${payment.id}`)
    await page.getByRole('checkbox', { name: /合成发票 A/ }).uncheck()
    await page.getByLabel('调整理由').fill('撤销全部合成分配')
    await page.getByRole('button', { name: '确认撤销分配' }).click()
    await expect(page.getByText('撤销全部分配前需要再次确认')).toBeVisible()

    await page.getByRole('checkbox', { name: /确认撤销全部/ }).check()
    await page.getByRole('button', { name: '确认撤销分配' }).click()
    const conflict = page.getByRole('alert')
    await expect(conflict).toContainText('当前草稿已保留')
    await expect(conflict.getByRole('button', { name: '刷新当前分配' })).toBeVisible()
    await expect(page.getByLabel('调整理由')).toHaveValue('撤销全部合成分配')
    await expect(page.getByRole('checkbox', { name: /合成发票 A/ })).not.toBeChecked()
    expect(pageErrors).toEqual([])
  })

  test('分配调整：权限不足不展示计划内容', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, reviewerSession())
    const payment = syntheticPayment()
    await page.route(allocationURL(payment.id), (route) =>
      fulfillError(route, 403, 'forbidden', '当前账号没有执行此操作的权限'),
    )

    await page.goto(`/allocations/payment/${payment.id}`)
    await expect(page.getByText('没有调整分配的权限')).toBeVisible()
    await expect(page.locator('.allocation-target-list')).toHaveCount(0)
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
    await expect(facts).toContainText('当前账号仅可处理授权范围内的资料，请联系管理员调整权限。')
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

function allocationURL(factID: string): (url: URL) => boolean {
  const workspacePath = `/api/v1/allocations/payment/${factID}`
  return (url) => url.pathname === workspacePath || url.pathname === `${workspacePath}/adjustments`
}

function reviewURL(jobID: string): (url: URL) => boolean {
  return (url) => url.pathname === `/api/v1/reviews/${jobID}`
}

function confirmURL(jobID: string): (url: URL) => boolean {
  return (url) => url.pathname === `/api/v1/reviews/${jobID}/confirm`
}

async function mockSession(page: Page, session: Session = ownerSession()) {
  await page.route(sessionURL, (route) =>
    route.fulfill({
      status: 200,
      json: session,
      headers: {
        'Set-Cookie': `sbm_csrf=${encodeURIComponent(session.csrf_token)}; Path=/; SameSite=Strict`,
      },
    }),
  )
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
    capabilities: ['documents.process', 'facts.read', 'allocations.manage', 'providers.manage'],
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
    ingestion_kind: 'upload',
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
      ingestion_kind: 'upload',
      detected_mime: 'image/png',
      status: 'needs_review',
      attempt_count: 1,
      created_at: timestamp,
      version: 1,
    },
    entry_mode: 'ai',
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
    bad_debt: false,
    id: '00000000-0000-4000-8000-000000000901',
    amount_minor: 32109,
    allocated_minor: 12345,
    remaining_minor: 19764,
    allocation_status: 'partial',
    currency: 'CNY',
    merchant: 'Synthetic State Merchant',
    transaction_time: timestamp,
    business_date: '2026-08-28',
    source_timezone: 'Asia/Shanghai',
    payment_method: 'Synthetic Card',
    order_number: 'SYN-STATE-001',
    category: '合成测试',
    created_at: timestamp,
  }
}

const allocationInvoiceA = '20000000-0000-4000-8000-000000000001'
const allocationInvoiceB = '20000000-0000-4000-8000-000000000002'

function allocationWorkspaceFixture(): AllocationWorkspace {
  return {
    anchor: {
      fact_type: 'payment',
      id: syntheticPayment().id,
      amount_minor: syntheticPayment().amount_minor,
      allocated_minor: 400,
      remaining_minor: syntheticPayment().amount_minor - 400,
      currency: 'CNY',
      business_date: '2026-08-28',
      display_name: 'Synthetic State Merchant',
    },
    links: [
      {
        id: '40000000-0000-4000-8000-000000000001',
        target_fact_type: 'invoice',
        target_fact_id: allocationInvoiceA,
        allocated_minor: 400,
        currency: 'CNY',
        created_at: timestamp,
      },
    ],
    targets: [
      allocationTarget(allocationInvoiceA, '合成发票 A', 600, 400, 400, 600, true, 0),
      allocationTarget(allocationInvoiceB, '合成发票 B', 700, 0, 0, 700, false, 1),
    ],
    plan_hash: 'a'.repeat(64),
  }
}

function adjustedAllocationWorkspaceFixture(): AllocationWorkspace {
  return {
    ...allocationWorkspaceFixture(),
    anchor: {
      ...allocationWorkspaceFixture().anchor,
      allocated_minor: 800,
      remaining_minor: syntheticPayment().amount_minor - 800,
    },
    links: [
      {
        id: '40000000-0000-4000-8000-000000000002',
        target_fact_type: 'invoice',
        target_fact_id: allocationInvoiceA,
        allocated_minor: 500,
        currency: 'CNY',
        created_at: timestamp,
      },
      {
        id: '40000000-0000-4000-8000-000000000003',
        target_fact_type: 'invoice',
        target_fact_id: allocationInvoiceB,
        allocated_minor: 300,
        currency: 'CNY',
        created_at: timestamp,
      },
    ],
    targets: [
      allocationTarget(allocationInvoiceA, '合成发票 A', 600, 500, 500, 600, true, 0),
      allocationTarget(allocationInvoiceB, '合成发票 B', 700, 300, 300, 700, false, 1),
    ],
    plan_hash: 'b'.repeat(64),
  }
}

function emptyAllocationWorkspaceFixture(): AllocationWorkspace {
  return {
    ...allocationWorkspaceFixture(),
    anchor: {
      ...allocationWorkspaceFixture().anchor,
      allocated_minor: 0,
      remaining_minor: syntheticPayment().amount_minor,
    },
    links: [],
    targets: [],
    plan_hash: 'c'.repeat(64),
  }
}

function allocationTarget(
  id: string,
  displayName: string,
  amountMinor: number,
  allocatedMinor: number,
  currentAllocatedMinor: number,
  maximumAllocatableMinor: number,
  nameExact: boolean,
  dateDistanceDays: number,
): AllocationWorkspace['targets'][number] {
  return {
    fact_type: 'invoice',
    id,
    amount_minor: amountMinor,
    allocated_minor: allocatedMinor,
    remaining_minor: amountMinor - allocatedMinor,
    currency: 'CNY',
    business_date: dateDistanceDays === 0 ? '2026-08-28' : '2026-08-29',
    display_name: displayName,
    name_exact: nameExact,
    date_distance_days: dateDistanceDays,
    current_link_id: currentAllocatedMinor
      ? currentAllocatedMinor === 400
        ? '40000000-0000-4000-8000-000000000001'
        : id === allocationInvoiceA
          ? '40000000-0000-4000-8000-000000000002'
          : '40000000-0000-4000-8000-000000000003'
      : undefined,
    current_allocated_minor: currentAllocatedMinor,
    maximum_allocatable_minor: maximumAllocatableMinor,
  }
}

function rowFor(queue: ReturnType<Page['locator']>, originalName: string) {
  return queue.locator('tbody tr').filter({ hasText: originalName })
}

test.describe('B1 显式人工录入', () => {
  test('失败转人工、填写证据、保存刷新和确认', async ({ page }, testInfo) => {
    const errors = trackPageErrors(page)
    await mockSession(page, {
      ...ownerSession(),
      capabilities: [...ownerSession().capabilities, 'claims.review'],
    })
    const failed = job('failed', 'synthetic-manual.png', '合成识别失败')
    let current = manualReviewFixture(failed)
    let confirmations = 0
    await mockJobs(page, () => [failed])
    await mockDocumentContent(page, failed.document_id)
    await page.route(reviewURL(failed.id), (route) => fulfillJSON(route, current))
    await page.route(
      (url) => url.pathname === `/api/v1/jobs/${failed.id}/manual-review`,
      async (route) => {
        expect(route.request().postDataJSON()).toEqual({
          document_type: 'payment',
          reason: '原件清晰，改为人工录入',
          expected_job_version: failed.version,
        })
        expect(route.request().headers()['idempotency-key']).toBeTruthy()
        await fulfillJSON(route, {
          job_id: failed.id,
          claim_set_id: current.claim_set_id,
          replayed: false,
        })
      },
    )
    await page.route(
      (url) => url.pathname === `/api/v1/reviews/${failed.id}/revisions`,
      async (route) => {
        const payload = route.request().postDataJSON()
        expect(
          payload.fields.find((field: { path: string }) => field.path === 'merchant'),
        ).toMatchObject({
          manual_evidence: [{ page: 1, quote: '用户核对商户的实际摘录' }],
          evidence_ids: [],
        })
        current = {
          ...current,
          revision: 2,
          optimistic_version: 1,
          claim_status: 'ready_for_review',
          validations: [],
          job: { ...current.job, status: 'needs_review', version: 3 },
          fields: current.fields.map((field) => {
            const edited = payload.fields.find(
              (entry: { path: string }) => entry.path === field.path,
            )
            if (!edited) return field
            return {
              ...field,
              presence: edited.presence,
              value: edited.value,
              evidence: (edited.manual_evidence ?? []).map(
                (evidence: { page: number; quote: string }) => ({
                  ...evidence,
                  id: `${field.id}-manual-evidence`,
                }),
              ),
            }
          }),
        }
        await route.fulfill({ status: 201, json: current })
      },
    )
    await page.route(confirmURL(failed.id), async (route) => {
      confirmations++
      await fulfillJSON(route, {
        review_decision_id: 'manual-review-decision',
        fact_type: 'payment',
        fact_id: 'manual-payment',
        link_ids: [],
        replayed: false,
      })
    })
    await page.goto('/inbox')
    await page.getByRole('button', { name: '转人工录入', exact: true }).click()
    const form = page.locator('section[aria-labelledby="manual-review-title"]')
    await form.getByRole('combobox', { name: '单据类型', exact: true }).selectOption('payment')
    await form.getByLabel('接管理由').fill('原件清晰，改为人工录入')
    await form.getByRole('button', { name: '确认转人工', exact: true }).click()
    await expect(page.getByText('已转人工', { exact: true })).toBeVisible()
    await expect(page.getByText('AI 提取', { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '修订字段', exact: true }).click()
    for (const [label, value] of [
      ['支付金额（最小单位）', '32109'],
      ['币种', 'CNY'],
      ['商户', '合成人工商户'],
      ['交易时间', '2026-08-28T08:00:00Z'],
      ['来源时区', 'Asia/Shanghai'],
    ]) {
      await page.getByLabel(`${label} 是否存在`, { exact: true }).selectOption('present')
      await page.getByLabel(label!, { exact: true }).fill(value!)
      if (label !== '来源时区') {
        await page.getByLabel(`${label} 来源页码`, { exact: true }).fill('1')
        await page
          .getByLabel(`${label} 原件摘录`, { exact: true })
          .fill(label === '商户' ? '用户核对商户的实际摘录' : '用户核对的实际摘录')
      }
    }
    await captureResponsiveReview(page, testInfo, 'manual-review-edit')
    expect(confirmations).toBe(0)
    await page.getByRole('button', { name: '保存修订版本', exact: true }).click()
    await expect(page.getByRole('button', { name: '修订字段', exact: true })).toBeVisible()
    await page.reload()
    await expect(page.getByText('合成人工商户', { exact: true })).toBeVisible()
    await expect(page.getByText('已转人工', { exact: true })).toBeVisible()
    await page.getByRole('radio', { name: /确认当前没有候选/ }).check()
    await page.getByRole('button', { name: '确认并保存记录', exact: true }).click()
    await expect(page.getByRole('heading', { name: '正式账单已创建' })).toBeVisible()
    expect(confirmations).toBe(1)
    expect(errors).toEqual([])
  })

  test('人工修订冲突刷新保留字段与摘录，重新核对后才可提交', async ({ page }) => {
    await mockSession(page)
    const failed = job('failed', 'manual-revision-conflict.png')
    let current = manualReviewFixture(failed)
    await mockReview(page, current)
    await page.route(
      (url) => url.pathname === `/api/v1/reviews/${failed.id}`,
      (route) => fulfillJSON(route, current),
    )
    let saves = 0
    await page.route(
      (url) => url.pathname === `/api/v1/reviews/${failed.id}/revisions`,
      async (route) => {
        saves++
        if (saves === 1) {
          current = {
            ...current,
            revision: 2,
            fields: current.fields.map((field) =>
              field.path === 'merchant'
                ? { ...field, presence: 'present', value: '服务器新商户' }
                : field,
            ),
          }
          await fulfillError(route, 409, 'version_conflict', '版本已变化')
          return
        }
        expect(route.request().postDataJSON()).toMatchObject({
          expected_revision: 2,
          fields: expect.arrayContaining([
            {
              path: 'merchant',
              value_type: 'string',
              presence: 'present',
              value: '保留我的商户',
              evidence_ids: [],
              manual_evidence: [{ page: 1, quote: '保留我的摘录' }],
            },
          ]),
        })
        await route.fulfill({ status: 201, json: { ...current, revision: 3 } })
      },
    )
    await page.goto(`/reviews/${failed.id}`)
    await page.getByRole('button', { name: '修订字段', exact: true }).click()
    await page.getByLabel('商户 是否存在', { exact: true }).selectOption('present')
    await page.getByLabel('商户', { exact: true }).fill('保留我的商户')
    await page.getByLabel('商户 来源页码', { exact: true }).fill('1')
    await page.getByLabel('商户 原件摘录', { exact: true }).fill('保留我的摘录')
    const save = page.getByRole('button', { name: '保存修订版本', exact: true })
    await save.click()
    await expect(save).toBeDisabled()
    await page.getByRole('button', { name: '刷新最新版本', exact: true }).click()
    await expect(page.getByLabel('商户', { exact: true })).toHaveValue('保留我的商户')
    await expect(page.getByLabel('商户 来源页码', { exact: true })).toHaveValue('1')
    await expect(page.getByLabel('商户 原件摘录', { exact: true })).toHaveValue('保留我的摘录')
    await expect(save).toBeDisabled()
    await page.getByText('查看服务器最新版本', { exact: false }).click()
    await expect(page.getByText('服务器新商户', { exact: true })).toBeVisible()
    await page.getByLabel('我已比较最新版本，确认继续使用当前草稿').check()
    await save.click()
    await expect(page.getByRole('button', { name: '修订字段', exact: true })).toBeVisible()
    expect(saves).toBe(2)
  })

  for (const outcome of ['version_conflict', 'response_lost'] as const) {
    test(`转人工 ${outcome} 保留输入并显式重试`, async ({ page }) => {
      await mockSession(page, {
        ...ownerSession(),
        capabilities: [...ownerSession().capabilities, 'claims.review'],
      })
      const failed = job('failed', `manual-${outcome}.png`)
      await mockJobs(page, () => [failed])
      await mockReview(page, manualReviewFixture(failed))
      const requests: Array<{ body: unknown; key: string | undefined }> = []
      await page.route(
        (url) => url.pathname === `/api/v1/jobs/${failed.id}`,
        (route) => fulfillJSON(route, { ...failed, version: 2 }),
      )
      await page.route(
        (url) => url.pathname === `/api/v1/jobs/${failed.id}/manual-review`,
        async (route) => {
          requests.push({
            body: route.request().postDataJSON(),
            key: route.request().headers()['idempotency-key'],
          })
          if (requests.length === 1) {
            if (outcome === 'response_lost') await route.abort('failed')
            else await fulfillError(route, 409, 'version_conflict', '任务版本已变化')
            return
          }
          await fulfillJSON(route, {
            job_id: failed.id,
            claim_set_id: 'manual-root',
            replayed: outcome === 'response_lost',
          })
        },
      )
      await page.goto('/inbox')
      await page.getByRole('button', { name: '转人工录入', exact: true }).click()
      const form = page.locator('section[aria-labelledby="manual-review-title"]')
      await form.getByRole('combobox', { name: '单据类型', exact: true }).selectOption('payment')
      await form.getByLabel('接管理由').fill('保留这次人工选择')
      await form.getByRole('button', { name: '确认转人工', exact: true }).click()
      await expect(form.getByRole('alert')).toBeVisible()
      await expect(form.getByLabel('接管理由')).toHaveValue('保留这次人工选择')
      if (outcome === 'version_conflict') {
        await expect(form.getByRole('button', { name: '确认转人工', exact: true })).toBeDisabled()
        await form.getByRole('button', { name: '刷新任务状态' }).click()
        await expect(form.getByRole('status')).toContainText('类型和理由已保留')
      }
      expect(requests).toHaveLength(1)
      await form.getByRole('button', { name: '确认转人工', exact: true }).click()
      await expect(page.getByRole('heading', { name: '审核单据', exact: true })).toBeVisible()
      expect(requests).toHaveLength(2)
      if (outcome === 'response_lost') expect(requests[1]).toEqual(requests[0])
      else {
        expect(requests[1]!.key).not.toBe(requests[0]!.key)
        expect(requests[1]!.body).toMatchObject({
          expected_job_version: 2,
          reason: '保留这次人工选择',
        })
      }
    })
  }
  test('无审核能力时不展示转人工入口', async ({ page }) => {
    await mockSession(page, reviewerSession())
    await mockJobs(page, () => [job('failed', 'manual-forbidden.png')])
    await page.goto('/inbox')
    await expect(page.getByText('manual-forbidden.png', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '转人工录入', exact: true })).toHaveCount(0)
  })
})

function manualReviewFixture(failed: JobSummary): Review {
  const review = blockedReview('manual-root')
  return {
    ...review,
    job: { ...failed, status: 'blocked', version: 2 },
    entry_mode: 'manual',
    fields: review.fields.map((field) => ({
      ...field,
      presence: field.path === 'document_type' ? 'present' : 'absent',
      value: field.path === 'document_type' ? 'payment' : undefined,
      source: 'user',
      evidence: [],
    })),
  }
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
