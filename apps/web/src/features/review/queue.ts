import { readonly, shallowRef } from 'vue'
import { sessionStore } from '../../app/session'
import type { Session } from '../../data/client'

export type ReviewQueueOutcome = 'confirmed' | 'rejected' | 'deferred'
export type ReviewQueueSnapshot = {
  readonly scope: string
  readonly jobIds: readonly string[]
  readonly index: number
  readonly outcomes: readonly ReviewQueueOutcome[]
}

export function reviewQueueScope(
  session: (Pick<Session, 'user' | 'tenant'> & { capabilities: readonly string[] }) | null,
): string {
  return session?.capabilities.includes('claims.review')
    ? `${session.tenant.id}:${session.user.id}`
    : ''
}

export function createReviewQueue() {
  const state = shallowRef<ReviewQueueSnapshot | null>(null)

  function forScope(scope: string): ReviewQueueSnapshot | null {
    return scope && state.value?.scope === scope ? state.value : null
  }

  function start(scope: string, jobIds: readonly string[]): string {
    if (
      !scope ||
      jobIds.length === 0 ||
      jobIds.length > 200 ||
      jobIds.some((id) => !id) ||
      new Set(jobIds).size !== jobIds.length
    )
      throw new Error('无法开启审核队列，请刷新收件箱后重试。')
    state.value = { scope, jobIds: [...jobIds], index: 0, outcomes: [] }
    return jobIds[0]!
  }

  function advance(scope: string, jobId: string, outcome: ReviewQueueOutcome): string | null {
    const current = forScope(scope)
    if (!current || current.jobIds[current.index] !== jobId)
      throw new Error('当前任务不在连续审核的位置，请返回收件箱继续。')
    const index = current.index + 1
    state.value = { ...current, index, outcomes: [...current.outcomes, outcome] }
    return current.jobIds[index] ?? null
  }

  function clear() {
    state.value = null
  }

  return { state: readonly(state), forScope, start, advance, clear }
}

export function continuousReviewLocation(jobId: string) {
  return { name: 'review', params: { jobId }, query: { continuous: '1' } }
}

export const reviewQueue = createReviewQueue()
sessionStore.onInvalidated(() => reviewQueue.clear())
