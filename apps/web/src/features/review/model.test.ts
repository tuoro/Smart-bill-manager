import { describe, expect, it } from 'vitest'
import type { Review } from '../../data/client'
import {
  allocationEditors,
  buildAssociationDecision,
  buildDuplicateResolutionDecision,
  buildRevisionRequest,
  buildFieldPayload,
  editableFields,
  fieldPageNumbers,
  fieldVisibleOnPage,
  firstFieldPage,
  instantInZone,
  itemPageLabel,
  newInvoiceItem,
  parseItemPath,
  refreshDraftFields,
  sourceTimezone,
} from './model'

const evidenceId = '00000000-0000-4000-8000-000000000010'

it('correction can annotate AI-origin fields without changing their genuine root identity', () => {
  const review = reviewFixture({ entry_mode: 'ai' })
  const fields = editableFields(review, 'payment')
  const amount = fields.find((field) => field.path === 'amount_minor')!
  amount.textValue = '5678'
  amount.evidenceIds = []
  amount.manualPage = 1
  amount.manualQuote = '明确摘录'
  expect(buildFieldPayload(review, fields).errors.amount_minor).toBeTruthy()
  const encoded = buildFieldPayload(review, fields, true)
  expect(encoded.fields?.find((field) => field.path === 'amount_minor')?.manual_evidence).toEqual([
    { page: 1, quote: '明确摘录' },
  ])
  expect(review.entry_mode).toBe('ai')
})

it('refresh preserves manual draft and maps only equivalent evidence to the latest revision', () => {
  const previous = reviewFixture()
  const fields = editableFields(previous, 'payment')
  const amount = fields.find((field) => field.path === 'amount_minor')!
  amount.textValue = '5678'
  amount.manualPage = 1
  amount.manualQuote = '合成保留摘录'
  amount.evidenceIds = [evidenceId]
  const latest = {
    ...previous,
    revision: previous.revision + 1,
    fields: previous.fields.map((field) => ({
      ...field,
      value: 9999,
      evidence: field.evidence.map((entry) => ({ ...entry, id: 'refreshed-evidence' })),
    })),
  }
  const refreshed = refreshDraftFields(previous, latest, fields)
  expect(refreshed.find((field) => field.path === 'amount_minor')).toMatchObject({
    textValue: '5678',
    manualPage: 1,
    manualQuote: '合成保留摘录',
    originalValue: 9999,
    evidenceIds: ['refreshed-evidence'],
  })
  expect(amount.evidenceIds).toEqual([evidenceId])
  const changed = { ...latest, fields: latest.fields.map((field) => ({ ...field, evidence: [] })) }
  const unresolved = refreshDraftFields(previous, changed, fields)
  expect(unresolved.find((field) => field.path === 'amount_minor')!.evidenceIds).toEqual([
    evidenceId,
  ])
  expect(buildRevisionRequest(changed, 'payment', unresolved).errors.amount_minor).toContain(
    '证据已不在最新版本',
  )
})

function reviewFixture(overrides: Partial<Review> = {}): Review {
  return {
    entry_mode: 'ai',
    job: {
      id: '00000000-0000-4000-8000-000000000001',
      document_id: '00000000-0000-4000-8000-000000000002',
      original_name: 'payment.png',
      ingestion_kind: 'upload',
      detected_mime: 'image/png',
      status: 'needs_review',
      attempt_count: 1,
      created_at: '2026-08-27T00:00:00Z',
      version: 1,
    },
    claim_set_id: '00000000-0000-4000-8000-000000000003',
    document_type: 'payment',
    revision: 2,
    optimistic_version: 4,
    claim_status: 'ready_for_review',
    page_count: 1,
    pages: [{ page_number: 1, field_paths: ['amount_minor'], item_keys: [] }],
    invoice_item_spans: [],
    fields: [
      {
        id: '00000000-0000-4000-8000-000000000004',
        path: 'amount_minor',
        value_type: 'money_minor',
        presence: 'present',
        value: 1234,
        source: 'ai',
        evidence: [{ id: evidenceId, page: 1, quote: '¥12.34' }],
      },
    ],
    validations: [],
    candidates: [],
    duplicate_candidates: [],
    ...overrides,
  }
}

