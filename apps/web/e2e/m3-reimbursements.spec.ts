import { expect, test, type Page, type Route } from '@playwright/test'
import type {
  ReimbursementDetail,
  ReimbursementPolicySnapshot,
  ReimbursementStatusRequest,
  ReimbursementSubmissionRequest,
  ReimbursementSummary,
  Session,
  Trip,
  TripAttributionCandidate,
} from '../src/data/client'
import { captureResponsiveReview } from './visual-review'

const timestamp = '2026-08-31T08:00:00Z'
const tripID = '00000000-0000-4000-8000-000000000701'
const existingID = '00000000-0000-4000-8000-000000000702'
const createdID = '00000000-0000-4000-8000-000000000703'
const relatedID = '00000000-0000-4000-8000-000000000704'
const paymentAssignmentID = '00000000-0000-4000-8000-000000000711'
const invoiceAssignmentID = '00000000-0000-4000-8000-000000000712'
const paymentID = '00000000-0000-4000-8000-000000000721'
const invoiceID = '00000000-0000-4000-8000-000000000722'
const findingKey = 'a'.repeat(64)

test.describe('M3 报销快照与状态历史真实组件状态矩阵', () => {
  test('Owner：明确选择、完整提示确认、不可变提交与冲突草稿保留', async ({ page }, testInfo) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(
      page,
      session('owner', ['facts.read', 'reimbursements.read', 'reimbursements.manage']),
    )
    const trip = syntheticTrip()
    const candidates = assignedCandidates()
    let summaries: ReimbursementSummary[] = [summary(existingID, 'reimbursed', 2, 2, 0)]
    const details = new Map<string, ReimbursementDetail>([
      [existingID, reimbursementDetail(existingID, 'reimbursed', 2, 0)],
    ])
    const submitted: ReimbursementSubmissionRequest[] = []
    const statusRequests: ReimbursementStatusRequest[] = []
    const idempotencyKeys: string[] = []
    let statusAttempts = 0

    await page.route(sessionURL, (route) => fulfillJSON(route, ownerSession()))
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
    await page.route(tripCandidatesURL, (route) =>
      fulfillJSON(route, { trip, rule_version: 'trip-attribution/1', items: candidates }),
    )
    await page.route(previewURL, (route) => fulfillJSON(route, reimbursementPreview()))
    await page.route(reimbursementsURL, async (route) => {
      if (route.request().method() === 'GET') {
        await fulfillJSON(route, { items: summaries })
        return
      }
      const body = route.request().postDataJSON() as ReimbursementSubmissionRequest
      submitted.push(body)
      idempotencyKeys.push(route.request().headers()['idempotency-key'] ?? '')
      const created = reimbursementDetail(createdID, 'submitted', 1, 1)
      summaries = [summary(createdID, 'submitted', 1, 2, 1), ...summaries]
      details.set(createdID, created)
      await fulfillJSON(
        route,
        {
          reimbursement_id: createdID,
          decision_id: created.decisions[0].id,
          status: 'submitted',
          version: 1,
          replayed: false,
        },
        201,
      )
    })
    await page.route(reimbursementDetailURL, async (route) => {
      const id = route.request().url().split('/').at(-1) ?? ''
      const current = details.get(id)
      if (!current) {
        await fulfillError(route, 404, 'not_found', '资源不存在')
        return
      }
      await fulfillJSON(route, current)
    })
    await page.route(reimbursementStatusURL, async (route) => {
      const body = route.request().postDataJSON() as ReimbursementStatusRequest
      statusRequests.push(body)
      idempotencyKeys.push(route.request().headers()['idempotency-key'] ?? '')
      statusAttempts += 1
      const current = details.get(createdID)!
      if (statusAttempts === 1) {
        const changed = {
          ...current,
          status: 'rejected' as const,
          version: 2,
          updated_at: '2026-08-31T09:00:00Z',
          decisions: [
            ...current.decisions,
            decision('reject', 'submitted', 'rejected', 1, 2, '其他窗口已退回'),
          ],
        }
        details.set(createdID, changed)
        summaries = [summary(createdID, 'rejected', 2, 2, 1), summaries[1]]
        await fulfillError(route, 409, 'reimbursement_status_stale', '状态已变化')
        return
      }
      const reopened = {
        ...current,
        status: 'submitted' as const,
        version: 3,
        updated_at: '2026-08-31T10:00:00Z',
        decisions: [
          ...current.decisions,
          decision('reopen', 'rejected', 'submitted', 2, 3, body.reason),
        ],
      }
      details.set(createdID, reopened)
      summaries = [summary(createdID, 'submitted', 3, 2, 1), summaries[1]]
      await fulfillJSON(route, {
        reimbursement_id: createdID,
        decision_id: reopened.decisions.at(-1)!.id,
        status: 'submitted',
        version: 3,
        replayed: false,
      })
    })

    await page.goto('/reimbursements')
    await expect(page.getByRole('heading', { name: '报销管理', exact: true })).toBeVisible()
    const payment = page.getByRole('checkbox', { name: /支付 · 合成交通商户/ })
    const invoice = page.getByRole('checkbox', { name: /发票 · 合成住宿发票/ })
    await expect(payment).not.toBeChecked()
    await expect(invoice).not.toBeChecked()
    await expect(page.getByText('已选 0 / 2')).toBeVisible()

    await payment.check()
    await invoice.check()
    await page.getByRole('button', { name: '运行政策预检' }).click()
    await expect(page.getByText('1 条政策提示')).toBeVisible()
    await expect(page.getByText('该单据已出现在其他有效报销中')).toBeVisible()
    await page.getByLabel('提交理由').fill(' 合成报销提交 ')
    await expect(page.getByRole('button', { name: '提交报销' })).toBeDisabled()
    await page.getByRole('checkbox', { name: /我已逐项核对/ }).check()
    await expect(page.getByRole('button', { name: '提交报销' })).toBeEnabled()
    await page.getByRole('button', { name: '提交报销' }).click()
    await expect(page.locator('.notice-success')).toContainText('报销快照已提交')
    expect(submitted).toEqual([
      {
        trip_id: tripID,
        assignment_ids: [invoiceAssignmentID, paymentAssignmentID].sort(),
        expected_snapshot_hash: 'b'.repeat(64),
        acknowledged_finding_keys: [findingKey],
        reason: '合成报销提交',
      },
    ])
    expect(idempotencyKeys[0]).toMatch(/^[0-9a-f-]{36}$/)

    const statusReason = page.getByLabel('状态变化理由')
    await statusReason.fill(' 保留这个状态理由 ')
    await page.getByRole('button', { name: '标记已报销' }).click()
    await expect(page.getByRole('alert')).toContainText('保留理由草稿')
    await expect(statusReason).toHaveValue(' 保留这个状态理由 ')
    await expect(page.getByRole('button', { name: '重新打开' })).toBeVisible()
    pageErrors.length = 0
    await page.getByRole('button', { name: '重新打开' }).click()
    await expect(page.locator('.notice-success')).toContainText('报销状态已更新')
    expect(statusRequests).toEqual([
      {
        expected_status: 'submitted',
        desired_status: 'reimbursed',
        expected_version: 1,
        reason: '保留这个状态理由',
      },
      {
        expected_status: 'rejected',
        desired_status: 'submitted',
        expected_version: 2,
        reason: '保留这个状态理由',
      },
    ])
    expect(idempotencyKeys[1]).not.toBe(idempotencyKeys[2])
    await expect(page.getByText('提交时政策提示')).toBeVisible()
    await expect(page.getByText('处理历史')).toBeVisible()
    await captureResponsiveReview(page, testInfo, 'reimbursements')

    for (const width of [768, 384]) {
      await page.setViewportSize({ width, height: 1000 })
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
        ),
      ).toBe(true)
      await page.getByRole('button', { name: '刷新' }).focus()
      await expect(page.getByRole('button', { name: '刷新' })).toBeFocused()
    }
    expect(pageErrors).toEqual([])
  })

  test('Viewer 只读，Reviewer 直接访问不发起报销请求', async ({ page }) => {
    await mockSession(page, session('viewer', ['facts.read', 'reimbursements.read']))
    let viewerRequests = 0
    await page.route(reimbursementsURL, async (route) => {
      viewerRequests += 1
      await fulfillJSON(route, { items: [summary(existingID, 'reimbursed', 2, 2, 0)] })
    })
    await page.route(reimbursementDetailURL, (route) =>
      fulfillJSON(route, reimbursementDetail(existingID, 'reimbursed', 2, 0)),
    )
    await page.goto('/reimbursements')
    await expect(page.getByText('当前账号为只读')).toBeVisible()
    await expect(page.getByRole('heading', { name: '新建报销', exact: true })).toHaveCount(0)
    await expect(page.getByLabel('状态变化理由')).toHaveCount(0)
    expect(viewerRequests).toBe(1)

    const reviewerPage = await page.context().newPage()
    await mockSession(reviewerPage, session('reviewer', ['documents.process', 'claims.review']))
    let forbiddenRequests = 0
    await reviewerPage.route(reimbursementsURL, async (route) => {
      forbiddenRequests += 1
      await fulfillJSON(route, { items: [] })
    })
    await reviewerPage.goto('/reimbursements')
    await expect(reviewerPage.getByText('没有查看报销的权限')).toBeVisible()
    expect(forbiddenRequests).toBe(0)
    await reviewerPage.close()
  })

  test('Viewer：三类提示、混合币种与已删除来源仍可完整解释', async ({ page }) => {
    await mockSession(page, session('viewer', ['facts.read', 'reimbursements.read']))
    const rich = richReimbursementDetail()
    await page.route(reimbursementsURL, (route) =>
      fulfillJSON(route, {
        items: [{ ...summary(existingID, 'reimbursed', 2, 3, 3), trip_deleted: true }],
      }),
    )
    await page.route(reimbursementDetailURL, (route) => fulfillJSON(route, rich))

    await page.goto('/reimbursements')
    await expect(page.getByText('原行程已删除')).toBeVisible()
    await expect(page.getByText(/原单据已删除/)).toBeVisible()
    await expect(page.getByText('所选支付缺少所选发票')).toBeVisible()
    await expect(page.getByText('当前选择中没有与该支付相连的活动发票分配。')).toBeVisible()
    await expect(page.getByText('所选支付与发票的分配金额不一致')).toBeVisible()
    await expect(page.getByText(/本次所选分配合计/)).toContainText('CNY 100.00')
    await expect(page.getByText('该单据已出现在其他有效报销中')).toBeVisible()
    await expect(page.getByText(new RegExp(`关联报销 ${relatedID} · 已报销`))).toBeVisible()
    await expect(page.getByText('CNY 快照合计')).toBeVisible()
    await expect(page.getByText('USD 快照合计')).toBeVisible()
  })

  test('Finance：分页、无提示提交与无需伪确认', async ({ page }) => {
    await mockSession(
      page,
      session('finance', ['facts.read', 'reimbursements.read', 'reimbursements.manage']),
    )
    const trip = syntheticTrip()
    const candidates = assignedCandidates()
    let submitted = false
    let submission: ReimbursementSubmissionRequest | undefined
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
    await page.route(tripCandidatesURL, (route) =>
      fulfillJSON(route, { trip, rule_version: 'trip-attribution/1', items: candidates }),
    )
    await page.route(previewURL, (route) =>
      fulfillJSON(route, {
        ...reimbursementPreview(),
        findings: [],
        snapshot_hash: 'c'.repeat(64),
      }),
    )
    await page.route(reimbursementsURL, async (route) => {
      if (route.request().method() === 'POST') {
        submission = route.request().postDataJSON() as ReimbursementSubmissionRequest
        submitted = true
        await fulfillJSON(
          route,
          {
            reimbursement_id: createdID,
            decision_id: crypto.randomUUID(),
            status: 'submitted',
            version: 1,
            replayed: false,
          },
          201,
        )
        return
      }
      const cursor = new URL(route.request().url()).searchParams.get('cursor')
      if (submitted) {
        await fulfillJSON(route, { items: [summary(createdID, 'submitted', 1, 2, 0)] })
      } else if (cursor === 'next-page') {
        await fulfillJSON(route, { items: [summary(createdID, 'rejected', 2, 2, 0)] })
      } else {
        await fulfillJSON(route, {
          items: [summary(existingID, 'reimbursed', 2, 2, 0)],
          next_cursor: 'next-page',
        })
      }
    })
    await page.route(reimbursementDetailURL, (route) => {
      const id = route.request().url().split('/').at(-1) ?? ''
      return fulfillJSON(
        route,
        reimbursementDetail(
          id,
          id === createdID ? 'submitted' : 'reimbursed',
          id === createdID ? 1 : 2,
          0,
        ),
      )
    })

    await page.goto('/reimbursements')
    await page.getByRole('button', { name: '加载更多记录' }).click()
    await expect(page.getByText('2 条已加载记录')).toBeVisible()
    await page.getByRole('checkbox', { name: /支付 · 合成交通商户/ }).check()
    await page.getByRole('checkbox', { name: /发票 · 合成住宿发票/ }).check()
    await page.getByRole('button', { name: '运行政策预检' }).click()
    await expect(page.getByText('未发现政策提示，可以继续填写提交理由。')).toBeVisible()
    await expect(page.getByRole('checkbox', { name: /我已逐项核对/ })).toHaveCount(0)
    await page.getByLabel('提交理由').fill('无提示合成提交')
    await expect(page.getByRole('button', { name: '提交报销' })).toBeEnabled()
    await page.getByRole('button', { name: '提交报销' }).click()
    await expect(page.locator('.notice-success')).toContainText('报销快照已提交')
    expect(submission?.acknowledged_finding_keys).toEqual([])
    expect(submission?.expected_snapshot_hash).toBe('c'.repeat(64))
  })

  test('详情加载失败即使没有详情也显示错误，刷新后可以恢复', async ({ page }) => {
    const pageErrors = trackPageErrors(page)
    await mockSession(page, session('viewer', ['facts.read', 'reimbursements.read']))
    await page.route(reimbursementsURL, (route) =>
      fulfillJSON(route, { items: [summary(existingID, 'reimbursed', 2, 2, 0)] }),
    )
    let attempts = 0
    await page.route(reimbursementDetailURL, async (route) => {
      attempts += 1
      if (attempts === 1) {
        await fulfillError(route, 503, 'unavailable', '合成报销详情暂时不可用')
        return
      }
      await fulfillJSON(route, reimbursementDetail(existingID, 'reimbursed', 2, 0))
    })
    await page.goto('/reimbursements')
    const detail = page.locator('.reimbursement-detail')
    await expect(detail.getByRole('alert')).toHaveText('合成报销详情暂时不可用')
    await expect(detail.getByRole('heading', { name: '快照项目' })).toHaveCount(0)
    await expect(detail.getByText('请选择一条报销记录')).toBeVisible()
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(detail.getByRole('alert')).toHaveCount(0)
    await expect(detail.getByRole('heading', { name: '快照项目' })).toBeVisible()
    expect(attempts).toBe(2)
    expect(pageErrors).toEqual([
      'console: Failed to load resource: the server responded with a status of 503 (Service Unavailable)',
    ])
  })

  test('加载失败可重试，空状态与离线门禁明确', async ({ page }) => {
    await mockSession(
      page,
      session('owner', ['facts.read', 'reimbursements.read', 'reimbursements.manage']),
    )
    let pendingRoute: Route | undefined
    let attempts = 0
    await page.route(reimbursementsURL, async (route) => {
      attempts += 1
      if (attempts === 1) {
        pendingRoute = route
        return
      }
      await fulfillJSON(route, { items: [] })
    })
    await page.route(tripsURL, (route) => fulfillJSON(route, { items: [] }))

    await page.goto('/reimbursements')
    await expect(page.getByText('正在读取报销工作台')).toBeVisible()
    await expect.poll(() => Boolean(pendingRoute)).toBe(true)
    await fulfillError(pendingRoute!, 503, 'unavailable', '合成服务暂不可用')
    await expect(page.getByRole('alert')).toContainText('合成服务暂不可用')
    await page.getByRole('button', { name: '重试' }).click()
    await expect(page.getByText('还没有报销记录')).toBeVisible()
    await expect(page.getByText('没有可用行程')).toBeVisible()

    await page.context().setOffline(true)
    await expect(page.getByText('当前离线。理由草稿会保留，恢复联网后可刷新。')).toBeVisible()
    await expect(page.getByRole('button', { name: '刷新' })).toBeDisabled()
    await page.context().setOffline(false)
  })
})

