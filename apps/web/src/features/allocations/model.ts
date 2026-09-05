import type { AllocationAdjustmentRequest, AllocationWorkspace } from '../../data/client'

const maxSafeMinorUnits = 9_007_199_254_740_991

export type AllocationDraftRow = {
  target: AllocationWorkspace['targets'][number]
  selected: boolean
  amountText: string
}

export type AllocationDraftValidation = {
  request?: AllocationAdjustmentRequest
  targetErrors: Record<string, string>
  reasonError: string
  planError: string
  withdrawAllError: string
  changed: boolean
  desiredTotalMinor: number
}

export function createAllocationDraft(workspace: AllocationWorkspace): AllocationDraftRow[] {
  return workspace.targets.map((target) => ({
    target,
    selected: Boolean(target.current_link_id),
    amountText: target.current_link_id ? String(target.current_allocated_minor) : '',
  }))
}

export function validateAllocationDraft(
  workspace: AllocationWorkspace,
  rows: AllocationDraftRow[],
  reason: string,
  withdrawAllConfirmed: boolean,
): AllocationDraftValidation {
  const targetErrors: Record<string, string> = {}
  const desired: AllocationAdjustmentRequest['desired_allocations'] = []
  let desiredTotalMinor = 0
  for (const row of rows) {
    if (!row.selected) continue
    if (!/^[1-9][0-9]*$/.test(row.amountText)) {
      targetErrors[row.target.id] = '请输入正整数最小单位金额'
      continue
    }
    const amount = Number(row.amountText)
    if (!Number.isSafeInteger(amount) || amount > maxSafeMinorUnits) {
      targetErrors[row.target.id] = '金额超出浏览器可安全处理范围'
      continue
    }
    if (amount > row.target.maximum_allocatable_minor) {
      targetErrors[row.target.id] = '金额超过该目标当前可调整上限'
      continue
    }
    desiredTotalMinor += amount
    desired.push({ target_fact_id: row.target.id, allocated_minor: amount })
  }
  desired.sort((left, right) => left.target_fact_id.localeCompare(right.target_fact_id))

  let planError = ''
  if (rows.filter((row) => row.selected).length > 200) planError = '一个分配计划最多选择 200 个目标'
  if (desiredTotalMinor > workspace.anchor.amount_minor) {
    planError = '期望分配合计超过当前账单总额'
  }
  const current = workspace.links
    .map((link) => ({ target_fact_id: link.target_fact_id, allocated_minor: link.allocated_minor }))
    .sort((left, right) => left.target_fact_id.localeCompare(right.target_fact_id))
  const changed = JSON.stringify(current) !== JSON.stringify(desired)
  if (!changed && Object.keys(targetErrors).length === 0) {
    planError = '分配计划没有变化'
  }

  const trimmedReason = reason.trim()
  let reasonError = ''
  if (!trimmedReason) reasonError = '请填写本次调整理由'
  else if ([...trimmedReason].length > 500) reasonError = '调整理由不能超过 500 个字符'

  let withdrawAllError = ''
  if (workspace.links.length > 0 && desired.length === 0 && !withdrawAllConfirmed) {
    withdrawAllError = '撤销全部分配前需要再次确认'
  }
  const valid =
    Object.keys(targetErrors).length === 0 && !planError && !reasonError && !withdrawAllError
  return {
    request: valid
      ? {
          expected_plan_hash: workspace.plan_hash,
          desired_allocations: desired,
          reason: trimmedReason,
        }
      : undefined,
    targetErrors,
    reasonError,
    planError,
    withdrawAllError,
    changed,
    desiredTotalMinor,
  }
}

export function allocationModeLabel(
  workspace: AllocationWorkspace,
  rows: AllocationDraftRow[],
): string {
  const current = new Map(
    workspace.links.map((link) => [link.target_fact_id, link.allocated_minor]),
  )
  const selected = rows.filter((row) => row.selected)
  let ended = 0
  let created = 0
  for (const [targetID, amount] of current) {
    const row = selected.find((entry) => entry.target.id === targetID)
    if (!row || Number(row.amountText) !== amount) ended += 1
  }
  for (const row of selected) {
    const amount = Number(row.amountText)
    if (!current.has(row.target.id) || current.get(row.target.id) !== amount) created += 1
  }
  if (ended === 0 && created > 0) return '补充分配'
  if (ended > 0 && created === 0) return '撤销分配'
  if (ended > 0 && created > 0) return '替换分配'
  return '没有变化'
}
