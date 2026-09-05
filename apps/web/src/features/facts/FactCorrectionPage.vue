<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { sessionStore } from '../../app/session'
import {
  ApiError,
  api,
  type CorrectionWorkspace,
  type CorrectionRequest,
  type CorrectionPreview,
  type CorrectionFactType,
  type CorrectionHistoryPage,
  type Review,
} from '../../data/client'
import {
  buildFieldPayload,
  editableFields,
  fieldLabel,
  newInvoiceItem,
  parseItemPath,
  refreshDraftFields,
  type EditableField,
} from '../review/model'
import { formatMinorUnits } from './money'

const route = useRoute()
const kind = computed(() =>
  ['payment', 'invoice', 'trip'].includes(String(route.params.factType))
    ? (String(route.params.factType) as CorrectionFactType)
    : undefined,
)
const factID = computed(() => String(route.params.factId))
const title = computed(
  () => ({ payment: '支付', invoice: '发票', trip: '行程凭证' })[kind.value ?? 'payment'],
)
const back = computed(() =>
  kind.value === 'trip' ? '/trips' : kind.value === 'invoice' ? '/invoices' : '/payments',
)
const allowed = computed(() =>
  ['facts.read', 'claims.review'].every((capability) =>
    sessionStore.current.value?.capabilities.includes(capability),
  ),
)
const workspace = ref<CorrectionWorkspace>()
const fields = ref<EditableField[]>([])
const reason = ref('')
const withdrawals = ref<string[]>([])
const preview = ref<CorrectionPreview>()
const previewBody = ref<CorrectionRequest>()
const duplicateKeys = ref<string[]>([])
const errors = ref<Record<string, string>>({})
const error = ref('')
const notice = ref('')
const loading = ref(false)
const busy = ref(false)
const needsRefresh = ref(false)
const mustRecheck = ref(false)
const rechecked = ref(false)
const page = ref(1)
const history = ref<CorrectionHistoryPage['items']>([])
const historyCursor = ref<number | null>(null)
const historyBusy = ref(false)
const historyError = ref('')
const historical = ref<Review>()
const historicalBusy = ref(false)
let epoch = 0
let attempt: { body: string; key: string } | undefined
let historyEpoch = 0
const evidence = computed(
  () =>
    workspace.value?.review.fields.flatMap((field) =>
      field.evidence.map((item) => ({ ...item, path: field.path })),
    ) ?? [],
)
const itemKeys = computed(() => [
  ...new Set(
    fields.value.flatMap((field) => {
      const item = parseItemPath(field.path)
      return item ? [item.itemKey] : []
    }),
  ),
])
const missingWithdrawals = computed(() =>
  withdrawals.value.filter((id) => !workspace.value?.state.links.some((link) => link.id === id)),
)
const draft = computed(() => JSON.stringify([fields.value, reason.value, withdrawals.value]))
const removedFields = computed(
  () =>
    workspace.value?.review.fields.filter(
      (field) =>
        field.path !== 'document_type' &&
        field.presence === 'present' &&
        !fields.value.some((draftField) => draftField.path === field.path),
    ) ?? [],
)
const canPreview = computed(() =>
  Boolean(
    workspace.value &&
    reason.value.trim() &&
    !busy.value &&
    !loading.value &&
    !needsRefresh.value &&
    !missingWithdrawals.value.length &&
    (!mustRecheck.value || rechecked.value),
  ),
)
const canConfirm = computed(
  () =>
    canPreview.value &&
    preview.value?.can_confirm &&
    preview.value.duplicates.every((item) => duplicateKeys.value.includes(item.key)),
)
const pageURL = computed(() =>
  workspace.value
    ? `/api/v1/documents/${encodeURIComponent(workspace.value.state.document_id)}/pages/${page.value}/content`
    : '',
)

watch(
  draft,
  () => {
    preview.value = undefined
    previewBody.value = undefined
    duplicateKeys.value = []
  },
  { flush: 'sync' },
)

function message(caught: unknown, fallback: string) {
  return caught instanceof ApiError ? caught.message : fallback
}
function display(value: unknown) {
  return value === undefined || value === null
    ? '缺失'
    : typeof value === 'object'
      ? JSON.stringify(value)
      : String(value)
}

