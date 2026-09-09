<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  RouterLink,
  onBeforeRouteLeave,
  onBeforeRouteUpdate,
  useRoute,
  useRouter,
  type RouteLocationNormalized,
} from 'vue-router'
import AppIcon from '../../components/AppIcon.vue'
import { sessionStore } from '../../app/session'
import ValidationResults from './ValidationResults.vue'
import {
  continuousReviewLocation,
  reviewQueue,
  reviewQueueScope,
  type ReviewQueueOutcome,
} from './queue'
import {
  ApiError,
  api,
  type ConfirmRequest,
  type ConfirmResult,
  type Review,
} from '../../data/client'
import {
  allocationEditors,
  buildAssociationDecision,
  buildDuplicateResolutionDecision,
  buildRevisionRequest,
  duplicateReasonLabel,
  editableFields,
  fieldInputMode,
  fieldLabel,
  fieldVisibleOnPage,
  firstFieldPage,
  itemPageLabel,
  newInvoiceItem,
  parseItemPath,
  refreshDraftFields,
  reviewCurrency,
  reviewSourceTimezone,
  sourceTimezone,
  type AllocationEditor,
  type ClaimField,
  type AssociationMode,
  type DocumentType,
  type EditableField,
} from './model'
import { formatMinorUnits } from '../facts/money'
import { instantInZone } from '../facts/time'

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
const needsRefresh = ref(false)
const draftRefreshed = ref(false)
const draftRechecked = ref(false)
const documentType = ref<DocumentType>('unknown')
const editors = ref<EditableField[]>([])
const fieldErrors = ref<Record<string, string>>({})
const selectedPath = ref('')
const activePage = ref(1)
// 原件缩放。固定列宽伺候不了两种版式：竖屏手机截图受限于高度，横版发票受限于
// 宽度——1890px 的发票扫描件在 1366 屏上只能画到 302px（16%），20 位号码约
// 3px 高，根本没法核对。默认整页看全貌与定位，读细节时切换到宽度或原始大小。
const documentZoom = ref<'page' | 'width' | 'actual'>('page')
// 横版发票即使按原始大小也要横向平移六屏才读得完。展开成整幅宽度后，1366 的
// 屏上能画到 58%，够读 20 位号码而不必平移。这是临时动作，不是默认——默认仍是
// 原件与识别结果并排。
const sourceExpanded = ref(false)
const zoomModes = [
  { value: 'page', label: '整页' },
  { value: 'width', label: '适应宽度' },
  { value: 'actual', label: '原始大小' },
] as const
const associationMode = ref<AssociationMode>('')
const allocationItems = ref<AllocationEditor[]>([])
const duplicateResolutionIds = ref<string[]>([])
const rejectPanelOpen = ref(false)
const rejectReason = ref('')
const navigating = ref(false)
const navigationError = ref('')
const finishedOutcome = ref<ReviewQueueOutcome | null>(null)
const uncertainAction = ref<'confirm' | 'reject' | null>(null)
const taskHeading = ref<HTMLElement | null>(null)
let taskEpoch = 0
let readEpoch = 0
let readController: AbortController | null = null
let allowedNavigation = ''
let initialEditorSignature = ''
let confirmAttempt: { body: ConfirmRequest; key: string } | null = null
let rejectAttempt: { revision: number; reason: string; key: string } | null = null

const queueScope = computed(() => reviewQueueScope(sessionStore.current.value))
const queue = computed(() => reviewQueue.forScope(queueScope.value))
const continuous = computed(() => route.query.continuous === '1')
const queuedJobId = computed(() => queue.value?.jobIds[queue.value.index])
const inQueue = computed(() => continuous.value && queuedJobId.value === jobId.value)
const queueFinished = computed(
  () =>
    continuous.value &&
    queue.value &&
    queue.value.index === queue.value.jobIds.length &&
    queue.value.jobIds.at(-1) === jobId.value,
)
const queueCounts = computed(() => {
  const outcomes = queue.value?.outcomes ?? []
  return {
    confirmed: outcomes.filter((outcome) => outcome === 'confirmed').length,
    rejected: outcomes.filter((outcome) => outcome === 'rejected').length,
    deferred: outcomes.filter((outcome) => outcome === 'deferred').length,
  }
})
const queueNotice = computed(() => {
  const last = queue.value?.outcomes.at(-1)
  const position = queue.value?.index ?? 0
  if (last === 'confirmed') return `第 ${position} 份已保存。请核对当前单据，不会自动确认。`
  if (last === 'rejected') return `第 ${position} 份已驳回，未生成正式记录。`
  if (last === 'deferred') return `第 ${position} 份已暂缓，仍需处理，未生成正式记录。`
  return '按开启时的顺序审核，不会自动加入新任务。'
})
const busy = computed(() => saving.value || confirming.value || rejecting.value || navigating.value)
const handled = computed(() => Boolean(completed.value || finishedOutcome.value))
const hasUnsavedChanges = computed(
  () =>
    !handled.value &&
    ((editing.value && editorSignature() !== initialEditorSignature) ||
      associationMode.value !== '' ||
      duplicateResolutionIds.value.length > 0 ||
      rejectReason.value !== ''),
)
const noAssociationCandidates = computed(
  () =>
    review.value && review.value.document_type !== 'trip' && review.value.candidates.length === 0,
)
const confirmLabel = computed(() => {
  if (confirming.value) return '正在保存…'
  if (uncertainAction.value === 'confirm') return '重试原确认'
  if (noAssociationCandidates.value)
    return inQueue.value ? '确认保存，不分配并继续' : '确认保存，不分配'
  return inQueue.value ? '确认保存并继续' : '确认并保存记录'
})
// 决策栏里没有待办的面板压成一行。三张写着「没问题」的卡片和唯一那个决定性
// 动作长得一模一样，等于告诉人它们同样重要——那就等于都不重要。
const validationResolved = computed(() => actionableValidations.value.length === 0)
const duplicatesResolved = computed(() => review.value?.duplicate_candidates.length === 0)

