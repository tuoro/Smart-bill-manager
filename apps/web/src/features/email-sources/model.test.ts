import { describe, expect, it } from 'vitest'
import {
  emailAttachmentStatusMeta,
  emailMessageStatusMeta,
  emailSourceStatusMeta,
  attachmentReasonLabel,
  formatArchiveBytes,
} from './model'

describe('邮箱归档展示模型', () => {
  it('为所有冻结状态提供明确中文语义', () => {
    expect(emailSourceStatusMeta.pending_connection.label).toBe('待连接')
    expect(emailSourceStatusMeta.active.label).toBe('已有本地归档')
    expect(emailMessageStatusMeta.blocked.tone).toBe('danger')
    expect(emailAttachmentStatusMeta.queued.label).toBe('已入队')
    expect(emailAttachmentStatusMeta.existing_document.label).toBe('已存在')
    expect(emailAttachmentStatusMeta.archived_only.label).toBe('仅归档')
  })

  it('只格式化安全的非负整数大小', () => {
    expect(formatArchiveBytes(0)).toBe('0 B')
    expect(formatArchiveBytes(1536)).toBe('1.5 KiB')
    expect(formatArchiveBytes(2 * 1024 * 1024)).toBe('2.0 MiB')
    expect(formatArchiveBytes(-1)).toBe('大小未知')
    expect(formatArchiveBytes(Number.MAX_SAFE_INTEGER + 1)).toBe('大小未知')
  })

  it('未知原因也只显示稳定安全文字', () => {
    expect(attachmentReasonLabel('document_deleted')).toContain('归档附件仍保留')
    expect(attachmentReasonLabel('untrusted-provider-text')).toBe('附件未进入处理队列')
    expect(attachmentReasonLabel()).toBe('')
  })
})
