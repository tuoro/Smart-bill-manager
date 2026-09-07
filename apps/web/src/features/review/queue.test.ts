import { describe, expect, it } from 'vitest'
import type { Session } from '../../data/client'
import { continuousReviewLocation, createReviewQueue, reviewQueueScope } from './queue'

describe('continuous review working set', () => {
  it('freezes order without retaining source objects and advances only the current item', () => {
    const queue = createReviewQueue()
    const ids = ['first', 'second']
    expect(queue.start('tenant:user', ids)).toBe('first')
    ids.push('late')
    expect(queue.forScope('tenant:user')?.jobIds).toEqual(['first', 'second'])
    expect(() => queue.advance('tenant:user', 'second', 'confirmed')).toThrow()
    expect(queue.forScope('tenant:user')?.index).toBe(0)
    expect(queue.advance('tenant:user', 'first', 'confirmed')).toBe('second')
    expect(() => queue.advance('tenant:user', 'first', 'confirmed')).toThrow()
    expect(queue.advance('tenant:user', 'second', 'deferred')).toBeNull()
    expect(queue.forScope('tenant:user')).toMatchObject({
      index: 2,
      outcomes: ['confirmed', 'deferred'],
    })
  })

  it('keeps rejection and deferral distinct from successful confirmation', () => {
    const queue = createReviewQueue()
    queue.start('scope', ['a', 'b', 'c'])
    queue.advance('scope', 'a', 'rejected')
    queue.advance('scope', 'b', 'deferred')
    queue.advance('scope', 'c', 'confirmed')
    expect(queue.forScope('scope')?.outcomes).toEqual(['rejected', 'deferred', 'confirmed'])
    expect(() => queue.advance('scope', 'c', 'confirmed')).toThrow()
  })

  it('cannot read or advance another identity and clears on explicit end', () => {
    const queue = createReviewQueue()
    queue.start('tenant:user', ['a'])
    expect(queue.forScope('tenant:other')).toBeNull()
    expect(queue.forScope('other:user')).toBeNull()
    expect(queue.forScope('')).toBeNull()
    expect(() => queue.advance('other:user', 'a', 'confirmed')).toThrow()
    queue.clear()
    expect(queue.forScope('tenant:user')).toBeNull()
  })

  it('rejects an empty, duplicate or unbounded queue instead of silently truncating', () => {
    const queue = createReviewQueue()
    for (const ids of [[], ['a', 'a'], [''], Array.from({ length: 201 }, (_, i) => String(i))])
      expect(() => queue.start('scope', ids)).toThrow()
    expect(() => queue.start('', ['a'])).toThrow()
    expect(queue.state.value).toBeNull()
    queue.start(
      'scope',
      Array.from({ length: 200 }, (_, i) => String(i)),
    )
    expect(queue.state.value?.jobIds).toHaveLength(200)
  })

  it('requires review capability and keeps only non-authorizing identity in its scope', () => {
    const session = {
      tenant: { id: 'tenant' },
      user: { id: 'user' },
      capabilities: ['claims.review'],
    } as Session
    expect(reviewQueueScope(session)).toBe('tenant:user')
    expect(reviewQueueScope({ ...session, capabilities: [] })).toBe('')
    expect(reviewQueueScope(null)).toBe('')
    expect(continuousReviewLocation('job')).toEqual({
      name: 'review',
      params: { jobId: 'job' },
      query: { continuous: '1' },
    })
  })
})
