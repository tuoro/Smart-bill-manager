<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, RouterLink, useRoute } from 'vue-router'
import {
  ApiError,
  api,
  type AllocationAdjustmentRequest,
  type AllocationFactType,
  type AllocationWorkspace,
} from '../../data/client'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import AllocationSuggestion from './AllocationSuggestion.vue'
import { buildAllocationSuggestion } from './suggestion'
import { formatMinorUnits } from '../facts/money'
import {
  allocationModeLabel,
  allocationDraftChanged,
  createAllocationDraft,
  validateAllocationDraft,
  type AllocationDraftRow,
} from './model'
import { minorToDecimalInput } from '../facts/money'

const route = useRoute()
const factType = computed(() => route.params.factType as AllocationFactType)
const factId = computed(() => String(route.params.factId ?? ''))
const workspace = ref<AllocationWorkspace | null>(null)
const rows = ref<AllocationDraftRow[]>([])
const reason = ref('')
const withdrawAllConfirmed = ref(false)
const loading = ref(true)
const submitting = ref(false)
const forbidden = ref(false)
const error = ref('')
const conflict = ref('')
const success = ref('')
const attempted = ref(false)
const query = ref('')
const view = ref('recommended')
const appliedSearch = ref({ q: '', view: 'recommended' })
const nextCursor = ref('')
const searching = ref(false)
const scopeTouched = ref(false)
const suggestionDismissed = ref(false)
const suggestionAdopted = ref(false)
const unknownResult = ref(false)
const editorHeading = ref<HTMLElement | null>(null)
let generation = 0
let submissionAttempt: { request: AllocationAdjustmentRequest; key: string } | null = null

const draftChanged = computed(() =>
  Boolean(workspace.value && allocationDraftChanged(workspace.value, rows.value)),
)
const dirty = computed(
  () => draftChanged.value || Boolean(reason.value) || withdrawAllConfirmed.value,
)
const locked = computed(
  () => loading.value || submitting.value || searching.value || unknownResult.value,
)
const suggestion = computed(() =>
  workspace.value
    ? buildAllocationSuggestion(workspace.value, !scopeTouched.value && !conflict.value)
    : null,
)

function captureScope() {
  const currentGeneration = generation
  const type = factType.value
  const id = factId.value
  const session = sessionStore.current.value
  return {
    type,
    id,
    current: () =>
      currentGeneration === generation &&
      type === factType.value &&
      id === factId.value &&
      session?.user.id === sessionStore.current.value?.user.id &&
      session?.tenant.id === sessionStore.current.value?.tenant.id,
  }
}

const validation = computed(() =>
  workspace.value
    ? validateAllocationDraft(workspace.value, rows.value, reason.value, withdrawAllConfirmed.value)
    : undefined,
)
const selectedCount = computed(() => rows.value.filter((row) => row.selected).length)
const modeLabel = computed(() =>
  workspace.value ? allocationModeLabel(workspace.value, rows.value) : '没有变化',
)
const canSubmit = computed(
  () =>
    Boolean(
      workspace.value && (unknownResult.value || draftChanged.value || validation.value?.changed),
    ) &&
    !loading.value &&
    !submitting.value &&
    !searching.value &&
    !conflict.value,
)
const returnPath = computed(() => (factType.value === 'invoice' ? '/invoices' : '/payments'))
const returnLabel = computed(() => (factType.value === 'invoice' ? '返回发票列表' : '返回支付列表'))

async function load() {
  generation++
  const scope = captureScope()
  workspace.value = null
  rows.value = []
  searching.value = false
  forbidden.value = false
  error.value = ''
  success.value = ''
  conflict.value = ''
  reason.value = ''
  withdrawAllConfirmed.value = false
  attempted.value = false
  scopeTouched.value = false
  suggestionDismissed.value = false
  suggestionAdopted.value = false
  unknownResult.value = false
  submissionAttempt = null
  if (!sessionStore.current.value) {
    loading.value = false
    return
  }
  if (!['payment', 'invoice'].includes(factType.value) || !factId.value) {
    loading.value = false
    error.value = '分配页面地址不合法'
    return
  }
  loading.value = true
  try {
    const latest = await api.allocationWorkspace(scope.type, scope.id)
    if (!scope.current()) return
    workspace.value = latest
    rows.value = createAllocationDraft(latest)
    query.value = ''
    view.value = 'recommended'
    appliedSearch.value = { q: '', view: 'recommended' }
    nextCursor.value = latest.next_cursor ?? ''
    reason.value = ''
    withdrawAllConfirmed.value = false
    forbidden.value = false
    error.value = ''
    conflict.value = ''
    attempted.value = false
  } catch (caught) {
    if (!scope.current()) return
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = caught instanceof ApiError ? caught.message : '分配工作区加载失败'
  } finally {
    if (scope.current()) loading.value = false
  }
}

