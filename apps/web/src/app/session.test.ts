import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import 'vue'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const identity = (id: string) => ({ user: { id }, capabilities: [] })
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })
const expired = () => response({ error: { code: 'unauthenticated', message: '会话已失效' } }, 401)

beforeEach(() => {
  vi.resetModules()
  vi.stubGlobal('document', { cookie: '' })
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('account session lifecycle', () => {
  it('treats an already expired logout as complete but keeps uncertain network failures visible', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(response(identity('current')))
        .mockRejectedValueOnce(new Error('synthetic logout unavailable'))
        .mockResolvedValueOnce(expired()),
    )
    const { sessionStore: store } = await import('./session')
    await store.resolve()
    await expect(store.logout()).rejects.toThrow('synthetic logout unavailable')
    expect(store.current.value?.user.id).toBe('current')
    await store.logout()
    expect(store.current.value).toBeNull()
  })
  it('shares normal resolution and prevents older force success from replacing current identity', async () => {
    const first = deferred<Response>(),
      second = deferred<Response>()
    const fetch = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    vi.stubGlobal('fetch', fetch)
    const { sessionStore: store } = await import('./session')
    const a = store.resolve(),
      shared = store.resolve(),
      b = store.resolve(true)
    expect(fetch).toHaveBeenCalledTimes(2)
    second.resolve(response(identity('new')))
    await b
    first.resolve(response(identity('old')))
    await Promise.all([a, shared])
    expect(store.current.value?.user.id).toBe('new')
  })

  it('ignores an older forced probe 401 after the newer probe succeeds', async () => {
    const first = deferred<Response>(),
      second = deferred<Response>()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise),
    )
    const { sessionStore: store } = await import('./session')
    const invalidated = vi.fn()
    store.onInvalidated(invalidated)
    const a = store.resolve(true),
      b = store.resolve(true)
    second.resolve(response(identity('new')))
    await b
    first.resolve(expired())
    await a
    expect(store.current.value?.user.id).toBe('new')
    expect(invalidated).not.toHaveBeenCalled()
  })

  it('ignores old business 401 after a new login but redirects on a current 401', async () => {
    const old = deferred<Response>()
    const fetch = vi
      .fn()
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce(response(identity('new')))
      .mockResolvedValueOnce(expired())
    vi.stubGlobal('fetch', fetch)
    const { sessionStore: store } = await import('./session')
    const { api } = await import('../data/client')
    const failed = api.members().catch((error) => error)
    await store.login('synthetic@example.invalid', 'synthetic-password')
    old.resolve(expired())
    await failed
    expect(store.current.value?.user.id).toBe('new')
    const invalidated = vi.fn()
    store.onInvalidated(invalidated)
    await expect(api.members()).rejects.toMatchObject({ status: 401 })
    expect(store.current.value).toBeNull()
    expect(invalidated).toHaveBeenCalledWith('expired')
  })

  it('does not cache a network failure and notifies a fresh expired probe', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockRejectedValueOnce(new Error('synthetic network failure'))
        .mockResolvedValueOnce(expired()),
    )
    const { sessionStore: store } = await import('./session')
    const invalidated = vi.fn()
    store.onInvalidated(invalidated)
    await expect(store.resolve()).rejects.toThrow('synthetic network failure')
    expect(store.resolved.value).toBe(false)
    expect(await store.resolve()).toBeNull()
    expect(invalidated).toHaveBeenCalledWith('expired')
  })

  it('serializes cookie mutations and waits before reading the resulting session', async () => {
    const logout = deferred<Response>(),
      login = deferred<Response>()
    const fetch = vi.fn().mockReturnValueOnce(logout.promise).mockReturnValueOnce(login.promise)
    vi.stubGlobal('fetch', fetch)
    const { sessionStore: store } = await import('./session')
    const a = store.logout(),
      b = store.login('synthetic@example.invalid', 'synthetic-password'),
      probe = store.resolve()
    expect(fetch).toHaveBeenCalledTimes(1)
    logout.resolve(new Response(null, { status: 204 }))
    await a
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    login.resolve(response(identity('new')))
    await b
    await probe
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(store.current.value?.user.id).toBe('new')
  })

  it('preserves the session on wrong current password and invalidates all local state on success', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(response(identity('current')))
        .mockResolvedValueOnce(
          response({ error: { code: 'invalid_credentials', message: '密码不正确' } }, 401),
        )
        .mockResolvedValueOnce(new Response(null, { status: 204 })),
    )
    const { sessionStore: store } = await import('./session')
    await store.resolve()
    await expect(store.changePassword('synthetic-old', 'synthetic-new')).rejects.toMatchObject({
      code: 'invalid_credentials',
    })
    expect(store.current.value?.user.id).toBe('current')
    const invalidated = vi.fn()
    store.onInvalidated(invalidated)
    await store.changePassword('synthetic-old', 'synthetic-new')
    expect(store.current.value).toBeNull()
    expect(invalidated).toHaveBeenCalledWith('password_changed')
  })
})
