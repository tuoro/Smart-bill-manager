import type {
  InsightAggregate,
  InsightAllocationStatusFilter,
  InsightFact,
  InsightFactTypeFilter,
  InsightFilter,
  InsightTripScope,
} from '../../data/client'

export type InsightFilterDraft = {
  fact_type: InsightFactTypeFilter
  date_from: string
  date_to: string
  currency: '' | InsightAggregate['currency']
  allocation_status: InsightAllocationStatusFilter
  trip_scope: InsightTripScope
  trip_id: string
}

export type InsightFilterDecision = { filter?: InsightFilter; error?: string }

export const insightFactTypeLabels: Record<InsightFactTypeFilter, string> = {
  all: '支付与发票',
  payment: '仅支付',
  invoice: '仅发票',
}

export const insightAllocationLabels: Record<InsightAllocationStatusFilter, string> = {
  all: '全部分配状态',
  unallocated: '未分配',
  partial: '部分分配',
  allocated: '已分配',
}

export const insightTripScopeLabels: Record<InsightTripScope, string> = {
  all: '全部行程范围',
  assigned: '已归属行程',
  unassigned: '未归属行程',
}

export function defaultInsightFilterDraft(): InsightFilterDraft {
  return {
    fact_type: 'all',
    date_from: '',
    date_to: '',
    currency: '',
    allocation_status: 'all',
    trip_scope: 'all',
    trip_id: '',
  }
}

export function buildInsightFilter(draft: InsightFilterDraft): InsightFilterDecision {
  if ((draft.date_from === '') !== (draft.date_to === '')) {
    return { error: '起止日期必须同时填写' }
  }
  if (draft.date_from) {
    if (
      !validDate(draft.date_from) ||
      !validDate(draft.date_to) ||
      draft.date_from > draft.date_to
    ) {
      return { error: '请输入合法且起始不晚于结束的日期范围' }
    }
  }
  if (draft.trip_id && draft.trip_scope !== 'assigned') {
    return { error: '指定行程时必须选择“已归属行程”' }
  }
  return {
    filter: {
      fact_type: draft.fact_type,
      allocation_status: draft.allocation_status,
      trip_scope: draft.trip_scope,
      ...(draft.date_from ? { date_from: draft.date_from, date_to: draft.date_to } : {}),
      ...(draft.currency ? { currency: draft.currency } : {}),
      ...(draft.trip_id ? { trip_id: draft.trip_id } : {}),
    },
  }
}

export function groupInsightAggregates(
  aggregates: InsightAggregate[],
): { currency: InsightAggregate['currency']; facts: InsightAggregate[] }[] {
  const groups = new Map<InsightAggregate['currency'], InsightAggregate[]>()
  for (const aggregate of aggregates) {
    const current = groups.get(aggregate.currency) ?? []
    current.push(aggregate)
    groups.set(aggregate.currency, current)
  }
  return [...groups.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([currency, facts]) => ({
      currency,
      facts: [...facts].sort((left, right) => right.fact_type.localeCompare(left.fact_type)),
    }))
}

export function appendInsightItems(current: InsightFact[], incoming: InsightFact[]): InsightFact[] {
  const identities = new Set(current.map(insightFactIdentity))
  for (const item of incoming) {
    const identity = insightFactIdentity(item)
    if (identities.has(identity)) {
      throw new Error('洞察分页结果已变化，请重新应用筛选')
    }
    identities.add(identity)
  }
  return [...current, ...incoming]
}

export function insightFactTypeLabel(factType: InsightFact['fact_type']): string {
  return factType === 'payment' ? '支付' : '发票'
}

function insightFactIdentity(item: InsightFact): string {
  return `${item.fact_type}:${item.fact_id}`
}

function validDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const [year, month, day] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day))
  return (
    parsed.getUTCFullYear() === year &&
    parsed.getUTCMonth() === month - 1 &&
    parsed.getUTCDate() === day
  )
}
