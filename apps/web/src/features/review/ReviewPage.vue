<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ApiError, api, type ConfirmResult, type Review } from '../../data/client'
import {
  allocationEditors,
  buildAssociationDecision,
  buildDuplicateResolutionDecision,
  buildRevisionRequest,
  editableFields,
  fieldLabel,
  fieldVisibleOnPage,
  firstFieldPage,
  itemPageLabel,
  newInvoiceItem,
  parseItemPath,
  type AllocationEditor,
  type AssociationMode,
  type DocumentType,
  type EditableField,
} from './model'

const route = useRoute()
const router = useRouter()
const jobId = computed(() => String(route.params.jobId ?? ''))
const review = ref<Review | null>(null)
const loading = ref(true)
const saving = ref(false)
const confirming = ref(false)
const rejecting = ref(false)
const error = ref('')
const completed = ref<ConfirmResult | null>(null)
const editing = ref(false)
const documentType = ref<DocumentType>('unknown')
const editors = ref<EditableField[]>([])
const fieldErrors = ref<Record<string, string>>({})
const selectedPath = ref('')
const activePage = ref(1)
const associationMode = ref<AssociationMode>('')
const allocationItems = ref<AllocationEditor[]>([])
const duplicateResolutionIds = ref<string[]>([])
const rejectPanelOpen = ref(false)
const rejectReason = ref('')

const selectedReviewField = computed(() =>
  review.value?.fields.find((field) => field.path === selectedPath.value),
)
const selectedEditor = computed(() =>
  editors.value.find((field) => field.path === selectedPath.value),
)
const readFields = computed(
  () => review.value?.fields.filter((field) => field.path !== 'document_type') ?? [],
)
const visibleReadFields = computed(() =>
  review.value
    ? readFields.value.filter((field) =>
        fieldVisibleOnPage(review.value!, field.path, activePage.value),
      )
    : [],
)
const visibleEditors = computed(() =>
  review.value
    ? editors.value.filter((field) =>
        fieldVisibleOnPage(review.value!, field.path, activePage.value),
      )
    : editors.value,
)
const allEvidence = computed(() => {
  const seen = new Set<string>()
  return (review.value?.fields ?? []).flatMap((field) =>
    field.evidence.filter((evidence) => {
      if (seen.has(evidence.id)) return false
      seen.add(evidence.id)
      return true
    }),
  )
})
const activeEvidence = computed(() =>
  allEvidence.value.filter((evidence) => evidence.page === activePage.value),
)
const activePageInfo = computed(() =>
  review.value?.pages.find((page) => page.page_number === activePage.value),
)
const activePageSpans = computed(() =>
  (review.value?.invoice_item_spans ?? []).filter((span) =>
    span.page_numbers.includes(activePage.value),
  ),
)
const itemKeys = computed(() => {
  const keys = new Set<string>()
  for (const field of editors.value) {
    const item = parseItemPath(field.path)
    if (item) keys.add(item.itemKey)
  }
  const orders = new Map<string, number>()
  for (const field of editors.value) {
    const item = parseItemPath(field.path)
    if (item?.property !== 'sort_order') continue
    const order = Number(field.textValue)
    if (Number.isSafeInteger(order)) orders.set(item.itemKey, order)
  }
  return [...keys].sort((left, right) => {
    const difference =
      (orders.get(left) ?? Number.MAX_SAFE_INTEGER) - (orders.get(right) ?? Number.MAX_SAFE_INTEGER)
    return difference || left.localeCompare(right)
  })
})
const associationDecision = computed(() =>
  review.value
    ? buildAssociationDecision(review.value, associationMode.value, allocationItems.value)
    : null,
)
const duplicateDecision = computed(() =>
  review.value
    ? buildDuplicateResolutionDecision(review.value, duplicateResolutionIds.value)
    : null,
)
const canConfirm = computed(() =>
  Boolean(
    review.value &&
    review.value.claim_status === 'ready_for_review' &&
    associationDecision.value?.request &&
    duplicateDecision.value?.request,
  ),
)
const documentURL = computed(() =>
  review.value
    ? `/api/v1/documents/${encodeURIComponent(review.value.job.document_id)}/content`
    : '',
)
const pageURL = computed(() =>
  review.value
    ? `/api/v1/documents/${encodeURIComponent(review.value.job.document_id)}/pages/${activePage.value}/content`
    : '',
)

