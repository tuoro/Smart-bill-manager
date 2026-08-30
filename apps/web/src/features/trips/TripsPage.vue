<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { sessionStore } from '../../app/session'
import {
  ApiError,
  api,
  type Trip,
  type TripAssignmentRequest,
  type TripAttributionCandidate,
  type TripAttributionView,
} from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import {
  assignmentFingerprint,
  buildTripAssignmentDecision,
  tripAssignmentActionLabel,
  tripReasonLabel,
  tripViewLabels,
} from './model'

type AssignmentAttempt = { fingerprint: string; idempotencyKey: string }

const trips = ref<Trip[]>([])
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

const canRead = computed(() => sessionStore.current.value?.capabilities.includes('facts.read'))
const canManage = computed(() =>
  sessionStore.current.value?.capabilities.includes('trip_assignments.manage'),
)
const selectedTrip = computed(() => trips.value.find((trip) => trip.id === selectedTripID.value))

async function loadTrips(preferredTripID = selectedTripID.value) {
  if (!canRead.value) {
    forbidden.value = true
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  forbidden.value = false
  try {
    trips.value = (await api.trips()).items
    selectedTripID.value = trips.value.some((trip) => trip.id === preferredTripID)
      ? preferredTripID
      : (trips.value[0]?.id ?? '')
    candidates.value = []
    nextCursor.value = ''
    if (selectedTripID.value) await loadCandidates(false)
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = forbidden.value
      ? ''
      : caught instanceof ApiError
        ? caught.message
        : '行程列表加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function loadCandidates(append: boolean) {
  const tripID = selectedTripID.value
  if (!tripID || offline.value || (append && !nextCursor.value)) return
  if (append) loadingMore.value = true
  else candidatesLoading.value = true
  error.value = ''
  try {
    const page = await api.tripAttributionCandidates(
      tripID,
      view.value,
      append ? nextCursor.value : '',
      20,
    )
    if (selectedTripID.value !== tripID) return
    candidates.value = append ? [...candidates.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor ?? ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '行程归属候选加载失败，请稍后重试'
  } finally {
    candidatesLoading.value = false
    loadingMore.value = false
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
  if (view.value === 'assigned') return '当前行程还没有已归属 Fact。'
  return '当前租户还没有可归属的支付或发票。'
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
        <h1>行程与单据归属</h1>
        <p>按确定性日期和既有关联给出建议，所有归属仍由人工明确提交。</p>
      </div>
      <button v-if="canRead" class="button" type="button" :disabled="offline" @click="loadTrips()">
        刷新
      </button>
    </header>

    <div v-if="offline" class="notice notice-warning" role="status">
      <span aria-hidden="true">!</span><span>当前离线。已加载的行程和理由草稿会保留。</span>
    </div>
    <div v-if="success" class="notice notice-success" role="status">
      <span aria-hidden="true">✓</span><span>{{ success }}</span>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span><span>{{ error }}</span
      ><button class="text-button" type="button" :disabled="offline" @click="loadTrips()">
        重试
      </button>
    </div>

    <div v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在读取行程</strong
      ><span>只读取当前租户已确认且未删除的 Fact。</span>
    </div>
    <div v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph" aria-hidden="true">锁</span><strong>没有查看行程的权限</strong
      ><span>Reviewer 可审核 Trip Claim，但不能读取正式 Fact 列表。</span>
    </div>
    <div v-else-if="trips.length === 0" class="panel state-layout">
      <span class="state-glyph" aria-hidden="true">行</span><strong>还没有正式行程</strong
      ><span>在审核工作台确认一条 Trip Claim 后会显示在这里。</span>
    </div>

    <div v-else class="trip-layout">
      <aside class="panel trip-list-panel" aria-labelledby="trip-list-title">
        <div class="panel-heading">
          <div>
            <h2 id="trip-list-title">行程列表</h2>
            <p>{{ trips.length }} 条未删除记录</p>
          </div>
        </div>
        <ul class="trip-list">
          <li v-for="trip in trips" :key="trip.id">
            <button
              type="button"
              :aria-current="trip.id === selectedTripID ? 'true' : undefined"
              @click="selectTrip(trip.id)"
            >
              <strong>{{ trip.destination }}</strong>
              <span>{{ trip.origin ? `${trip.origin} → ` : '' }}{{ trip.destination }}</span>
              <time>{{ trip.start_date }} 至 {{ trip.end_date }}</time>
              <small
                >支付 {{ trip.assigned_payment_count }} · 发票
                {{ trip.assigned_invoice_count }}</small
              >
            </button>
          </li>
        </ul>
      </aside>

      <section class="panel trip-workspace" aria-labelledby="trip-workspace-title">
        <div class="panel-heading trip-workspace-heading">
          <div>
            <h2 id="trip-workspace-title">{{ selectedTrip?.destination }}</h2>
            <p>
              {{ selectedTrip?.start_date }} 至 {{ selectedTrip?.end_date }} · trip-attribution/1
            </p>
          </div>
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

        <div v-if="candidatesLoading" class="state-layout" role="status">
          <span class="spinner" aria-hidden="true"></span><strong>正在计算归属候选</strong>
        </div>
        <div v-else-if="candidates.length === 0" class="state-layout">
          <span class="state-glyph" aria-hidden="true">∅</span><strong>当前筛选没有 Fact</strong
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
                  <small>{{ candidate.business_date }} · {{ candidate.fact_id }}</small>
                </div>
                <strong class="numeric">{{
                  formatMinorUnits(candidate.amount_minor, candidate.currency)
                }}</strong>
              </header>
              <p class="trip-current-assignment">
                当前行程：{{ candidate.current_trip_destination || '未归属' }}
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
  </div>
</template>