const actionableValidations = computed(
  () => review.value?.validations.filter((validation) => validation.status !== 'passed') ?? [],
)
const passedValidations = computed(
  () => review.value?.validations.filter((validation) => validation.status === 'passed') ?? [],
)

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
    ? buildAssociationDecision(
        review.value,
        noAssociationCandidates.value ? 'no_candidate' : associationMode.value,
        allocationItems.value,
      )
    : null,
)
const duplicateDecision = computed(() =>
  review.value
    ? buildDuplicateResolutionDecision(review.value, duplicateResolutionIds.value)
    : null,
)
const isTripReview = computed(() => review.value?.document_type === 'trip')
const canConfirm = computed(() =>
  Boolean(
    review.value &&
    !loading.value &&
    !busy.value &&
    !handled.value &&
    uncertainAction.value !== 'reject' &&
    !editing.value &&
    !needsRefresh.value &&
    review.value.claim_status === 'ready_for_review' &&
    (isTripReview.value || associationDecision.value?.request) &&
    duplicateDecision.value?.request,
  ),
)
const documentURL = computed(() =>
  review.value
    ? `/api/v1/documents/${encodeURIComponent(review.value.job.document_id)}/content`
    : '',
)
// 原件加载失败必须说出来。这个页面的工作是「对着原件核字段」，加载失败时
// 原来只是留一块空白，审核人分不清是没有原件、还在加载、还是失败了——可能在
// 从未看到原件的情况下确认字段。重试用递增的序号强制重新取，而不是改 URL 语义。
const documentLoadFailed = ref(false)
const documentAttempt = ref(0)
const pageURL = computed(() =>
  review.value
    ? `/api/v1/documents/${encodeURIComponent(review.value.job.document_id)}/pages/${activePage.value}/content${documentAttempt.value ? `?retry=${documentAttempt.value}` : ''}`
    : '',
)

function retryDocument() {
  documentLoadFailed.value = false
  documentAttempt.value += 1
}

async function load() {
  if (busy.value || uncertainAction.value) return
  readController?.abort()
  const controller = new AbortController()
  readController = controller
  const read = ++readEpoch
  const task = captureTask()
  const isCurrent = () => task.isCurrent() && read === readEpoch && !controller.signal.aborted
  loading.value = true
  error.value = ''
  try {
    const latest = await api.getReview(task.jobId, controller.signal)
    if (!isCurrent()) return
    if (latest.job.id !== task.jobId) throw new Error('审核响应与当前单据不一致')
    if (editing.value && review.value) {
      editors.value = refreshDraftFields(review.value, latest, editors.value)
      review.value = latest
      needsRefresh.value = false
      draftRefreshed.value = true
      draftRechecked.value = false
      associationMode.value = ''
      allocationItems.value = allocationEditors(latest)
      duplicateResolutionIds.value = []
    } else {
      review.value = latest
      resetEditor()
    }
  } catch (caught) {
    if (!isCurrent()) return
    if (caught instanceof ApiError && caught.status === 404) {
      error.value = '该审核已结束或不存在，请返回收件箱查看最新状态。'
    } else {
      error.value = caught instanceof ApiError ? caught.message : '审核资料加载失败'
    }
  } finally {
    if (isCurrent()) {
      loading.value = false
      if (!editing.value) await focusTaskHeading()
    }
  }
}

function captureTask() {
  const epoch = taskEpoch
  const id = jobId.value
  const session = sessionStore.current.value
  return {
    jobId: id,
    isCurrent: () =>
      epoch === taskEpoch &&
      id === jobId.value &&
      session?.user.id === sessionStore.current.value?.user.id &&
      session?.tenant.id === sessionStore.current.value?.tenant.id,
  }
}

function editorSignature() {
  return JSON.stringify([documentType.value, editors.value])
}

async function focusTaskHeading() {
  await nextTick()
  taskHeading.value?.focus()
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
  needsRefresh.value = false
  draftRefreshed.value = false
  draftRechecked.value = false
  initialEditorSignature = editorSignature()
  confirmAttempt = null
  rejectAttempt = null
}

function startEditing() {
  if (!review.value || editing.value || busy.value || uncertainAction.value) return
  editing.value = true
  editors.value = editableFields(review.value, documentType.value)
  initialEditorSignature = editorSignature()
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
  // 换页等于换一张图，上一页放大到哪里对下一页没有意义。
  documentZoom.value = 'page'
  documentLoadFailed.value = false
}

function toggleEvidence(evidenceId: string) {
  if (busy.value || uncertainAction.value) return
  const editor = selectedEditor.value
  if (!editor) return
  const index = editor.evidenceIds.indexOf(evidenceId)
  if (index >= 0) editor.evidenceIds.splice(index, 1)
  else editor.evidenceIds.push(evidenceId)
}

