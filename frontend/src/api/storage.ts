import type { User } from '@/types'

const TOKEN_KEY = 'token'
const USER_KEY = 'user'
const ACT_AS_USER_ID_KEY = 'sbm_act_as_user_id'
const ACT_AS_USERNAME_KEY = 'sbm_act_as_username'
const ACT_AS_EVENT = 'sbm-act-as-change'

export const getToken = (): string | null => localStorage.getItem(TOKEN_KEY)

export const setToken = (token: string | null) => {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
    return
  }
  localStorage.removeItem(TOKEN_KEY)
}

export const getStoredUser = (): User | null => {
  const value = localStorage.getItem(USER_KEY)
  if (!value) return null
  try {
    return JSON.parse(value) as User
  } catch {
    return null
  }
}

export const setStoredUser = (user: User | null) => {
  if (user) {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    return
  }
  localStorage.removeItem(USER_KEY)
}

export const getActAsUserId = (): string | null => localStorage.getItem(ACT_AS_USER_ID_KEY)
export const getActAsUsername = (): string | null => localStorage.getItem(ACT_AS_USERNAME_KEY)

const dispatchActAsChange = () => {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(ACT_AS_EVENT))
}

export const setActAsUser = (userId: string, username?: string) => {
  const trimmed = String(userId || '').trim()
  if (!trimmed) return
  localStorage.setItem(ACT_AS_USER_ID_KEY, trimmed)
  localStorage.setItem(ACT_AS_USERNAME_KEY, String(username || '').trim())
  dispatchActAsChange()
}

export const clearActAs = () => {
  localStorage.removeItem(ACT_AS_USER_ID_KEY)
  localStorage.removeItem(ACT_AS_USERNAME_KEY)
  dispatchActAsChange()
}

export const clearAuth = () => {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
  clearActAs()
}