describe('review model', () => {
  it('creates a complete editable payment field set', () => {
    const fields = editableFields(reviewFixture(), 'payment')

    expect(fields).toHaveLength(10)
    // 商户全称为可选字段：票面只印一个名称时留空，不复制显示名。见 ADR-0039。
    expect(fields.find((field) => field.path === 'merchant_full_name')).toMatchObject({
      presence: 'absent',
      textValue: '',
    })
    // 金额按币种精度显示为十进制；最小单位是内部表示，不要求人去换算。
    expect(fields[0]).toMatchObject({
      path: 'amount_minor',
      presence: 'present',
      textValue: '12.34',
      evidenceIds: [evidenceId],
    })
    expect(fields.find((field) => field.path === 'merchant')).toMatchObject({
      presence: 'absent',
      textValue: '',
    })
  })

  it('creates the complete Trip field set without payment allocation fields', () => {
    const trip = reviewFixture({
      document_type: 'trip',
      fields: [
        {
          id: '00000000-0000-4000-8000-000000000050',
          path: 'destination',
          value_type: 'string',
          presence: 'present',
          value: '北京',
          source: 'ai',
          evidence: [
            {
              id: '00000000-0000-4000-8000-000000000051',
              page: 1,
              quote: '北京',
            },
          ],
        },
      ],
    })

    const fields = editableFields(trip, 'trip')
    expect(fields).toHaveLength(8)
    expect(fields.map((field) => field.path)).toEqual([
      'origin',
      'destination',
      'start_date',
      'end_date',
      'traveler_name',
      'transport_type',
      'booking_reference',
      'supplementary_fields',
    ])
    expect(fields.find((field) => field.path === 'destination')).toMatchObject({
      presence: 'present',
      textValue: '北京',
    })
  })

  it('requires evidence for a changed value and emits exact integer payloads', () => {
    const review = reviewFixture()
    const fields = editableFields(review, 'payment')
    const amount = fields.find((field) => field.path === 'amount_minor')!
    // 输入十进制，提交为最小单位：56.78 元 -> 5678
    amount.textValue = '56.78'
    amount.evidenceIds = []

    expect(buildRevisionRequest(review, 'payment', fields).errors.amount_minor).toContain('证据')

    amount.evidenceIds = [evidenceId]
    const built = buildRevisionRequest(review, 'payment', fields)
    expect(built.errors).toEqual({})
    expect(built.request).toMatchObject({
      expected_revision: 2,
      expected_optimistic_version: 4,
      document_type: 'payment',
    })
    expect(built.request?.fields[0]).toMatchObject({
      path: 'amount_minor',
      value: 5678,
      evidence_ids: [evidenceId],
    })
  })

  it('builds stable UUID-keyed invoice item paths', () => {
    const key = '00000000-0000-4000-8000-000000000020'
    const fields = newInvoiceItem(key, 3)

    expect(fields).toHaveLength(7)
    expect(fields.at(-1)).toMatchObject({
      path: `items[${key}].sort_order`,
      presence: 'present',
      textValue: '3',
    })
    expect(parseItemPath(fields[0].path)).toEqual({ itemKey: key, property: 'name' })
    expect(parseItemPath('items[0].name')).toBeNull()
  })

  it('keeps one stable cross-page item visible on every evidence page', () => {
    const key = '00000000-0000-4000-8000-000000000020'
    const namePath = `items[${key}].name`
    const amountPath = `items[${key}].amount_minor`
    const review = reviewFixture({
      document_type: 'invoice',
      page_count: 3,
      pages: [
        { page_number: 1, field_paths: [namePath], item_keys: [key] },
        { page_number: 2, field_paths: [amountPath], item_keys: [key] },
        { page_number: 3, field_paths: [], item_keys: [] },
      ],
      invoice_item_spans: [
        {
          item_key: key,
          sort_order: 0,
          page_numbers: [1, 2],
          start_page: 1,
          end_page: 2,
          cross_page: true,
        },
      ],
      fields: [
        {
          id: '00000000-0000-4000-8000-000000000021',
          path: namePath,
          value_type: 'string',
          presence: 'present',
          value: '跨页服务',
          source: 'ai',
          evidence: [{ id: '00000000-0000-4000-8000-000000000022', page: 1, quote: '跨页服务' }],
        },
        {
          id: '00000000-0000-4000-8000-000000000023',
          path: amountPath,
          value_type: 'money_minor',
          presence: 'present',
          value: 100,
          source: 'ai',
          evidence: [{ id: '00000000-0000-4000-8000-000000000024', page: 2, quote: '1.00' }],
        },
      ],
    })

    expect(fieldPageNumbers(review, amountPath)).toEqual([2])
    expect(firstFieldPage(review, amountPath)).toBe(2)
    expect(fieldVisibleOnPage(review, namePath, 1)).toBe(true)
    expect(fieldVisibleOnPage(review, namePath, 2)).toBe(true)
    expect(fieldVisibleOnPage(review, namePath, 3)).toBe(false)
    expect(itemPageLabel(review, namePath)).toBe('跨页 1–2')
    expect(
      editableFields(review, 'invoice').filter((field) => field.path.includes(key)),
    ).toHaveLength(7)
  })

  it('keeps an unresolved blocked item reachable on every review page', () => {
    const key = '00000000-0000-4000-8000-000000000025'
    const path = `items[${key}].name`
    const review = reviewFixture({
      document_type: 'invoice',
      page_count: 2,
      pages: [
        { page_number: 1, field_paths: [], item_keys: [] },
        { page_number: 2, field_paths: [], item_keys: [] },
      ],
      invoice_item_spans: [{ item_key: key, sort_order: 0, page_numbers: [], cross_page: false }],
    })

    expect(fieldVisibleOnPage(review, path, 1)).toBe(true)
    expect(fieldVisibleOnPage(review, path, 2)).toBe(true)
    expect(itemPageLabel(review, path)).toBe('未定位页面')
  })

  it('round-trips supplementary review data as JSON without requiring invented evidence', () => {
    const review = reviewFixture({
      fields: [
        ...reviewFixture().fields,
        {
          id: '00000000-0000-4000-8000-000000000011',
          path: 'supplementary_fields',
          value_type: 'supplementary',
          presence: 'present',
          value: [{ path: 'payment.discount', label: '优惠', value: '2.00' }],
          source: 'ai',
          evidence: [],
        },
      ],
    })
    const fields = editableFields(review, 'payment')
    const supplementary = fields.find((field) => field.path === 'supplementary_fields')!
    supplementary.textValue = JSON.stringify([
      { path: 'payment.discount', label: '优惠金额', value: '2.00' },
    ])

    const built = buildRevisionRequest(review, 'payment', fields)
    expect(built.errors).toEqual({})
    expect(
      built.request?.fields.find((field) => field.path === 'supplementary_fields'),
    ).toMatchObject({
      value: [{ path: 'payment.discount', label: '优惠金额', value: '2.00' }],
      evidence_ids: [],
    })
  })

  it('accepts explicit manual page annotations but never invents quotes', () => {
    const review = reviewFixture({ entry_mode: 'manual', fields: [] })
    const fields = editableFields(review, 'payment')
    const merchant = fields.find((field) => field.path === 'merchant')!
    merchant.presence = 'present'
    merchant.textValue = '人工商户'
    expect(buildRevisionRequest(review, 'payment', fields).errors.merchant).toBeTruthy()
    merchant.manualPage = 1
    merchant.manualQuote = '用户核对的原文'
    const built = buildRevisionRequest(review, 'payment', fields)
    expect(built.errors).toEqual({})
    expect(built.request?.fields.find((field) => field.path === 'merchant')).toMatchObject({
      manual_evidence: [{ page: 1, quote: '用户核对的原文' }],
    })
    expect(
      buildRevisionRequest({ ...review, entry_mode: 'ai' }, 'payment', fields).errors.merchant,
    ).toBeTruthy()
    merchant.manualPage = 2
    expect(buildRevisionRequest(review, 'payment', fields).errors.merchant).toBeTruthy()
    merchant.presence = 'absent'
    expect(
      buildRevisionRequest(review, 'payment', fields).request?.fields.find(
        (field) => field.path === 'merchant',
      ),
    ).not.toHaveProperty('manual_evidence')
  })

  it('requires an explicit association decision', () => {
    const withoutCandidates = reviewFixture()
    expect(buildAssociationDecision(withoutCandidates, '', []).request).toBeUndefined()
    expect(buildAssociationDecision(withoutCandidates, 'no_candidate', []).request).toEqual({
      association_mode: 'no_candidate',
      allocations: [],
    })

    const candidateId = '00000000-0000-4000-8000-000000000030'
    const candidateId2 = '00000000-0000-4000-8000-000000000032'
    const withCandidates = reviewFixture({
      candidates: [
        {
          id: candidateId,
          target_type: 'invoice',
          target_id: '00000000-0000-4000-8000-000000000031',
          amount_minor: 1234,
          allocated_minor: 234,
          remaining_minor: 1000,
          currency: 'CNY',
          business_date: '2026-08-27',
          display_name: '示例商户',
          available: true,
          name_exact: true,
          date_distance_days: 0,
          reason_codes: ['currency_exact', 'remaining_available'],
        },
        {
          id: candidateId2,
          target_type: 'invoice',
          target_id: '00000000-0000-4000-8000-000000000033',
          amount_minor: 800,
          allocated_minor: 0,
          remaining_minor: 800,
          currency: 'CNY',
          business_date: '2026-08-28',
          display_name: '第二候选',
          available: true,
          name_exact: false,
          date_distance_days: 1,
          reason_codes: ['currency_exact', 'partial_allocation'],
        },
      ],
    })
    const editors = allocationEditors(withCandidates)
    editors[0].selected = true
    // 分配金额同样按币种精度输入十进制：5.00 元 -> 500 最小单位
    editors[0].textValue = '5.00'
    editors[1].selected = true
    editors[1].textValue = '7.00'
    expect(
      buildAssociationDecision(withCandidates, 'allocate_candidates', editors).request,
    ).toEqual({
      association_mode: 'allocate_candidates',
      allocations: [
        { candidate_id: candidateId, allocated_minor: 500 },
        { candidate_id: candidateId2, allocated_minor: 700 },
      ],
    })
    expect(buildAssociationDecision(withCandidates, 'reject_all', editors).request).toEqual({
      association_mode: 'reject_all',
      allocations: [],
    })
    expect(
      buildAssociationDecision(withCandidates, 'no_candidate', editors).request,
    ).toBeUndefined()

    editors[0].textValue = '10.01'
    expect(
      buildAssociationDecision(withCandidates, 'allocate_candidates', editors).errors[candidateId],
    ).toContain('剩余余额')
  })

  it('requires a complete available duplicate resolution plan', () => {
    const first = '00000000-0000-4000-8000-000000000040'
    const second = '00000000-0000-4000-8000-000000000041'
    const review = reviewFixture({
      duplicate_candidates: [
        {
          id: first,
          kind: 'near_file',
          display_name: '相似单据.png',
          available: true,
          reason_codes: ['ordered_page_visual_match'],
        },
        {
          id: second,
          kind: 'field_combination',
          display_name: '示例商户',
          available: true,
          reason_codes: ['amount_exact'],
        },
      ],
    })

    expect(buildDuplicateResolutionDecision(review, [first]).request).toBeUndefined()
    expect(buildDuplicateResolutionDecision(review, [second, first]).request).toEqual({
      duplicate_resolutions: [
        { candidate_id: first, action: 'keep_distinct' },
        { candidate_id: second, action: 'keep_distinct' },
      ],
    })

    review.duplicate_candidates[1].available = false
    expect(buildDuplicateResolutionDecision(review, [first, second]).error).toContain('不可用')
  })
})