async function saveRevision() {
  if (
    !review.value ||
    busy.value ||
    uncertainAction.value ||
    needsRefresh.value ||
    (draftRefreshed.value && !draftRechecked.value)
  )
    return
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
  const task = captureTask()
  try {
    const latest = await api.revise(task.jobId, built.request)
    if (!task.isCurrent()) return
    review.value = latest
    resetEditor()
  } catch (caught) {
    if (task.isCurrent()) handleMutationError(caught)
  } finally {
    if (task.isCurrent()) saving.value = false
  }
}

async function confirmReview() {
  const association = associationDecision.value?.request
  const duplicates = duplicateDecision.value?.request
  if (!review.value || !canConfirm.value || !duplicates || (!isTripReview.value && !association))
    return
  confirming.value = true
  error.value = ''
  const task = captureTask()
  let result: ConfirmResult
  try {
    if (!confirmAttempt || uncertainAction.value !== 'confirm') {
      const body: ConfirmRequest = {
        expected_revision: review.value.revision,
        duplicate_resolutions: duplicates.duplicate_resolutions,
      }
      if (!isTripReview.value && association) {
        body.association_mode = association.association_mode
        body.allocations = association.allocations
      }
      confirmAttempt = { body, key: crypto.randomUUID() }
    }
    result = await api.confirm(task.jobId, confirmAttempt.body, confirmAttempt.key)
  } catch (caught) {
    if (task.isCurrent()) handleDecisionError(caught, 'confirm')
    return
  } finally {
    if (task.isCurrent()) confirming.value = false
  }
  if (!task.isCurrent()) return
  uncertainAction.value = null
  confirmAttempt = null
  completed.value = result
  await finishTask('confirmed')
}

function candidateFor(editor: AllocationEditor) {
  return review.value?.candidates.find((candidate) => candidate.id === editor.candidateId)
}

// 候选金额按其自身币种展示，与输入框的十进制口径一致。
function candidateMoney(
  editor: AllocationEditor,
  key: 'amount_minor' | 'allocated_minor' | 'remaining_minor',
) {
  const candidate = candidateFor(editor)
  if (!candidate) return ''
  return formatMinorUnits(candidate[key], candidate.currency)
}

// 交易时间是绝对时刻，票面印的是来源时区的本地时间。把同一时刻按当前填写的
// 来源时区显示出来，核对时不必再心算时差。
function localInstant(field: EditableField) {
  if (field.presence !== 'present' || field.valueType !== 'instant') return ''
  const timezone = sourceTimezone(editors.value)
  const local = instantInZone(field.textValue, timezone)
  return local ? `${timezone} 当地时间 ${local}` : ''
}

// 合计与输入框同口径：都按 Claim 当前币种展示，不再暴露最小单位。
function allocationMoney(minor: number) {
  return review.value ? formatMinorUnits(minor, reviewCurrency(review.value)) : String(minor)
}

function selectAllocation(editor: AllocationEditor) {
  if (editor.selected) associationMode.value = 'allocate_candidates'
}

function rejectAllCandidates() {
  associationMode.value = 'reject_all'
  for (const item of allocationItems.value) item.selected = false
}

async function rejectReview() {
  if (
    !review.value ||
    editing.value ||
    busy.value ||
    handled.value ||
    needsRefresh.value ||
    uncertainAction.value === 'confirm'
  )
    return
  rejecting.value = true
  error.value = ''
  const task = captureTask()
  try {
    if (!rejectAttempt || uncertainAction.value !== 'reject')
      rejectAttempt = {
        revision: review.value.revision,
        reason: rejectReason.value,
        key: crypto.randomUUID(),
      }
    await api.reject(task.jobId, rejectAttempt.revision, rejectAttempt.reason, rejectAttempt.key)
  } catch (caught) {
    if (task.isCurrent()) handleDecisionError(caught, 'reject')
    return
  } finally {
    if (task.isCurrent()) rejecting.value = false
  }
  if (!task.isCurrent()) return
  uncertainAction.value = null
  rejectAttempt = null
  await finishTask('rejected')
}

function handleDecisionError(caught: unknown, action: 'confirm' | 'reject') {
  if (!(caught instanceof ApiError) || caught.status >= 500) {
    uncertainAction.value = action
    error.value = '上次提交结果尚未确认。请重试原决定以核对结果，不会自动跳过或改用新的决定。'
    return
  }
  uncertainAction.value = null
  confirmAttempt = null
  rejectAttempt = null
  handleMutationError(caught)
}

async function finishTask(outcome: ReviewQueueOutcome) {
  finishedOutcome.value = outcome
  if (!inQueue.value) {
    if (outcome === 'rejected') await navigateAfterDecision('/inbox')
    else await focusTaskHeading()
    return
  }
  const next = reviewQueue.advance(queueScope.value, jobId.value, outcome)
  if (next) await navigateAfterDecision(router.resolve(continuousReviewLocation(next)).fullPath)
  else await focusTaskHeading()
}

