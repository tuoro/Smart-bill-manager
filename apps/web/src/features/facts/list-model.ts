import type { LocationQuery } from 'vue-router'
import type { FactKind, FactListQuery } from '../../data/client'

const queryKeys = ['cursor', 'limit', 'date_from', 'date_to', 'q', 'allocation_status'] as const

export function factListQuery(query: LocationQuery): FactListQuery {
  const result: FactListQuery = {}
  for (const [key, value] of Object.entries(query)) {
    if (!queryKeys.includes(key as (typeof queryKeys)[number]) || typeof value !== 'string') {
      throw new Error('筛选参数不合法，请返回首屏重新查询')
    }
    result[key as keyof FactListQuery] = value
  }
  return result
}

export function factListPath(kind: FactKind): string {
  return kind === 'payment' ? '/payments' : '/invoices'
}

export function factReturnPath(kind: FactKind, value: unknown): string {
  const base = factListPath(kind)
  if (
    typeof value !== 'string' ||
    !(value === base || value.startsWith(`${base}?`)) ||
    value.includes('#')
  )
    return base
  return value
}