async function load() {
  loading.value = true
  error.value = ''
  try {
    review.value = await api.getReview(jobId.value)
    resetEditor()
  } catch (caught) {
    if (caught instanceof ApiError && caught.status === 404) {
      error.value = '该审核已结束或不存在，请返回收件箱查看最新状态。'
    } else {
      error.value = caught instanceof ApiError ? caught.message : '审核资料加载失败'
    }
  } finally {
    loading.value = false
  }
}

function resetEditor() {
  if (!review.value) return
  documentType.value = review.value.document_type
  editors.value = editableFields(review.value, documentType.value)
  activePage.value = 1
  selectPath(editors.value[0]?.path ?? 'document_type')
  associationMode.value = ''
  allocationItems.value = allocationEditors(review.value)
  duplicateResolutionIds.value = []
  fieldErrors.value = {}
  editing.value = false
}

function startEditing() {
  if (!review.value) return
  editing.value = true
  editors.value = editableFields(review.value, documentType.value)
}

function changeDocumentType() {
  if (!review.value) return
  editors.value = editableFields(review.value, documentType.value)
  activePage.value = 1
  selectPath(editors.value[0]?.path ?? '')
  fieldErrors.value = {}
}

function addItem() {
  const key = crypto.randomUUID()
  editors.value.push(...newInvoiceItem(key, itemKeys.value.length))
  selectPath(`items[${key}].name`)
}

function removeItem(itemKey: string) {
  editors.value = editors.value.filter((field) => parseItemPath(field.path)?.itemKey !== itemKey)
  for (const [index, key] of itemKeys.value.entries()) {
    const order = editors.value.find((field) => field.path === `items[${key}].sort_order`)
    if (order) order.textValue = String(index)
  }
  selectPath(editors.value[0]?.path ?? '')
}

function selectPath(path: string) {
  selectedPath.value = path
  if (!review.value) return
  const page = firstFieldPage(review.value, path)
  if (page) activePage.value = page
}

function selectPage(pageNumber: number) {
  if (!review.value || pageNumber < 1 || pageNumber > review.value.page_count) return
  activePage.value = pageNumber
}

function toggleEvidence(evidenceId: string) {
  const editor = selectedEditor.value
  if (!editor) return
  const index = editor.evidenceIds.indexOf(evidenceId)
  if (index >= 0) editor.evidenceIds.splice(index, 1)
  else editor.evidenceIds.push(evidenceId)
}

async function saveRevision() {
  if (!review.value) return
  const built = buildRevisionRequest(review.value, documentType.value, editors.value)
  fieldErrors.value = built.errors
  if (!built.request) {
    await nextTick()
    const first = document.querySelector<HTMLElement>('[aria-invalid="true"]')
    first?.focus()
    return
  }
  saving.value = true
  error.value = ''
  try {
    review.value = await api.revise(jobId.value, built.request)
    resetEditor()
  } catch (caught) {
    handleMutationError(caught)
  } finally {
    saving.value = false
  }
}

async function confirmReview() {
  const association = associationDecision.value?.request
  const duplicates = duplicateDecision.value?.request
  if (!review.value || !canConfirm.value || !association || !duplicates) return
  confirming.value = true
  error.value = ''
  try {
    completed.value = await api.confirm(
      jobId.value,
      {
        expected_revision: review.value.revision,
        association_mode: association.association_mode,
        allocations: association.allocations,
        duplicate_resolutions: duplicates.duplicate_resolutions,
      },
      crypto.randomUUID(),
    )
  } catch (caught) {
    handleMutationError(caught)
  } finally {
    confirming.value = false
  }
}

function candidateFor(editor: AllocationEditor) {
  return review.value?.candidates.find((candidate) => candidate.id === editor.candidateId)
}

function selectAllocation(editor: AllocationEditor) {
  if (editor.selected) associationMode.value = 'allocate_candidates'
}

