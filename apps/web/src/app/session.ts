import { readonly, ref } from 'vue'
import { ApiError, api, setAuthenticationLifecycle, type Session } from '../data/client'

const current = ref<Session | null>(null)
const resolved = ref(false)
let generation = 0
let resolutionID = 0
let pending: { id: number; promise: Promise<Session | null> } | null = null
let mutation: Promise<unknown> | null = null
type InvalidationReason = 'expired' | 'signed_out' | 'password_changed'
const listeners = new Set<(reason: InvalidationReason) => void>()

function invalidate(reason: InvalidationReason = 'expired'): void {
  generation++
  resolutionID++
  pending = null
  current.value = null
  resolved.value = true
  for (const listener of listeners) listener(reason)
}

setAuthenticationLifecycle({
  generation: () => generation,
  expired: (requestGeneration) => {
    if (requestGeneration === generation) invalidate('expired')
  },
})

async function resolveSession(force = false): Promise<Session | null> {
  // Cookie 写入期间不能发出旧身份探测；失败仍由原显式操作的调用方呈现。
  while (mutation) {
    const active = mutation
    await active.then(
      () => undefined,
      () => undefined,
    )
  }
  if (!force && resolved.value) return current.value
  if (!force && pending) return pending.promise
  const capturedGeneration = generation
  const id = ++resolutionID
  const isCurrent = () => capturedGeneration === generation && id === resolutionID
  const promise = api
    .session()
    .then((session) => {
      if (isCurrent()) {
        current.value = session
        resolved.value = true
      }
      return current.value
    })
    .catch((error: unknown) => {
      if (!isCurrent()) return current.value
      if (!(error instanceof ApiError) || error.status !== 401) throw error
      invalidate('expired')
      return null
    })
    .finally(() => {
      if (pending?.id === id) pending = null
    })
  pending = { id, promise }
  return promise
}

// 登录/退出/改密会设置或清除 Cookie，必须按发送顺序完成，不能只保护 Vue 缓存。
function mutateAuthentication<T>(
  operation: (capturedGeneration: number) => Promise<T>,
): Promise<T> {
  const previous = mutation
  const capturedGeneration = ++generation
  resolutionID++
  pending = null
  const next = (async () => {
    // 前一操作的错误由原调用方处理；排队中的显式新操作仍可继续。
    if (previous)
      await previous.then(
        () => undefined,
        () => undefined,
      )
    return operation(capturedGeneration)
  })()
  mutation = next
  const finish = () => {
    if (mutation === next) mutation = null
  }
  void next.then(finish, finish)
  return next
}

function login(email: string, password: string, tenantId = ''): Promise<Session> {
  return mutateAuthentication(async (capturedGeneration) => {
    const session = await api.login(email, password, tenantId)
    if (generation === capturedGeneration) {
      current.value = session
      resolved.value = true
    }
    return session
  })
}

function logout(): Promise<void> {
  return mutateAuthentication(async (capturedGeneration) => {
    try {
      await api.logout()
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 401 || error.code !== 'unauthenticated')
        throw error
    }
    if (generation === capturedGeneration) invalidate('signed_out')
  })
}

function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return mutateAuthentication(async (capturedGeneration) => {
    await api.changePassword(currentPassword, newPassword)
    if (generation === capturedGeneration) invalidate('password_changed')
  })
}

export const sessionStore = {
  current: readonly(current),
  resolved: readonly(resolved),
  resolve: resolveSession,
  login,
  logout,
  changePassword,
  invalidate,
  onInvalidated(listener: (reason: InvalidationReason) => void): () => void {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },
}
