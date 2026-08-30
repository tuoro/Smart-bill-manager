import { ApiError, type UploadResult } from '../../data/client'

export const MAX_BATCH_FILES = 20
export const MAX_DOCUMENT_BYTES = 20 * 1024 * 1024

export type BatchUploadState = 'waiting' | 'uploading' | 'queued' | 'duplicate' | 'rejected'

export type BatchUploadItem = {
  key: string
  index: number
  file: File
  state: BatchUploadState
  code?: string
  message: string
  documentId?: string
  jobId?: string
}

export type BatchUploadSummary = {
  total: number
  waiting: number
  uploading: number
  queued: number
  duplicate: number
  rejected: number
}

export const batchUploadStateMeta: Record<
  BatchUploadState,
  { label: string; tone: 'neutral' | 'info' | 'danger' | 'success' }
> = {
  waiting: { label: '等待上传', tone: 'neutral' },
  uploading: { label: '正在上传', tone: 'info' },
  queued: { label: '已入队', tone: 'success' },
  duplicate: { label: '已存在', tone: 'neutral' },
  rejected: { label: '已拒绝', tone: 'danger' },
}

export function createUploadBatch(files: readonly File[]): BatchUploadItem[] {
  return files.map((file, index) => {
    const item: BatchUploadItem = {
      key: `batch-item-${index + 1}`,
      index,
      file,
      state: 'waiting',
      message: '等待上传',
    }
    if (index >= MAX_BATCH_FILES) {
      return reject(item, 'batch_file_limit_exceeded', '单次最多选择 20 个文件')
    }
    if (file.size > MAX_DOCUMENT_BYTES) {
      return reject(item, 'document_too_large', '文件不能超过 20 MiB')
    }
    return item
  })
}

export async function runUploadBatch(
  initial: readonly BatchUploadItem[],
  upload: (file: File) => Promise<UploadResult>,
  onUpdate: (items: readonly BatchUploadItem[]) => void = () => undefined,
): Promise<BatchUploadItem[]> {
  let items = initial.map((item) => ({ ...item }))
  onUpdate(items)

  for (let index = 0; index < items.length; index += 1) {
    if (items[index]?.state !== 'waiting') continue

    items = replace(items, index, { state: 'uploading', message: '正在上传' })
    onUpdate(items)
    try {
      const result = await upload(items[index]!.file)
      items = replace(items, index, {
        state: 'queued',
        code: undefined,
        message: `Document ${result.document_id} 已创建，任务已入队`,
        documentId: result.document_id,
        jobId: result.job_id,
      })
    } catch (caught) {
      items = replace(items, index, uploadFailure(caught))
    }
    onUpdate(items)
  }

  return items
}

export function summarizeUploadBatch(items: readonly BatchUploadItem[]): BatchUploadSummary {
  const summary: BatchUploadSummary = {
    total: items.length,
    waiting: 0,
    uploading: 0,
    queued: 0,
    duplicate: 0,
    rejected: 0,
  }
  for (const item of items) summary[item.state] += 1
  return summary
}

function uploadFailure(caught: unknown): Partial<BatchUploadItem> {
  if (caught instanceof ApiError) {
    if (caught.code === 'duplicate_document' && caught.resourceId) {
      return {
        state: 'duplicate',
        code: caught.code,
        message: `Document ${caught.resourceId} 已存在，未创建重复任务`,
        documentId: caught.resourceId,
      }
    }
    return { state: 'rejected', code: caught.code, message: caught.message }
  }
  return {
    state: 'rejected',
    code: 'network_error',
    message: '文件上传失败，请检查网络后重试',
  }
}

function reject(item: BatchUploadItem, code: string, message: string): BatchUploadItem {
  return { ...item, state: 'rejected', code, message }
}

function replace(
  items: readonly BatchUploadItem[],
  index: number,
  update: Partial<BatchUploadItem>,
): BatchUploadItem[] {
  return items.map((item, itemIndex) => (itemIndex === index ? { ...item, ...update } : item))
}