function chooseNonAllocation(mode: 'reject_all' | 'no_candidate') {
  associationMode.value = mode
  for (const item of allocationItems.value) item.selected = false
}

async function rejectReview() {
  if (!review.value) return
  rejecting.value = true
  error.value = ''
  try {
    await api.reject(jobId.value, review.value.revision, rejectReason.value, crypto.randomUUID())
    await router.replace('/inbox')
  } catch (caught) {
    handleMutationError(caught)
  } finally {
    rejecting.value = false
  }
}

function handleMutationError(caught: unknown) {
  if (caught instanceof ApiError && caught.code === 'duplicate_candidate_set_stale') {
    error.value = '疑似重复候选已变化。请保存当前完整字段为新 revision 后重新核对。'
  } else if (caught instanceof ApiError && caught.status === 409) {
    error.value = '审核版本已变化。请刷新最新 revision 后重新核对。'
  } else {
    error.value = caught instanceof ApiError ? caught.message : '操作失败，请检查网络后重试'
  }
}

function duplicateKindLabel(kind: Review['duplicate_candidates'][number]['kind']) {
  if (kind === 'near_file') return '近似文件'
  if (kind === 'cross_page') return '重复页面'
  return '字段组合重复'
}

function validationForField(fieldId?: string) {
  return (
    review.value?.validations.filter((validation) => validation.field_claim_id === fieldId) ?? []
  )
}

function displayValue(value: unknown) {
  if (value === undefined || value === null || value === '') return '未提供'
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}

function statusLabel(status: Review['claim_status']) {
  return status === 'blocked' ? '阻断，需修订' : '校验通过，待确认'
}

onMounted(() => void load())
</script>

