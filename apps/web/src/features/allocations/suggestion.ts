import type { AllocationWorkspace } from '../../data/client'

const maximumPlanTargets = 200
const recommendationDays = 30
type Target = AllocationWorkspace['targets'][number]

export type AllocationSuggestion = {
  status: 'ready' | 'amount_required' | 'ambiguous' | 'none' | 'unavailable'
  message: string
  items: Array<{ target: Target; amountMinor: number | null; maximumMinor: number }>
  remainingMinor: number | null
}

export function buildAllocationSuggestion(
  workspace: AllocationWorkspace,
  initialScope = true,
): AllocationSuggestion {
  const unavailable = (message: string): AllocationSuggestion => ({
    status: 'unavailable',
    message,
    items: [],
    remainingMinor: null,
  })
  if (!initialScope)
    return unavailable('已进行搜索或翻页，请手工选择；刷新完整工作区后才重新准备建议。')
  if (workspace.next_cursor)
    return unavailable('初始候选尚未完整加载，不会把这一页当成全部候选。请手工查找和分配。')
  const anchor = workspace.anchor
  if (
    ![anchor.amount_minor, anchor.allocated_minor, anchor.remaining_minor].every(validMinor) ||
    anchor.amount_minor - anchor.allocated_minor !== anchor.remaining_minor
  )
    return unavailable('当前金额无法安全计算，不能准备分配建议。')
  const ids = new Set(workspace.targets.map((target) => target.id))
  if (
    ids.size !== workspace.targets.length ||
    workspace.links.some((link) => {
      const target = workspace.targets.find((entry) => entry.id === link.target_fact_id)
      return (
        !target ||
        target.current_link_id !== link.id ||
        target.current_allocated_minor !== link.allocated_minor
      )
    })
  )
    return unavailable('当前关联资料不完整，请刷新后核对，不能准备补充建议。')
  if (anchor.remaining_minor === 0)
    return {
      status: 'none',
      message: '当前没有剩余可分配金额；既有关联的调整仍由你手工决定。',
      items: [],
      remainingMinor: 0,
    }

  const targets = workspace.targets
    .filter(
      (target) =>
        !target.current_link_id &&
        target.fact_type !== anchor.fact_type &&
        target.currency === anchor.currency &&
        target.name_exact &&
        Number.isInteger(target.date_distance_days) &&
        target.date_distance_days >= 0 &&
        target.date_distance_days <= recommendationDays &&
        target.remaining_minor > 0,
    )
    .sort((left, right) => left.id.localeCompare(right.id))
  if (!targets.length)
    return {
      status: 'none',
      message: '没有满足同名、同币种和 30 天范围的新目标建议；仍可手工核对其他候选。',
      items: [],
      remainingMinor: anchor.remaining_minor,
    }
  if (
    targets.some(
      (target) =>
        ![
          target.amount_minor,
          target.allocated_minor,
          target.remaining_minor,
          target.maximum_allocatable_minor,
        ].every(validMinor) ||
        target.amount_minor - target.allocated_minor !== target.remaining_minor ||
        target.maximum_allocatable_minor !== target.remaining_minor,
    )
  )
    return unavailable('候选金额无法安全计算，请刷新后核对。')
  if (workspace.links.length + targets.length > maximumPlanTargets)
    return unavailable('采用后会超过 200 个分配目标，请手工缩小本次计划。')
  const total = targets.reduce((sum, target) => sum + target.remaining_minor, 0)
  if (!Number.isSafeInteger(total))
    return unavailable('候选合计超出可安全计算范围，请手工缩小本次计划。')
  if (targets.length > 1 && total !== anchor.remaining_minor) {
    return {
      status: 'ambiguous',
      message: '存在多种分配可能，余额合计也不吻合。请自行选择目标与金额，不会自动凑数或选第一条。',
      items: [],
      remainingMinor: anchor.remaining_minor,
    }
  }
  const amountRequired = total !== anchor.remaining_minor
  return {
    status: amountRequired ? 'amount_required' : 'ready',
    message: amountRequired
      ? '当前只有一个同名新目标，但双方余额不同。可采用目标，本次实际金额需由你填写。'
      : targets.length === 1
        ? '当前只有一个同名新目标，双方余额相等。已准备待核对金额。'
        : '这些同名新目标的余额合计恰好等于当前剩余，已准备待核对组合。',
    items: targets.map((target) => ({
      target,
      amountMinor: amountRequired ? null : target.remaining_minor,
      maximumMinor: Math.min(anchor.remaining_minor, target.remaining_minor),
    })),
    remainingMinor: amountRequired ? null : 0,
  }
}

function validMinor(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 0
}