// 同一次修订里币种和金额可能一起改。若换算时用旧币种，金额会被记错一个数量级——
// JPY 精度为 0，"100" 是 100 而不是 10000。
it('converts the amount with the currency being submitted, not the stored one', () => {
  const review = reviewFixture()
  const fields = editableFields(review, 'payment')
  const amount = fields.find((field) => field.path === 'amount_minor')!
  const currency = fields.find((field) => field.path === 'currency')!
  amount.textValue = '100'
  amount.evidenceIds = [evidenceId]
  currency.presence = 'present'
  currency.textValue = 'JPY'
  currency.evidenceIds = [evidenceId]
  const encoded = buildFieldPayload(review, fields)
  expect(encoded.errors).toEqual({})
  expect(encoded.fields?.find((field) => field.path === 'amount_minor')?.value).toBe(100)

  // 同样的输入在 CNY 下是 10000 最小单位。
  currency.textValue = 'CNY'
  const asCNY = buildFieldPayload(review, fields)
  expect(asCNY.fields?.find((field) => field.path === 'amount_minor')?.value).toBe(10000)
})

// 前端放行、后端拒绝是最差的组合：用户填完整页才收到一个本可当场说清的错误。
it('rejects amounts the backend would reject, with the currency precision named', () => {
  const review = reviewFixture()
  const fields = editableFields(review, 'payment')
  const amount = fields.find((field) => field.path === 'amount_minor')!
  amount.evidenceIds = [evidenceId]
  for (const bad of ['1,234.00', '1.234', ' 12', '+12', '']) {
    amount.textValue = bad
    expect(buildFieldPayload(review, fields).errors.amount_minor).toBeTruthy()
  }
})

