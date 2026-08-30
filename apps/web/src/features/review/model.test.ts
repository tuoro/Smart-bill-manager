import { describe, expect, it } from 'vitest'
import type { Review } from '../../data/client'
import {
  allocationEditors,
  buildAssociationDecision,
  buildDuplicateResolutionDecision,
  buildRevisionRequest,
  editableFields,
  newInvoiceItem,
  parseItemPath,
} from './model'

const evidenceId = '00000000-0000-4000-8000-000000000010'

function reviewFixture(overrides: Partial<Review> = {}): Review {
  return {
    job: {
      id: '00000000-0000-4000-8000-000000000001',
      document_id: '00000000-0000-4000-8000-000000000002',
      original_name: 'payment.png',
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

    expect(fields).toHaveLength(9)
    expect(fields[0]).toMatchObject({
      path: 'amount_minor',
      presence: 'present',
      textValue: '1234',
      evidenceIds: [evidenceId],
    })
    expect(fields.find((field) => field.path === 'merchant')).toMatchObject({
      presence: 'absent',
      textValue: '',
    })
  })

  it('requires evidence for a changed value and emits exact integer payloads', () => {
    const review = reviewFixture()
    const fields = editableFields(review, 'payment')
    const amount = fields.find((field) => field.path === 'amount_minor')!
    amount.textValue = '5678'
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
    editors[0].textValue = '500'
    editors[1].selected = true
    editors[1].textValue = '700'
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

    editors[0].textValue = '1001'
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
