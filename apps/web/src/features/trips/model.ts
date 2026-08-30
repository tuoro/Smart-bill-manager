import type {
  TripAssignmentRequest,
  TripAttributionCandidate,
  TripAttributionView,
} from '../../data/client'

export const tripViewLabels: Record<TripAttributionView, string> = {
  all: '全部',
  suggested: '建议',
  assigned: '已归属',
}

const reasonLabels: Record<TripAttributionCandidate['reason_codes'][number], string> = {
  currently_assigned: '当前已归属本行程',
  date_inside_trip: '业务日期在行程区间内',
  date_within_3_days_before: '业务日期在行程开始前 3 日内',
  date_within_3_days_after: '业务日期在行程结束后 3 日内',
  linked_fact_assigned_to_trip: '关联支付或发票已归属本行程',
}

export type TripAssignmentDecision = {
  request?: TripAssignmentRequest
  error?: string
}

export function tripReasonLabel(reason: TripAttributionCandidate['reason_codes'][number]): string {
  return reasonLabels[reason]
}

export function tripAssignmentActionLabel(
  candidate: TripAttributionCandidate,
  selectedTripID: string,
): string {
  if (candidate.current_trip_id === selectedTripID) return '撤销当前归属'
  if (candidate.current_trip_id) return '从原行程移动到当前行程'
  return '归属到当前行程'
}

export function buildTripAssignmentDecision(
  candidate: TripAttributionCandidate,
  selectedTripID: string,
  reason: string,
): TripAssignmentDecision {
  const normalizedReason = reason.trim()
  const reasonLength = [...normalizedReason].length
  if (reasonLength < 1 || reasonLength > 500) {
    return { error: '请填写 1～500 字符的归属理由' }
  }
  if (!selectedTripID) return { error: '请先选择行程' }
  return {
    request: {
      fact_type: candidate.fact_type,
      fact_id: candidate.fact_id,
      desired_trip_id: candidate.current_trip_id === selectedTripID ? null : selectedTripID,
      expected_assignment_id: candidate.current_assignment_id ?? null,
      reason: normalizedReason,
    },
  }
}

export function assignmentFingerprint(request: TripAssignmentRequest): string {
  return JSON.stringify(request)
}
