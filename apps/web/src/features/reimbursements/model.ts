import type {
  ReimbursementDetail,
  ReimbursementPolicyFinding,
  ReimbursementPolicySnapshot,
  ReimbursementStatus,
  ReimbursementStatusRequest,
  ReimbursementSubmissionRequest,
} from '../../data/client'

export const reimbursementStatusLabels: Record<ReimbursementStatus, string> = {
  submitted: '已提交',
  reimbursed: '已报销',
  rejected: '已驳回',
}

const findingLabels: Record<ReimbursementPolicyFinding['code'], string> = {
  missing_invoice: '所选支付缺少所选发票',
  amount_conflict: '所选支付与发票的分配金额不一致',
  duplicate_reimbursement: '该 Fact 已出现在其他有效报销中',
}

export type RequestDecision<T> = { request?: T; error?: string }

export function reimbursementFindingLabel(code: ReimbursementPolicyFinding['code']): string {
  return findingLabels[code]
}

export function reimbursementStatusActions(status: ReimbursementStatus): ReimbursementStatus[] {
  if (status === 'submitted') return ['reimbursed', 'rejected']
  return ['submitted']
}

export function buildReimbursementSubmission(
  snapshot: ReimbursementPolicySnapshot | undefined,
  selectedAssignmentIDs: string[],
  findingsAcknowledged: boolean,
  reason: string,
): RequestDecision<ReimbursementSubmissionRequest> {
  if (!snapshot) return { error: '请先运行政策预检' }
  const selected = canonicalIDs(selectedAssignmentIDs)
  const snapshotIDs = canonicalIDs(snapshot.items.map((item) => item.assignment_id))
  if (selected.length === 0 || selected.length > 200 || !sameStrings(selected, snapshotIDs)) {
    return { error: '所选项目已变化，请重新运行政策预检' }
  }
  if (snapshot.findings.length > 0 && !findingsAcknowledged) {
    return { error: '请先确认当前完整政策提示' }
  }
  const normalizedReason = reason.trim()
  if ([...normalizedReason].length < 1 || [...normalizedReason].length > 500) {
    return { error: '请填写 1～500 字符的提交理由' }
  }
  return {
    request: {
      trip_id: snapshot.trip.id,
      assignment_ids: selected,
      expected_snapshot_hash: snapshot.snapshot_hash,
      acknowledged_finding_keys: snapshot.findings.map((finding) => finding.finding_key).sort(),
      reason: normalizedReason,
    },
  }
}

export function buildReimbursementStatusRequest(
  detail: ReimbursementDetail,
  desiredStatus: ReimbursementStatus,
  reason: string,
): RequestDecision<ReimbursementStatusRequest> {
  if (!reimbursementStatusActions(detail.status).includes(desiredStatus)) {
    return { error: '当前状态不能执行该操作' }
  }
  const normalizedReason = reason.trim()
  if ([...normalizedReason].length < 1 || [...normalizedReason].length > 500) {
    return { error: '请填写 1～500 字符的状态理由' }
  }
  return {
    request: {
      expected_status: detail.status,
      desired_status: desiredStatus,
      expected_version: detail.version,
      reason: normalizedReason,
    },
  }
}

export function reimbursementRequestFingerprint(
  resourceID: string,
  request: ReimbursementSubmissionRequest | ReimbursementStatusRequest,
): string {
  return JSON.stringify({ resource_id: resourceID, request })
}

function canonicalIDs(input: string[]): string[] {
  return [...new Set(input)].sort()
}

function sameStrings(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index])
}
