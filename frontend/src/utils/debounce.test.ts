import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { debounce } from './debounce'

describe('debounce', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('window', globalThis)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('只执行等待窗口内的最后一次调用', () => {
    const callback = vi.fn()
    const debounced = debounce(callback, 100)

    debounced('first')
    debounced('second')
    vi.advanceTimersByTime(99)
    expect(callback).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(callback).toHaveBeenCalledOnce()
    expect(callback).toHaveBeenCalledWith('second')
  })

  it('取消后不再执行回调', () => {
    const callback = vi.fn()
    const debounced = debounce(callback, 100)

    debounced()
    debounced.cancel()
    vi.runAllTimers()

    expect(callback).not.toHaveBeenCalled()
  })
})