async function load(preserve = false) {
  if (!allowed.value || !kind.value) return
  const ticket = ++epoch
  historyEpoch++
  historicalBusy.value = false
  historical.value = undefined
  loading.value = true
  error.value = ''
  preview.value = undefined
  previewBody.value = undefined
  try {
    const latest = await api.correction(kind.value, factID.value)
    if (ticket !== epoch) return
    fields.value =
      preserve && workspace.value
        ? refreshDraftFields(workspace.value.review, latest.review, fields.value)
        : editableFields(latest.review, latest.state.fact_type)
    workspace.value = latest
    needsRefresh.value = false
    mustRecheck.value = preserve
    rechecked.value = false
    if (!preserve) {
      reason.value = ''
      withdrawals.value = []
      page.value = 1
    }
    void loadHistory()
  } catch (caught) {
    if (ticket === epoch) error.value = message(caught, '纠错工作区加载失败，草稿仍保留')
  } finally {
    if (ticket === epoch) loading.value = false
  }
}

async function loadHistory(append = false) {
  if (!kind.value) return
  const ticket = epoch
  historyBusy.value = true
  historyError.value = ''
  try {
    const result = await api.correctionHistory(
      kind.value,
      factID.value,
      append ? (historyCursor.value ?? 0) : 0,
    )
    if (ticket !== epoch) return
    history.value = append ? [...history.value, ...result.items] : result.items
    historyCursor.value = result.next_before_revision
  } catch (caught) {
    if (ticket === epoch) historyError.value = message(caught, '历史加载失败，请重试')
  } finally {
    if (ticket === epoch) historyBusy.value = false
  }
}

async function showHistory(id: string) {
  const ticket = ++historyEpoch
  const routeTicket = epoch
  historicalBusy.value = true
  historical.value = undefined
  historyError.value = ''
  try {
    const result = await api.claimSet(id)
    if (ticket === historyEpoch && routeTicket === epoch) historical.value = result
  } catch (caught) {
    if (ticket === historyEpoch && routeTicket === epoch)
      historyError.value = message(caught, '历史字段读取失败')
  } finally {
    if (ticket === historyEpoch && routeTicket === epoch) historicalBusy.value = false
  }
}

async function requestPreview() {
  if (!canPreview.value || !workspace.value || !kind.value) return
  const encoded = buildFieldPayload(workspace.value.review, fields.value, true)
  errors.value = encoded.errors
  if (!encoded.fields) {
    error.value = '请修正标出的字段后重新预览'
    return
  }
  const body: CorrectionRequest = {
    expected_version: workspace.value.state.version,
    current_review_decision_id: workspace.value.state.current_review_decision_id,
    fields: encoded.fields,
    reason: reason.value,
    withdraw_link_ids: [...withdrawals.value],
  }
  const fingerprint = draft.value
  const ticket = epoch
  busy.value = true
  error.value = ''
  notice.value = ''
  preview.value = undefined
  try {
    const result = await api.previewCorrection(kind.value, factID.value, body)
    if (ticket === epoch && fingerprint === draft.value) {
      preview.value = result
      previewBody.value = body
      duplicateKeys.value = []
    }
  } catch (caught) {
    if (ticket === epoch) {
      error.value = message(caught, '预览失败，草稿已保留')
      if (caught instanceof ApiError && caught.status === 409) needsRefresh.value = true
    }
  } finally {
    if (ticket === epoch) busy.value = false
  }
}

async function confirm() {
  if (!canConfirm.value || !preview.value || !previewBody.value || !kind.value) return
  const body = {
    ...previewBody.value,
    preview_hash: preview.value.preview_hash,
    acknowledged_duplicate_keys: [...duplicateKeys.value].sort(),
  }
  const encoded = JSON.stringify(body)
  if (attempt?.body !== encoded) attempt = { body: encoded, key: crypto.randomUUID() }
  const ticket = epoch
  busy.value = true
  error.value = ''
  try {
    const result = await api.confirmCorrection(kind.value, factID.value, body, attempt.key)
    if (ticket !== epoch) return
    attempt = undefined
    notice.value = `纠错已确认，当前聚合版本 ${result.version}；既有报销快照保持不变。`
    busy.value = false
    await load()
  } catch (caught) {
    if (ticket === epoch) {
      error.value = message(caught, '提交结果未确认，草稿已保留；重试沿用同一请求')
      if (caught instanceof ApiError && caught.status === 409) {
        needsRefresh.value = true
        preview.value = undefined
      }
    }
  } finally {
    if (ticket === epoch) busy.value = false
  }
}

