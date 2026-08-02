import { describe, expect, it } from 'vitest'

import { createConcurrencyLimiter } from './client'

describe('createConcurrencyLimiter', () => {
  it('达到上限后排队，并在释放时继续下一个请求', async () => {
    const limiter = createConcurrencyLimiter(1)
    const releaseFirst = await limiter.acquire()
    let secondGranted = false
    const second = limiter.acquire().then((release) => {
      secondGranted = true
      return release
    })

    await Promise.resolve()
    expect(secondGranted).toBe(false)

    releaseFirst()
    const releaseSecond = await second
    expect(secondGranted).toBe(true)

    releaseFirst()
    releaseSecond()
  })

  it('将无效上限收敛为至少一个并发请求', async () => {
    const limiter = createConcurrencyLimiter(Number.NaN)
    const release = await limiter.acquire()
    expect(release).toBeTypeOf('function')
    release()
  })
})
