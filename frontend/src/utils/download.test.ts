import { describe, expect, it } from 'vitest'

import { getDownloadFilename, getHeaderString, sanitizeDownloadFilename } from './download'

describe('getDownloadFilename', () => {
  it.each([
    ['attachment; filename="report.zip"', 'report.zip'],
    ['attachment; filename=report.zip', 'report.zip'],
    ["attachment; filename*=UTF-8''%E8%A1%8C%E7%A8%8B.zip", '行程.zip'],
    ['', ''],
  ])('解析 Content-Disposition：%s', (header, expected) => {
    expect(getDownloadFilename(header)).toBe(expected)
  })

  it('无法解码扩展文件名时保留原值', () => {
    expect(getDownloadFilename("attachment; filename*=UTF-8''bad%ZZ.zip")).toBe('bad%ZZ.zip')
  })
})

describe('sanitizeDownloadFilename', () => {
  it('替换路径与控制字符并限制长度', () => {
    const filename = sanitizeDownloadFilename(`../trip:\u0000${'a'.repeat(140)}.zip`, 'fallback.zip')
    expect(filename).not.toMatch(/[\\/:]/)
    expect(filename).not.toContain(String.fromCharCode(0))
    expect(filename.length).toBeLessThanOrEqual(120)
  })

  it('空文件名回退到默认值', () => {
    expect(sanitizeDownloadFilename('  ', 'fallback.zip')).toBe('fallback.zip')
  })
})

describe('getHeaderString', () => {
  it('仅接受字符串响应头', () => {
    expect(getHeaderString('message/rfc822')).toBe('message/rfc822')
    expect(getHeaderString(['message/rfc822'])).toBeUndefined()
    expect(getHeaderString(null)).toBeUndefined()
  })
})
