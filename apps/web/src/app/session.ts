import { readonly, ref } from 'vue'
import { ApiError, api, type Session } from '../data/client'

const current = ref<Session | null>(null)
const resolved = ref(false)
let pending: Promise<Session | null> | null = null

async function resolveSession(force = false): Promise<Session | null> {
  if (!force && resolved.value) return current.value
  if (!force && pending) return pending
  pending = api
    .session()
    .then((session) => {
      current.value = session
      return session
    })
    .catch((error: unknown) => {
      if (!(error instanceof ApiError) || error.status !== 401) throw error
      current.value = null
      return null
    })
    .finally(() => {
      resolved.value = true
      pending = null
    })
  return pending
}

async function login(email: string, password: string): Promise<Session> {
  const session = await api.login(email, password)
  current.value = session
  resolved.value = true
  return session
}

async function logout(): Promise<void> {
  try {
    await api.logout()
  } finally {
    current.value = null
    resolved.value = true
  }
}

export const sessionStore = {
  current: readonly(current),
  resolved: readonly(resolved),
  resolve: resolveSession,
  login,
  logout,
}