// 交易时间与开票日期以前落在通用分支里，什么都不校验：手输一个 2026-9-4
// 要等提交后才被后端拒绝，而那句拒绝并不说明正确写法。
// 下面每一条都逐个对照过 Go 的 time.Parse(time.RFC3339Nano)，取舍一致。
it('rejects instants and dates the backend would reject', () => {
  const review = reviewFixture()
  const fields = editableFields(review, 'payment')
  const time = fields.find((field) => field.path === 'transaction_time')!
  time.presence = 'present'
  time.evidenceIds = [evidenceId]
  for (const bad of [
    '2026-9-4T08:00:00Z',
    '2026-09-04 08:00:00Z',
    '2026-09-04T08:00Z',
    '2026-09-04T08:00:00+0800',
    '2026-09-04T08:00:00',
    '2026-02-30T08:00:00Z',
    '2026-09-04t08:00:00Z',
    '2026-09-04T08:00:00z',
    '2026-09-04T24:00:00Z',
    '2026-09-04T08:00:60Z',
    '2026-09-04T08:00:00+25:00',
    '',
  ]) {
    time.textValue = bad
    expect(buildFieldPayload(review, fields).errors.transaction_time).toBeTruthy()
  }
  for (const good of [
    '2026-09-04T08:00:00Z',
    '2026-09-04T16:00:00+08:00',
    '2026-09-04T08:00:00.5Z',
  ]) {
    time.textValue = good
    expect(buildFieldPayload(review, fields).errors.transaction_time).toBeUndefined()
  }

  const invoiceFields = editableFields(reviewFixture(), 'invoice')
  const date = invoiceFields.find((field) => field.path === 'invoice_date')!
  date.presence = 'present'
  date.evidenceIds = [evidenceId]
  for (const bad of ['2026-9-4', '2026/09/04', '2026-02-30', '']) {
    date.textValue = bad
    expect(buildFieldPayload(review, invoiceFields).errors.invoice_date).toBeTruthy()
  }
  date.textValue = '2026-09-04'
  expect(buildFieldPayload(review, invoiceFields).errors.invoice_date).toBeUndefined()
})

