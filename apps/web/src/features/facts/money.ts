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

export type SupportedCurrency = 'CNY' | 'USD' | 'EUR' | 'JPY'

export function currencyExponent(currency: string): number | null {
  if (currency === 'JPY') return 0
  if (currency === 'CNY' || currency === 'USD' || currency === 'EUR') return 2
  return null
}

// 把用户输入的十进制金额转成最小单位。规则与后端 domain.ParseMoney 一一对应：
// 非负、无前导 +、无千位分隔符、无首尾空白、小数位不超过币种精度。
// 前端多接受一种写法都会在提交时被后端拒绝，因此这里不放宽任何一条。
export function parseDecimalToMinor(
  decimal: string,
  currency: string,
): { minor: number } | { error: string } {
  const exponent = currencyExponent(currency)
  if (exponent === null) return { error: '仅支持 CNY、USD、EUR 和 JPY' }
  if (decimal === '' || decimal.trim() !== decimal || decimal.startsWith('+'))
    return { error: '金额必须是普通非负十进制数，不使用空格或正号' }
  const parts = decimal.split('.')
  if (parts.length > 2 || parts[0] === '' || !/^[0-9]+$/.test(parts[0]))
    return { error: '金额格式不正确，不使用千位分隔符' }
  let fraction = ''
  if (parts.length === 2) {
    fraction = parts[1]
    if (fraction === '' || !/^[0-9]+$/.test(fraction)) return { error: '金额格式不正确' }
  }
  if (fraction.length > exponent)
    return {
      error:
        exponent === 0 ? `${currency} 不使用小数位` : `${currency} 小数位不能超过 ${exponent} 位`,
    }
  const padded = fraction.padEnd(exponent, '0')
  const minor = Number(parts[0]) * 10 ** exponent + (padded === '' ? 0 : Number(padded))
  if (!Number.isSafeInteger(minor)) return { error: '金额超出允许范围' }
  return { minor }
}

// 把最小单位显示成可编辑的十进制文本（不带币种前缀，供输入框使用）。
export function minorToDecimalInput(value: number, currency: string): string {
  const exponent = currencyExponent(currency)
  if (exponent === null || !Number.isSafeInteger(value)) return String(value)
  const digits = Math.abs(value)
    .toString()
    .padStart(exponent + 1, '0')
  const amount =
    exponent === 0 ? digits : `${digits.slice(0, -exponent)}.${digits.slice(-exponent)}`
  return `${value < 0 ? '-' : ''}${amount}`
}
