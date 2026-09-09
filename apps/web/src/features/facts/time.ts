// 时间与日期的形状要求跟着后端 domain 走：RFC3339 要求大写 T、秒必填、
// 偏移量带冒号；日期是 YYYY-MM-DD。
const rfc3339Pattern =
  /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/
const datePattern = /^\d{4}-\d{2}-\d{2}$/

// Date 不能用来判断真实性：JS 会把 2026-02-30 悄悄滚成 2026-03-02，而后端
// time.Parse 直接拒绝。日历部分回写比对，时分秒与偏移量按范围逐段检查。
export function isRealDate(value: string): boolean {
  if (!datePattern.test(value)) return false
  const instant = new Date(`${value}T00:00:00Z`)
  return !Number.isNaN(instant.getTime()) && instant.toISOString().slice(0, 10) === value
}

export function parseRFC3339(value: string): Date | null {
  const match = rfc3339Pattern.exec(value)
  if (!match) return null
  const [, date, hour, minute, second, zone] = match
  if (!isRealDate(date)) return null
  if (Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59) return null
  if (zone !== 'Z' && (Number(zone.slice(1, 3)) > 23 || Number(zone.slice(4, 6)) > 59)) return null
  const instant = new Date(value)
  return Number.isNaN(instant.getTime()) ? null : instant
}

// 交易时间存的是绝对时刻，票面印的是来源时区的本地时间。让人拿 RFC3339 去跟
// 票面对账等于要他心算时差，这里把同一时刻按来源时区再显示一遍。
export function instantInZone(value: string, timezone: string): string | null {
  const instant = parseRFC3339(value)
  if (!instant) return null
  let parts: Intl.DateTimeFormatPart[]
  try {
    parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    }).formatToParts(instant)
  } catch {
    // 时区名无效时不猜：宁可不显示，也不显示一个错的本地时间。
    return null
  }
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((entry) => entry.type === type)?.value ?? ''
  const hour = part('hour') === '24' ? '00' : part('hour')
  return `${part('year')}-${part('month')}-${part('day')} ${hour}:${part('minute')}:${part('second')}`
}

// 系统时间戳（入库、收件、过期）说的是「这台机器上的人什么时候看到它」，
// 按浏览器所在时区显示，与账单自身的来源时区无关，两者不要混用。
export function formatSystemTime(value: string): string {
  const instant = new Date(value)
  if (Number.isNaN(instant.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    instant,
  )
}
