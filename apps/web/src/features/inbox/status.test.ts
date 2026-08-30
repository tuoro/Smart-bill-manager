import { describe, expect, it } from 'vitest'
import type { JobSummary } from '../../data/client'
import { canCancel, canRetry, canReview, jobStatusMeta } from './status'

const statuses = [
  'queued',
  'processing',
  'needs_review',
  'blocked',
  'failed',
  'cancel_requested',
  'cancelled',
  'completed',
  'rejected',
] as const satisfies readonly JobSummary['status'][]

describe('job status presentation', () => {
  it('covers every API status with non-empty accessible copy', () => {
    expect(Object.keys(jobStatusMeta)).toEqual(statuses)
    for (const status of statuses) {
      expect(jobStatusMeta[status].label).not.toBe('')
      expect(jobStatusMeta[status].description).not.toBe('')
    }
  })

  it('exposes only valid actions for each lifecycle state', () => {
    expect(statuses.filter(canCancel)).toEqual(['queued', 'processing', 'needs_review', 'blocked'])
    expect(statuses.filter(canRetry)).toEqual(['failed'])
    expect(statuses.filter(canReview)).toEqual(['needs_review', 'blocked'])
  })
})
