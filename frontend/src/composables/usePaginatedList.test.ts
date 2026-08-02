import { effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { usePaginatedList } from './usePaginatedList'

type Row = { id: string }

const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

describe('usePaginatedList', () => {
  it('取消旧请求并且只接受最新请求的结果', async () => {
    const requests: Array<{
      signal: AbortSignal
      result: ReturnType<typeof deferred<{ items: Row[]; total: number }>>
    }> = []
    const scope = effectScope()
    const list = scope.run(() =>
      usePaginatedList<Row>({
        fetchPage: ({ signal }) => {
          const result = deferred<{ items: Row[]; total: number }>()
          requests.push({ signal, result })
          return result.promise
        },
      }),
    )!

    const firstLoad = list.load()
    const secondLoad = list.load()
    expect(requests[0].signal.aborted).toBe(true)

    requests[1].result.resolve({ items: [{ id: 'latest' }], total: 1 })
    await secondLoad
    requests[0].result.resolve({ items: [{ id: 'stale' }], total: 1 })
    await firstLoad

    expect(list.items.value).toEqual([{ id: 'latest' }])
    expect(list.loading.value).toBe(false)
    scope.stop()
  })

  it('删除末页数据后自动回退一页并重试', async () => {
    const offsets: number[] = []
    const fetchPage = vi.fn(async ({ offset }: { offset: number }) => {
      offsets.push(offset)
      if (offset === 20) return { items: [], total: 12 }
      return { items: [{ id: 'remaining' }], total: 12 }
    })
    const scope = effectScope()
    const list = scope.run(() => usePaginatedList<Row>({ fetchPage }))!
    list.first.value = 20

    await list.load()

    expect(offsets).toEqual([20, 10])
    expect(list.first.value).toBe(10)
    expect(list.items.value).toEqual([{ id: 'remaining' }])
    scope.stop()
  })

  it('切换分页时更新游标并清空已选项', async () => {
    const fetchPage = vi.fn(async () => ({ items: [{ id: 'next' }], total: 31 }))
    const scope = effectScope()
    const list = scope.run(() => usePaginatedList<Row>({ fetchPage }))!
    list.selectedItems.value = [{ id: 'selected' }]

    await list.onPage({ first: 20, rows: 20 })

    expect(list.first.value).toBe(20)
    expect(list.pageSize.value).toBe(20)
    expect(list.selectedItems.value).toEqual([])
    scope.stop()
  })
})