async function navigateAfterDecision(target: string) {
  const task = captureTask()
  navigating.value = true
  navigationError.value = ''
  allowedNavigation = target
  try {
    const failure = await router.replace(target)
    if (failure && task.isCurrent())
      navigationError.value = '本项处理结果已保留，但未能打开下一页面。请继续本轮审核或返回收件箱。'
  } catch {
    if (task.isCurrent())
      navigationError.value = '本项处理结果已保留，但未能打开下一页面。请继续本轮审核或返回收件箱。'
  } finally {
    if (task.isCurrent()) navigating.value = false
    if (allowedNavigation === target) allowedNavigation = ''
  }
}

async function deferTask() {
  if (!inQueue.value || editing.value || busy.value || uncertainAction.value || handled.value)
    return
  await finishTask('deferred')
}

async function locateValidation(fieldId: string) {
  if (busy.value || uncertainAction.value) return
  const field = review.value?.fields.find((field) => field.id === fieldId)
  if (!field) return
  if (field.path === 'document_type') {
    startEditing()
    await nextTick()
    document.getElementById('document-type')?.focus()
    return
  }
  selectPath(field.path)
  await nextTick()
  document
    .querySelector<HTMLElement>(`[data-field-path="${CSS.escape(field.path)}"] button`)
    ?.focus()
}

