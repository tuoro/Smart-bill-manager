import { describe, expect, it } from 'vitest'
import type { TripAttributionCandidate } from '../../data/client'
import {
  assignmentFingerprint,
  buildTripAssignmentDecision,
  tripAssignmentActionLabel,
  tripReasonLabel,
} from './model'

const candidate: TripAttributionCandidate = {
  fact_type: 'payment',
  fact_id: '00000000-0000-4000-8000-000000000001',
  display_name: '示例商户',
  business_date: '2026-08-27',
  amount_minor: 12345,
  currency: 'CNY',
  suggested: true,
  reason_codes: ['date_inside_trip'],
}

describe('Trip attribution model', () => {
  it('builds assign, move and unassign requests with explicit nullable expectations', () => {
    const tripID = '00000000-0000-4000-8000-000000000010'
    expect(buildTripAssignmentDecision(candidate, tripID, ' 日期命中 ')).toEqual({
      request: {
        fact_type: 'payment',
        fact_id: candidate.fact_id,
        desired_trip_id: tripID,
        expected_assignment_id: null,
        reason: '日期命中',
      },
    })

    const assigned = {
      ...candidate,
      current_assignment_id: '00000000-0000-4000-8000-000000000020',
      current_trip_id: '00000000-0000-4000-8000-000000000021',
    }
    expect(tripAssignmentActionLabel(assigned, tripID)).toContain('移动')
    expect(buildTripAssignmentDecision(assigned, tripID, '人工核对')).toMatchObject({
      request: {
        desired_trip_id: tripID,
        expected_assignment_id: assigned.current_assignment_id,
      },
    })

    const current = { ...assigned, current_trip_id: tripID }
    expect(tripAssignmentActionLabel(current, tripID)).toContain('撤销')
    expect(buildTripAssignmentDecision(current, tripID, '撤销误归属')).toMatchObject({
      request: { desired_trip_id: null, expected_assignment_id: assigned.current_assignment_id },
    })
  })

  it('validates reasons and keeps request fingerprints deterministic', () => {
    const tripID = '00000000-0000-4000-8000-000000000010'
    expect(buildTripAssignmentDecision(candidate, tripID, '').error).toContain('1～500')
    expect(buildTripAssignmentDecision(candidate, tripID, '理'.repeat(501)).error).toContain(
      '1～500',
    )
    const first = buildTripAssignmentDecision(candidate, tripID, '人工核对').request!
    const second = buildTripAssignmentDecision(candidate, tripID, '人工核对').request!
    expect(assignmentFingerprint(first)).toBe(assignmentFingerprint(second))
    expect(tripReasonLabel('date_inside_trip')).toContain('行程区间')
  })
})