function addItem() {
  fields.value.push(...newInvoiceItem(crypto.randomUUID(), itemKeys.value.length))
}
function removeItem(key: string) {
  fields.value = fields.value.filter((field) => parseItemPath(field.path)?.itemKey !== key)
  itemKeys.value.forEach((item, index) => {
    const field = fields.value.find((field) => field.path === `items[${item}].sort_order`)
    if (field) field.textValue = String(index)
  })
}

watch(
  () => route.fullPath,
  () => {
    epoch++
    historyEpoch++
    workspace.value = undefined
    fields.value = []
    reason.value = ''
    withdrawals.value = []
    preview.value = undefined
    previewBody.value = undefined
    history.value = []
    historyCursor.value = null
    historyBusy.value = false
    historicalBusy.value = false
    historical.value = undefined
    notice.value = ''
    errors.value = {}
    error.value = ''
    historyError.value = ''
    attempt = undefined
    busy.value = false
    needsRefresh.value = false
    mustRecheck.value = false
    rechecked.value = false
    void load()
  },
  { immediate: true },
)
</script>

<template>
  <div class="page-stack correction-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink :to="back">{{ title }}管理</RouterLink><span>/</span><strong>纠正字段</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>纠正{{ title }}字段</h1>
        <p>同一记录保留每次确认与原件来源；既有报销快照不会被改写。</p>
      </div>
      <RouterLink class="button" :to="back">返回列表</RouterLink>
    </header>
    <p v-if="!allowed" class="notice">只有具备账单读取与审核权限的 Owner / Finance 可以纠错。</p>
    <p v-else-if="!kind" class="notice">单据类型无效。</p>
    <template v-else>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <div v-if="error" class="notice" role="alert">
        <p>{{ error }}</p>
        <button class="button" :disabled="loading || busy" @click="load(Boolean(workspace))">
          刷新并保留草稿
        </button>
      </div>
      <p v-if="loading" role="status">正在加载当前字段与关联…</p>
      <template v-if="workspace">
        <p class="quiet-block">
          预览不会保存草稿，离开或浏览器刷新会丢失输入。原始来源：{{
            workspace.review.entry_mode === 'manual' ? '人工录入' : 'AI 候选经人工确认'
          }}；当前字段修订 {{ workspace.review.revision }}。
        </p>
        <section v-if="mustRecheck" class="panel correction-section">
          <h2>已保留草稿，请核对新版本</h2>
          <p>下方“当前值”、证据和活动分配已更新，未保存输入保持原样。</p>
          <label class="check-label"
            ><input v-model="rechecked" type="checkbox" />我已核对最新字段与关联</label
          >
        </section>
        <div class="correction-grid">
          <section class="panel correction-section source-panel">
            <div class="panel-heading">
              <h2>原件与证据</h2>
              <a
                class="text-button"
                :href="api.documentContentURL(workspace.state.document_id)"
                target="_blank"
                rel="noopener"
                >打开原件</a
              >
            </div>
            <label
              >查看页码
              <select v-model.number="page" class="select">
                <option v-for="number in workspace.review.page_count" :key="number" :value="number">
                  第 {{ number }} 页
                </option>
              </select></label
            ><img :src="pageURL" :alt="`${title}原件第 ${page} 页`" />
          </section>
          <section class="panel correction-section">
            <h2>完整字段</h2>
            <p class="quiet">
              金额填写整数最小单位（CNY 为分）。修改字段后选择已有证据，或标注原件页码和实际摘录。
            </p>
            <fieldset :disabled="busy || loading" class="correction-fields">
              <article v-for="field in fields" :key="field.path" class="correction-field">
                <div class="field-heading">
                  <strong>{{ fieldLabel(field.path) }}</strong
                  ><select
                    v-model="field.presence"
                    class="select select-small"
                    :aria-label="`${fieldLabel(field.path)} 是否存在`"
                  >
                    <option value="present">存在</option>
                    <option value="absent">缺失</option>
                  </select>
                </div>
                <p class="quiet current-value">当前值：{{ display(field.originalValue) }}</p>
                <template v-if="field.presence === 'present'">
                  <textarea
                    v-if="field.valueType === 'supplementary'"
                    v-model="field.textValue"
                    class="textarea"
                    :aria-label="fieldLabel(field.path)"
                    :aria-invalid="Boolean(errors[field.path])"
                  ></textarea>
                  <input
                    v-else
                    v-model="field.textValue"
                    class="input"
                    :aria-label="fieldLabel(field.path)"
                    :inputmode="
                      ['money_minor', 'integer'].includes(field.valueType) ? 'numeric' : 'text'
                    "
                    :aria-invalid="Boolean(errors[field.path])"
                  />
                  <details>
                    <summary>来源证据 · 已选 {{ field.evidenceIds.length }} 条</summary>
                    <button
                      v-if="field.evidenceIds.length"
                      type="button"
                      class="text-button"
                      @click="field.evidenceIds = []"
                    >
                      清空证据选择</button
                    ><label v-for="item in evidence" :key="item.id" class="evidence-choice"
                      ><input v-model="field.evidenceIds" type="checkbox" :value="item.id" />第
                      {{ item.page }} 页 · {{ fieldLabel(item.path) }} ·
                      {{ item.quote || '原件区域' }}</label
                    >
                    <label
                      >来源页码<input
                        v-model.number="field.manualPage"
                        class="input"
                        type="number"
                        min="1"
                        :max="workspace.review.page_count"
                        :aria-label="`${fieldLabel(field.path)} 来源页码`" /></label
                    ><label
                      >实际摘录<textarea
                        v-model="field.manualQuote"
                        class="textarea"
                        rows="2"
                        maxlength="500"
                        :aria-label="`${fieldLabel(field.path)} 原件摘录`"
                      ></textarea>
                    </label>
                  </details>
                </template>
                <p v-if="errors[field.path]" class="field-error" role="alert">
                  {{ errors[field.path] }}
                </p>
              </article>
              <div v-if="kind === 'invoice'" class="item-actions">
                <button class="button" type="button" @click="addItem">新增发票明细</button
                ><button
                  v-for="(key, index) in itemKeys"
                  :key="key"
                  type="button"
                  class="text-button"
                  @click="removeItem(key)"
                >
                  移除明细 {{ index + 1 }}
                </button>
              </div>
            </fieldset>
          </section>
        </div>
        <section class="panel correction-section">
          <h2>活动金额分配</h2>
          <p>
            默认保留全部分配；只有明确勾选的链接会随本次纠错撤销。新增或更改分配金额请先到<RouterLink
              v-if="kind !== 'trip'"
              :to="`/allocations/${kind}/${encodeURIComponent(factID)}`"
              >分配工作区</RouterLink
            ><span v-else>支付或发票的分配工作区</span>操作。
          </p>
          <p v-if="!workspace.state.links.length" class="quiet">没有活动分配。</p>
          <label v-for="link in workspace.state.links" :key="link.id" class="evidence-choice"
            ><input
              v-model="withdrawals"
              type="checkbox"
              :value="link.id"
              :disabled="busy || loading"
            />撤销 {{ formatMinorUnits(link.allocated_minor, link.currency) }} · 对端日期
            {{ link.target_business_date }} · {{ link.target_id }}</label
          >
          <p v-if="missingWithdrawals.length" role="alert">
            此前选择的 {{ missingWithdrawals.length }} 条分配已不再活动。<button
              class="text-button"
              @click="withdrawals = withdrawals.filter((id) => !missingWithdrawals.includes(id))"
            >
              确认移除失效撤销选择
            </button>
          </p>
        </section>
        <section class="panel correction-section">
          <label class="page-stack"
            ><strong>纠错理由</strong
            ><textarea
              v-model="reason"
              class="textarea"
              maxlength="500"
              rows="3"
              :disabled="busy || loading"
              aria-label="纠错理由"
            ></textarea></label
          ><button class="button button-primary" :disabled="!canPreview" @click="requestPreview">
            {{ busy ? '处理中…' : '预览纠错' }}
          </button>
        </section>
        <section
          v-if="preview"
          class="panel correction-section"
          aria-labelledby="correction-preview"
        >
          <h2 id="correction-preview">确认前预览</h2>
          <p>明确撤销 {{ preview.withdraw_link_ids.length }} 条分配，其余保持。</p>
          <p v-if="preview.state.attribution.mode === 'auto'">
            自动归属：{{ preview.state.attribution.current_trip_id || '未归属' }} →
            {{ preview.state.attribution.desired_trip_id || '未归属' }}（匹配
            {{ preview.state.attribution.matching_trip_count }} 个行程）
          </p>
          <p v-else>人工归属偏好及行程材料关联保持不变。</p>
          <ul v-if="preview.issues.length" role="alert">
            <li v-for="(issue, index) in preview.issues" :key="index">{{ issue.message }}</li>
          </ul>
          <details>
            <summary>核对新旧字段</summary>
            <dl class="change-values">
              <template v-for="field in fields" :key="field.path"
                ><dt>{{ fieldLabel(field.path) }}</dt>
                <dd>
                  {{ display(field.originalValue) }} →
                  {{ field.presence === 'present' ? field.textValue : '缺失' }}
                </dd></template
              >
              <template v-for="field in removedFields" :key="`removed-${field.path}`"
                ><dt>{{ fieldLabel(field.path) }}</dt>
                <dd>{{ display(field.value) }} → 移除明细字段</dd></template
              >
            </dl>
          </details>
          <label
            v-for="duplicate in preview.duplicates"
            :key="duplicate.key"
            class="evidence-choice"
            ><input
              v-model="duplicateKeys"
              type="checkbox"
              :value="duplicate.key"
              :disabled="busy"
            />已核对疑似重复（{{
              duplicate.kind === 'field_combination'
                ? '字段组合'
                : duplicate.kind === 'near_file'
                  ? '近似文件'
                  : '跨页'
            }}）{{
              duplicate.existing_payment_id ||
              duplicate.existing_invoice_id ||
              duplicate.existing_document_id
            }}，保留为不同记录</label
          >
          <button class="button button-primary" :disabled="!canConfirm" @click="confirm">
            确认纠错
          </button>
        </section>
        <section class="panel correction-section">
          <h2>确认历史</h2>
          <p v-if="historyError" role="alert">
            {{ historyError }} <button class="text-button" @click="loadHistory()">重试历史</button>
          </p>
          <ol class="correction-history">
            <li v-for="item in history" :key="item.review_decision_id">
              <button class="text-button" @click="showHistory(item.claim_set_id)">
                查看字段修订 {{ item.revision }}</button
              ><span>{{ item.reason || '首次审核确认' }} · {{ item.created_at }}</span>
            </li>
          </ol>
          <button
            v-if="historyCursor"
            class="button"
            :disabled="historyBusy"
            @click="loadHistory(true)"
          >
            更早的确认记录
          </button>
          <p v-if="historicalBusy" role="status">正在读取历史字段…</p>
          <div v-if="historical">
            <h3>字段修订 {{ historical.revision }} · 只读</h3>
            <dl class="change-values">
              <template v-for="field in historical.fields" :key="field.id"
                ><dt>{{ fieldLabel(field.path) }} · {{ field.source === 'ai' ? 'AI' : '人工' }}</dt>
                <dd>
                  {{ display(field.value)
                  }}<small v-for="item in field.evidence" :key="item.id"
                    >第 {{ item.page }} 页：{{ item.quote || '原件区域' }}</small
                  >
                </dd></template
              >
            </dl>
          </div>
        </section>
      </template>
    </template>
  </div>
