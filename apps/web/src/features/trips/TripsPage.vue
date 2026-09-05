<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import {
  ApiError,
  api,
  type Trip,
  type TripAssignmentRequest,
  type TripAttributionCandidate,
  type TripAttributionView,
} from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import TripEditor from './TripEditor.vue'
import TripMaterialsPanel from './TripMaterialsPanel.vue'
import MaterialExportPanel from '../reimbursements/MaterialExportPanel.vue'
import {
  assignmentFingerprint,
  buildTripAssignmentDecision,
  tripAssignmentActionLabel,
  tripReasonLabel,
  tripViewLabels,
  tripAssignmentStates,
} from './model'

type AssignmentAttempt = { fingerprint: string; idempotencyKey: string }

const trips = ref<Trip[]>([])
const editorOpen = ref(false)
const editingTrip = ref<Trip>()
const selectedTripID = ref('')
const view = ref<TripAttributionView>('suggested')
const candidates = ref<TripAttributionCandidate[]>([])
const nextCursor = ref('')
const loading = ref(true)
const candidatesLoading = ref(false)
const loadingMore = ref(false)
const changingFactID = ref('')
const forbidden = ref(false)
const offline = ref(!navigator.onLine)
const error = ref('')
const success = ref('')
const reasonDrafts = ref<Record<string, string>>({})
const rowErrors = ref<Record<string, string>>({})
const attempts = new Map<string, AssignmentAttempt>()
const tripViews: TripAttributionView[] = ['all', 'suggested', 'assigned']
let tripLoadRevision = 0
let candidateLoadRevision = 0
let editorTrigger: HTMLElement | null = null
const createButton = ref<HTMLButtonElement>()

const canRead = computed(() => sessionStore.current.value?.capabilities.includes('facts.read'))
const canManage = computed(() =>
  sessionStore.current.value?.capabilities.includes('trip_assignments.manage'),
)
const selectedTrip = computed(() => trips.value.find((trip) => trip.id === selectedTripID.value))
const canDelete = computed(
  () => sessionStore.current.value?.capabilities.includes('resources.delete') ?? false,
)

function openEditor(trip?: Trip) {
  editorTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
  editingTrip.value = trip
  editorOpen.value = true
}

async function closeEditor() {
  editorOpen.value = false
  await nextTick()
  if (editorTrigger?.isConnected) editorTrigger.focus()
  else createButton.value?.focus()
}

async function savedTrip(id: string) {
  editorOpen.value = false
  success.value = id
    ? '行程已保存，允许自动归属的支付已重新计算。'
    : '行程已删除，费用、凭证与报销历史均已保留。'
  await loadTrips(id)
  await nextTick()
  createButton.value?.focus()
}

async function changePreference(candidate: TripAttributionCandidate, mode: 'auto' | 'blocked') {
  if (offline.value || changingFactID.value) return
  changingFactID.value = candidate.fact_id
  rowErrors.value[candidate.fact_id] = ''
  try {
    await api.tripPreference(candidate.fact_id, mode, candidate.fact_version)
    success.value =
      mode === 'auto' ? '已恢复自动归属并重新计算。' : '已保持无归属，后续自动重算不会覆盖该选择。'
    await loadTrips()
  } catch (caught) {
    rowErrors.value[candidate.fact_id] =
      caught instanceof ApiError ? caught.message : '偏好更新失败，请刷新后重试'
  } finally {
    changingFactID.value = ''
  }
}

