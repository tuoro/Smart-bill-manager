import type { JobSummary } from '../../data/client'

export type JobStatusMeta = {
  label: string
  tone: 'neutral' | 'info' | 'warning' | 'danger' | 'success'
  description: string
}

export const jobStatusMeta: Record<JobSummary['status'], JobStatusMeta> = {
  queued: { label: '等待处理', tone: 'neutral', description: '单据已上传，正在等待识别' },
  processing: { label: 'AI 提取中', tone: 'info', description: '正在处理全部文档页面' },
  needs_review: { label: '待人工确认', tone: 'warning', description: '结构化结果已通过本地校验' },
  blocked: { label: '部分结果', tone: 'danger', description: '存在缺失、冲突或证据问题' },
  failed: { label: '处理失败', tone: 'danger', description: '可查看原因并在允许时重试' },
  cancel_requested: { label: '正在取消', tone: 'warning', description: '取消信号已送达执行任务' },
  cancelled: {
    label: '已取消',
    tone: 'neutral',
    description: '已停止处理，不会生成新的识别结果或账单',
  },
  completed: { label: '已生成账单', tone: 'success', description: '已由人工确认并写入账单' },
  rejected: { label: '已驳回', tone: 'neutral', description: '审核已终止，未保存为正式账单' },
}

export function canCancel(status: JobSummary['status']): boolean {
  return ['queued', 'processing', 'needs_review', 'blocked'].includes(status)
}

export function canRetry(status: JobSummary['status']): boolean {
  return status === 'failed'
}

export function canReview(status: JobSummary['status']): boolean {
  return status === 'needs_review' || status === 'blocked'
}