</template>

<style scoped>
.correction-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 24px;
  align-items: start;
}
.correction-section {
  padding: 24px;
  display: grid;
  gap: 16px;
  min-width: 0;
}
.source-panel {
  position: sticky;
  top: 92px;
}
.source-panel img {
  width: 100%;
  max-height: 70vh;
  object-fit: contain;
  background: var(--bg-muted);
}
.correction-fields {
  border: 0;
  padding: 0;
  min-width: 0;
  display: grid;
  gap: 18px;
}
.correction-field {
  display: grid;
  gap: 10px;
  padding-block: 12px;
  border-bottom: 1px solid var(--border);
}
.field-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.evidence-choice {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding-block: 8px;
  overflow-wrap: anywhere;
}
.evidence-choice input {
  flex-shrink: 0;
  margin-top: 4px;
}
details {
  display: grid;
  gap: 8px;
  min-width: 0;
}
details label {
  display: block;
  margin-block: 10px;
}
details label.evidence-choice {
  display: flex;
}
summary {
  cursor: pointer;
  color: var(--text-secondary);
}
.current-value,
.change-values {
  overflow-wrap: anywhere;
}
.change-values {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 12px;
}
.change-values dd {
  margin: 0;
}
.change-values small {
  display: block;
  color: var(--text-secondary);
}
.correction-history {
  display: grid;
  gap: 14px;
  padding-left: 24px;
}
.correction-history span {
  display: block;
  overflow-wrap: anywhere;
}
.item-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
@media (max-width: 1000px) {
  .correction-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .source-panel {
    position: static;
  }
}
@media (max-width: 600px) {
  .correction-section {
    padding: 16px;
  }
  .change-values {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
