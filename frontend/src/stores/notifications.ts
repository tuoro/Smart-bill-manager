import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

export type NotificationSeverity = 'success' | 'info' | 'warn' | 'error'

export type AppNotification = {
  id: string
  createdAt: number
  severity: NotificationSeverity
  title: string
  detail?: string
  read: boolean
}

type AddNotificationInput = {
  severity: NotificationSeverity
  title: string
  detail?: string
}

const STORAGE_KEY = 'sbm.notifications.v1'
const MAX_ITEMS = 50

const getStorage = () => {
  try {
    if (typeof window === 'undefined') return null
    return window.localStorage
  } catch {
    return null
  }
}

const safeParse = (raw: string | null): AppNotification[] => {
  if (!raw) return []
  try {
    const data = JSON.parse(raw)
    if (!Array.isArray(data)) return []
    return data
      .filter((x) => x && typeof x === 'object')
      .map((x) => ({
        id: String((x as any).id || ''),
        createdAt: Number((x as any).createdAt || Date.now()),
        severity: ((x as any).severity as NotificationSeverity) || 'info',
        title: String((x as any).title || ''),
        detail: (x as any).detail ? String((x as any).detail) : undefined,
        read: Boolean((x as any).read),
      }))
      .filter((x) => x.id && x.title)
      .slice(0, MAX_ITEMS)
  } catch {
    return []
  }
}

const makeId = () => `${Date.now()}_${Math.random().toString(16).slice(2)}`

export const useNotificationStore = defineStore('notifications', () => {
  const storage = getStorage()
  const items = ref<AppNotification[]>(safeParse(storage?.getItem(STORAGE_KEY) || null))

  watch(
    items,
    (v) => {
      try {
        storage?.setItem(STORAGE_KEY, JSON.stringify(v.slice(0, MAX_ITEMS)))
      } catch {
        // ignore
      }
    },
    { deep: true }
  )

  const unreadCount = computed(() => items.value.filter((x) => !x.read).length)

  const add = (input: AddNotificationInput) => {
    const n: AppNotification = {
      id: makeId(),
      createdAt: Date.now(),
      severity: input.severity,
      title: input.title,
      detail: input.detail,
      read: false,
    }
    items.value = [n, ...items.value].slice(0, MAX_ITEMS)
  }

  const markRead = (id: string) => {
    const target = items.value.find((x) => x.id === id)
    if (target) target.read = true
  }

  const markAllRead = () => {
    for (const n of items.value) n.read = true
  }

  const clear = () => {
    items.value = []
  }

  return { items, unreadCount, add, markRead, markAllRead, clear }
})
