import { test, expect, type Page, type Route } from '@playwright/test'
import type {
  CorrectionWorkspace,
  CorrectionRequest,
  CorrectionConfirmRequest,
  CorrectionPreview,
  CorrectionFactType,
  Session,
  Review,
} from '../src/data/client'
import { fieldLabel } from '../src/features/review/model'
import { captureResponsiveReview } from './visual-review'

const id = '00000000-0000-4000-8000-000000008001'
const reviewID = '00000000-0000-4000-8000-000000008002'
const claimID = '00000000-0000-4000-8000-000000008003'
const timestamp = '2026-09-04T08:00:00Z'
const session: Session = {
  user: { id, email: 'synthetic@example.invalid', display_name: '合成用户' },
  tenant: { id, name: '合成纠错工作区', default_currency: 'CNY', timezone: 'Asia/Shanghai' },
  role: 'owner',
  capabilities: ['facts.read', 'claims.review', 'allocations.manage'],
  csrf_token: 'synthetic-ui-csrf',
  expires_at: '2099-01-01T00:00:00Z',
}

function workspace(kind: CorrectionFactType): CorrectionWorkspace {
  const values: Record<string, unknown> =
    kind === 'payment'
      ? {
          amount_minor: 12345,
          currency: 'CNY',
          merchant: '合成商户',
          transaction_time: '2026-08-27T12:00:00+08:00',
          source_timezone: 'Asia/Shanghai',
        }
      : kind === 'invoice'
        ? {
            invoice_number: 'SYNTHETIC-CORRECTION',
            invoice_date: '2026-08-27',
            total_minor: 12345,
            currency: 'CNY',
            seller_name: '合成销售方',
            buyer_name: '合成购买方',
          }
        : {
            origin: '合成出发地',
            destination: '合成目的地',
            start_date: '2026-08-26',
            end_date: '2026-08-28',
          }
  const fields: Review['fields'] = Object.entries(values).map(([path, value], index) => ({
    id: `field-${index}`,
    path,
    value_type: path.endsWith('_minor')
      ? 'money_minor'
      : path === 'transaction_time'
        ? 'instant'
        : path.endsWith('_date')
          ? 'date'
          : 'string',
    presence: 'present',
    value,
    source: 'ai',
    evidence: [{ id: `evidence-${index}`, page: 1, quote: `合成原件 ${String(value)}` }],
  }))
  return {
    state: {
      fact_type: kind,
      fact_id: id,
      version: 1,
      current_review_decision_id: reviewID,
      claim_set_id: claimID,
      document_id: id,
      links: [],
      attribution: {
        mode: kind === 'trip' ? 'preserve_material_links' : 'auto',
        assignment_id: '',
        current_trip_id: '',
        desired_trip_id: '',
        matching_trip_count: 0,
        matching_trip_version: 0,
      },
    },
    review: {
      entry_mode: 'ai',
      job: {
        id,
        document_id: id,
        original_name: 'synthetic-correction.png',
        status: 'completed',
        ingestion_kind: 'upload',
        detected_mime: 'image/png',
        attempt_count: 1,
        created_at: timestamp,
        version: 4,
      },
      claim_set_id: claimID,
      document_type: kind,
      revision: 1,
      optimistic_version: 2,
      claim_status: 'confirmed',
      page_count: 1,
      pages: [{ page_number: 1, field_paths: fields.map((field) => field.path), item_keys: [] }],
      invoice_item_spans: [],
      fields,
      validations: [],
      candidates: [],
      duplicate_candidates: [],
    },
  }
}

async function json(route: Route, value: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
}

