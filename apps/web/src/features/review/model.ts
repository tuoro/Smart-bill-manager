import type { ConfirmRequest, Review, RevisionRequest } from '../../data/client'

export type DocumentType = Review['document_type']
export type ClaimField = Review['fields'][number]

export type EditableField = {
  path: string
  valueType: string
  presence: 'present' | 'absent'
  textValue: string
  evidenceIds: string[]
  originalValue: unknown
  originalPresence: 'present' | 'absent'
}

export type AllocationEditor = {
  candidateId: string
  selected: boolean
  textValue: string
}

export type AssociationMode = ConfirmRequest['association_mode'] | ''

export type AssociationDecision = {
  request?: Pick<ConfirmRequest, 'association_mode' | 'allocations'>
  errors: Record<string, string>
  totalMinor: number
  factAmountMinor: number
}

type FieldSpec = { path: string; valueType: string; required: boolean; label: string }

const paymentSpecs: FieldSpec[] = [
  { path: 'amount_minor', valueType: 'money_minor', required: true, label: '支付金额（最小单位）' },
  { path: 'currency', valueType: 'string', required: true, label: '币种' },
  { path: 'merchant', valueType: 'string', required: true, label: '商户' },
  { path: 'transaction_time', valueType: 'instant', required: true, label: '交易时间' },
  { path: 'source_timezone', valueType: 'string', required: true, label: '来源时区' },
  { path: 'payment_method', valueType: 'string', required: false, label: '支付方式' },
  { path: 'order_number', valueType: 'string', required: false, label: '订单号' },
  { path: 'category', valueType: 'string', required: false, label: '分类' },
  {
    path: 'supplementary_fields',
    valueType: 'supplementary',
    required: false,
    label: '补充识别字段',
  },
]

const invoiceSpecs: FieldSpec[] = [
  { path: 'invoice_number', valueType: 'string', required: true, label: '发票号码' },
  { path: 'invoice_date', valueType: 'date', required: true, label: '开票日期' },
  { path: 'total_minor', valueType: 'money_minor', required: true, label: '价税合计（最小单位）' },
  { path: 'tax_minor', valueType: 'money_minor', required: false, label: '税额（最小单位）' },
  { path: 'currency', valueType: 'string', required: true, label: '币种' },
  { path: 'seller_name', valueType: 'string', required: true, label: '销售方' },
  { path: 'buyer_name', valueType: 'string', required: true, label: '购买方' },
  {
    path: 'supplementary_fields',
    valueType: 'supplementary',
    required: false,
    label: '补充识别字段',
  },
]

const invoiceItemSpecs = [
  { property: 'name', valueType: 'string', required: true, label: '名称' },
  { property: 'quantity', valueType: 'decimal', required: false, label: '数量' },
  { property: 'unit', valueType: 'string', required: false, label: '单位' },
  { property: 'unit_price_minor', valueType: 'money_minor', required: false, label: '单价' },
  { property: 'amount_minor', valueType: 'money_minor', required: true, label: '金额' },
  { property: 'tax_minor', valueType: 'money_minor', required: false, label: '税额' },
  { property: 'sort_order', valueType: 'integer', required: true, label: '顺序' },
] as const

export function editableFields(review: Review, documentType: DocumentType): EditableField[] {
  const current = new Map(review.fields.map((field) => [field.path, field]))
  const specs =
    documentType === 'payment' ? paymentSpecs : documentType === 'invoice' ? invoiceSpecs : []
  const result = specs.map((spec) => toEditable(current.get(spec.path), spec))
  if (documentType === 'invoice') {
    const itemKeys = new Set<string>()
    for (const field of review.fields) {
      const parsed = parseItemPath(field.path)
      if (parsed && field.presence === 'present') itemKeys.add(parsed.itemKey)
    }
    for (const itemKey of itemKeys) {
      for (const spec of invoiceItemSpecs) {
        const path = `items[${itemKey}].${spec.property}`
        result.push(
          toEditable(current.get(path), {
            path,
            valueType: spec.valueType,
            required: spec.required,
            label: spec.label,
          }),
        )
      }
    }
  }
  return result
}

