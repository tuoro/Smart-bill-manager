import { afterEach, describe, expect, it, vi } from 'vitest'

import { parseStoredNotifications } from './notifications'

describe('parseStoredNotifications', () => {
  afterEach(() => vi.restoreAllMocks())

  it('只保留具备标识和标题的通知', () => {
    const result = parseStoredNotifications(
      JSON.stringify([
        { id: 'n1', title: '已完成', severity: 'success', createdAt: 123, read: true },
        { id: '', title: '缺少标识' },
        { id: 'n2', title: '' },
        null,
      ]),
    )

    expect(result).toEqual([
      { id: 'n1', title: '已完成', severity: 'success', createdAt: 123, read: true, detail: undefined },
    ])
  })

  it('修正非法级别和时间字段', () => {
    vi.spyOn(Date, 'now').mockReturnValue(456)

    const result = parseStoredNotifications(
      JSON.stringify([{ id: 'n1', title: '通知', severity: 'critical', createdAt: 'invalid', read: 1 }]),
    )

    expect(result[0]).toMatchObject({ severity: 'info', createdAt: 456, read: false })
  })

  it('遇到损坏 JSON 时返回空列表', () => {
    expect(parseStoredNotifications('{invalid')).toEqual([])
  })
})