function refresh() {
  if (locked.value) return
  if (dirty.value && !window.confirm('刷新会丢弃当前未保存的目标、金额和理由，是否继续？')) return
  void load()
}

async function adoptSuggestion() {
  if (
    !suggestion.value?.items.length ||
    locked.value ||
    conflict.value ||
    draftChanged.value ||
    suggestionAdopted.value
  )
    return
  const scope = captureScope()
  const items = new Map(suggestion.value.items.map((item) => [item.target.id, item]))
  rows.value = rows.value.map((row) => {
    const item = items.get(row.target.id)
    return item
      ? {
          ...row,
          selected: true,
          // 与输入框同一口径：按币种精度写入十进制，而不是最小单位整数。
          amountText:
            item.amountMinor === null
              ? ''
              : minorToDecimalInput(item.amountMinor, row.target.currency),
        }
      : row
  })
  suggestionAdopted.value = true
  attempted.value = false
  await nextTick()
  if (!scope.current()) return
  const blank = rows.value.find((row) => row.selected && !row.amountText)
  if (blank) document.getElementById(`allocation-amount-${blank.target.id}`)?.focus()
  else editorHeading.value?.focus()
}

function toggleRow(row: AllocationDraftRow) {
  if (selectedCount.value > 200) {
    row.selected = false
    error.value = '一个分配计划最多选择 200 个目标'
    return
  }
  if (row.selected && !row.amountText) {
    row.amountText = row.target.current_link_id
      ? minorToDecimalInput(row.target.current_allocated_minor, row.target.currency)
      : ''
  }
  if (selectedCount.value > 0) withdrawAllConfirmed.value = false
  attempted.value = false
  success.value = ''
}

async function searchTargets(next = false) {
  if (locked.value || !workspace.value) return
  const scope = captureScope()
  scopeTouched.value = true
  suggestionDismissed.value = false
  suggestionAdopted.value = false
  const filter = next ? appliedSearch.value : { q: query.value, view: view.value }
  searching.value = true
  try {
    const page = await api.allocationTargets(
      scope.type,
      scope.id,
      filter.q,
      filter.view,
      next ? nextCursor.value : '',
    )
    if (!scope.current()) return
    const retained = rows.value.filter(
      (row) => row.selected || row.amountText || row.target.current_link_id,
    )
    const ids = new Set(retained.map((row) => row.target.id))
    rows.value = [
      ...retained,
      ...page.items
        .filter((item) => !ids.has(item.id))
        .map((target) => ({ target, selected: false, amountText: '' })),
    ]
    appliedSearch.value = filter
    nextCursor.value = page.next_cursor ?? ''
    error.value = ''
  } catch (caught) {
    if (scope.current())
      error.value =
        caught instanceof ApiError ? caught.message : '查询失败，当前草稿和上一页结果已保留'
  } finally {
    if (scope.current()) searching.value = false
  }
}