export function newInvoiceItem(itemKey: string, sortOrder: number): EditableField[] {
  return invoiceItemSpecs.map((spec) => ({
    path: `items[${itemKey}].${spec.property}`,
    valueType: spec.valueType,
    presence: spec.property === 'sort_order' ? 'present' : 'absent',
    textValue: spec.property === 'sort_order' ? String(sortOrder) : '',
    evidenceIds: [],
    originalValue: undefined,
    originalPresence: 'absent',
  }))
}

export function buildRevisionRequest(
  review: Review,
  documentType: DocumentType,
  fields: EditableField[],
): { request?: RevisionRequest; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  const payloadFields: RevisionRequest['fields'] = []
  for (const field of fields) {
    if (field.presence === 'absent') {
      payloadFields.push({ path: field.path, value_type: field.valueType, presence: 'absent' })
      continue
    }
    let value: unknown
    if (['money_minor', 'integer'].includes(field.valueType)) {
      if (!/^(0|[1-9][0-9]*)$/.test(field.textValue)) {
        errors[field.path] = '请输入非负整数，不使用小数或千位分隔符'
        continue
      }
      value = Number(field.textValue)
      if (!Number.isSafeInteger(value)) {
        errors[field.path] = '整数超出浏览器可安全提交范围'
        continue
      }
    } else if (field.valueType === 'supplementary') {
      try {
        value = JSON.parse(field.textValue)
      } catch {
        errors[field.path] = '补充识别字段必须是有效 JSON'
        continue
      }
      if (!Array.isArray(value) || value.length === 0) {
        errors[field.path] = '补充识别字段必须是非空数组'
        continue
      }
    } else {
      value = field.textValue
      if (!value) {
        errors[field.path] = '存在字段不能为空'
        continue
      }
    }
    const changed =
      field.originalPresence !== field.presence || !sameValue(field.originalValue, value)
    if (changed && field.evidenceIds.length === 0 && fieldRequiresEvidence(field)) {
      errors[field.path] = '新增或修改字段必须选择至少一条原始证据'
      continue
    }
    payloadFields.push({
      path: field.path,
      value_type: field.valueType,
      presence: 'present',
      value,
      evidence_ids: field.evidenceIds,
    })
  }
  if (Object.keys(errors).length) return { errors }
  return {
    request: {
      expected_revision: review.revision,
      expected_optimistic_version: review.optimistic_version,
      document_type: documentType,
      fields: payloadFields,
    },
    errors,
  }
}

export function fieldLabel(path: string): string {
  const direct = [...paymentSpecs, ...invoiceSpecs].find((spec) => spec.path === path)
  if (direct) return direct.label
  const item = parseItemPath(path)
  if (item) {
    const spec = invoiceItemSpecs.find((entry) => entry.property === item.property)
    return `明细 ${item.itemKey.slice(0, 8)} · ${spec?.label ?? item.property}`
  }
  if (path === 'document_type') return '文档类型'
  return path
}

export function parseItemPath(path: string): { itemKey: string; property: string } | null {
  const match = /^items\[([a-f0-9-]{36})\]\.([a-z][a-z0-9_]*)$/.exec(path)
  return match ? { itemKey: match[1], property: match[2] } : null
}

export function allocationEditors(review: Review): AllocationEditor[] {
  const factAmount = reviewFactAmount(review)
  return review.candidates.map((candidate) => ({
    candidateId: candidate.id,
    selected: false,
    textValue:
      candidate.available && factAmount > 0
        ? String(Math.min(candidate.remaining_minor, factAmount))
        : '',
  }))
}

