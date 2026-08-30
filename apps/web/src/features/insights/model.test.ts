import { describe, expect, it } from 'vitest'
import type { InsightAggregate, InsightFact } from '../../data/client'
import {
  appendInsightItems,
  buildInsightFilter,
  defaultInsightFilterDraft,
  groupInsightAggregates,
} from './model'

describe('洞察筛选模型', () => {
  it('生成无隐藏日期的封闭默认筛选', () => {
    const draft = defaultInsightFilterDraft()
    expect(buildInsightFilter(draft)).toEqual({
      filter: { fact_type: 'all', allocation_status: 'all', trip_scope: 'all' },
    })
    expect(draft).toEqual(defaultInsightFilterDraft())
  })

  it('保留完整日期、币种、分配与具体行程筛选', () => {
    expect(
      buildInsightFilter({
        fact_type: 'invoice',
        date_from: '2026-08-01',
        date_to: '2026-08-31',
        currency: 'CNY',
        allocation_status: 'partial',
        trip_scope: 'assigned',
        trip_id: '11111111-1111-4111-8111-111111111111',
      }),
    ).toEqual({
      filter: {
        fact_type: 'invoice',
        date_from: '2026-08-01',
        date_to: '2026-08-31',
        currency: 'CNY',
        allocation_status: 'partial',
        trip_scope: 'assigned',
        trip_id: '11111111-1111-4111-8111-111111111111',
      },
    })
  })

  it.each([
    [{ ...defaultInsightFilterDraft(), date_from: '2026-08-01' }, '起止日期必须同时填写'],
    [
      { ...defaultInsightFilterDraft(), date_from: '2026-02-30', date_to: '2026-03-01' },
      '请输入合法且起始不晚于结束的日期范围',
    ],
    [
      { ...defaultInsightFilterDraft(), date_from: '2026-08-02', date_to: '2026-08-01' },
      '请输入合法且起始不晚于结束的日期范围',
    ],
    [
      {
        ...defaultInsightFilterDraft(),
        trip_scope: 'unassigned' as const,
        trip_id: '11111111-1111-4111-8111-111111111111',
      },
      '指定行程时必须选择“已归属行程”',
    ],
  ])('拒绝不完整或矛盾筛选 %#', (draft, message) => {
    expect(buildInsightFilter(draft)).toEqual({ error: message })
  })
})

describe('洞察汇总与分页模型', () => {
  it('按币种分组并保持支付在发票之前', () => {
    const aggregates = [
      aggregate('USD', 'invoice'),
      aggregate('CNY', 'invoice'),
      aggregate('CNY', 'payment'),
    ]
    expect(groupInsightAggregates(aggregates)).toEqual([
      { currency: 'CNY', facts: [aggregate('CNY', 'payment'), aggregate('CNY', 'invoice')] },
      { currency: 'USD', facts: [aggregate('USD', 'invoice')] },
    ])
    expect(aggregates[0]?.currency).toBe('USD')
  })

  it('追加稳定页面并拒绝重复 Fact', () => {
    const payment = fact('payment', '11111111-1111-4111-8111-111111111111')
    const invoice = fact('invoice', '22222222-2222-4222-8222-222222222222')
    expect(appendInsightItems([payment], [invoice])).toEqual([payment, invoice])
    expect(() => appendInsightItems([payment], [payment])).toThrow('洞察分页结果已变化')
  })
})

function aggregate(
  currency: InsightAggregate['currency'],
  factType: InsightAggregate['fact_type'],
): InsightAggregate {
  return {
    currency,
    fact_type: factType,
    count: 1,
    total_minor: 100,
    allocated_minor: 0,
    remaining_minor: 100,
    unallocated_count: 1,
    partial_count: 0,
    allocated_count: 0,
  }
}

function fact(factType: InsightFact['fact_type'], factId: string): InsightFact {
  return {
    fact_type: factType,
    fact_id: factId,
    business_date: '2026-08-01',
    display_name: '合成摘要',
    amount_minor: 100,
    allocated_minor: 0,
    remaining_minor: 100,
    currency: 'CNY',
    allocation_status: 'unallocated',
  }
}