function syntheticTrip(): Trip {
  return {
    bad_debt_locked: false,
    id: tripID,
    name: '北京',
    timezone: 'Asia/Shanghai',
    version: 1,
    notes: '',
    origin_kind: 'manual',
    material_count: 0,
    start_date: '2026-08-26',
    end_date: '2026-08-28',
    assigned_payment_count: 1,
    assigned_invoice_count: 1,
    created_at: timestamp,
  }
}

function assignedCandidates(): TripAttributionCandidate[] {
  return [
    {
      fact_type: 'payment',
      fact_id: paymentID,
      display_name: '合成交通商户',
      business_date: '2026-08-27',
      amount_minor: 12345,
      currency: 'CNY',
      current_assignment_id: paymentAssignmentID,
      current_trip_id: tripID,
      current_trip_name: '北京',
      fact_version: 1,
      assignment_mode: 'manual',
      assignment_state: 'manual',
      match_count: 1,
      suggested: true,
      reason_codes: ['currently_assigned'],
    },
    {
      fact_type: 'invoice',
      fact_id: invoiceID,
      display_name: '合成住宿发票',
      business_date: '2026-08-27',
      amount_minor: 12345,
      currency: 'CNY',
      current_assignment_id: invoiceAssignmentID,
      current_trip_id: tripID,
      current_trip_name: '北京',
      fact_version: 1,
      assignment_mode: 'manual',
      assignment_state: 'manual',
      match_count: 0,
      suggested: true,
      reason_codes: ['currently_assigned'],
    },
  ]
}

