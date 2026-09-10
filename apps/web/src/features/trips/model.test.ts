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
  fact_version: 1,
  assignment_mode: 'manual',
  assignment_state: 'manual_unassigned',
  match_count: 0,
  suggested: true,
  reason_codes: ['date_inside_trip'],
}

describe('Trip attribution model', () => {
  it('builds assign, move and unassign requests with explicit nullable expectations', () => {
    const tripID = '00000000-0000-4000-8000-000000000010'
    expect(buildTripAssignmentDecision(candidate, tripID)).toEqual({
      request: {
        fact_type: 'payment',
        fact_id: candidate.fact_id,
        desired_trip_id: tripID,
        expected_assignment_id: null,
        expected_fact_version: 1,
      },
    })

    const assigned = {
      ...candidate,
      current_assignment_id: '00000000-0000-4000-8000-000000000020',
      current_trip_id: '00000000-0000-4000-8000-000000000021',
    }
    expect(tripAssignmentActionLabel(assigned, tripID)).toContain('移动')
    expect(buildTripAssignmentDecision(assigned, tripID)).toMatchObject({
      request: {
        desired_trip_id: tripID,
        expected_assignment_id: assigned.current_assignment_id,
      },
    })

    const current = { ...assigned, current_trip_id: tripID }
    expect(tripAssignmentActionLabel(current, tripID)).toContain('撤销')
    expect(buildTripAssignmentDecision(current, tripID)).toMatchObject({
      request: { desired_trip_id: null, expected_assignment_id: assigned.current_assignment_id },
    })
  })

  it('keeps request fingerprints deterministic', () => {
    const tripID = '00000000-0000-4000-8000-000000000010'
    const first = buildTripAssignmentDecision(candidate, tripID).request!
    const second = buildTripAssignmentDecision(candidate, tripID).request!
    expect(assignmentFingerprint(first)).toBe(assignmentFingerprint(second))
    expect(tripReasonLabel('date_inside_trip')).toContain('行程区间')
  })
})