async function setup(
  page: Page,
  kind: CorrectionFactType,
  options: { conflict?: boolean; linked?: boolean; role?: Session['role'] } = {},
) {
  const state = workspace(kind)
  if (options.linked)
    state.state.links = [
      {
        id: 'link-one',
        target_id: 'target-invoice',
        allocated_minor: 12000,
        currency: 'CNY',
        target_currency: 'CNY',
        target_business_date: '2026-08-27',
        target_amount_minor: 12345,
        target_allocated_minor: 12000,
        target_version: 1,
        target_available: true,
      },
    ]
  const calls: CorrectionConfirmRequest[] = []
  const keys: string[] = []
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  await page.route('**/api/v1/session', (route) =>
    json(route, {
      ...session,
      role: options.role ?? 'owner',
      capabilities:
        options.role === 'reviewer'
          ? ['claims.review']
          : options.role === 'viewer'
            ? ['facts.read']
            : session.capabilities,
    }),
  )
  await page.route('**/api/v1/documents/**/content', (route) =>
    route.fulfill({
      contentType: 'image/svg+xml',
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="300" height="240"><rect width="300" height="240" fill="#fff"/><text x="20" y="50" fill="#123">Synthetic source</text></svg>',
    }),
  )
  await page.route('**/api/v1/claim-sets/*', (route) => json(route, state.review))
  await page.route('**/api/v1/facts/**/correction/history?**', (route) =>
    json(route, {
      items: [
        {
          review_decision_id: state.state.current_review_decision_id,
          previous_review_decision_id: '',
          claim_set_id: state.review.claim_set_id,
          revision: state.review.revision,
          actor_user_id: id,
          reason: '合成历史确认',
          created_at: timestamp,
        },
      ],
      next_before_revision: null,
    }),
  )
  await page.route('**/api/v1/facts/**/correction/preview', async (route) => {
    const body = route.request().postDataJSON() as CorrectionRequest
    const blocked = Boolean(options.linked && !body.withdraw_link_ids.includes('link-one'))
    const preview: CorrectionPreview = {
      state: state.state,
      issues: blocked
        ? [
            {
              link_id: 'link-one',
              code: 'correction_overallocated',
              message: '保留分配总额超过新金额，请明确撤销或先调整分配',
            },
          ]
        : [],
      duplicates: [],
      withdraw_link_ids: body.withdraw_link_ids,
      preview_hash: 'a'.repeat(64),
      can_confirm: !blocked,
    }
    await json(route, preview)
  })
  await page.route('**/api/v1/facts/**/correction', async (route) => {
    if (route.request().method() === 'GET') {
      await json(route, state)
      return
    }
    const body = route.request().postDataJSON() as CorrectionConfirmRequest
    calls.push(body)
    keys.push(route.request().headers()['idempotency-key'] ?? '')
    if (options.conflict && calls.length === 1) {
      state.state.version++
      await json(
        route,
        { error: { code: 'stale_correction_preview', message: '其他成员已修改关联，请刷新' } },
        409,
      )
      return
    }
    state.state.version++
    state.state.current_review_decision_id = 'new-review'
    state.review.revision++
    state.review.fields = body.fields.map((field, index) => ({
      id: `new-field-${index}`,
      path: field.path,
      value_type: field.value_type,
      presence: field.presence,
      value: field.value,
      source: 'user',
      evidence:
        field.manual_evidence?.map((e, i) => ({ id: `new-evidence-${index}-${i}`, ...e })) ?? [],
    }))
    await json(route, {
      fact_type: kind,
      fact_id: id,
      review_decision_id: 'new-review',
      claim_set_id: claimID,
      version: state.state.version,
      replayed: false,
    })
  })
  return { state, calls, keys, pageErrors }
}

for (const kind of ['payment', 'invoice', 'trip'] as const) {
  test(`${kind} 字段纠错保留来源并查看历史`, async ({ page }, testInfo) => {
    const f = await setup(page, kind)
    await page.goto(`/facts/${kind}/${id}/correction`)
    await expect(page.getByText('AI 候选经人工确认', { exact: false })).toBeVisible()
    const path =
      kind === 'payment' ? 'merchant' : kind === 'invoice' ? 'seller_name' : 'destination'
    const label = fieldLabel(path)
    await page.getByRole('textbox', { name: label, exact: true }).fill('合成更正值')
    const field = page
      .locator('.correction-field')
      .filter({ has: page.getByRole('textbox', { name: label, exact: true }) })
    await field.locator('summary').click()
    await field.getByRole('button', { name: '清空证据选择' }).click()
    await page.getByRole('spinbutton', { name: `${label} 来源页码` }).fill('1')
    await page.getByRole('textbox', { name: `${label} 原件摘录` }).fill('原件中的合成更正摘录')
    await page.getByRole('textbox', { name: '纠错理由' }).fill('核对原件后更正')
    await page.getByRole('button', { name: '预览纠错', exact: true }).click()
    await expect(page.getByRole('button', { name: '确认纠错', exact: true })).toBeEnabled()
    if (kind === 'payment') await captureResponsiveReview(page, testInfo, 'fact-correction')
    await page.getByRole('button', { name: '确认纠错', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: '纠错已确认' })).toBeVisible()
    expect(f.calls[0].fields.find((field) => field.path === path)?.manual_evidence).toEqual([
      { page: 1, quote: '原件中的合成更正摘录' },
    ])
    await page.getByRole('button', { name: '查看字段修订 2' }).click()
    await expect(page.getByRole('heading', { name: '字段修订 2 · 只读' })).toBeVisible()
    expect(f.pageErrors).toEqual([])
  })
}

