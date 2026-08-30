import { describe, expect, it } from 'vitest'
import { ApiError, type UploadResult } from '../../data/client'
import {
  MAX_BATCH_FILES,
  MAX_DOCUMENT_BYTES,
  createUploadBatch,
  runUploadBatch,
  summarizeUploadBatch,
  type BatchUploadItem,
} from './batch'

describe('batch upload orchestration', () => {
  it('queues three valid files strictly in their original selection order', async () => {
    const calls: string[] = []
    const result = await runUploadBatch(
      createUploadBatch([file('first.png'), file('second.png'), file('third.png')]),
      async (current) => {
        calls.push(current.name)
        return uploadResult(current.name)
      },
    )

    expect(calls).toEqual(['first.png', 'second.png', 'third.png'])
    expect(result.map((item) => item.state)).toEqual(['queued', 'queued', 'queued'])
    expect(result.map((item) => item.documentId)).toEqual([
      'document-first.png',
      'document-second.png',
      'document-third.png',
    ])
  })

  it('keeps overflow and oversized files as explicit rejected rows', () => {
    const files = Array.from({ length: MAX_BATCH_FILES + 2 }, (_, index) =>
      file(`item-${index}.png`),
    )
    files[1] = file('oversized.png', MAX_DOCUMENT_BYTES + 1)

    const items = createUploadBatch(files)

    expect(items).toHaveLength(MAX_BATCH_FILES + 2)
    expect(items[1]).toMatchObject({ state: 'rejected', code: 'document_too_large' })
    expect(items[MAX_BATCH_FILES]).toMatchObject({
      state: 'rejected',
      code: 'batch_file_limit_exceeded',
    })
    expect(items[MAX_BATCH_FILES + 1]).toMatchObject({
      state: 'rejected',
      code: 'batch_file_limit_exceeded',
    })
    expect(summarizeUploadBatch(items)).toEqual({
      total: MAX_BATCH_FILES + 2,
      waiting: MAX_BATCH_FILES - 1,
      uploading: 0,
      queued: 0,
      duplicate: 0,
      rejected: 3,
    })
  })

  it('uploads strictly in order and continues after duplicate, server and network failures', async () => {
    const items = createUploadBatch([
      file('first.png'),
      file('same.png'),
      file('invalid.png'),
      file('offline.png'),
      file('last.png'),
    ])
    const calls: string[] = []
    const snapshots: BatchUploadItem[][] = []
    let active = 0
    let maximumActive = 0

    const result = await runUploadBatch(
      items,
      async (current) => {
        calls.push(current.name)
        active += 1
        maximumActive = Math.max(maximumActive, active)
        await Promise.resolve()
        active -= 1
        if (current.name === 'same.png') {
          throw new ApiError(409, {
            error: {
              code: 'duplicate_document',
              message: 'duplicate',
              resource_id: 'document-existing',
            },
          })
        }
        if (current.name === 'invalid.png') {
          throw new ApiError(400, {
            error: { code: 'unsupported_document', message: '文件格式不受支持' },
          })
        }
        if (current.name === 'offline.png') throw new TypeError('synthetic network detail')
        return uploadResult(current.name)
      },
      (updated) => snapshots.push(updated.map((item) => ({ ...item }))),
    )

    expect(calls).toEqual(['first.png', 'same.png', 'invalid.png', 'offline.png', 'last.png'])
    expect(maximumActive).toBe(1)
    expect(result.map((item) => item.state)).toEqual([
      'queued',
      'duplicate',
      'rejected',
      'rejected',
      'queued',
    ])
    expect(result[1]).toMatchObject({
      code: 'duplicate_document',
      documentId: 'document-existing',
    })
    expect(result[2]?.message).toBe('文件格式不受支持')
    expect(result[3]).toMatchObject({
      code: 'network_error',
      message: '文件上传失败，请检查网络后重试',
    })
    expect(
      snapshots.every(
        (snapshot) => snapshot.filter((item) => item.state === 'uploading').length <= 1,
      ),
    ).toBe(true)
  })

  it('never calls the server for locally rejected items', async () => {
    const uploadNames: string[] = []
    const result = await runUploadBatch(
      createUploadBatch([file('large.pdf', MAX_DOCUMENT_BYTES + 1), file('valid.pdf')]),
      async (current) => {
        uploadNames.push(current.name)
        return uploadResult(current.name)
      },
    )

    expect(uploadNames).toEqual(['valid.pdf'])
    expect(result.map((item) => item.state)).toEqual(['rejected', 'queued'])
  })
})

function file(name: string, size = 1): File {
  return { name, size, type: 'image/png', lastModified: 0 } as File
}

function uploadResult(label: string): UploadResult {
  return {
    document_id: `document-${label}`,
    job_id: `job-${label}`,
    status: 'queued',
    sha256: 'a'.repeat(64),
  }
}