export function buildAssociationDecision(
  review: Review,
  mode: AssociationMode,
  editors: AllocationEditor[],
): AssociationDecision {
  const errors: Record<string, string> = {}
  const factAmountMinor = reviewFactAmount(review)
  if (review.candidates.length === 0) {
    if (mode !== 'no_candidate') errors.$association = '请确认当前没有关联候选'
    return {
      ...(Object.keys(errors).length
        ? {}
        : { request: { association_mode: 'no_candidate' as const, allocations: [] } }),
      errors,
      totalMinor: 0,
      factAmountMinor,
    }
  }
  if (mode === 'reject_all') {
    return {
      request: { association_mode: 'reject_all', allocations: [] },
      errors,
      totalMinor: 0,
      factAmountMinor,
    }
  }
  if (mode !== 'allocate_candidates') {
    errors.$association = '请选择金额分配或明确不关联任何候选'
    return { errors, totalMinor: 0, factAmountMinor }
  }

  const candidates = new Map(review.candidates.map((candidate) => [candidate.id, candidate]))
  const allocations: ConfirmRequest['allocations'] = []
  let totalMinor = 0
  for (const editor of editors.filter((item) => item.selected)) {
    const candidate = candidates.get(editor.candidateId)
    if (!candidate) {
      errors[editor.candidateId] = '候选已变化，请刷新后重试'
      continue
    }
    if (!candidate.available) {
      errors[editor.candidateId] = '候选已删除或没有可分配余额'
      continue
    }
    if (!/^[1-9][0-9]*$/.test(editor.textValue)) {
      errors[editor.candidateId] = '请输入正整数最小单位金额'
      continue
    }
    const allocatedMinor = Number(editor.textValue)
    if (!Number.isSafeInteger(allocatedMinor)) {
      errors[editor.candidateId] = '分配金额超出浏览器可安全提交范围'
      continue
    }
    if (allocatedMinor > candidate.remaining_minor) {
      errors[editor.candidateId] = '分配金额超过候选剩余余额'
      continue
    }
    totalMinor += allocatedMinor
    allocations.push({ candidate_id: candidate.id, allocated_minor: allocatedMinor })
  }
  if (allocations.length === 0 && Object.keys(errors).length === 0) {
    errors.$association = '请至少选择一个可用候选并填写分配金额'
  }
  if (!Number.isSafeInteger(factAmountMinor) || factAmountMinor < 0) {
    errors.$association = '当前 Fact 金额无效，请先修订字段'
  } else if (totalMinor > factAmountMinor) {
    errors.$association = '本次分配合计超过当前 Fact 金额'
  }
  return {
    ...(Object.keys(errors).length
      ? {}
      : { request: { association_mode: 'allocate_candidates' as const, allocations } }),
    errors,
    totalMinor,
    factAmountMinor,
  }
}

function reviewFactAmount(review: Review): number {
  const path = review.document_type === 'payment' ? 'amount_minor' : 'total_minor'
  const value = review.fields.find(
    (field) => field.path === path && field.presence === 'present',
  )?.value
  return typeof value === 'number' && Number.isSafeInteger(value) ? value : -1
}

function toEditable(field: ClaimField | undefined, spec: FieldSpec): EditableField {
  if (!field) {
    return {
      path: spec.path,
      valueType: spec.valueType,
      presence: 'absent',
      textValue: '',
      evidenceIds: [],
      originalValue: undefined,
      originalPresence: 'absent',
    }
  }
  const value = field.value
  return {
    path: field.path,
    valueType: field.value_type,
    presence: field.presence,
    textValue:
      value === undefined || value === null
        ? ''
        : field.value_type === 'supplementary'
          ? JSON.stringify(value, null, 2)
          : String(value),
    evidenceIds: field.evidence.map((evidence) => evidence.id),
    originalValue: value,
    originalPresence: field.presence,
  }
}

function fieldRequiresEvidence(field: EditableField): boolean {
  return (
    field.valueType !== 'supplementary' &&
    field.path !== 'source_timezone' &&
    !field.path.endsWith('].sort_order')
  )
}

function sameValue(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}
