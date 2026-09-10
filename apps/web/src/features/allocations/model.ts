import type { AllocationAdjustmentRequest, AllocationWorkspace } from '../../data/client'
import { minorToDecimalInput, parseDecimalToMinor } from '../facts/money'

const maxSafeMinorUnits = 9_007_199_254_740_991

export type AllocationDraftRow = {
  target: AllocationWorkspace['targets'][number]
  selected: boolean
  amountText: string
}

export type AllocationDraftValidation = {
  request?: AllocationAdjustmentRequest
  targetErrors: Record<string, string>
  planError: string
  withdrawAllError: string
  changed: boolean
  desiredTotalMinor: number
}

export function createAllocationDraft(workspace: AllocationWorkspace): AllocationDraftRow[] {
  return workspace.targets.map((target) => ({
    target,
    selected: Boolean(target.current_link_id),
    // 与输入口径一致：显示为按币种精度的十进制，而不是最小单位整数。
    amountText: target.current_link_id
      ? minorToDecimalInput(target.current_allocated_minor, target.currency)
      : '',
  }))
}

export function allocationDraftChanged(
  workspace: AllocationWorkspace,
  rows: AllocationDraftRow[],
): boolean {
  const signature = (items: AllocationDraftRow[]) =>
    JSON.stringify(
      items
        .filter((row) => row.selected || row.amountText)
        .map((row) => [row.target.id, row.selected, row.amountText])
        .sort((left, right) => String(left[0]).localeCompare(String(right[0]))),
    )
  return signature(rows) !== signature(createAllocationDraft(workspace))
}

export function validateAllocationDraft(
  workspace: AllocationWorkspace,
  rows: AllocationDraftRow[],
  withdrawAllConfirmed: boolean,
): AllocationDraftValidation {
  const targetErrors: Record<string, string> = {}
  const desired: AllocationAdjustmentRequest['desired_allocations'] = []
  let desiredTotalMinor = 0
  for (const row of rows) {
    if (!row.selected) continue
    // 与审核台同一口径：用户输入十进制，按币种精度换算回最小单位。
    const parsedAmount = parseDecimalToMinor(row.amountText, row.target.currency)
    if ('error' in parsedAmount) {
      targetErrors[row.target.id] = parsedAmount.error
      continue
    }
    const amount = parsedAmount.minor
    if (amount <= 0) {
      targetErrors[row.target.id] = '分配金额必须大于零'
      continue
    }
    if (amount > maxSafeMinorUnits) {
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

  let withdrawAllError = ''
  if (workspace.links.length > 0 && desired.length === 0 && !withdrawAllConfirmed) {
    withdrawAllError = '撤销全部分配前需要再次确认'
  }
  const valid = Object.keys(targetErrors).length === 0 && !planError && !withdrawAllError
  return {
    request: valid
      ? {
          expected_plan_hash: workspace.plan_hash,
          desired_allocations: desired,
        }
      : undefined,
    targetErrors,
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
  // amountText 是按币种精度的十进制，必须换算回最小单位再与既有分配比较；
  // 直接 Number() 会把 "4.00" 当成 4，把未改动的分配误判成替换。
  const minorOf = (row: AllocationDraftRow): number | null => {
    const parsed = parseDecimalToMinor(row.amountText, row.target.currency)
    return 'error' in parsed ? null : parsed.minor
  }
  for (const [targetID, amount] of current) {
    const row = selected.find((entry) => entry.target.id === targetID)
    if (!row || minorOf(row) !== amount) ended += 1
  }
  for (const row of selected) {
    const amount = minorOf(row)
    if (!current.has(row.target.id) || current.get(row.target.id) !== amount) created += 1
  }
  if (ended === 0 && created > 0) return '补充分配'
  if (ended > 0 && created === 0) return '撤销分配'
  if (ended > 0 && created > 0) return '替换分配'
  return '没有变化'
}
