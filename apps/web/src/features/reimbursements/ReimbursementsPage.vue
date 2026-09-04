<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import {
  ApiError,
  api,
  type ReimbursementDetail,
  type ReimbursementPolicySnapshot,
  type ReimbursementStatus,
  type ReimbursementSummary,
  type Trip,
  type TripAttributionCandidate,
} from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import {
  buildReimbursementStatusRequest,
  buildReimbursementSubmission,
  reimbursementFindingLabel,
  reimbursementRequestFingerprint,
  reimbursementStatusActions,
  reimbursementStatusLabels,
} from './model'

type Attempt = { fingerprint: string; idempotencyKey: string }

const decisionActionLabels = {
  submit: '提交报销',
  mark_reimbursed: '标记已报销',
  reject: '退回报销',
  reopen: '重新打开',
} as const

const reimbursements = ref<ReimbursementSummary[]>([])
const reimbursementCursor = ref('')
const trips = ref<Trip[]>([])
const selectedTripID = ref('')
const candidates = ref<TripAttributionCandidate[]>([])
const candidateCursor = ref('')
const selectedAssignmentIDs = ref<string[]>([])
const preview = ref<ReimbursementPolicySnapshot>()
const findingsAcknowledged = ref(false)
const submissionReason = ref('')
const selectedReimbursementID = ref('')
const detail = ref<ReimbursementDetail>()
const statusReason = ref('')
const loading = ref(true)
const loadingCandidates = ref(false)
const loadingMoreCandidates = ref(false)
const loadingMoreReimbursements = ref(false)
const loadingDetail = ref(false)
const previewing = ref(false)
const submitting = ref(false)
const changingStatus = ref<ReimbursementStatus>()
const forbidden = ref(false)
const offline = ref(!navigator.onLine)
const error = ref('')
const workspaceError = ref('')
const statusError = ref('')
const success = ref('')
const attempts = new Map<string, Attempt>()

const capabilities = computed(() => new Set(sessionStore.current.value?.capabilities ?? []))
const canRead = computed(() => capabilities.value.has('reimbursements.read'))
const canManage = computed(() => capabilities.value.has('reimbursements.manage'))
const selectedTrip = computed(() => trips.value.find((trip) => trip.id === selectedTripID.value))
const selectedCount = computed(() => selectedAssignmentIDs.value.length)
const submissionDecision = computed(() =>
  buildReimbursementSubmission(
    preview.value,
    selectedAssignmentIDs.value,
    findingsAcknowledged.value,
    submissionReason.value,
  ),
)

