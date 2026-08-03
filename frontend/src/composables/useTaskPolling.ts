import { tasksApi, type TaskDTO } from '@/api/tasks'

export type WaitForTaskOptions = {
  timeoutMs?: number
  pollIntervalMs?: number
  shouldStop?: () => boolean
  signal?: AbortSignal
  missingTaskMessage?: string
  timeoutMessage?: string
}

type TaskFetcher<TResult> = (taskId: string) => Promise<TaskDTO<TResult> | null>

const terminalStatuses = new Set(['succeeded', 'failed', 'canceled'])
const delay = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, Math.max(0, ms)))

const defaultFetcher = async <TResult>(taskId: string): Promise<TaskDTO<TResult> | null> => {
  const response = await tasksApi.getById<TResult>(taskId)
  return response.data?.data || null
}

const canceledTask = <TResult>(taskId: string): TaskDTO<TResult> => ({
  id: taskId,
  type: '',
  status: 'canceled',
  target_id: '',
})

export const useTaskPolling = <TResult = unknown>(
  defaults: WaitForTaskOptions = {},
  fetchTask: TaskFetcher<TResult> = defaultFetcher<TResult>,
) => {
  const waitForTask = async (taskId: string, options: WaitForTaskOptions = {}): Promise<TaskDTO<TResult>> => {
    const opts = { ...defaults, ...options }
    const timeoutMs = opts.timeoutMs ?? 120000
    const pollIntervalMs = opts.pollIntervalMs ?? 800
    const startedAt = Date.now()

    while (Date.now() - startedAt < timeoutMs) {
      if (opts.signal?.aborted || opts.shouldStop?.()) return canceledTask(taskId)

      const task = await fetchTask(taskId)
      if (!task) throw new Error(opts.missingTaskMessage || '任务状态获取失败')
      if (terminalStatuses.has(task.status)) return task

      await delay(pollIntervalMs)
    }

    throw new Error(opts.timeoutMessage || '识别超时，请稍后重试')
  }

  return { waitForTask }
}
