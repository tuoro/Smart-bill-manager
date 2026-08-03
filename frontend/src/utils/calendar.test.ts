import { describe, expect, it } from 'vitest'

import { primeDateSlotMonthToIndex, primeMonthEventToIndex } from './calendar'

describe('primeMonthEventToIndex', () => {
  it.each([
    [1, 0],
    [6, 5],
    [12, 11],
  ])('将 PrimeVue 月份事件 %i 转为索引 %i', (month, expected) => {
    expect(primeMonthEventToIndex(month)).toBe(expected)
  })

  it('限制异常事件值的范围', () => {
    expect(primeMonthEventToIndex(0)).toBe(0)
    expect(primeMonthEventToIndex(13)).toBe(11)
    expect(primeMonthEventToIndex(Number.NaN)).toBe(0)
  })
})

describe('primeDateSlotMonthToIndex', () => {
  it.each([
    [0, 0],
    [5, 5],
    [11, 11],
  ])('保留 PrimeVue 日期槽月份索引 %i', (month, expected) => {
    expect(primeDateSlotMonthToIndex(month)).toBe(expected)
  })

  it('限制异常插槽值的范围', () => {
    expect(primeDateSlotMonthToIndex(-1)).toBe(0)
    expect(primeDateSlotMonthToIndex(12)).toBe(11)
    expect(primeDateSlotMonthToIndex(Number.POSITIVE_INFINITY)).toBe(0)
  })
})
