import { describe, expect, it } from 'vitest'

import { getApiErrorMessage, isRequestCanceled } from './http'

describe('isRequestCanceled', () => {
  it.each([
    [{ code: 'ERR_CANCELED' }, true],
    [{ name: 'CanceledError' }, true],
    [{ name: 'AbortError' }, true],
    [{ code: 'NETWORK_ERROR' }, false],
    [null, false],
  ] as const)('正确识别取消请求 %#', (error, expected) => {
    expect(isRequestCanceled(error)).toBe(expected)
  })
})

describe('getApiErrorMessage', () => {
  it('优先使用接口返回的 message', () => {
    const error = { response: { data: { message: '接口错误' } } }
    expect(getApiErrorMessage(error, '默认错误')).toBe('接口错误')
  })

  it('在没有详细错误时返回默认信息', () => {
    expect(getApiErrorMessage({}, '默认错误')).toBe('默认错误')
  })

  it('依次回退到接口 error 和原生错误消息', () => {
    expect(getApiErrorMessage({ response: { data: { error: '详细错误' } } }, '默认错误')).toBe('详细错误')
    expect(getApiErrorMessage(new Error('网络错误'), '默认错误')).toBe('网络错误')
  })

  it('忽略非结构化对象并安全返回默认信息', () => {
    expect(getApiErrorMessage('invalid', '默认错误')).toBe('默认错误')
    expect(getApiErrorMessage(null, '默认错误')).toBe('默认错误')
  })
})
