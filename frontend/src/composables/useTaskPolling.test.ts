import { describe, expect, it } from 'vitest'

import type { TaskDTO } from '@/api/tasks'
import { useTaskPolling } from './useTaskPolling'

const task = (status: string): TaskDTO => ({
  id: 'task-1',
  type: 'payment_ocr',
  status,
  target_id: 'payment-1',
})

describe('useTaskPolling', () => {
  it('持续轮询直到任务进入终态', async () => {
    const statuses = [task('pending'), task('succeeded')]
    const { waitForTask } = useTaskPolling({ pollIntervalMs: 0 }, async () => statuses.shift() || null)

    await expect(waitForTask('task-1')).resolves.toMatchObject({ status: 'succeeded' })
  })

  it('主动停止时不再请求任务状态', async () => {
    let calls = 0
    const { waitForTask } = useTaskPolling({}, async () => {
      calls += 1
      return task('pending')
    })

    await expect(waitForTask('task-1', { shouldStop: () => true })).resolves.toMatchObject({ status: 'canceled' })
    expect(calls).toBe(0)
  })

  it('超过截止时间后返回稳定的超时错误', async () => {
    const { waitForTask } = useTaskPolling(
      { timeoutMs: 1, pollIntervalMs: 2 },
      async () => task('pending'),
    )

    await expect(waitForTask('task-1')).rejects.toThrow('识别超时，请稍后重试')
  })
})
