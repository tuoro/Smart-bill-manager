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
const severities = new Set<NotificationSeverity>(['success', 'info', 'warn', 'error'])

const asRecord = (value: unknown): Record<string, unknown> | null =>
  typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : null

const normalizeSeverity = (value: unknown): NotificationSeverity =>
  typeof value === 'string' && severities.has(value as NotificationSeverity)
    ? (value as NotificationSeverity)
    : 'info'

const getStorage = () => {
  try {
    if (typeof window === 'undefined') return null
    return window.localStorage
  } catch {
    return null
  }
}

export const parseStoredNotifications = (raw: string | null): AppNotification[] => {
  if (!raw) return []
  try {
    const data: unknown = JSON.parse(raw)
    if (!Array.isArray(data)) return []
    return data
      .map(asRecord)
      .filter((item): item is Record<string, unknown> => item !== null)
      .map((item) => {
        const createdAt = Number(item.createdAt)
        return {
          id: typeof item.id === 'string' ? item.id : '',
          createdAt: Number.isFinite(createdAt) ? createdAt : Date.now(),
          severity: normalizeSeverity(item.severity),
          title: typeof item.title === 'string' ? item.title : '',
          detail: typeof item.detail === 'string' && item.detail ? item.detail : undefined,
          read: item.read === true,
        }
      })
      .filter((x) => x.id && x.title)
      .slice(0, MAX_ITEMS)
  } catch {
    return []
  }
}

const makeId = () => `${Date.now()}_${Math.random().toString(16).slice(2)}`

export const useNotificationStore = defineStore('notifications', () => {
  const storage = getStorage()
  const items = ref<AppNotification[]>(parseStoredNotifications(storage?.getItem(STORAGE_KEY) || null))

  watch(
    items,
    (v) => {
      try {
        storage?.setItem(STORAGE_KEY, JSON.stringify(v.slice(0, MAX_ITEMS)))
      } catch {
        // 存储失败不影响当前会话内通知。
      }
    },
    { deep: true },
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
