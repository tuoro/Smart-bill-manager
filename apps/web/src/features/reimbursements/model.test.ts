import { describe, expect, it } from 'vitest'
import type { ReimbursementDetail, ReimbursementPolicySnapshot } from '../../data/client'
import {
  buildReimbursementStatusRequest,
  buildReimbursementSubmission,
  reimbursementFindingLabel,
  reimbursementRequestFingerprint,
  reimbursementStatusActions,
} from './model'

const snapshot: ReimbursementPolicySnapshot = {
  rule_version: 'reimbursement-policy/1',
  trip: {
    id: '00000000-0000-4000-8000-000000000001',
    destination: '北京',
    start_date: '2026-08-26',
    end_date: '2026-08-28',
  },
  items: [
    {
      assignment_id: '00000000-0000-4000-8000-000000000012',
      fact_type: 'invoice',
      fact_id: '00000000-0000-4000-8000-000000000022',
      display_name: '合成商户',
      business_date: '2026-08-27',
      amount_minor: 12345,
      currency: 'CNY',
    },
    {
      assignment_id: '00000000-0000-4000-8000-000000000011',
      fact_type: 'payment',
      fact_id: '00000000-0000-4000-8000-000000000021',
      display_name: '合成商户',
      business_date: '2026-08-27',
      amount_minor: 12345,
      currency: 'CNY',
    },
  ],
  findings: [
    {
      finding_key: 'a'.repeat(64),
      code: 'duplicate_reimbursement',
      assignment_id: '00000000-0000-4000-8000-000000000011',
      fact_type: 'payment',
      fact_id: '00000000-0000-4000-8000-000000000021',
      related_reimbursement_id: '00000000-0000-4000-8000-000000000099',
      related_status: 'reimbursed',
    },
  ],
  totals_by_currency: [{ currency: 'CNY', amount_minor: 24690 }],
  snapshot_hash: 'b'.repeat(64),
}

const detail: ReimbursementDetail = {
  id: '00000000-0000-4000-8000-000000000031',
  trip: snapshot.trip,
  trip_deleted: false,
  status: 'submitted',
  version: 2,
  item_count: 2,
  finding_count: 1,
  created_at: '2026-08-31T00:00:00Z',
  updated_at: '2026-08-31T00:00:00Z',
  rule_version: snapshot.rule_version,
  snapshot_hash: snapshot.snapshot_hash,
  totals_by_currency: snapshot.totals_by_currency,
  items: [],
  findings: [],
  decisions: [],
}

describe('reimbursement workflow model', () => {
  it('requires a matching explicit selection, complete finding acknowledgement and reason', () => {
    expect(buildReimbursementSubmission(undefined, [], false, '').error).toContain('预检')
    expect(
      buildReimbursementSubmission(snapshot, [snapshot.items[0].assignment_id], true, '提交').error,
    ).toContain('已变化')
    expect(
      buildReimbursementSubmission(
        snapshot,
        snapshot.items.map((item) => item.assignment_id),
        false,
        '提交',
      ).error,
    ).toContain('完整政策提示')

    const decision = buildReimbursementSubmission(
      snapshot,
      snapshot.items.map((item) => item.assignment_id).reverse(),
      true,
      '  合成提交理由  ',
    )
    expect(decision.request).toEqual({
      trip_id: snapshot.trip.id,
      assignment_ids: snapshot.items.map((item) => item.assignment_id).sort(),
      expected_snapshot_hash: snapshot.snapshot_hash,
      acknowledged_finding_keys: ['a'.repeat(64)],
      reason: '合成提交理由',
    })

    const oversized: ReimbursementPolicySnapshot = {
      ...snapshot,
      items: Array.from({ length: 201 }, (_, index) => ({
        ...snapshot.items[0],
        assignment_id: `assignment-${index}`,
        fact_id: `fact-${index}`,
      })),
    }
    expect(
      buildReimbursementSubmission(
        oversized,
        oversized.items.map((item) => item.assignment_id),
        true,
        '提交',
      ).error,
    ).toContain('已变化')
  })

  it('derives only valid status actions and keeps fingerprints stable', () => {
    expect(reimbursementStatusActions('submitted')).toEqual(['reimbursed', 'rejected'])
    expect(reimbursementStatusActions('reimbursed')).toEqual(['submitted'])
    expect(buildReimbursementStatusRequest(detail, 'submitted', '无变化').error).toContain(
      '不能执行',
    )
    const decision = buildReimbursementStatusRequest(detail, 'reimbursed', '  已完成  ')
    expect(decision.request).toEqual({
      expected_status: 'submitted',
      desired_status: 'reimbursed',
      expected_version: 2,
      reason: '已完成',
    })
    expect(reimbursementRequestFingerprint(detail.id, decision.request!)).toBe(
      reimbursementRequestFingerprint(detail.id, decision.request!),
    )
    expect(reimbursementFindingLabel('missing_invoice')).toContain('缺少')
    expect(reimbursementFindingLabel('amount_conflict')).toContain('不一致')
    expect(reimbursementFindingLabel('duplicate_reimbursement')).toContain('其他有效报销')
  })

  it('allows a no-finding snapshot without an acknowledgement checkbox', () => {
    const withoutFindings = { ...snapshot, findings: [] }
    expect(
      buildReimbursementSubmission(
        withoutFindings,
        withoutFindings.items.map((item) => item.assignment_id),
        false,
        '无提示提交',
      ).request?.acknowledged_finding_keys,
    ).toEqual([])
  })
})
