import type { EmailAttachment, EmailMessage, EmailSource } from '../../data/client'

export type EmailStatusMeta = {
  label: string
  tone: 'neutral' | 'info' | 'warning' | 'danger' | 'success'
}

export const emailSourceStatusMeta: Record<EmailSource['status'], EmailStatusMeta> = {
  pending_connection: { label: '待连接', tone: 'warning' },
  active: { label: '已有本地归档', tone: 'success' },
}

export const emailMessageStatusMeta: Record<EmailMessage['status'], EmailStatusMeta> = {
  archived: { label: '已归档', tone: 'success' },
  blocked: { label: '已阻断', tone: 'danger' },
}

export const emailAttachmentStatusMeta: Record<
  EmailAttachment['processing_status'],
  EmailStatusMeta
> = {
  queued: { label: '已入队', tone: 'info' },
  existing_document: { label: '已存在', tone: 'success' },
  archived_only: { label: '仅归档', tone: 'warning' },
}

export function formatArchiveBytes(value: number): string {
  if (!Number.isSafeInteger(value) || value < 0) return '大小未知'
  if (value >= 1024 * 1024)
    return `${(value / (1024 * 1024)).toFixed(value >= 10 * 1024 * 1024 ? 0 : 1)} MiB`
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${value} B`
}

const attachmentReasonLabels: Record<string, string> = {
  empty_attachment: '附件为空，未进入处理队列',
  unsupported_attachment_type: '文件类型不支持处理，仅保留归档',
  attachment_too_large_for_processing: '附件超过 20 MiB，仅保留归档',
  invalid_attachment_name: '原文件名不安全，已使用安全替代名称',
  invalid_attachment_mime: '声明类型不合法，仅保留归档',
  document_signature_mismatch: '扩展名、声明类型和文件签名不一致',
  corrupt_document: '图片损坏或无法解码',
  invalid_image_dimensions: '图片尺寸不正确',
  pdf_inspection_timeout: 'PDF 检查超时',
  corrupt_pdf: 'PDF 损坏、加密或无法读取',
  encrypted_pdf: 'PDF 已加密',
  pdf_page_limit: 'PDF 超过 20 页',
  attachment_inspection_failed: '附件检查失败，仅保留归档',
  document_deleted: '关联 Document 已删除，归档附件仍保留',
}

export function attachmentReasonLabel(code?: string): string {
  if (!code) return ''
  return attachmentReasonLabels[code] ?? '附件未进入处理队列'
}