function handleMutationError(caught: unknown) {
  if (caught instanceof ApiError && caught.code === 'duplicate_candidate_set_stale') {
    error.value = '疑似重复候选已变化。请保存当前字段为新版本后重新核对。'
  } else if (caught instanceof ApiError && caught.status === 409) {
    needsRefresh.value = true
    error.value = '审核版本已变化。请刷新最新版本后重新核对。'
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

// 只读展示与编辑框同口径。以前这里直接 String(value)，于是金额显示成 32109、
// 交易时间显示成 2026-08-28T08:00:00Z——人绝大多数时间处在只读态，内部表示
// 恰恰泄漏在最常看的那一屏上。
function displayValue(field: ClaimField) {
  const value = field.value
  if (value === undefined || value === null || value === '') return '未提供'
  if (field.value_type === 'money_minor' && typeof value === 'number' && review.value) {
    return formatMinorUnits(value, reviewCurrency(review.value))
  }
  if (field.value_type === 'instant' && typeof value === 'string' && review.value) {
    const timezone = reviewSourceTimezone(review.value)
    const local = instantInZone(value, timezone)
    // 时区无效或时刻不合法时退回原值，不显示一个错的本地时间。
    if (local) return `${local}（${timezone}）`
  }
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}

function statusLabel(status: Review['claim_status']) {
  return status === 'blocked' ? '阻断，需修订' : '校验通过，待确认'
}

function documentTypeLabel(type?: string) {
  if (type === 'payment') return '支付'
  if (type === 'invoice') return '发票'
  if (type === 'trip') return '行程'
  return '单据'
}

function mayLeave(to: RouteLocationNormalized) {
  if (!sessionStore.current.value || to.fullPath === allowedNavigation || handled.value) return true
  if (busy.value) return false
  if (uncertainAction.value)
    return window.confirm('上次提交结果尚未确认。离开后请在收件箱核对服务端状态，确定离开吗？')
  return !hasUnsavedChanges.value || window.confirm('尚未保存的修订或审核选择会丢失，确定离开吗？')
}

function warnBeforeUnload(event: BeforeUnloadEvent) {
  if (!busy.value && !hasUnsavedChanges.value && !uncertainAction.value) return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteLeave(mayLeave)
onBeforeRouteUpdate(mayLeave)
onMounted(() => window.addEventListener('beforeunload', warnBeforeUnload))
onBeforeUnmount(() => {
  taskEpoch++
  readController?.abort()
  window.removeEventListener('beforeunload', warnBeforeUnload)
})

watch(
  () => [jobId.value, continuous.value],
  () => {
    taskEpoch++
    readController?.abort()
    review.value = null
    completed.value = null
    finishedOutcome.value = null
    editing.value = false
    saving.value = false
    confirming.value = false
    rejecting.value = false
    navigating.value = false
    error.value = ''
    navigationError.value = ''
    needsRefresh.value = false
    draftRefreshed.value = false
    draftRechecked.value = false
    documentType.value = 'unknown'
    editors.value = []
    fieldErrors.value = {}
    selectedPath.value = ''
    activePage.value = 1
    associationMode.value = ''
    allocationItems.value = []
    duplicateResolutionIds.value = []
    rejectPanelOpen.value = false
    rejectReason.value = ''
    uncertainAction.value = null
    confirmAttempt = null
    rejectAttempt = null
    if (queueFinished.value) {
      loading.value = false
      void focusTaskHeading()
    } else void load()
  },
  { immediate: true },
)
</script>

<template>
  <div class="page-stack review-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink to="/inbox">AI 收件箱</RouterLink><span aria-hidden="true">/</span
      ><strong>审核工作台</strong>
    </nav>

    <section v-if="inQueue && queue" class="panel review-queue-bar" aria-label="连续审核进度">
      <div>
        <strong>连续审核 · 第 {{ queue.index + 1 }} / {{ queue.jobIds.length }} 份</strong>
        <p role="status" aria-live="polite">{{ queueNotice }}</p>
      </div>
      <div class="page-actions">
        <button
          class="button button-small"
          type="button"
          :disabled="loading || busy || editing || Boolean(uncertainAction) || handled"
          @click="deferTask"
        >
          稍后处理，不保存
        </button>
        <RouterLink class="text-button" to="/inbox">返回收件箱</RouterLink>
      </div>
      <p v-if="editing" class="quiet queue-draft-note">请先保存或放弃修订，再继续其他单据。</p>
    </section>
    <p
      v-else-if="continuous && !queueFinished && !navigating"
      class="notice notice-warning"
      role="status"
    >
      {{
        queuedJobId
          ? '当前链接不在本轮队列的位置。本单按单独审核处理。'
          : '连续审核队列未保留（刷新或结束后会清空）。本单按单独审核处理，不会自动继续。'
      }}
      <RouterLink v-if="queuedJobId" :to="continuousReviewLocation(queuedJobId)"
        >回到本轮审核</RouterLink
      >
    </p>
    <div v-if="navigationError" class="notice notice-danger" role="alert">
      {{ navigationError }}
      <RouterLink v-if="queuedJobId" :to="continuousReviewLocation(queuedJobId)"
        >继续本轮审核</RouterLink
      >
    </div>

    <div v-if="loading || navigating" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span
      ><strong>正在加载识别结果与原件</strong><span>准备当前版本的字段和证据。</span>
    </div>

    <section
      v-else-if="queueFinished"
      class="panel completion-state"
      aria-labelledby="queue-completion-title"
    >
      <span class="completion-mark"><AppIcon name="check" /></span>
      <h1 id="queue-completion-title" ref="taskHeading" tabindex="-1">本轮审核结束</h1>
      <p>
        已保存 {{ queueCounts.confirmed }} 份 · 已驳回 {{ queueCounts.rejected }} 份 · 暂缓
        {{ queueCounts.deferred }} 份
      </p>
      <p>暂缓项仍需处理；新任务不在本轮内，可返回收件箱查看。</p>
      <RouterLink class="button button-primary" to="/inbox">返回收件箱</RouterLink>
    </section>

    <template v-else-if="completed">
      <section class="panel completion-state" aria-labelledby="completion-title">
        <span class="completion-mark"><AppIcon name="check" /></span>
        <h1 id="completion-title" ref="taskHeading" tabindex="-1">
          {{ completed.fact_type === 'trip' ? '行程凭证审核完成' : '正式账单已创建' }}
        </h1>
        <p>
          {{
            completed.fact_type === 'trip'
              ? '凭证与审核来源已保存。可将多张机票等材料关联到同一趟行程，不会自动创建行程。'
              : '审核已完成，字段来源与操作记录已一并保存。'
          }}
        </p>
        <dl class="completion-details">
          <div>
            <dt>记录类型</dt>
            <dd>{{ documentTypeLabel(completed.fact_type) }}</dd>
          </div>
          <div v-if="completed.link_ids.length">
            <dt>金额分配</dt>
            <dd>{{ completed.link_ids.length }} 条</dd>
          </div>
        </dl>
        <div class="page-actions">
          <RouterLink
            v-if="sessionStore.current.value?.capabilities.includes('facts.read')"
            class="button button-primary"
            :to="
              completed.fact_type === 'payment'
                ? '/payments'
                : completed.fact_type === 'invoice'
                  ? '/invoices'
                  : '/trips#trip-materials'
            "
            >查看正式记录</RouterLink
          ><RouterLink class="button" to="/inbox">返回收件箱</RouterLink>
        </div>
      </section>
    </template>

    <section
      v-else-if="finishedOutcome"
      class="panel completion-state"
      aria-labelledby="handled-title"
    >
      <h1 id="handled-title" ref="taskHeading" tabindex="-1">
        {{ finishedOutcome === 'rejected' ? '识别结果已驳回' : '本单已暂缓' }}
      </h1>
      <p>未生成正式记录。</p>
      <RouterLink
        v-if="queuedJobId"
        class="button button-primary"
        :to="continuousReviewLocation(queuedJobId)"
        >继续本轮审核</RouterLink
      >
      <RouterLink class="button" to="/inbox">返回收件箱</RouterLink>
    </section>

    <template v-else-if="review">
      <header class="page-header review-header">
        <div>
          <h1 ref="taskHeading" tabindex="-1">审核单据</h1>
          <p class="review-document-meta">
            <strong class="review-document-name">{{ review.job.original_name }}</strong
            ><span>{{ review.entry_mode === 'manual' ? '已转人工' : 'AI 提取' }}</span
            ><span>版本 {{ review.revision }}</span
            ><span>共 {{ review.page_count }} 页</span>
          </p>
        </div>
        <div class="page-actions">
          <span class="status" :data-tone="review.claim_status === 'blocked' ? 'danger' : 'warning'"
            ><span aria-hidden="true">●</span>{{ statusLabel(review.claim_status) }}</span
          ><button
            v-if="!editing"
            class="button"
            type="button"
            :disabled="busy || Boolean(uncertainAction)"
            @click="startEditing"
          >
            修订字段
          </button>
        </div>
      </header>

      <p v-if="review.entry_mode === 'manual'" class="notice manual-source-notice">
        此单据由用户显式接管，字段及证据由人工填写，不代表 AI 识别成功。原件与识别失败历史保留。
        {{ review.job.safe_error_message ? `原失败：${review.job.safe_error_message}` : '' }}
      </p>

      <div v-if="error" class="notice notice-danger" role="alert">
        <AppIcon name="alert" /><span>{{ error }}</span
        ><button
          class="text-button"
          type="button"
          :disabled="busy || Boolean(uncertainAction)"
          @click="load"
        >
          刷新最新版本
        </button>
      </div>

      <section v-if="editing && draftRefreshed" class="panel page-stack" aria-label="修订冲突核对">
        <p role="status">
          最新版本已加载，当前草稿、页码和摘录已保留；不会自动提交。请比较最新内容后重新核对。
        </p>
        <details>
          <summary>
            查看服务器最新版本 {{ review.revision }} · {{ documentTypeLabel(review.document_type) }}
          </summary>
          <dl>
            <template v-for="field in readFields" :key="field.path"
              ><dt>{{ fieldLabel(field.path) }}</dt>
              <dd>{{ displayValue(field) }}</dd></template
            >
          </dl>
        </details>
        <label
          ><input
            v-model="draftRechecked"
            type="checkbox"
          />我已比较最新版本，确认继续使用当前草稿</label
        >
      </section>

      <div class="review-grid" :data-source-expanded="sourceExpanded">
        <section class="panel source-panel" aria-labelledby="source-title">
          <div class="panel-heading">
            <div>
              <h2 id="source-title">原始单据</h2>
              <p>原始文件保留不变，选择字段可定位原件依据。</p>
            </div>
            <div class="source-panel-actions">
              <button
                class="button button-small"
                type="button"
                :aria-pressed="sourceExpanded"
                @click="sourceExpanded = !sourceExpanded"
              >
                {{ sourceExpanded ? '收起原件' : '展开原件' }}
              </button>
              <a class="text-button" :href="documentURL" target="_blank" rel="noreferrer"
                >新窗口查看</a
              >
            </div>
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
            <div class="document-zoom" role="group" aria-label="原件缩放">
              <button
                v-for="mode in zoomModes"
                :key="mode.value"
                class="button button-small"
                type="button"
                :aria-pressed="documentZoom === mode.value"
                @click="documentZoom = mode.value"
              >
                {{ mode.label }}
              </button>
            </div>
          </nav>
          <div class="document-stage" :data-zoom="documentZoom" tabindex="0" aria-label="原件预览">
            <img
              v-show="!documentLoadFailed"
              :src="pageURL"
              :alt="`${review.job.original_name} 的第 ${activePage} 页规范化审核图`"
              @error="documentLoadFailed = true"
              @load="documentLoadFailed = false"
            />
            <p v-if="documentLoadFailed" class="document-load-error" role="alert">
              <AppIcon name="alert" /><span
                >原件加载失败，无法与识别结果核对。确认前请先看到原件。</span
              ><button class="text-button" type="button" @click="retryDocument">重试</button>
            </p>
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

        <section
          class="panel fields-panel"
          aria-labelledby="fields-title"
          :inert="busy || Boolean(uncertainAction)"
        >
          <div class="panel-heading">
            <div>
              <h2 id="fields-title">识别结果</h2>
              <p>
                {{
                  editing
                    ? `尚未保存 · 离开或浏览器刷新会丢失草稿 · 当前第 ${activePage} 页`
                    : `核对字段与原件 · 当前第 ${activePage} 页`
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
              <option value="trip">行程</option>
              <option value="unknown">未知 / 无法归类</option>
            </select>
          </div>

          <div class="claim-fields">
            <template v-if="editing">
              <article
                v-for="field in visibleEditors"
                :key="field.path"
                :data-field-path="field.path"
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
                    :inputmode="fieldInputMode(field.valueType)"
                    :aria-label="fieldLabel(field.path)"
                    :aria-invalid="Boolean(fieldErrors[field.path])"
                    :aria-describedby="
                      fieldErrors[field.path] ? `field-error-${field.path}` : undefined
                    "
                  />
                  <p v-if="localInstant(field)" class="field-hint">{{ localInstant(field) }}</p>
                  <p
                    v-if="fieldErrors[field.path]"
                    :id="`field-error-${field.path}`"
                    class="field-error"
                  >
                    {{ fieldErrors[field.path] }}
                  </p>
                  <fieldset
                    v-if="review.entry_mode === 'manual' && field.presence === 'present'"
                    class="page-stack manual-evidence-group"
                  >
                    <legend>人工标注原件来源</legend>
                    <label
                      >原件页码<input
                        v-model.number="field.manualPage"
                        class="input"
                        type="number"
                        min="1"
                        :max="review.page_count"
                        :aria-label="`${fieldLabel(field.path)} 来源页码`"
                    /></label>
                    <label
                      >实际摘录<textarea
                        v-model="field.manualQuote"
                        class="textarea"
                        rows="2"
                        maxlength="500"
                        :aria-label="`${fieldLabel(field.path)} 原件摘录`"
                      ></textarea>
                    </label>
                    <small class="quiet"
                      >请从原件标注；不会将填写值自动伪装成票面证据。也可选择已保存的证据。</small
                    >
                  </fieldset>
                  <p class="field-evidence-count">
                    已选择 {{ field.evidenceIds.length }} 条证据<span
                      v-if="review.entry_mode === 'manual'"
                    >
                      · 人工录入</span
                    >
                  </p>
                  <button
                    v-if="field.evidenceIds.length"
                    type="button"
                    class="text-button"
                    :aria-label="`${fieldLabel(field.path)} 清空证据选择`"
                    @click="field.evidenceIds = []"
                  >
                    清空证据选择
                  </button>
                </div>
              </article>
            </template>
            <template v-else>
              <article
                v-for="field in visibleReadFields"
                :key="field.path"
                :data-field-path="field.path"
                class="claim-field"
                :class="{ selected: selectedPath === field.path }"
                @click="selectPath(field.path)"
              >
                <button class="claim-field-button" type="button" @click="selectPath(field.path)">
                  <span
                    ><strong>{{ fieldLabel(field.path) }}</strong
                    ><small v-if="itemPageLabel(review, field.path)" class="field-page-meta">{{
                      itemPageLabel(review, field.path)
                    }}</small></span
                  ><span class="claim-value">{{ displayValue(field) }}</span>
                </button>
                <ul v-if="validationForField(field.id).length" class="inline-validations">
                  <li
                    v-for="validation in validationForField(field.id)"
                    :key="validation.id"
                    :data-status="validation.status"
                  >
                    {{ validation.safe_message }}
                  </li>
                </ul>
              </article>
            </template>
          </div>

          <div v-if="editing && documentType === 'invoice'" class="item-actions">
            <button class="button button-small" type="button" @click="addItem">新增发票明细</button
            ><button
              v-for="(key, index) in itemKeys"
              :key="key"
              class="text-button danger-text"
              type="button"
              @click="removeItem(key)"
            >
              删除第 {{ index + 1 }} 条明细
            </button>
          </div>
          <div v-if="editing" class="editor-actions">
            <button class="button" type="button" @click="resetEditor">取消</button
            ><button
              class="button button-primary"
              type="button"
              :disabled="saving || needsRefresh || (draftRefreshed && !draftRechecked)"
              @click="saveRevision"
            >
              {{ saving ? '正在保存…' : '保存修订版本' }}
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
                  :disabled="busy || Boolean(uncertainAction)"
                  @change="toggleEvidence(evidence.id)"
                /><span
                  ><strong>第 {{ evidence.page }} 页</strong
                  ><small>{{ evidence.quote || '区域标注' }}</small></span
                ></label
              >
            </div>
            <p v-else class="quiet-block">
              {{
                review.entry_mode === 'manual'
                  ? '当前页尚无已保存证据，请在字段下标注原件页码和实际摘录后保存。'
                  : '当前页没有可选择的证据，请切换页面或保持阻断。'
              }}
            </p>
          </section>

          <section
            class="panel decision-panel"
            :data-resolved="validationResolved"
            aria-labelledby="validation-title"
          >
            <div class="panel-heading">
              <div>
                <h2 id="validation-title">规则校验</h2>
                <p>
                  {{
                    actionableValidations.length
                      ? `${actionableValidations.length} 项需要留意，请核对后处理。`
                      : '未发现问题，仍请核对原件。'
                  }}
                </p>
              </div>
            </div>
            <ValidationResults
              :validations="actionableValidations"
              :disabled="busy || Boolean(uncertainAction)"
              @locate="locateValidation"
            />
            <details
              v-if="passedValidations.length"
              :key="review.job.id"
              class="passed-validations"
            >
              <summary>查看 {{ passedValidations.length }} 项已通过规则</summary>
              <ValidationResults :validations="passedValidations" />
            </details>
          </section>

          <section
            class="panel decision-panel"
            :data-resolved="duplicatesResolved"
            aria-labelledby="duplicate-title"
          >
            <div class="panel-heading">
              <div>
                <h2 id="duplicate-title">疑似重复</h2>
                <p>
                  {{
                    duplicatesResolved
                      ? '未发现近似文件、重复页面或字段组合候选。'
                      : '逐项核对后决定是否保留，不会自动合并或删除。'
                  }}
                </p>
              </div>
            </div>
            <fieldset
              v-if="review.duplicate_candidates.length"
              class="association-options duplicate-options"
              :disabled="busy || Boolean(uncertainAction)"
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
                  <!-- 金额与币种来自同一个联结，要么都有要么都没有；缺币种就不显示裸数字。 -->
                  <small v-if="candidate.amount_minor !== undefined && candidate.currency">
                    {{ candidate.business_date }} ·
                    {{ formatMinorUnits(candidate.amount_minor, candidate.currency) }}
                  </small>
                  <small>
                    {{
                      candidate.available
                        ? '勾选表示仍保留为独立记录'
                        : '目标状态已变化，请保存修订版本'
                    }}
                  </small>
                  <small class="candidate-reasons"
                    >判断依据：{{
                      candidate.reason_codes.map(duplicateReasonLabel).join(' · ')
                    }}</small
                  >
                </span>
              </label>
            </fieldset>
            <p
              v-if="duplicateDecision?.error"
              id="duplicate-resolution-error"
              class="danger-text"
              role="alert"
            >
              {{ duplicateDecision.error }}
            </p>
          </section>

          <section
            v-if="review.document_type !== 'trip'"
            class="panel decision-panel"
            :data-resolved="noAssociationCandidates"
            aria-labelledby="association-title"
          >
            <div class="panel-heading">
              <div>
                <h2 id="association-title">金额分配</h2>
                <p>
                  {{
                    noAssociationCandidates
                      ? '当前没有候选，确认保存时不创建。'
                      : '选择关联单据和金额，或明确不关联。'
                  }}
                </p>
              </div>
            </div>
            <fieldset
              v-if="review.candidates.length"
              class="association-options"
              :disabled="busy || Boolean(uncertainAction)"
            >
              <legend class="visually-hidden">选择关联方式</legend>
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
                      分配给{{ documentTypeLabel(candidateFor(editor)?.target_type) }} ·
                      {{ candidateFor(editor)?.display_name }}
                    </strong>
                    <small>
                      总额 {{ candidateMoney(editor, 'amount_minor') }} · 已分配
                      {{ candidateMoney(editor, 'allocated_minor') }} · 剩余
                      {{ candidateMoney(editor, 'remaining_minor') }}
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
                  <label :for="`allocation-${editor.candidateId}`"
                    >本次分配（{{ candidateFor(editor)?.currency }}）</label
                  >
                  <input
                    :id="`allocation-${editor.candidateId}`"
                    v-model="editor.textValue"
                    class="input"
                    inputmode="decimal"
                    :aria-invalid="Boolean(associationDecision?.errors[editor.candidateId])"
                  />
                  <small v-if="associationDecision?.errors[editor.candidateId]" class="danger-text">
                    {{ associationDecision.errors[editor.candidateId] }}
                  </small>
                </div>
              </div>
              <div class="review-allocation-summary" aria-live="polite">
                <span
                  >单据总额 {{ allocationMoney(associationDecision?.factAmountMinor ?? 0) }}</span
                >
                <span>本次合计 {{ allocationMoney(associationDecision?.totalMinor ?? 0) }}</span>
                <span>
                  分配后剩余
                  {{
                    allocationMoney(
                      Math.max(
                        (associationDecision?.factAmountMinor ?? 0) -
                          (associationDecision?.totalMinor ?? 0),
                        0,
                      ),
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
                  @change="rejectAllCandidates"
                /><span>
                  <strong>不关联任何候选</strong>
                  <small>仅保存当前单据，不创建金额分配</small>
                </span>
              </label>
            </fieldset>
          </section>

          <section class="panel final-actions" aria-labelledby="final-title">
            <h2 id="final-title">完成审核</h2>
            <p v-if="uncertainAction" class="danger-text">
              上次{{
                uncertainAction === 'confirm' ? '确认' : '驳回'
              }}结果未知，仅可重试原决定；重试前不会切换单据。
            </p>
            <p v-else-if="review.claim_status === 'blocked'" class="danger-text">
              当前识别结果未通过校验，请先修订字段并保存，再完成审核。
            </p>
            <p v-else-if="duplicateDecision && !duplicateDecision.request">
              {{ duplicateDecision.error }}
            </p>
            <p
              v-else-if="
                review.document_type !== 'trip' &&
                associationDecision &&
                !associationDecision.request
              "
            >
              {{ associationDecision.errors.$association || '请修正候选分配金额。' }}
            </p>
            <p v-else>
              确认后将保存正式{{
                documentTypeLabel(review.document_type)
              }}记录，并保留原件与审核依据。{{
                noAssociationCandidates ? '本次不创建金额分配。' : ''
              }}{{ inQueue ? '保存成功后进入下一项，失败停留当前单据。' : '' }}
            </p>
            <button
              class="button button-primary button-block"
              type="button"
              :disabled="!canConfirm || confirming || editing"
              @click="confirmReview"
            >
              {{ confirmLabel }}
            </button>
            <button
              class="button button-block"
              type="button"
              :disabled="busy || Boolean(uncertainAction)"
              @click="rejectPanelOpen = !rejectPanelOpen"
            >
              驳回识别结果
            </button>
            <div v-if="rejectPanelOpen" class="reject-panel">
              <label for="reject-reason">驳回原因（可选）</label
              ><textarea
                id="reject-reason"
                v-model="rejectReason"
                class="textarea"
                maxlength="500"
                rows="3"
                :disabled="busy || Boolean(uncertainAction)"
              ></textarea
              ><button
                class="button button-danger button-block"
                type="button"
                :disabled="busy || editing || needsRefresh || uncertainAction === 'confirm'"
                @click="rejectReview"
              >
                {{
                  rejecting
                    ? '正在驳回…'
                    : uncertainAction === 'reject'
                      ? '重试原驳回'
                      : inQueue
                        ? '确认驳回并继续，不保存正式记录'
                        : '确认驳回，不保存正式记录'
                }}
              </button>
            </div>
          </section>
        </aside>
      </div>
    </template>

    <section v-else class="panel state-layout" role="alert">
      <span class="state-glyph"><AppIcon name="alert" /></span><strong>无法打开审核</strong>
      <p>{{ error }}</p>
      <button class="button button-primary" type="button" @click="load">重试读取当前单据</button>
      <RouterLink class="button" to="/inbox">返回收件箱</RouterLink>
    </section>
  </div>
</template>

<style scoped>
.review-queue-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
}
.review-queue-bar p {
  margin: 5px 0 0;
  color: var(--text-secondary);
  font-size: 13px;
}
.queue-draft-note {
  flex-basis: 100%;
}
.passed-validations > summary {
  padding: 12px 14px;
  color: var(--text-secondary);
  cursor: pointer;
}
.manual-source-notice {
  display: block;
  overflow-wrap: anywhere;
}
.manual-evidence-group {
  min-inline-size: 0;
  margin: 0;
  padding: 12px;
  gap: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-subtle);
}
.manual-evidence-group legend {
  color: var(--text-secondary);
  padding-inline: 4px;
}
.manual-evidence-group label {
  display: grid;
  gap: 6px;
}
</style>
