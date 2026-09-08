import { describe, expect, it } from 'vitest'
import { formatMinorUnits, minorToDecimalInput, parseDecimalToMinor } from './money'

describe('金额十进制与最小单位互转', () => {
  it('按币种精度换算', () => {
    expect(parseDecimalToMinor('123.45', 'CNY')).toEqual({ minor: 12345 })
    expect(parseDecimalToMinor('123', 'CNY')).toEqual({ minor: 12300 })
    expect(parseDecimalToMinor('123.4', 'CNY')).toEqual({ minor: 12340 })
    expect(parseDecimalToMinor('0', 'CNY')).toEqual({ minor: 0 })
    // JPY 精度为 0，整数即最小单位
    expect(parseDecimalToMinor('123', 'JPY')).toEqual({ minor: 123 })
  })

  // 这些写法后端 domain.ParseMoney 一律拒绝；前端多接受一种，用户就会在提交时
  // 收到一个本可以在输入时说清楚的错误。
  it('拒绝后端同样拒绝的写法', () => {
    for (const bad of ['', ' 1', '1 ', '+1', '1,234.00', '1.', '.5', '1.2.3', 'abc', '-1']) {
      expect(parseDecimalToMinor(bad, 'CNY')).toHaveProperty('error')
    }
  })

  it('小数位超过币种精度时拒绝并说明精度', () => {
    expect(parseDecimalToMinor('1.234', 'CNY')).toEqual({ error: 'CNY 小数位不能超过 2 位' })
    expect(parseDecimalToMinor('1.2', 'JPY')).toEqual({ error: 'JPY 不使用小数位' })
  })

  it('不支持的币种不猜测精度', () => {
    expect(parseDecimalToMinor('1.00', 'GBP')).toHaveProperty('error')
  })

  it('往返一致', () => {
    for (const [minor, currency] of [
      [12345, 'CNY'],
      [0, 'CNY'],
      [123, 'JPY'],
      [99999999, 'USD'],
    ] as const) {
      const text = minorToDecimalInput(minor, currency)
      expect(parseDecimalToMinor(text, currency)).toEqual({ minor })
    }
  })

  it('展示格式保持带币种前缀，输入格式不带', () => {
    expect(formatMinorUnits(12345, 'CNY')).toBe('CNY 123.45')
    expect(minorToDecimalInput(12345, 'CNY')).toBe('123.45')
  })
})
