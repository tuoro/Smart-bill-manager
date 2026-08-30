export function formatMinorUnits(value: number, currency: 'CNY' | 'USD' | 'EUR' | 'JPY'): string {
  if (!Number.isSafeInteger(value)) return `${currency} ${String(value)}`
  const exponent = currency === 'JPY' ? 0 : 2
  const negative = value < 0
  const digits = BigInt(Math.abs(value))
    .toString()
    .padStart(exponent + 1, '0')
  const amount =
    exponent === 0 ? digits : `${digits.slice(0, -exponent)}.${digits.slice(-exponent)}`
  return `${currency} ${negative ? '-' : ''}${amount}`
}
