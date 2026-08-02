import { onScopeDispose, ref, shallowRef } from 'vue'

export type PageRequest = {
  limit: number
  offset: number
  signal: AbortSignal
}

export type PageResult<T> = {
  items: T[]
  total: number
}

export type PageChangeEvent = {
  first?: number
  rows?: number
}

type PaginatedListOptions<T> = {
  fetchPage: (request: PageRequest) => Promise<PageResult<T>>
  onError?: (error: unknown) => void
  initialPageSize?: number
}

export const usePaginatedList = <T>(options: PaginatedListOptions<T>) => {
  const items = shallowRef<T[]>([])
  const selectedItems = shallowRef<T[]>([])
  const loading = ref(false)
  const pageSize = ref(options.initialPageSize ?? 10)
  const first = ref(0)
  const totalRecords = ref(0)
  const activeController = shallowRef<AbortController | null>(null)

  const load = async () => {
    activeController.value?.abort()
    const controller = new AbortController()
    activeController.value = controller
    loading.value = true

    try {
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const page = await options.fetchPage({
          limit: pageSize.value,
          offset: first.value,
          signal: controller.signal,
        })
        if (controller.signal.aborted || activeController.value !== controller) return

        items.value = page.items
        totalRecords.value = Number.isFinite(page.total) ? Math.max(0, page.total) : 0
        if (page.items.length === 0 && totalRecords.value > 0 && first.value > 0) {
          first.value = Math.max(0, first.value - pageSize.value)
          continue
        }
        break
      }
    } catch (error: unknown) {
      if (!controller.signal.aborted) options.onError?.(error)
    } finally {
      if (activeController.value === controller) {
        activeController.value = null
        loading.value = false
      }
    }
  }

  const onPage = async (event: PageChangeEvent) => {
    first.value = typeof event.first === 'number' ? event.first : 0
    pageSize.value = typeof event.rows === 'number' ? event.rows : pageSize.value
    selectedItems.value = []
    await load()
  }

  const resetPage = () => {
    first.value = 0
    selectedItems.value = []
  }

  onScopeDispose(() => activeController.value?.abort())

  return {
    items,
    selectedItems,
    loading,
    pageSize,
    first,
    totalRecords,
    load,
    onPage,
    resetPage,
  }
}