async function submit() {
  if (
    !workspace.value ||
    !validation.value ||
    submitting.value ||
    searching.value ||
    conflict.value
  )
    return
  const scope = captureScope()
  attempted.value = true
  conflict.value = ''
  error.value = ''
  success.value = ''
  if (!unknownResult.value) {
    if (!validation.value.request) return
    submissionAttempt = {
      request: validation.value.request,
      key: `allocation-${crypto.randomUUID()}`,
    }
  }
  if (!submissionAttempt) return
  const attempt = submissionAttempt
  submitting.value = true
  try {
    const result = await api.adjustAllocation(scope.type, scope.id, attempt.request, attempt.key)
    if (!scope.current()) return
    const saved = `${result.mode === 'supplement' ? '补充' : result.mode === 'withdraw' ? '撤销' : '替换'}分配已保存`
    const reload = load()
    const reloadScope = captureScope()
    success.value = `${saved}，正在刷新余额`
    await reload
    if (reloadScope.current()) {
      success.value = workspace.value
        ? `${saved}，余额已刷新`
        : `${saved}，但工作区刷新失败。可重试读取，无需再次提交。`
      submitting.value = false
    }
  } catch (caught) {
    if (!scope.current()) return
    if (caught instanceof ApiError && caught.status === 409) {
      unknownResult.value = false
      submissionAttempt = null
      scopeTouched.value = true
      suggestionAdopted.value = false
      suggestionDismissed.value = false
      conflict.value = `${caught.message}。当前草稿已保留，请刷新后重新确认。`
    } else {
      unknownResult.value = !(caught instanceof ApiError) || caught.status >= 500
      if (!unknownResult.value) submissionAttempt = null
      error.value = caught instanceof ApiError ? caught.message : '分配调整提交失败'
    }
  } finally {
    if (scope.current()) submitting.value = false
  }
}

function mayLeave() {
  if (!sessionStore.current.value) return true
  if (submitting.value) return false
  if (unknownResult.value)
    return window.confirm('上次分配结果尚未确认，离开后请核对当前分配，勿重复提交。确定离开？')
  return !dirty.value || window.confirm('当前分配有未保存修改，确定离开并丢弃？')
}

