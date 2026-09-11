import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomUUID } from './random'

const v4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('randomUUID', () => {
  afterEach(() => vi.restoreAllMocks())

  it('优先使用原生 randomUUID', () => {
    const native = vi.spyOn(globalThis.crypto, 'randomUUID')
    expect(randomUUID()).toMatch(v4)
    expect(native).toHaveBeenCalledOnce()
  })

  it('明文局域网访问（无 randomUUID）时仍能生成合法 v4，且不重复', () => {
    // 模拟非安全上下文：randomUUID 不存在，getRandomValues 仍在。
    Object.defineProperty(globalThis.crypto, 'randomUUID', { value: undefined, configurable: true })
    try {
      const seen = new Set<string>()
      for (let i = 0; i < 200; i += 1) {
        const id = randomUUID()
        expect(id).toMatch(v4)
        seen.add(id)
      }
      expect(seen.size).toBe(200)
    } finally {
      delete (globalThis.crypto as { randomUUID?: unknown }).randomUUID
    }
  })
})