// 票面印的是本地时间，字段存的是绝对时刻；不换算一遍就要人自己心算时差。
it('renders the instant in the source timezone', () => {
  expect(instantInZone('2026-09-04T08:00:00Z', 'Asia/Shanghai')).toBe('2026-09-04 16:00:00')
  expect(instantInZone('2026-09-04T08:00:00Z', 'UTC')).toBe('2026-09-04 08:00:00')
  // 跨日：UTC 的 4 日晚上在上海已经是 5 日凌晨，业务日期因此不同。
  expect(instantInZone('2026-09-04T20:00:00Z', 'Asia/Shanghai')).toBe('2026-09-05 04:00:00')
  // 午夜按 00 显示，不是某些实现里的 24。
  expect(instantInZone('2026-09-04T16:00:00Z', 'Asia/Shanghai')).toBe('2026-09-05 00:00:00')
  // 时区名无效或时间无效时不猜，宁可不显示。
  expect(instantInZone('2026-09-04T08:00:00Z', 'Mars/Olympus')).toBeNull()
  expect(instantInZone('2026-9-4', 'UTC')).toBeNull()
})

// 时区和时间可能在同一次修订里一起改，显示要跟着当前填写的值走。
it('reads the source timezone being edited, not the stored one', () => {
  const review = reviewFixture()
  const fields = editableFields(review, 'payment')
  expect(sourceTimezone(fields)).toBe('UTC')
  const timezone = fields.find((field) => field.path === 'source_timezone')!
  timezone.presence = 'present'
  timezone.textValue = 'Asia/Shanghai'
  expect(sourceTimezone(fields)).toBe('Asia/Shanghai')
})