async function loadTrips(preferredTripID = selectedTripID.value) {
  const revision = ++tripLoadRevision
  if (!canRead.value) {
    forbidden.value = true
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  forbidden.value = false
  try {
    const page = await api.trips()
    if (revision !== tripLoadRevision) return
    trips.value = page.items
    selectedTripID.value = trips.value.some((trip) => trip.id === preferredTripID)
      ? preferredTripID
      : (trips.value[0]?.id ?? '')
    candidates.value = []
    nextCursor.value = ''
    if (selectedTripID.value) await loadCandidates(false)
  } catch (caught) {
    if (revision !== tripLoadRevision) return
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = forbidden.value
      ? ''
      : caught instanceof ApiError
        ? caught.message
        : '行程列表加载失败，请稍后重试'
  } finally {
    if (revision === tripLoadRevision) loading.value = false
  }
}

async function loadCandidates(append: boolean) {
  const tripID = selectedTripID.value
  if (!tripID || offline.value || (append && !nextCursor.value)) return
  const revision = ++candidateLoadRevision
  const selectedView = view.value
  if (append) loadingMore.value = true
  else candidatesLoading.value = true
  error.value = ''
  try {
    const page = await api.tripAttributionCandidates(
      tripID,
      selectedView,
      append ? nextCursor.value : '',
      20,
    )
    if (
      selectedTripID.value !== tripID ||
      view.value !== selectedView ||
      revision !== candidateLoadRevision
    )
      return
    candidates.value = append ? [...candidates.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor ?? ''
  } catch (caught) {
    if (revision !== candidateLoadRevision) return
    error.value = caught instanceof ApiError ? caught.message : '行程归属候选加载失败，请稍后重试'
  } finally {
    if (revision === candidateLoadRevision) {
      candidatesLoading.value = false
      loadingMore.value = false
    }
  }
}

async function selectTrip(tripID: string) {
  if (tripID === selectedTripID.value && !error.value) return
  selectedTripID.value = tripID
  candidates.value = []
  nextCursor.value = ''
  success.value = ''
  await loadCandidates(false)
}

async function selectView(nextView: TripAttributionView) {
  if (nextView === view.value) return
  view.value = nextView
  candidates.value = []
  nextCursor.value = ''
  success.value = ''
  await loadCandidates(false)
}

async function changeAssignment(candidate: TripAttributionCandidate) {
  if (!canManage.value || offline.value || !selectedTripID.value) return
  const decision = buildTripAssignmentDecision(
    candidate,
    selectedTripID.value,
    reasonDrafts.value[candidate.fact_id] ?? '',
  )
  if (!decision.request) {
    rowErrors.value[candidate.fact_id] = decision.error ?? '归属请求不完整'
    return
  }
  rowErrors.value[candidate.fact_id] = ''
  success.value = ''
  changingFactID.value = candidate.fact_id
  const key = assignmentKey(candidate.fact_id, decision.request)
  try {
    await api.changeTripAssignment(decision.request, key)
    reasonDrafts.value[candidate.fact_id] = ''
    rowErrors.value[candidate.fact_id] = ''
    attempts.delete(candidate.fact_id)
    success.value = `${candidate.display_name} 的行程归属已更新。`
    await loadTrips(selectedTripID.value)
  } catch (caught) {
    if (caught instanceof ApiError && caught.status === 409) {
      rowErrors.value[candidate.fact_id] = '当前归属已变化，请刷新后重试；已保留填写的理由。'
    } else {
      rowErrors.value[candidate.fact_id] =
        caught instanceof ApiError ? caught.message : '归属更新失败，请检查网络后重试'
    }
  } finally {
    changingFactID.value = ''
  }
}

function assignmentKey(factID: string, request: TripAssignmentRequest): string {
  const fingerprint = assignmentFingerprint(request)
  const existing = attempts.get(factID)
  if (existing?.fingerprint === fingerprint) return existing.idempotencyKey
  const attempt = { fingerprint, idempotencyKey: crypto.randomUUID() }
  attempts.set(factID, attempt)
  return attempt.idempotencyKey
}

function factTypeLabel(type: TripAttributionCandidate['fact_type']) {
  return type === 'payment' ? '支付' : '发票'
}

function emptyCandidateMessage() {
  if (view.value === 'suggested') return '当前规则没有建议项；切换“全部”仍可手工归属。'
  if (view.value === 'assigned') return '当前行程还没有已归属单据。'
  return '还没有可归属的支付或发票，请先在收件箱完成审核。'
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value) void loadTrips(selectedTripID.value)
}

onMounted(() => {
  void loadTrips()
  window.addEventListener('online', setOnlineState)
  window.addEventListener('offline', setOnlineState)
})

onUnmounted(() => {
  window.removeEventListener('online', setOnlineState)
  window.removeEventListener('offline', setOnlineState)
})
</script>

<template>
  <div class="page-stack trip-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>财务数据</span><span aria-hidden="true">/</span><strong>行程归属</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>行程归属</h1>
        <p>手动创建一趟出差，归集多张机票、行程凭证和相关费用。</p>
      </div>
      <div class="page-actions">
        <button
          v-if="canManage"
          ref="createButton"
          class="button button-primary"
          type="button"
          :disabled="offline || editorOpen"
          @click="openEditor()"
        >
          新建行程
        </button>
        <button
          v-if="canRead"
          class="button"
          type="button"
          :disabled="offline || loading"
          @click="loadTrips()"
        >
          刷新
        </button>
      </div>
    </header>

    <div v-if="offline" class="notice notice-warning" role="status">
      <AppIcon name="alert" /><span>当前离线。已加载的行程和理由草稿会保留。</span>
    </div>
    <div v-if="success" class="notice notice-success" role="status">
      <AppIcon name="check" /><span>{{ success }}</span>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <AppIcon name="alert" /><span>{{ error }}</span
      ><button class="text-button" type="button" :disabled="offline" @click="loadTrips()">
        重试
      </button>
    </div>

    <TripEditor
      v-if="editorOpen && canManage"
      :key="editingTrip?.id ?? 'new'"
      :trip="editingTrip"
      :can-delete="canDelete"
      :offline="offline"
      @saved="savedTrip"
      @cancel="closeEditor"
    />

    <div v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在读取行程</strong
      ><span>正在整理行程与单据。</span>
    </div>
    <div v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph"><AppIcon name="lock" /></span><strong>没有查看行程的权限</strong
      ><span>请联系管理员开通查看权限。</span>
    </div>
    <div v-else-if="trips.length === 0" class="panel state-layout">
      <span class="state-glyph"><AppIcon name="trip" /></span><strong>还没有行程</strong
      ><span>点击“新建行程”填写名称、日期与时区。凭证可以稍后上传，同一行程可关联多张机票。</span>
    </div>

    <div v-else class="trip-layout">
      <aside class="panel trip-list-panel" aria-labelledby="trip-list-title">
        <div class="panel-heading">
          <div>
            <h2 id="trip-list-title">行程列表</h2>
            <p>{{ trips.length }} 个行程</p>
          </div>
        </div>
        <ul class="trip-list">
          <li v-for="trip in trips" :key="trip.id">
            <button
              type="button"
              :aria-current="trip.id === selectedTripID ? 'true' : undefined"
              @click="selectTrip(trip.id)"
            >
              <strong>{{ trip.name }}</strong>
              <span v-if="trip.bad_debt_locked" class="status-pill status-warning"
                >坏账删除保护</span
              >
              <span>{{ trip.timezone || '待确认时区' }}</span>
              <time>{{ trip.start_date }} 至 {{ trip.end_date }}</time>
              <small
                >支付 {{ trip.assigned_payment_count }} · 发票 {{ trip.assigned_invoice_count }} ·
                凭证 {{ trip.material_count }}</small
              >
            </button>
          </li>
        </ul>
      </aside>

      <section class="panel trip-workspace" aria-labelledby="trip-workspace-title">
        <div class="panel-heading trip-workspace-heading">
          <div>
            <h2 id="trip-workspace-title">{{ selectedTrip?.name }}</h2>
            <p>{{ selectedTrip?.start_date }} 至 {{ selectedTrip?.end_date }}</p>
            <p v-if="selectedTrip?.notes">{{ selectedTrip.notes }}</p>
            <p v-if="selectedTrip?.bad_debt_locked" class="notice notice-warning">
              关联单据已标记坏账；处理坏账或调整关联前，不能删除此行程。
            </p>
          </div>
          <button
            v-if="canManage"
            class="button button-small"
            :disabled="editorOpen || offline"
            @click="openEditor(selectedTrip)"
          >
            编辑行程
          </button>
          <div class="trip-view-switch" role="group" aria-label="归属候选筛选">
            <button
              v-for="labelView in tripViews"
              :key="labelView"
              class="button button-small"
              type="button"
              :aria-pressed="view === labelView"
              @click="selectView(labelView)"
            >
              {{ tripViewLabels[labelView] }}
            </button>
          </div>
        </div>

        <p v-if="selectedTrip && !selectedTrip.timezone" class="notice notice-warning">
          此行程保留自之前版本，尚未确认时区。编辑并保存时区后才会参与自动归属；已有人工归属保持不变。
        </p>
        <MaterialExportPanel
          v-if="selectedTrip"
          :key="selectedTrip.id"
          :scope="{ kind: 'trip', id: selectedTrip.id }"
          :disabled="editorOpen || offline || !!changingFactID || candidatesLoading"
        />
        <p class="quiet-block">
          支付按实际交易时间与行程时区自动匹配，重叠时由人工选择。下方日期与关联建议仅作参考，发票始终人工归属。
        </p>

        <div v-if="candidatesLoading" class="state-layout" role="status">
          <span class="spinner" aria-hidden="true"></span><strong>正在计算归属候选</strong>
        </div>
        <div v-else-if="candidates.length === 0" class="state-layout">
          <span class="state-glyph"><AppIcon name="search" /></span><strong>当前筛选没有单据</strong
          ><span>{{ emptyCandidateMessage() }}</span>
        </div>
        <ul v-else class="trip-candidate-list">
          <li v-for="candidate in candidates" :key="`${candidate.fact_type}:${candidate.fact_id}`">
            <article class="trip-candidate">
              <header>
                <div>
                  <span class="status" :data-tone="candidate.suggested ? 'info' : 'neutral'">
                    <span aria-hidden="true">●</span
                    >{{ candidate.suggested ? '规则建议' : '全部记录' }}
                  </span>
                  <strong
                    >{{ factTypeLabel(candidate.fact_type) }} · {{ candidate.display_name }}</strong
                  >
                  <small>{{ candidate.business_date }} · 记录编号 {{ candidate.fact_id }}</small>
                </div>
                <strong class="numeric">{{
                  formatMinorUnits(candidate.amount_minor, candidate.currency)
                }}</strong>
              </header>
              <p class="trip-current-assignment">
                当前行程：{{ candidate.current_trip_name || '未归属' }} ·
                {{ tripAssignmentStates[candidate.assignment_state] }}
              </p>
              <ul class="trip-reasons" aria-label="建议原因">
                <li v-for="reason in candidate.reason_codes" :key="reason">
                  {{ tripReasonLabel(reason) }}
                </li>
                <li v-if="candidate.reason_codes.length === 0">没有规则建议，需人工判断</li>
              </ul>
              <div v-if="canManage" class="trip-assignment-form">
                <label :for="`trip-reason-${candidate.fact_id}`">归属理由</label>
                <textarea
                  :id="`trip-reason-${candidate.fact_id}`"
                  v-model="reasonDrafts[candidate.fact_id]"
                  class="textarea"
                  rows="2"
                  maxlength="500"
                  :aria-invalid="Boolean(rowErrors[candidate.fact_id])"
                  :aria-describedby="
                    rowErrors[candidate.fact_id] ? `trip-error-${candidate.fact_id}` : undefined
                  "
                ></textarea>
                <div class="trip-assignment-actions">
                  <small>{{ [...(reasonDrafts[candidate.fact_id] ?? '')].length }} / 500</small>
                  <button
                    v-if="candidate.fact_type === 'payment' && candidate.assignment_mode !== 'auto'"
                    class="button"
                    :disabled="Boolean(changingFactID) || offline"
                    @click="changePreference(candidate, 'auto')"
                  >
                    恢复自动归属
                  </button>
                  <button
                    v-if="
                      candidate.fact_type === 'payment' &&
                      !candidate.current_assignment_id &&
                      candidate.assignment_mode === 'auto'
                    "
                    class="button"
                    :disabled="Boolean(changingFactID) || offline"
                    @click="changePreference(candidate, 'blocked')"
                  >
                    保持无归属
                  </button>
                  <button
                    class="button button-primary"
                    type="button"
                    :disabled="changingFactID === candidate.fact_id || offline"
                    @click="changeAssignment(candidate)"
                  >
                    {{
                      changingFactID === candidate.fact_id
                        ? '正在提交…'
                        : tripAssignmentActionLabel(candidate, selectedTripID)
                    }}
                  </button>
                </div>
                <p
                  v-if="rowErrors[candidate.fact_id]"
                  :id="`trip-error-${candidate.fact_id}`"
                  class="danger-text"
                  role="alert"
                >
                  {{ rowErrors[candidate.fact_id] }}
                </p>
              </div>
              <p v-else class="quiet-block">当前账号为只读；可查看建议与活动归属。</p>
            </article>
          </li>
        </ul>
        <div v-if="nextCursor" class="trip-load-more">
          <button
            class="button"
            type="button"
            :disabled="loadingMore || offline"
            @click="loadCandidates(true)"
          >
            {{ loadingMore ? '正在加载…' : '加载更多' }}
          </button>
        </div>
        <p v-else-if="candidates.length" class="trip-list-end">当前筛选已全部加载。</p>
      </section>
    </div>
    <TripMaterialsPanel
      v-if="canRead && !forbidden"
      :trip="selectedTrip"
      :can-manage="Boolean(canManage)"
      :offline="offline"
      @changed="loadTrips()"
    />
  </div>
</template>