async function loadPage() {
  if (!canRead.value) {
    forbidden.value = true
    loading.value = false
    return
  }
  loading.value = true
  forbidden.value = false
  error.value = ''
  try {
    const page = await api.reimbursements('', 50)
    reimbursements.value = page.items
    reimbursementCursor.value = page.next_cursor ?? ''
    if (canManage.value) {
      invalidatePreview()
      trips.value = (await api.trips()).items
      const preferred = selectedTripID.value
      selectedTripID.value = trips.value.some((trip) => trip.id === preferred)
        ? preferred
        : (trips.value[0]?.id ?? '')
      if (selectedTripID.value) await loadCandidates(false)
    }
    const selectedID = reimbursements.value.some(
      (item) => item.id === selectedReimbursementID.value,
    )
      ? selectedReimbursementID.value
      : (reimbursements.value[0]?.id ?? '')
    if (selectedID) await selectReimbursement(selectedID, true)
    else {
      selectedReimbursementID.value = ''
      detail.value = undefined
    }
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = forbidden.value
      ? ''
      : caught instanceof ApiError
        ? caught.message
        : '报销工作台加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function loadMoreReimbursements() {
  if (!reimbursementCursor.value || offline.value) return
  loadingMoreReimbursements.value = true
  error.value = ''
  try {
    const page = await api.reimbursements(reimbursementCursor.value, 50)
    reimbursements.value = [...reimbursements.value, ...page.items]
    reimbursementCursor.value = page.next_cursor ?? ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '更多报销记录加载失败'
  } finally {
    loadingMoreReimbursements.value = false
  }
}

async function changeTrip() {
  invalidatePreview()
  candidates.value = []
  candidateCursor.value = ''
  await loadCandidates(false)
}

async function loadCandidates(append: boolean) {
  const tripID = selectedTripID.value
  if (!tripID || offline.value || (append && !candidateCursor.value)) return
  if (append) loadingMoreCandidates.value = true
  else loadingCandidates.value = true
  workspaceError.value = ''
  try {
    const page = await api.tripAttributionCandidates(
      tripID,
      'assigned',
      append ? candidateCursor.value : '',
      100,
    )
    if (tripID !== selectedTripID.value) return
    candidates.value = append ? [...candidates.value, ...page.items] : page.items
    candidateCursor.value = page.next_cursor ?? ''
  } catch (caught) {
    workspaceError.value =
      caught instanceof ApiError ? caught.message : '已归属项目加载失败，请稍后重试'
  } finally {
    loadingCandidates.value = false
    loadingMoreCandidates.value = false
  }
}

function toggleAssignment(assignmentID: string) {
  if (!assignmentID) return
  const current = new Set(selectedAssignmentIDs.value)
  if (current.has(assignmentID)) current.delete(assignmentID)
  else {
    if (current.size >= 200) {
      workspaceError.value = '一次报销最多选择 200 个已归属项目。'
      return
    }
    current.add(assignmentID)
  }
  selectedAssignmentIDs.value = [...current]
  invalidatePreview(false)
}

function invalidatePreview(clearSelection = true) {
  preview.value = undefined
  findingsAcknowledged.value = false
  if (clearSelection) selectedAssignmentIDs.value = []
  workspaceError.value = ''
  attempts.delete('submission')
}

async function runPreview() {
  if (!canManage.value || offline.value) return
  if (!selectedTripID.value || selectedAssignmentIDs.value.length === 0) {
    workspaceError.value = '请明确勾选至少一个已归属项目；系统不会默认选择。'
    return
  }
  if (selectedAssignmentIDs.value.length > 200) {
    workspaceError.value = '一次报销最多选择 200 个已归属项目。'
    return
  }
  previewing.value = true
  workspaceError.value = ''
  success.value = ''
  try {
    preview.value = await api.reimbursementPreview({
      trip_id: selectedTripID.value,
      assignment_ids: [...selectedAssignmentIDs.value].sort(),
    })
    findingsAcknowledged.value = false
    attempts.delete('submission')
  } catch (caught) {
    workspaceError.value = caught instanceof ApiError ? caught.message : '政策预检失败，请稍后重试'
  } finally {
    previewing.value = false
  }
}

async function submitReimbursement() {
  const decision = submissionDecision.value
  if (!decision.request || offline.value) {
    workspaceError.value = decision.error ?? '当前离线，不能提交报销'
    return
  }
  submitting.value = true
  workspaceError.value = ''
  success.value = ''
  const fingerprint = reimbursementRequestFingerprint(decision.request.trip_id, decision.request)
  try {
    const result = await api.submitReimbursement(
      decision.request,
      idempotencyKey('submission', fingerprint),
    )
    success.value = result.replayed ? '已返回同一报销提交结果。' : '报销快照已提交。'
    attempts.delete('submission')
    submissionReason.value = ''
    findingsAcknowledged.value = false
    preview.value = undefined
    selectedAssignmentIDs.value = []
    await reloadReimbursements(result.reimbursement_id)
  } catch (caught) {
    workspaceError.value =
      caught instanceof ApiError && caught.status === 409
        ? '报销输入或当前状态已变化，请重新预检；提交理由已保留。'
        : caught instanceof ApiError
          ? caught.message
          : '报销提交失败，请检查网络后重试'
    if (caught instanceof ApiError && caught.status === 409) {
      preview.value = undefined
      findingsAcknowledged.value = false
    }
  } finally {
    submitting.value = false
  }
}

async function reloadReimbursements(preferredID = selectedReimbursementID.value) {
  const page = await api.reimbursements('', 50)
  reimbursements.value = page.items
  reimbursementCursor.value = page.next_cursor ?? ''
  if (preferredID) await selectReimbursement(preferredID, true)
}

async function selectReimbursement(reimbursementID: string, preserveDraft = false) {
  if (!reimbursementID || offline.value) return
  selectedReimbursementID.value = reimbursementID
  loadingDetail.value = true
  if (!preserveDraft) {
    statusReason.value = ''
    statusError.value = ''
  }
  try {
    detail.value = await api.reimbursement(reimbursementID)
  } catch (caught) {
    detail.value = undefined
    statusError.value = caught instanceof ApiError ? caught.message : '报销详情加载失败'
  } finally {
    loadingDetail.value = false
  }
}

async function changeStatus(desiredStatus: ReimbursementStatus) {
  if (!detail.value || !canManage.value || offline.value) return
  const current = detail.value
  const decision = buildReimbursementStatusRequest(current, desiredStatus, statusReason.value)
  if (!decision.request) {
    statusError.value = decision.error ?? '状态请求不完整'
    return
  }
  changingStatus.value = desiredStatus
  statusError.value = ''
  success.value = ''
  const scope = `status:${current.id}`
  const fingerprint = reimbursementRequestFingerprint(current.id, decision.request)
  try {
    const result = await api.changeReimbursementStatus(
      current.id,
      decision.request,
      idempotencyKey(scope, fingerprint),
    )
    attempts.delete(scope)
    statusReason.value = ''
    success.value = result.replayed ? '已返回同一状态决定结果。' : '报销状态已更新。'
    await reloadReimbursements(current.id)
  } catch (caught) {
    statusError.value =
      caught instanceof ApiError && caught.status === 409
        ? '状态或版本已变化，已刷新详情并保留理由草稿。'
        : caught instanceof ApiError
          ? caught.message
          : '状态更新失败，请检查网络后重试'
    if (caught instanceof ApiError && caught.status === 409) {
      await selectReimbursement(current.id, true)
    }
  } finally {
    changingStatus.value = undefined
  }
}

function idempotencyKey(scope: string, fingerprint: string): string {
  const existing = attempts.get(scope)
  if (existing?.fingerprint === fingerprint) return existing.idempotencyKey
  const attempt = { fingerprint, idempotencyKey: crypto.randomUUID() }
  attempts.set(scope, attempt)
  return attempt.idempotencyKey
}

function candidateAssignmentID(candidate: TripAttributionCandidate): string {
  return candidate.current_assignment_id ?? ''
}

function isSelected(candidate: TripAttributionCandidate): boolean {
  return selectedAssignmentIDs.value.includes(candidateAssignmentID(candidate))
}

function candidateSelectionDisabled(candidate: TripAttributionCandidate): boolean {
  const assignmentID = candidateAssignmentID(candidate)
  return !assignmentID || (!isSelected(candidate) && selectedCount.value >= 200)
}

function reimbursementFindingContext(finding: {
  code: 'missing_invoice' | 'amount_conflict' | 'duplicate_reimbursement'
  expected_minor?: number
  actual_minor?: number
  currency?: 'CNY' | 'USD' | 'EUR' | 'JPY'
  related_reimbursement_id?: string
  related_status?: ReimbursementStatus
}): string {
  if (finding.code === 'missing_invoice') {
    return '当前选择中没有与该支付相连的活动发票分配。'
  }
  if (
    finding.code === 'amount_conflict' &&
    finding.expected_minor !== undefined &&
    finding.actual_minor !== undefined &&
    finding.currency
  ) {
    return `单据金额 ${formatMinorUnits(finding.expected_minor, finding.currency)}，本次所选分配合计 ${formatMinorUnits(finding.actual_minor, finding.currency)}。`
  }
  if (finding.related_reimbursement_id) {
    const status = finding.related_status
      ? reimbursementStatusLabels[finding.related_status]
      : '状态未知'
    return `关联报销 ${finding.related_reimbursement_id} · ${status}`
  }
  return '相关政策输入已冻结在提交快照中。'
}

function factTypeLabel(type: 'payment' | 'invoice') {
  return type === 'payment' ? '支付' : '发票'
}

function statusTone(status: ReimbursementStatus) {
  if (status === 'reimbursed') return 'success'
  if (status === 'rejected') return 'danger'
  return 'warning'
}

function statusActionLabel(status: ReimbursementStatus) {
  if (status === 'reimbursed') return '标记已报销'
  if (status === 'rejected') return '退回'
  return '重新打开'
}

function displayTimestamp(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value) void loadPage()
}