<template>
  <div class="page-stack review-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink to="/inbox">AI 收件箱</RouterLink><span aria-hidden="true">/</span
      ><strong>审核工作台</strong>
    </nav>

    <div v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span
      ><strong>正在加载 Claim 与证据</strong><span>仅展示当前 revision。</span>
    </div>

    <template v-else-if="completed">
      <section class="panel completion-state" aria-labelledby="completion-title">
        <span class="completion-mark" aria-hidden="true">✓</span>
        <h1 id="completion-title">正式账单已创建</h1>
        <p>本次人工确认、字段来源和审计记录已在同一事务中提交。</p>
        <dl class="completion-details">
          <div>
            <dt>事实类型</dt>
            <dd>{{ completed.fact_type }}</dd>
          </div>
          <div>
            <dt>Fact ID</dt>
            <dd>{{ completed.fact_id }}</dd>
          </div>
          <div v-if="completed.link_ids.length">
            <dt>分配 Link</dt>
            <dd>{{ completed.link_ids.length }} 条</dd>
          </div>
        </dl>
        <div class="page-actions">
          <RouterLink
            class="button button-primary"
            :to="completed.fact_type === 'payment' ? '/payments' : '/invoices'"
            >查看账单列表</RouterLink
          ><RouterLink class="button" to="/inbox">返回收件箱</RouterLink>
        </div>
      </section>
    </template>

    <template v-else-if="review">
      <header class="page-header review-header">
        <div>
          <h1>审核 {{ review.job.original_name }}</h1>
          <p>Revision {{ review.revision }} · {{ review.page_count }} 页 · {{ review.job.id }}</p>
        </div>
        <div class="page-actions">
          <span class="status" :data-tone="review.claim_status === 'blocked' ? 'danger' : 'warning'"
            ><span aria-hidden="true">●</span>{{ statusLabel(review.claim_status) }}</span
          ><button v-if="!editing" class="button" type="button" @click="startEditing">
            修订字段
          </button>
        </div>
      </header>

      <ol class="review-steps" aria-label="处理进度">
        <li class="done"><span>1</span><strong>上传完成</strong></li>
        <li class="done"><span>2</span><strong>AI 提取</strong></li>
        <li class="current"><span>3</span><strong>人工审核</strong></li>
        <li><span>4</span><strong>生成事实</strong></li>
      </ol>

      <div v-if="error" class="notice notice-danger" role="alert">
        <span aria-hidden="true">!</span><span>{{ error }}</span
        ><button class="text-button" type="button" @click="load">刷新 revision</button>
      </div>

      <div class="review-grid">
        <section class="panel source-panel" aria-labelledby="source-title">
          <div class="panel-heading">
            <div>
              <h2 id="source-title">原始单据</h2>
              <p>不可变 Source · 当前显示规范化审核页</p>
            </div>
            <a class="text-button" :href="documentURL" target="_blank" rel="noreferrer"
              >新窗口查看</a
            >
          </div>
          <nav class="page-review-toolbar" aria-label="单据分页">
            <button
              class="button button-small"
              type="button"
              :disabled="activePage === 1"
              @click="selectPage(activePage - 1)"
            >
              上一页
            </button>
            <div class="page-number-list" aria-label="直接选择页码">
              <button
                v-for="page in review.pages"
                :key="page.page_number"
                class="page-number-button"
                type="button"
                :aria-current="page.page_number === activePage ? 'page' : undefined"
                :aria-label="`查看第 ${page.page_number} 页`"
                @click="selectPage(page.page_number)"
              >
                {{ page.page_number }}
              </button>
            </div>
            <button
              class="button button-small"
              type="button"
              :disabled="activePage === review.page_count"
              @click="selectPage(activePage + 1)"
            >
              下一页
            </button>
            <strong class="page-position" aria-live="polite">
              第 {{ activePage }} / {{ review.page_count }} 页
            </strong>
          </nav>
          <div class="document-stage">
            <img
              :src="pageURL"
              :alt="`${review.job.original_name} 的第 ${activePage} 页规范化审核图`"
            />
          </div>
          <div class="page-review-summary">
            <span>本页 {{ activePageInfo?.field_paths.length ?? 0 }} 个证据字段</span>
            <span>本页 {{ activePageInfo?.item_keys.length ?? 0 }} 个明细</span>
            <span v-if="activePageSpans.some((span) => span.cross_page)">含跨页明细</span>
          </div>
          <div class="evidence-focus" aria-live="polite">
            <strong>{{ fieldLabel(selectedPath) }} 的证据</strong>
            <ul v-if="selectedReviewField?.evidence.length">
              <li v-for="evidence in selectedReviewField.evidence" :key="evidence.id">
                <button class="text-button" type="button" @click="selectPage(evidence.page)">
                  第 {{ evidence.page }} 页</button
                ><q v-if="evidence.quote">{{ evidence.quote }}</q
                ><span v-else>已标注区域</span>
              </li>
            </ul>
            <p v-else>该字段当前没有证据。</p>
          </div>
        </section>

        <section class="panel fields-panel" aria-labelledby="fields-title">
          <div class="panel-heading">
            <div>
              <h2 id="fields-title">结构化字段</h2>
              <p>
                {{
                  editing
                    ? `完整用户 revision · 当前第 ${activePage} 页`
                    : `AI / 用户来源 · 第 ${activePage} 页相关路径`
                }}
              </p>
            </div>
            <button v-if="editing" class="text-button" type="button" @click="resetEditor">
              放弃修订
            </button>
          </div>

          <div v-if="editing" class="document-type-control">
            <label for="document-type">文档类型</label
            ><select
              id="document-type"
              v-model="documentType"
              class="select"
              @change="changeDocumentType"
            >
              <option value="payment">支付</option>
              <option value="invoice">发票</option>
              <option value="unknown">未知 / 无法归类</option>
            </select>
          </div>

          <div class="claim-fields">
            <template v-if="editing">
              <article
                v-for="field in visibleEditors"
                :key="field.path"
                class="claim-field"
                :class="{ selected: selectedPath === field.path }"
                @click="selectPath(field.path)"
              >
                <div class="field-editor">
                  <div class="field-editor-heading">
                    <button
                      class="field-label-button"
                      type="button"
                      @click="selectPath(field.path)"
                    >
                      <strong>{{ fieldLabel(field.path) }}</strong
                      ><small>{{ field.path }}</small
                      ><small v-if="itemPageLabel(review, field.path)" class="field-page-meta">{{
                        itemPageLabel(review, field.path)
                      }}</small></button
                    ><select
                      v-model="field.presence"
                      class="select select-small"
                      :aria-label="`${fieldLabel(field.path)} 是否存在`"
                    >
                      <option value="present">存在</option>
                      <option value="absent">缺失</option>
                    </select>
                  </div>
                  <textarea
                    v-if="field.presence === 'present' && field.valueType === 'supplementary'"
                    v-model="field.textValue"
                    class="textarea"
                    rows="8"
                    :aria-label="fieldLabel(field.path)"
                    :aria-invalid="Boolean(fieldErrors[field.path])"
                    :aria-describedby="
                      fieldErrors[field.path] ? `field-error-${field.path}` : undefined
                    "
                  ></textarea>
                  <input
                    v-else-if="field.presence === 'present'"
                    v-model="field.textValue"
                    class="input"
                    :inputmode="
                      ['money_minor', 'integer'].includes(field.valueType) ? 'numeric' : 'text'
                    "
                    :aria-label="fieldLabel(field.path)"
                    :aria-invalid="Boolean(fieldErrors[field.path])"
                    :aria-describedby="
                      fieldErrors[field.path] ? `field-error-${field.path}` : undefined
                    "
                  />
                  <p
                    v-if="fieldErrors[field.path]"
                    :id="`field-error-${field.path}`"
                    class="field-error"
                  >
                    {{ fieldErrors[field.path] }}
                  </p>
                  <p class="field-evidence-count">已选择 {{ field.evidenceIds.length }} 条证据</p>
                </div>
              </article>
            </template>
            <template v-else>
              <article
                v-for="field in visibleReadFields"
                :key="field.path"
                class="claim-field"
                :class="{ selected: selectedPath === field.path }"
                @click="selectPath(field.path)"
              >
                <button class="claim-field-button" type="button" @click="selectPath(field.path)">
                  <span
                    ><strong>{{ fieldLabel(field.path) }}</strong
                    ><small>{{ field.path }}</small
                    ><small v-if="itemPageLabel(review, field.path)" class="field-page-meta">{{
                      itemPageLabel(review, field.path)
                    }}</small></span
                  ><span class="claim-value">{{ displayValue(field.value) }}</span>
                </button>
                <ul v-if="validationForField(field.id).length" class="inline-validations">
                  <li v-for="validation in validationForField(field.id)" :key="validation.id">
                    {{ validation.safe_message }}
                  </li>
                </ul>
              </article>
            </template>
          </div>

          <div v-if="editing && documentType === 'invoice'" class="item-actions">
            <button class="button button-small" type="button" @click="addItem">
              ＋ 新增发票明细</button
            ><button
              v-for="key in itemKeys"
              :key="key"
              class="text-button danger-text"
              type="button"
              @click="removeItem(key)"
            >
              删除明细 {{ key.slice(0, 8) }}
            </button>
          </div>
          <div v-if="editing" class="editor-actions">
            <button class="button" type="button" @click="resetEditor">取消</button
            ><button
              class="button button-primary"
              type="button"
              :disabled="saving"
              @click="saveRevision"
            >
              {{ saving ? '正在保存…' : '保存新 revision' }}
            </button>
          </div>
        </section>

        <aside class="review-decision-column" aria-label="校验、证据与确认">
          <section
            v-if="editing && selectedEditor?.presence === 'present'"
            class="panel decision-panel"
            aria-labelledby="evidence-select-title"
          >
            <div class="panel-heading">
              <div>
                <h2 id="evidence-select-title">选择字段证据</h2>
                <p>第 {{ activePage }} 页 · 可切页继续选择</p>
              </div>
            </div>
            <div v-if="activeEvidence.length" class="evidence-options">
              <label v-for="evidence in activeEvidence" :key="evidence.id"
                ><input
                  type="checkbox"
                  :checked="selectedEditor.evidenceIds.includes(evidence.id)"
                  @change="toggleEvidence(evidence.id)"
                /><span
                  ><strong>第 {{ evidence.page }} 页</strong
                  ><small>{{ evidence.quote || '区域标注' }}</small></span
                ></label
              >
            </div>
            <p v-else class="quiet-block">当前页没有可选择的证据，请切换页面或保持阻断。</p>
          </section>

          <section class="panel decision-panel" aria-labelledby="validation-title">
            <div class="panel-heading">
              <div>
                <h2 id="validation-title">规则校验</h2>
                <p>服务端确定性结果</p>
              </div>
            </div>
            <ul class="validation-list">
              <li
                v-for="validation in review.validations"
                :key="validation.id"
                :data-status="validation.status"
              >
                <span aria-hidden="true">{{
                  validation.status === 'passed' ? '✓' : validation.status === 'warning' ? '!' : '×'
                }}</span>
                <div>
                  <strong>{{ validation.rule_code }}</strong>
                  <p>{{ validation.safe_message }}</p>
                </div>
              </li>
            </ul>
          </section>

          <section class="panel decision-panel" aria-labelledby="duplicate-title">
            <div class="panel-heading">
              <div>
                <h2 id="duplicate-title">疑似重复</h2>
                <p>本地确定性检测 · 不会自动合并或删除</p>
              </div>
            </div>
            <fieldset
              v-if="review.duplicate_candidates.length"
              class="association-options duplicate-options"
              aria-labelledby="duplicate-title"
              :aria-describedby="
                duplicateDecision?.error ? 'duplicate-resolution-error' : undefined
              "
            >
              <legend class="visually-hidden">逐项确认疑似重复候选</legend>
              <label
                v-for="candidate in review.duplicate_candidates"
                :key="candidate.id"
                class="allocation-option"
                :data-unavailable="!candidate.available"
              >
                <input
                  v-model="duplicateResolutionIds"
                  type="checkbox"
                  :value="candidate.id"
                  :disabled="!candidate.available"
                /><span>
                  <strong
                    >{{ duplicateKindLabel(candidate.kind) }} ·
                    {{ candidate.display_name || '目标已不可用' }}</strong
                  >
                  <small v-if="candidate.current_page_number || candidate.existing_page_number">
                    当前第 {{ candidate.current_page_number ?? '—' }} 页 · 目标第
                    {{ candidate.existing_page_number ?? '—' }} 页
                  </small>
                  <small v-if="candidate.amount_minor !== undefined">
                    {{ candidate.business_date }} · {{ candidate.amount_minor }} 最小货币单位
                  </small>
                  <small>
                    {{
                      candidate.available
                        ? '勾选表示仍保留为独立记录'
                        : '目标状态已变化，请保存新 revision'
                    }}
                  </small>
                  <small>{{ candidate.reason_codes.join(' · ') }}</small>
                </span>
              </label>
            </fieldset>
            <p v-else class="quiet-block">未发现近似文件、重复页面或字段组合候选。</p>
            <p
              v-if="duplicateDecision?.error"
              id="duplicate-resolution-error"
              class="danger-text"
              role="alert"
            >
              {{ duplicateDecision.error }}
            </p>
          </section>

          <section class="panel decision-panel" aria-labelledby="association-title">
            <div class="panel-heading">
              <div>
                <h2 id="association-title">支付 / 发票关联</h2>
                <p>不会由 AI 自动接受</p>
              </div>
            </div>
            <fieldset class="association-options">
              <legend class="visually-hidden">选择关联方式</legend>
              <template v-if="review.candidates.length">
                <div
                  v-for="editor in allocationItems"
                  :key="editor.candidateId"
                  class="allocation-option"
                  :data-unavailable="!candidateFor(editor)?.available"
                >
                  <label>
                    <input
                      v-model="editor.selected"
                      type="checkbox"
                      :disabled="!candidateFor(editor)?.available"
                      @change="selectAllocation(editor)"
                    /><span>
                      <strong>
                        分配给 {{ candidateFor(editor)?.target_type }} ·
                        {{ candidateFor(editor)?.display_name }}
                      </strong>
                      <small>
                        总额 {{ candidateFor(editor)?.amount_minor }} · 已分配
                        {{ candidateFor(editor)?.allocated_minor }} · 剩余
                        {{ candidateFor(editor)?.remaining_minor }}
                        {{ candidateFor(editor)?.currency }}
                      </small>
                      <small>
                        {{ candidateFor(editor)?.business_date }} ·
                        {{
                          candidateFor(editor)?.available
                            ? candidateFor(editor)?.name_exact
                              ? '名称一致'
                              : '名称不一致，需判断'
                            : '候选已不可用，请刷新'
                        }}
                      </small>
                    </span>
                  </label>
                  <div v-if="editor.selected" class="allocation-amount">
                    <label :for="`allocation-${editor.candidateId}`">本次分配（最小单位）</label>
                    <input
                      :id="`allocation-${editor.candidateId}`"
                      v-model="editor.textValue"
                      class="input"
                      inputmode="numeric"
                      :aria-invalid="Boolean(associationDecision?.errors[editor.candidateId])"
                    />
                    <small
                      v-if="associationDecision?.errors[editor.candidateId]"
                      class="danger-text"
                    >
                      {{ associationDecision.errors[editor.candidateId] }}
                    </small>
                  </div>
                </div>
                <div class="allocation-summary" aria-live="polite">
                  <span>本次合计 {{ associationDecision?.totalMinor ?? 0 }}</span>
                  <span>
                    当前 Fact 剩余
                    {{
                      Math.max(
                        (associationDecision?.factAmountMinor ?? 0) -
                          (associationDecision?.totalMinor ?? 0),
                        0,
                      )
                    }}
                  </span>
                </div>
                <label>
                  <input
                    :checked="associationMode === 'reject_all'"
                    type="radio"
                    name="association"
                    value="reject_all"
                    @change="chooseNonAllocation('reject_all')"
                  /><span>
                    <strong>不关联任何候选</strong>
                    <small>保留全部拒绝决定，不创建 Link</small>
                  </span>
                </label>
              </template>
              <label v-else>
                <input
                  :checked="associationMode === 'no_candidate'"
                  type="radio"
                  name="association"
                  value="no_candidate"
                  @change="chooseNonAllocation('no_candidate')"
                /><span> <strong>确认当前没有候选</strong><small>本次只创建 Fact</small> </span>
              </label>
            </fieldset>
          </section>

          <section class="panel final-actions" aria-labelledby="final-title">
            <h2 id="final-title">完成审核</h2>
            <p v-if="review.claim_status === 'blocked'" class="danger-text">
              Claim 被服务端校验阻断，请先创建通过校验的新 revision。
            </p>
            <p v-else-if="duplicateDecision && !duplicateDecision.request">
              {{ duplicateDecision.error }}
            </p>
            <p v-else-if="associationDecision && !associationDecision.request">
              {{ associationDecision.errors.$association || '请修正候选分配金额。' }}
            </p>
            <p v-else>确认后将创建正式 {{ review.document_type }} Fact，并保留完整来源链。</p>
            <button
              class="button button-primary button-block"
              type="button"
              :disabled="!canConfirm || confirming || editing"
              @click="confirmReview"
            >
              {{ confirming ? '正在提交事务…' : '确认并生成事实' }}
            </button>
            <button
              class="button button-block"
              type="button"
              @click="rejectPanelOpen = !rejectPanelOpen"
            >
              驳回 Claim
            </button>
            <div v-if="rejectPanelOpen" class="reject-panel">
              <label for="reject-reason">驳回原因（可选）</label
              ><textarea
                id="reject-reason"
                v-model="rejectReason"
                class="textarea"
                maxlength="500"
                rows="3"
              ></textarea
              ><button
                class="button button-danger button-block"
                type="button"
                :disabled="rejecting"
                @click="rejectReview"
              >
                {{ rejecting ? '正在驳回…' : '确认驳回且不创建 Fact' }}
              </button>
            </div>
          </section>
        </aside>
      </div>
    </template>

    <section v-else class="panel state-layout">
      <span class="state-glyph" aria-hidden="true">!</span><strong>无法打开审核</strong>
      <p>{{ error }}</p>
      <RouterLink class="button" to="/inbox">返回收件箱</RouterLink>
    </section>
  </div>
</template>