function warnBeforeUnload(event: BeforeUnloadEvent) {
  if (!sessionStore.current.value || (!dirty.value && !submitting.value && !unknownResult.value))
    return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteLeave(mayLeave)
onBeforeRouteUpdate(mayLeave)
onMounted(() => window.addEventListener('beforeunload', warnBeforeUnload))

watch(
  () => [
    factType.value,
    factId.value,
    sessionStore.current.value?.user.id,
    sessionStore.current.value?.tenant.id,
  ],
  () => {
    submitting.value = false
    void load()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  generation++
  window.removeEventListener('beforeunload', warnBeforeUnload)
})
</script>

<template>
  <div class="page-stack allocation-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink :to="returnPath">财务数据</RouterLink><span aria-hidden="true">/</span
      ><strong>调整分配</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>调整金额分配</h1>
        <p>选择关联单据并填写分配金额。更改会保留历史记录，不会覆盖原有分配。</p>
      </div>
      <RouterLink class="button button-small" :to="returnPath">{{ returnLabel }}</RouterLink>
    </header>

    <div v-if="success" class="notice notice-success" role="status" aria-live="polite">
      <AppIcon name="check" /><strong>{{ success }}</strong>
    </div>
    <section v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span
      ><strong>正在加载当前分配</strong>
    </section>
    <section v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph"><AppIcon name="lock" /></span><strong>没有调整分配的权限</strong
      ><span>只有管理员或财务人员可以更改已确认单据的分配关系。</span>
    </section>
    <section v-else-if="!workspace" class="panel state-layout" role="alert">
      <span class="state-glyph"><AppIcon name="alert" /></span><strong>分配工作区不可用</strong
      ><span>{{ error }}</span
      ><button class="button" type="button" @click="refresh">重试</button>
    </section>

    <template v-else>
      <div v-if="conflict" class="notice notice-warning" role="alert">
        <AppIcon name="alert" /><span>{{ conflict }}</span
        ><button class="text-button" type="button" :disabled="locked" @click="refresh">
          刷新当前分配
        </button>
      </div>
      <div v-if="error" class="notice notice-danger" role="alert">
        <AppIcon name="alert" /><span>{{ error }}</span>
      </div>
      <p v-if="unknownResult" class="notice notice-warning" role="alert">
        上次保存结果未知，编辑和刷新已暂停。请点击“重试原分配”，仅重发原请求与原请求键，不会生成新计划。
      </p>

      <section class="panel allocation-anchor" aria-labelledby="allocation-anchor-title">
        <div class="panel-heading">
          <div>
            <h2 id="allocation-anchor-title">
              当前 {{ workspace.anchor.fact_type === 'payment' ? '支付' : '发票' }}
            </h2>
            <p>{{ workspace.anchor.display_name }} · {{ workspace.anchor.business_date }}</p>
          </div>
          <button class="button button-small" type="button" :disabled="locked" @click="refresh">
            刷新
          </button>
        </div>
        <dl class="allocation-summary">
          <div>
            <dt>总额</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.amount_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>当前已分配</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.allocated_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>当前剩余</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.remaining_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>有效分配</dt>
            <dd>{{ workspace.links.length }} 条</dd>
          </div>
        </dl>
      </section>

      <AllocationSuggestion
        v-if="suggestion && !suggestionDismissed"
        :suggestion="suggestion"
        :workspace="workspace"
        :disabled="locked || Boolean(conflict)"
        :adopted="suggestionAdopted"
        :edited="draftChanged"
        @adopt="adoptSuggestion"
        @decline="suggestionDismissed = true"
      />
      <p v-else-if="suggestionDismissed" class="notice notice-info" role="status">
        本次未采用草案，可在下方手工分配。
      </p>

      <form
        class="panel allocation-form"
        aria-labelledby="allocation-targets-title"
        @submit.prevent="submit"
      >
        <div class="panel-heading">
          <div>
            <h2 id="allocation-targets-title" ref="editorHeading" tabindex="-1">选择分配单据</h2>
            <p>已选择 {{ selectedCount }} / {{ rows.length }} 个目标 · {{ modeLabel }}</p>
          </div>
        </div>

        <div class="allocation-search">
          <label class="field-stack"
            ><span>查找分配单据</span
            ><input
              v-model="query"
              :disabled="locked"
              class="input"
              maxlength="200"
              placeholder="商户、购销方、单号或 ID"
              @keydown.enter.prevent="searchTargets()"
          /></label>
          <label class="field-stack"
            ><span>日期范围</span
            ><select v-model="view" class="input" :disabled="locked">
              <option value="recommended">30 天内推荐</option>
              <option value="all_dates">全部日期（可跨期）</option>
            </select></label
          >
          <button class="button" type="button" :disabled="locked" @click="searchTargets()">
            查询单据
          </button>
          <button
            v-if="nextCursor"
            class="button"
            type="button"
            :disabled="locked"
            @click="searchTargets(true)"
          >
            下一页候选
          </button>
        </div>
        <p class="quiet allocation-search-note">
          每页最多 50 个候选；已选和当前关联始终保留。查询范围：{{
            appliedSearch.view === 'all_dates' ? '全部日期' : '30 天内推荐'
          }}。
        </p>
        <p v-if="searching" class="allocation-search-note" role="status">正在查找单据</p>
        <p
          v-if="rows.some((row) => row.selected && row.target.date_distance_days > 30)"
          class="notice notice-warning allocation-search-note"
          role="status"
        >
          已选择超过 30 天的跨期单据，请在调整理由中说明关联依据。
        </p>
        <div v-if="rows.length === 0" class="state-layout compact">
          <span class="state-glyph"><AppIcon name="receipt" /></span
          ><strong>没有可分配的单据</strong
          ><span>可切换全部日期搜索同币种单据；跨期分配须填写理由。</span>
        </div>
        <ul v-else class="allocation-target-list">
          <li v-for="row in rows" :key="row.target.id" class="allocation-target-row">
            <label class="allocation-target-choice">
              <input
                v-model="row.selected"
                type="checkbox"
                :disabled="locked"
                @change="toggleRow(row)"
              />
              <span>
                <strong>{{ row.target.display_name }}</strong>
                <small>
                  <span v-if="row.target.date_distance_days > 30" class="status-pill status-warning"
                    >跨期分配</span
                  >
                  {{ row.target.business_date }} · 相差 {{ row.target.date_distance_days }} 天 ·
                  {{ row.target.name_exact ? '名称一致' : '名称不一致，仅作提示' }}
                </small>
              </span>
            </label>
            <dl class="allocation-target-balance">
              <div>
                <dt>目标总额</dt>
                <dd>{{ formatMinorUnits(row.target.amount_minor, row.target.currency) }}</dd>
              </div>
              <div>
                <dt>已分配给其他单据</dt>
                <dd>
                  {{
                    formatMinorUnits(
                      row.target.allocated_minor - row.target.current_allocated_minor,
                      row.target.currency,
                    )
                  }}
                </dd>
              </div>
              <div>
                <dt>当前分配</dt>
                <dd>
                  {{ formatMinorUnits(row.target.current_allocated_minor, row.target.currency) }}
                </dd>
              </div>
              <div>
                <dt>可调整上限</dt>
                <dd>
                  {{ formatMinorUnits(row.target.maximum_allocatable_minor, row.target.currency) }}
                </dd>
              </div>
            </dl>
            <label class="field-stack allocation-amount">
              <span>分配金额</span>
              <input
                v-model="row.amountText"
                :id="`allocation-amount-${row.target.id}`"
                class="input numeric"
                type="text"
                inputmode="decimal"
                :disabled="locked || !row.selected"
                :aria-invalid="attempted && Boolean(validation?.targetErrors[row.target.id])"
                :aria-describedby="
                  validation?.targetErrors[row.target.id]
                    ? `allocation-error-${row.target.id}`
                    : undefined
                "
                @input="attempted = false"
              />
              <small v-if="row.target.current_link_id && !row.selected" class="danger-text"
                >提交后将撤销这条分配，历史记录保留</small
              >
              <small
                v-if="attempted && validation?.targetErrors[row.target.id]"
                :id="`allocation-error-${row.target.id}`"
                class="danger-text"
                >{{ validation.targetErrors[row.target.id] }}</small
              >
            </label>
          </li>
        </ul>

        <div class="allocation-decision">
          <div class="allocation-projection" aria-live="polite">
            <span>提交后合计</span>
            <strong>{{
              formatMinorUnits(validation?.desiredTotalMinor ?? 0, workspace.anchor.currency)
            }}</strong>
            <small
              >提交后剩余
              {{
                formatMinorUnits(
                  workspace.anchor.amount_minor - (validation?.desiredTotalMinor ?? 0),
                  workspace.anchor.currency,
                )
              }}</small
            >
          </div>
          <label class="field-stack">
            <span>调整理由</span>
            <textarea
              v-model="reason"
              :disabled="locked"
              class="textarea"
              rows="3"
              maxlength="500"
              :aria-invalid="attempted && Boolean(validation?.reasonError)"
              :aria-describedby="
                validation?.reasonError ? 'allocation-reason-error' : 'allocation-reason-note'
              "
              @input="attempted = false"
            ></textarea>
            <small id="allocation-reason-note" class="form-note"
              >必填，说明本次调整的原因；最多 500 个字符。</small
            >
            <small
              v-if="attempted && validation?.reasonError"
              id="allocation-reason-error"
              class="danger-text"
              >{{ validation.reasonError }}</small
            >
          </label>
          <label
            v-if="workspace.links.length > 0 && selectedCount === 0"
            class="allocation-withdraw-all"
          >
            <input
              v-model="withdrawAllConfirmed"
              :disabled="locked"
              type="checkbox"
              :aria-invalid="attempted && Boolean(validation?.withdrawAllError)"
              :aria-describedby="
                attempted && validation?.withdrawAllError ? 'allocation-withdraw-error' : undefined
              "
            />
            <span>确认撤销全部 {{ workspace.links.length }} 条活动分配</span>
          </label>
          <p
            v-if="attempted && validation?.withdrawAllError"
            id="allocation-withdraw-error"
            class="danger-text"
          >
            {{ validation.withdrawAllError }}
          </p>
          <p
            v-if="validation?.planError"
            class="form-note"
            :class="{ 'danger-text': attempted && validation.changed }"
          >
            {{ validation.planError }}
          </p>
          <div class="allocation-actions">
            <RouterLink class="button" :to="returnPath">取消</RouterLink>
            <button class="button button-primary" type="submit" :disabled="!canSubmit">
              {{ submitting ? '正在保存…' : unknownResult ? '重试原分配' : `确认${modeLabel}` }}
            </button>
          </div>
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.allocation-search {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 12px;
  padding: 20px;
}
.allocation-search label:first-child {
  flex: 1 1 220px;
  min-width: 0;
}
.allocation-search-note {
  margin: 0 20px 16px;
}
@media (max-width: 600px) {
  .allocation-search {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
