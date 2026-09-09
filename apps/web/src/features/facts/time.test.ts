import { describe, expect, it } from 'vitest'
import { formatSystemTime, instantInZone, isRealDate, parseRFC3339 } from './time'

describe('时刻与日期', () => {
  // 票面印的是本地时间，字段存的是绝对时刻；不换算一遍就要人自己心算时差。
  it('renders the instant in the source timezone', () => {
    expect(instantInZone('2026-09-04T08:00:00Z', 'Asia/Shanghai')).toBe('2026-09-04 16:00:00')
    expect(instantInZone('2026-09-04T08:00:00Z', 'UTC')).toBe('2026-09-04 08:00:00')
    // 跨日：UTC 的 4 日晚上在上海已经是 5 日凌晨，业务日期因此不同。
    expect(instantInZone('2026-09-04T20:00:00Z', 'Asia/Shanghai')).toBe('2026-09-05 04:00:00')
    // 午夜按 00 显示，不是某些实现里的 24。
    expect(instantInZone('2026-09-04T16:00:00Z', 'Asia/Shanghai')).toBe('2026-09-05 00:00:00')
    // 时区名无效或时间无效时不猜，宁可不显示。
    expect(instantInZone('2026-09-04T08:00:00Z', 'Mars/Olympus')).toBeNull()
    expect(instantInZone('2026-9-4', 'UTC')).toBeNull()
  })

  // 每一条都逐个对照过 Go 的 time.Parse(time.RFC3339Nano)，取舍一致。
  it('accepts exactly what the backend accepts', () => {
    for (const good of [
      '2026-09-04T08:00:00Z',
      '2026-09-04T16:00:00+08:00',
      '2026-09-04T08:00:00.5Z',
    ]) {
      expect(parseRFC3339(good)).not.toBeNull()
    }
    for (const bad of [
      '2026-9-4T08:00:00Z',
      '2026-09-04 08:00:00Z',
      '2026-09-04T08:00Z',
      '2026-09-04T08:00:00+0800',
      '2026-09-04T08:00:00',
      '2026-02-30T08:00:00Z',
      '2026-09-04t08:00:00Z',
      '2026-09-04T08:00:00z',
      '2026-09-04T24:00:00Z',
      '2026-09-04T08:00:60Z',
      '2026-09-04T08:00:00+25:00',
      '',
    ]) {
      expect(parseRFC3339(bad)).toBeNull()
    }
  })

  // JS 的 Date 会把 2026-02-30 悄悄滚成 03-02，不能直接拿来判断真实性。
  it('rejects calendar dates that do not exist', () => {
    expect(isRealDate('2026-09-04')).toBe(true)
    for (const bad of ['2026-02-30', '2026-13-01', '2026-9-4', '2026/09/04', '']) {
      expect(isRealDate(bad)).toBe(false)
    }
  })

  // 系统时间戳按看的人所在时区显示；非法输入不抛异常，也不显示 Invalid Date。
  it('formats system timestamps and survives bad input', () => {
    expect(formatSystemTime('2026-09-04T08:00:00Z')).toContain('2026')
    expect(formatSystemTime('not-a-time')).toBe('时间未知')
  })
})