test('B4：迟到预检不覆盖新选择，材料冲突保留理由，提交期间锁定创建输入', async ({ page }) => {
  await mockSession(page, ownerSession())
  const trip = syntheticTrip()
  await page.route(tripsURL, (route) => fulfillJSON(route, { items: [trip] }))
  await page.route(tripCandidatesURL, (route) =>
    fulfillJSON(route, { trip, rule_version: 'trip-attribution/1', items: assignedCandidates() }),
  )
  let releasePreview!: () => void, releaseSubmit!: () => void
  const previewWait = new Promise<void>((resolve) => {
    releasePreview = resolve
  })
  const submitWait = new Promise<void>((resolve) => {
    releaseSubmit = resolve
  })
  let previews = 0,
    submitted = false
  await page.route(previewURL, async (route) => {
    previews++
    if (previews === 1) await previewWait
    return fulfillJSON(route, {
      ...reimbursementPreview(),
      findings: [],
      materials: [{ invoice_id: invoiceID, link_id: createdID, document_id: relatedID }],
    })
  })
  await page.route(reimbursementsURL, async (route) => {
    if (route.request().method() === 'GET') return fulfillJSON(route, { items: [] })
    submitted = true
    await submitWait
    return fulfillError(route, 409, 'reimbursement_snapshot_stale', '材料集合已变化')
  })
  await page.goto('/reimbursements')
  const payment = page.getByRole('checkbox', { name: /支付 · 合成交通商户/ })
  const invoice = page.getByRole('checkbox', { name: /发票 · 合成住宿发票/ })
  await payment.check()
  await page.getByRole('button', { name: '运行政策预检' }).click()
  await expect.poll(() => previews).toBe(1)
  await invoice.check()
  const finished = page.waitForResponse((response) => previewURL(new URL(response.url())))
  releasePreview()
  await finished
  await expect(page.locator('.reimbursement-preview')).toHaveCount(0)
  await page.getByRole('button', { name: '运行政策预检' }).click()
  await expect(page.locator('.reimbursement-preview')).toContainText('1 份辅助材料')
  await page.getByLabel('提交理由').fill('合成材料变更后保留的理由')
  await page.getByRole('button', { name: '提交报销', exact: true }).click()
  await expect.poll(() => submitted).toBe(true)
  await expect(page.getByRole('combobox', { name: '行程', exact: true })).toBeDisabled()
  await expect(payment).toBeDisabled()
  await expect(invoice).toBeDisabled()
  await expect(page.getByLabel('提交理由')).toBeDisabled()
  await expect(page.getByRole('button', { name: '运行政策预检' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  releaseSubmit()
  await expect(
    page.getByText('报销输入或当前状态已变化，请重新预检；提交理由已保留。'),
  ).toBeVisible()
  await page.getByRole('button', { name: '运行政策预检' }).click()
  await expect(page.getByLabel('提交理由')).toHaveValue('合成材料变更后保留的理由')
})

test('B4：迟到报销详情不能把辅助材料状态切回上一条', async ({ page }) => {
  await mockSession(page, session('viewer', ['reimbursements.read']))
  const a = {
    ...reimbursementDetail(existingID, 'submitted', 1, 0),
    trip: { ...reimbursementPreview().trip, name: '合成先选记录' },
    materials_captured: true,
    material_count: null,
  }
  const b = {
    ...reimbursementDetail(relatedID, 'rejected', 2, 0),
    trip: { ...reimbursementPreview().trip, name: '合成后选记录' },
    materials_captured: false,
    material_count: null,
  }
  await page.route(reimbursementsURL, (route) => fulfillJSON(route, { items: [a, b] }))
  let release!: () => void,
    requested = false
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route(reimbursementDetailURL, async (route) => {
    if (route.request().url().endsWith(relatedID)) {
      requested = true
      await pending
      return fulfillJSON(route, b)
    }
    return fulfillJSON(route, a)
  })
  await page.goto('/reimbursements')
  await expect(page.locator('.reimbursement-detail h2')).toHaveText('合成先选记录')
  await page.locator('.reimbursement-list button').nth(1).click()
  await expect.poll(() => requested).toBe(true)
  await page.locator('.reimbursement-list button').nth(0).click()
  await expect(page.locator('.reimbursement-detail h2')).toHaveText('合成先选记录')
  const done = page.waitForResponse((response) => response.url().endsWith(relatedID))
  release()
  await done
  await expect(page.locator('.reimbursement-detail h2')).toHaveText('合成先选记录')
  await expect(
    page.getByText('辅助材料集合已在提交时固定；当前账号无权查看数量和原件。'),
  ).toBeVisible()
})

function reimbursementPreview(): ReimbursementPolicySnapshot {
  const [payment, invoice] = assignedCandidates()
  return {
    rule_version: 'reimbursement-policy/2',
    materials: [],
    trip: {
      id: tripID,
      name: '北京',
      start_date: '2026-08-26',
      end_date: '2026-08-28',
      timezone: 'Asia/Shanghai',
      version: 1,
    },
    items: [
      {
        assignment_id: paymentAssignmentID,
        fact_type: payment.fact_type,
        fact_id: payment.fact_id,
        display_name: payment.display_name,
        business_date: payment.business_date,
        amount_minor: payment.amount_minor,
        currency: payment.currency,
      },
      {
        assignment_id: invoiceAssignmentID,
        fact_type: invoice.fact_type,
        fact_id: invoice.fact_id,
        display_name: invoice.display_name,
        business_date: invoice.business_date,
        amount_minor: invoice.amount_minor,
        currency: invoice.currency,
      },
    ],
    findings: [
      {
        finding_key: findingKey,
        code: 'duplicate_reimbursement',
        assignment_id: paymentAssignmentID,
        fact_type: 'payment',
        fact_id: paymentID,
        related_reimbursement_id: relatedID,
        related_status: 'reimbursed',
      },
    ],
    totals_by_currency: [{ currency: 'CNY', amount_minor: 24690 }],
    snapshot_hash: 'b'.repeat(64),
  }
}

function summary(
  id: string,
  status: ReimbursementSummary['status'],
  version: number,
  itemCount: number,
  findingCount: number,
): ReimbursementSummary {
  return {
    id,
    trip: reimbursementPreview().trip,
    trip_deleted: false,
    status,
    version,
    item_count: itemCount,
    finding_count: findingCount,
    created_at: timestamp,
    updated_at: timestamp,
  }
}

function reimbursementDetail(
  id: string,
  status: ReimbursementDetail['status'],
  version: number,
  findingCount: number,
): ReimbursementDetail {
  const preview = reimbursementPreview()
  return {
    ...summary(id, status, version, 2, findingCount),
    rule_version: 'reimbursement-policy/2',
    materials_captured: true,
    material_count: 0,
    snapshot_hash: preview.snapshot_hash,
    totals_by_currency: preview.totals_by_currency,
    items: preview.items.map((item, index) => ({
      id: `00000000-0000-4000-8000-00000000073${index}`,
      ...item,
      source_deleted: false,
      sort_order: index,
    })),
    findings:
      findingCount > 0
        ? [
            {
              id: '00000000-0000-4000-8000-000000000740',
              item_id: '00000000-0000-4000-8000-000000000730',
              finding_key: findingKey,
              code: 'duplicate_reimbursement',
              related_reimbursement_id: existingID,
              related_status: 'reimbursed',
            },
          ]
        : [],
    decisions: [decision('submit', null, 'submitted', 0, 1, '合成提交')],
  }
}

function richReimbursementDetail(): ReimbursementDetail {
  const preview = reimbursementPreview()
  const paymentItemID = '00000000-0000-4000-8000-000000000751'
  const invoiceItemID = '00000000-0000-4000-8000-000000000752'
  const usdItemID = '00000000-0000-4000-8000-000000000753'
  return {
    ...summary(existingID, 'reimbursed', 2, 3, 3),
    trip_deleted: true,
    rule_version: 'reimbursement-policy/1',
    materials_captured: false,
    material_count: null,
    snapshot_hash: preview.snapshot_hash,
    totals_by_currency: [
      { currency: 'CNY', amount_minor: 24690 },
      { currency: 'USD', amount_minor: 5000 },
    ],
    items: [
      {
        id: paymentItemID,
        ...preview.items[0],
        source_deleted: false,
        sort_order: 0,
      },
      {
        id: invoiceItemID,
        ...preview.items[1],
        source_deleted: true,
        sort_order: 1,
      },
      {
        id: usdItemID,
        assignment_id: '00000000-0000-4000-8000-000000000754',
        fact_type: 'payment',
        fact_id: '00000000-0000-4000-8000-000000000755',
        display_name: '合成美元交通商户',
        business_date: '2026-08-28',
        amount_minor: 5000,
        currency: 'USD',
        source_deleted: false,
        sort_order: 2,
      },
    ],
    findings: [
      {
        id: '00000000-0000-4000-8000-000000000761',
        item_id: usdItemID,
        finding_key: 'd'.repeat(64),
        code: 'missing_invoice',
        expected_minor: 5000,
        actual_minor: 0,
        currency: 'USD',
      },
      {
        id: '00000000-0000-4000-8000-000000000762',
        item_id: invoiceItemID,
        finding_key: 'e'.repeat(64),
        code: 'amount_conflict',
        expected_minor: 12345,
        actual_minor: 10000,
        currency: 'CNY',
      },
      {
        id: '00000000-0000-4000-8000-000000000763',
        item_id: paymentItemID,
        finding_key: findingKey,
        code: 'duplicate_reimbursement',
        related_reimbursement_id: relatedID,
        related_status: 'reimbursed',
      },
    ],
    decisions: [
      decision('submit', null, 'submitted', 0, 1, '合成提交'),
      decision('mark_reimbursed', 'submitted', 'reimbursed', 1, 2, '合成完成'),
    ],
  }
}

function decision(
  action: 'submit' | 'mark_reimbursed' | 'reject' | 'reopen',
  previousStatus: ReimbursementDetail['status'] | null,
  desiredStatus: ReimbursementDetail['status'],
  expectedVersion: number,
  resultVersion: number,
  reason: string,
) {
  return {
    id: crypto.randomUUID(),
    action,
    previous_status: previousStatus,
    desired_status: desiredStatus,
    expected_version: expectedVersion,
    result_version: resultVersion,
    reason,
    created_at: timestamp,
  }
}

function ownerSession(): Session {
  return session('owner', ['facts.read', 'reimbursements.read', 'reimbursements.manage'])
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
  await page.route(sessionURL, (route) => fulfillJSON(route, value))
}

const sessionURL = (url: URL) => url.pathname === '/api/v1/session'
const tripsURL = (url: URL) => url.pathname === '/api/v1/trips'
const tripCandidatesURL = (url: URL) =>
  url.pathname === `/api/v1/trips/${tripID}/attribution-candidates`
const previewURL = (url: URL) => url.pathname === '/api/v1/reimbursement-previews'
const reimbursementsURL = (url: URL) => url.pathname === '/api/v1/reimbursements'
const reimbursementDetailURL = (url: URL) => /^\/api\/v1\/reimbursements\/[^/]+$/.test(url.pathname)
const reimbursementStatusURL = (url: URL) =>
  url.pathname === `/api/v1/reimbursements/${createdID}/status-decisions`

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