onMounted(() => {
  void loadPage()
  window.addEventListener('online', setOnlineState)
  window.addEventListener('offline', setOnlineState)
})

onUnmounted(() => {
  window.removeEventListener('online', setOnlineState)
  window.removeEventListener('offline', setOnlineState)
})
</script>

<template>
  <div class="page-stack reimbursement-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>财务数据</span><span aria-hidden="true">/</span><strong>报销管理</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>报销管理</h1>
        <p>选择行程单据、核对政策提示，提交报销并跟踪处理状态。</p>
      </div>
      <button v-if="canRead" class="button" type="button" :disabled="offline" @click="loadPage">
        刷新
      </button>
    </header>

    <div v-if="offline" class="notice notice-warning" role="status">
      <AppIcon name="alert" /><span>当前离线。理由草稿会保留，恢复联网后可刷新。</span>
    </div>
    <div v-if="success" class="notice notice-success" role="status">
      <AppIcon name="check" /><span>{{ success }}</span>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <AppIcon name="alert" /><span>{{ error }}</span>
      <button class="text-button" type="button" :disabled="offline" @click="loadPage">重试</button>
    </div>

    <div v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span
      ><strong>正在读取报销工作台</strong>
    </div>
    <div v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph"><AppIcon name="lock" /></span><strong>没有查看报销的权限</strong>
      <span>请联系管理员开通查看权限。</span>
    </div>

    <template v-else>
      <section
        v-if="canManage"
        class="panel reimbursement-create"
        aria-labelledby="reimbursement-create-title"
      >
        <div class="panel-heading">
          <div>
            <h2 id="reimbursement-create-title">新建报销</h2>
            <p>请自行勾选需要报销的项目，预检完成后再确认提交。</p>
          </div>
        </div>

        <div v-if="trips.length === 0" class="state-layout compact">
          <span class="state-glyph"><AppIcon name="trip" /></span><strong>没有可用行程</strong>
          <span>先审核确认行程，并为它归属支付或发票。</span>
          <RouterLink class="button" to="/trips">前往行程归属</RouterLink>
        </div>
        <template v-else>
          <div class="reimbursement-trip-picker">
            <label class="field-stack" for="reimbursement-trip">
              <span>行程</span>
              <select
                id="reimbursement-trip"
                v-model="selectedTripID"
                class="select"
                @change="changeTrip"
              >
                <option v-for="trip in trips" :key="trip.id" :value="trip.id">
                  {{ trip.destination }} · {{ trip.start_date }} 至 {{ trip.end_date }}
                </option>
              </select>
            </label>
            <div v-if="selectedTrip" class="reimbursement-trip-summary">
              <strong>{{ selectedTrip.destination }}</strong>
              <span
                >支付 {{ selectedTrip.assigned_payment_count }} · 发票
                {{ selectedTrip.assigned_invoice_count }}</span
              >
            </div>
          </div>

          <div
            v-if="workspaceError"
            class="notice notice-danger reimbursement-inline-notice"
            role="alert"
          >
            <AppIcon name="alert" /><span>{{ workspaceError }}</span>
          </div>
          <div v-if="loadingCandidates" class="state-layout compact" role="status">
            <span class="spinner" aria-hidden="true"></span><strong>正在读取已归属项目</strong>
          </div>
          <div v-else-if="candidates.length === 0" class="state-layout compact">
            <span class="state-glyph"><AppIcon name="document" /></span
            ><strong>该行程没有已归属项目</strong>
            <span>先在行程归属页为该行程添加支付或发票。</span>
            <RouterLink class="button" to="/trips">前往行程归属</RouterLink>
          </div>
          <fieldset v-else class="reimbursement-selection">
            <legend>选择报销项目（已选 {{ selectedCount }} / {{ candidates.length }}）</legend>
            <label
              v-for="candidate in candidates"
              :key="`${candidate.fact_type}:${candidate.fact_id}`"
            >
              <input
                type="checkbox"
                :checked="isSelected(candidate)"
                :disabled="candidateSelectionDisabled(candidate)"
                @change="toggleAssignment(candidateAssignmentID(candidate))"
              />
              <span>
                <strong
                  >{{ factTypeLabel(candidate.fact_type) }} · {{ candidate.display_name }}</strong
                >
                <small>{{ candidate.business_date }} · 记录编号 {{ candidate.fact_id }}</small>
              </span>
              <strong class="numeric">{{
                formatMinorUnits(candidate.amount_minor, candidate.currency)
              }}</strong>
            </label>
          </fieldset>
          <div v-if="candidateCursor" class="reimbursement-load-more">
            <button
              class="button button-small"
              type="button"
              :disabled="loadingMoreCandidates || offline"
              @click="loadCandidates(true)"
            >
              {{ loadingMoreCandidates ? '正在加载…' : '加载更多已归属项目' }}
            </button>
          </div>

          <div class="reimbursement-preview-action">
            <span>检查发票缺失、金额冲突和重复报销；预检不会提交报销。</span>
            <button
              class="button"
              type="button"
              :disabled="previewing || selectedCount === 0 || offline"
              @click="runPreview"
            >
              {{ previewing ? '正在预检…' : '运行政策预检' }}
            </button>
          </div>

          <div v-if="preview" class="reimbursement-preview" aria-live="polite">
            <div class="reimbursement-preview-heading">
              <div>
                <strong>预检完成</strong>
                <small
                  >快照 {{ preview.snapshot_hash.slice(0, 12) }}… ·
                  {{ preview.items.length }} 项</small
                >
              </div>
              <dl>
                <div v-for="total in preview.totals_by_currency" :key="total.currency">
                  <dt>{{ total.currency }}</dt>
                  <dd>{{ formatMinorUnits(total.amount_minor, total.currency) }}</dd>
                </div>
              </dl>
            </div>
            <div v-if="preview.findings.length > 0" class="reimbursement-findings">
              <strong>{{ preview.findings.length }} 条政策提示</strong>
              <ul>
                <li v-for="finding in preview.findings" :key="finding.finding_key">
                  <span class="status" data-tone="warning"
                    ><span aria-hidden="true">●</span
                    >{{ reimbursementFindingLabel(finding.code) }}</span
                  >
                  <small>{{ factTypeLabel(finding.fact_type) }} · {{ finding.fact_id }}</small>
                  <small>{{ reimbursementFindingContext(finding) }}</small>
                </li>
              </ul>
              <label class="reimbursement-acknowledgement">
                <input v-model="findingsAcknowledged" type="checkbox" />
                <span>我已逐项核对以上全部政策提示，确认继续提交。</span>
              </label>
            </div>
            <p v-else class="quiet-block">未发现政策提示，可以继续填写提交理由。</p>
            <p class="quiet-block">提交后，所选项目、金额与政策提示会固定保存，不随原单据变动。</p>
            <div class="reimbursement-submit">
              <label class="field-stack">
                <span>提交理由</span>
                <textarea
                  v-model="submissionReason"
                  class="textarea"
                  rows="3"
                  maxlength="500"
                  :aria-invalid="Boolean(workspaceError && !submissionDecision.request)"
                ></textarea>
                <small class="form-note"
                  >必填，最多 500 个字符；不同币种仅分组展示，不进行汇率换算。</small
                >
              </label>
              <button
                class="button button-primary"
                type="button"
                :disabled="submitting || offline || !submissionDecision.request"
                @click="submitReimbursement"
              >
                {{ submitting ? '正在提交…' : '提交报销' }}
              </button>
            </div>
          </div>
        </template>
      </section>
      <div v-else class="notice" role="status">
        <AppIcon name="lock" />
        <span>当前账号为只读，可查看报销快照与完整状态历史。</span>
      </div>

      <div class="reimbursement-history-layout">
        <section class="panel reimbursement-list-panel" aria-labelledby="reimbursement-list-title">
          <div class="panel-heading">
            <div>
              <h2 id="reimbursement-list-title">报销记录</h2>
              <p>{{ reimbursements.length }} 条已加载记录</p>
            </div>
          </div>
          <div v-if="reimbursements.length === 0" class="state-layout compact">
            <span class="state-glyph"><AppIcon name="receipt" /></span
            ><strong>还没有报销记录</strong>
            <span v-if="canManage">完成上方项目选择与预检后，即可提交第一笔报销。</span>
            <span v-else>已提交的报销会显示在这里。</span>
          </div>
          <ul v-else class="reimbursement-list">
            <li v-for="item in reimbursements" :key="item.id">
              <button
                type="button"
                :aria-current="item.id === selectedReimbursementID ? 'true' : undefined"
                @click="selectReimbursement(item.id)"
              >
                <span>
                  <strong>{{ item.trip.destination }}</strong>
                  <small>{{ item.trip.start_date }} 至 {{ item.trip.end_date }}</small>
                </span>
                <span class="status" :data-tone="statusTone(item.status)"
                  ><span aria-hidden="true">●</span
                  >{{ reimbursementStatusLabels[item.status] }}</span
                >
                <small
                  >{{ item.item_count }} 项 · {{ item.finding_count }} 条提示 · v{{
                    item.version
                  }}</small
                >
              </button>
            </li>
          </ul>
          <div v-if="reimbursementCursor" class="reimbursement-load-more">
            <button
              class="button button-small"
              type="button"
              :disabled="loadingMoreReimbursements || offline"
              @click="loadMoreReimbursements"
            >
              {{ loadingMoreReimbursements ? '正在加载…' : '加载更多记录' }}
            </button>
          </div>
        </section>

        <section
          class="panel reimbursement-detail"
          :aria-labelledby="detail && !loadingDetail ? 'reimbursement-detail-title' : undefined"
          :aria-label="!detail || loadingDetail ? '报销详情' : undefined"
        >
          <div
            v-if="statusError && (!detail || !canManage)"
            class="notice notice-danger reimbursement-inline-notice"
            role="alert"
          >
            <AppIcon name="alert" /><span>{{ statusError }}</span>
          </div>
          <div v-if="loadingDetail" class="state-layout" role="status">
            <span class="spinner" aria-hidden="true"></span><strong>正在读取报销详情</strong>
          </div>
          <div v-else-if="!detail" class="state-layout">
            <span class="state-glyph"><AppIcon name="receipt" /></span
            ><strong>请选择一条报销记录</strong>
          </div>
          <template v-else>
            <div class="panel-heading reimbursement-detail-heading">
              <div>
                <h2 id="reimbursement-detail-title">{{ detail.trip.destination }}</h2>
                <p>
                  {{ detail.trip.start_date }} 至 {{ detail.trip.end_date }} · v{{ detail.version }}
                </p>
              </div>
              <span class="status" :data-tone="statusTone(detail.status)"
                ><span aria-hidden="true">●</span
                >{{ reimbursementStatusLabels[detail.status] }}</span
              >
            </div>
            <div
              v-if="detail.trip_deleted"
              class="notice notice-warning reimbursement-inline-notice"
              role="status"
            >
              <AppIcon name="alert" />
              <span>原行程已删除；此处保留提交时的记录用于审计。</span>
            </div>
            <dl class="reimbursement-totals">
              <div v-for="total in detail.totals_by_currency" :key="total.currency">
                <dt>{{ total.currency }} 快照合计</dt>
                <dd>{{ formatMinorUnits(total.amount_minor, total.currency) }}</dd>
              </div>
            </dl>
            <div class="reimbursement-detail-section">
              <h3>快照项目</h3>
              <ul class="reimbursement-detail-items">
                <li v-for="item in detail.items" :key="item.id">
                  <span>
                    <strong>{{ factTypeLabel(item.fact_type) }} · {{ item.display_name }}</strong>
                    <small
                      >{{ item.business_date
                      }}<template v-if="item.source_deleted"> · 原单据已删除</template></small
                    >
                  </span>
                  <strong class="numeric">{{
                    formatMinorUnits(item.amount_minor, item.currency)
                  }}</strong>
                </li>
              </ul>
            </div>
            <div class="reimbursement-detail-section">
              <h3>提交时政策提示</h3>
              <p v-if="detail.findings.length === 0" class="quiet">没有提示。</p>
              <ul v-else class="reimbursement-detail-findings">
                <li v-for="finding in detail.findings" :key="finding.id">
                  <strong>{{ reimbursementFindingLabel(finding.code) }}</strong>
                  <small>{{ reimbursementFindingContext(finding) }}</small>
                </li>
              </ul>
            </div>
            <div class="reimbursement-detail-section">
              <h3>处理历史</h3>
              <ol class="reimbursement-decisions">
                <li v-for="decision in detail.decisions" :key="decision.id">
                  <span
                    >{{ decisionActionLabels[decision.action] }} · 版本
                    {{ decision.result_version }}</span
                  >
                  <strong>{{ reimbursementStatusLabels[decision.desired_status] }}</strong>
                  <p>{{ decision.reason }}</p>
                  <time>{{ displayTimestamp(decision.created_at) }}</time>
                </li>
              </ol>
            </div>
            <div v-if="canManage" class="reimbursement-status-form">
              <label class="field-stack">
                <span>状态变化理由</span>
                <textarea
                  v-model="statusReason"
                  class="textarea"
                  rows="3"
                  maxlength="500"
                  :aria-invalid="Boolean(statusError)"
                ></textarea>
                <small class="form-note">发生版本冲突时会刷新详情并保留此草稿。</small>
              </label>
              <p v-if="statusError" class="danger-text" role="alert">{{ statusError }}</p>
              <div class="reimbursement-status-actions">
                <button
                  v-for="desired in reimbursementStatusActions(detail.status)"
                  :key="desired"
                  class="button"
                  :class="{
                    'button-primary': desired === 'reimbursed',
                    'button-danger': desired === 'rejected',
                  }"
                  type="button"
                  :disabled="Boolean(changingStatus) || offline"
                  @click="changeStatus(desired)"
                >
                  {{ changingStatus === desired ? '正在保存…' : statusActionLabel(desired) }}
                </button>
              </div>
            </div>
          </template>
        </section>
      </div>
    </template>
  </div>
</template>