test('分配冲突明确撤销，409 刷新保留草稿并重新核对', async ({ page }) => {
  const f = await setup(page, 'payment', { linked: true, conflict: true })
  await page.goto(`/facts/payment/${id}/correction`)
  await page.getByRole('textbox', { name: '支付金额（最小单位）', exact: true }).fill('5000')
  await page.getByRole('textbox', { name: '纠错理由' }).fill('保留纠错草稿')
  await page.getByRole('button', { name: '预览纠错', exact: true }).click()
  await expect(page.getByRole('button', { name: '确认纠错', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: /撤销.*对端日期/ }).check()
  await expect(page.getByRole('heading', { name: '确认前预览' })).toHaveCount(0)
  await page.getByRole('button', { name: '预览纠错', exact: true }).click()
  await page.getByRole('button', { name: '确认纠错', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('其他成员已修改关联')
  await page.getByRole('button', { name: '刷新并保留草稿' }).click()
  await expect(
    page.getByRole('textbox', { name: '支付金额（最小单位）', exact: true }),
  ).toHaveValue('5000')
  await expect(page.getByRole('textbox', { name: '纠错理由' })).toHaveValue('保留纠错草稿')
  await expect(page.getByRole('button', { name: '预览纠错', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: '我已核对最新字段与关联' }).check()
  await page.getByRole('button', { name: '预览纠错', exact: true }).click()
  await page.getByRole('button', { name: '确认纠错', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '纠错已确认' })).toBeVisible()
  expect(f.calls[1].expected_version).toBe(2)
  expect(f.calls[1].withdraw_link_ids).toEqual(['link-one'])
  expect(f.pageErrors).toEqual([])
})

for (const role of ['reviewer', 'viewer'] as const)
  test(`${role} 不能进入正式字段纠错`, async ({ page }) => {
    await setup(page, 'payment', { role })
    await page.goto(`/facts/payment/${id}/correction`)
    await expect(page.getByText('只有具备账单读取与审核权限', { exact: false })).toBeVisible()
    await expect(page.getByRole('textbox', { name: '纠错理由' })).toHaveCount(0)
  })

test('移除已有明细进入最终新旧对照', async ({ page }) => {
  const f = await setup(page, 'invoice')
  const key = '12345678-0000-4000-8000-000000000001'
  for (const [property, value, valueType] of [
    ['name', '合成旧明细', 'string'],
    ['amount_minor', 12345, 'money_minor'],
    ['sort_order', 0, 'integer'],
  ] as const) {
    f.state.review.fields.push({
      id: `item-${property}`,
      path: `items[${key}].${property}`,
      value_type: valueType,
      presence: 'present',
      value,
      source: 'ai',
      evidence: [{ id: `item-evidence-${property}`, page: 1, quote: '合成原始明细' }],
    })
  }
  await page.goto(`/facts/invoice/${id}/correction`)
  await page.getByRole('button', { name: '移除明细 1', exact: true }).click()
  await page.getByRole('textbox', { name: '纠错理由' }).fill('原明细不应计入')
  await page.getByRole('button', { name: '预览纠错', exact: true }).click()
  await page.getByText('核对新旧字段', { exact: true }).click()
  await expect(page.getByText('合成旧明细 → 移除明细字段', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '确认纠错', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '纠错已确认' })).toBeVisible()
  expect(f.calls[0].fields.some((field) => field.path.startsWith(`items[${key}]`))).toBe(false)
})

test('刷新使迟到历史详情失效并结束加载状态', async ({ page }) => {
  await setup(page, 'payment', { conflict: true })
  let release = () => {}
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/claim-sets/*', async (route) => {
    await pending
    await json(route, workspace('payment').review)
  })
  await page.goto(`/facts/payment/${id}/correction`)
  await page.getByRole('button', { name: '查看字段修订 1' }).click()
  await expect(page.getByText('正在读取历史字段…')).toBeVisible()
  await page.getByRole('textbox', { name: '纠错理由' }).fill('合成并发刷新')
  await page.getByRole('button', { name: '预览纠错', exact: true }).click()
  await page.getByRole('button', { name: '确认纠错', exact: true }).click()
  await page.getByRole('button', { name: '刷新并保留草稿' }).click()
  await expect(page.getByRole('checkbox', { name: '我已核对最新字段与关联' })).toBeVisible()
  release()
  await expect(page.getByText('正在读取历史字段…')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '字段修订 1 · 只读' })).toHaveCount(0)
})
