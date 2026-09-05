import { describe, expect, it } from 'vitest'
import type { LocationQuery } from 'vue-router'
import { factListPath, factListQuery, factReturnPath } from './list-model'

describe('单据管理查询范围', () => {
  it('保留字面查询、日期和游标，不接受多值或未知参数', () => {
    expect(factListQuery({ q: '合成 %_\\', cursor: 'next', date_from: '2026-09-01' })).toEqual({
      q: '合成 %_\\',
      cursor: 'next',
      date_from: '2026-09-01',
    })
    const invalid: LocationQuery[] = [{ q: ['a', 'b'] }, { q: null }, { other: 'x' }]
    for (const query of invalid) expect(() => factListQuery(query)).toThrow()
  })
  it('返回路径只能指向同类账单列表', () => {
    expect(factListPath('invoice')).toBe('/invoices')
    expect(factReturnPath('payment', '/payments?q=合成&cursor=next')).toBe(
      '/payments?q=合成&cursor=next',
    )
    for (const value of [
      'https://outside.invalid',
      '//outside.invalid',
      '/invoices',
      '/payments/one',
      '/payments#fragment',
      undefined,
      ['/payments'],
    ])
      expect(factReturnPath('payment', value)).toBe('/payments')
  })
})
