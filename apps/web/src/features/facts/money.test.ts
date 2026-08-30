import { describe, expect, it } from 'vitest'
import { formatMinorUnits } from './money'

describe('formatMinorUnits', () => {
  it('keeps two decimal minor units exact for CNY, USD and EUR', () => {
    expect(formatMinorUnits(0, 'CNY')).toBe('CNY 0.00')
    expect(formatMinorUnits(5, 'USD')).toBe('USD 0.05')
    expect(formatMinorUnits(12_345, 'EUR')).toBe('EUR 123.45')
    expect(formatMinorUnits(-1, 'CNY')).toBe('CNY -0.01')
  })

  it('does not invent decimal places for JPY', () => {
    expect(formatMinorUnits(123_456, 'JPY')).toBe('JPY 123456')
  })

  it('preserves the maximum safe integer without floating-point division', () => {
    expect(formatMinorUnits(Number.MAX_SAFE_INTEGER, 'CNY')).toBe('CNY 90071992547409.91')
  })
})
