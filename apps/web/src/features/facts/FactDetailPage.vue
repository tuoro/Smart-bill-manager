<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { sessionStore } from '../../app/session'
import { ApiError, api, type FactDetail, type FactKind, type Review } from '../../data/client'
import { fieldLabel } from '../review/model'
import { factListPath, factReturnPath } from './list-model'
import { formatMinorUnits } from './money'
import InvoiceMaterialsPanel from './InvoiceMaterialsPanel.vue'

const props = defineProps<{ kind: FactKind }>()
const route = useRoute(),
  router = useRouter()
const detail = ref<FactDetail | null>(null),
  review = ref<Review | null>(null)
const loading = ref(false),
  error = ref(''),
  sourceError = ref(''),
  sourceBusy = ref(false)
const deleteOpen = ref(false),
  deleteConfirmed = ref(false),
  deleting = ref(false),
  deleteError = ref('')
const page = ref(1)
const badDebtOpen = ref(false),
  badDebtReason = ref(''),
  badDebtBusy = ref(false),
  badDebtError = ref(''),
  badDebtNotice = ref('')
const badDebtMarked = computed(() =>
  Boolean(detail.value?.payment?.bad_debt || detail.value?.invoice?.bad_debt),
)
const deleteButton = ref<HTMLButtonElement | null>(null)
const title = computed(() => (props.kind === 'payment' ? '支付详情' : '发票详情'))
const id = computed(() => (typeof route.params.factId === 'string' ? route.params.factId : ''))
const back = computed(() => factReturnPath(props.kind, route.query.back))
const allowed = (capability: string) =>
  sessionStore.current.value?.capabilities.includes(capability) === true
const canCorrect = computed(() => allowed('facts.read') && allowed('claims.review'))
const canManageMaterials = computed(() =>
  ['facts.read', 'review.source.read', 'documents.process'].every(allowed),
)
const targetKind = computed(() => (props.kind === 'payment' ? 'invoice' : 'payment'))
const rows = computed<[string, string][]>(() => {
  const p = detail.value?.payment,
    i = detail.value?.invoice
  if (p)
    return [
      ['商户', p.merchant],
      ['金额', formatMinorUnits(p.amount_minor, p.currency)],
      ['交易时间', p.transaction_time],
      ['业务日期', p.business_date],
      ['来源时区', p.source_timezone],
      ['支付方式', p.payment_method || '未填写'],
      ['订单号', p.order_number || '未填写'],
      ['分类', p.category || '未填写'],
      ['已分配', formatMinorUnits(p.allocated_minor, p.currency)],
      ['剩余', formatMinorUnits(p.remaining_minor, p.currency)],
      ['确认入库时间', p.created_at],
    ]
  if (i)
    return [
      ['发票号码', i.invoice_number],
      ['开票日期', i.invoice_date],
      ['销售方', i.seller_name],
      ['购买方', i.buyer_name],
      ['价税合计', formatMinorUnits(i.total_minor, i.currency)],
      ['税额', i.tax_minor === undefined ? '未填写' : formatMinorUnits(i.tax_minor, i.currency)],
      ['已分配', formatMinorUnits(i.allocated_minor, i.currency)],
      ['剩余', formatMinorUnits(i.remaining_minor, i.currency)],
      ['确认入库时间', i.created_at],
    ]
  return []
})
let epoch = 0
const badDebtTrigger = ref<HTMLButtonElement>()
const badDebtReasonField = ref<HTMLTextAreaElement>()
let badDebtAttempt = { fingerprint: '', key: '' }

async function load() {
  const current = ++epoch
  detail.value = null
  review.value = null
  error.value = ''
  sourceError.value = ''
  sourceBusy.value = false
  page.value = 1
  deleteOpen.value = false
  deleteConfirmed.value = false
  deleteError.value = ''
  deleting.value = false
  loading.value = true
  badDebtOpen.value = false
  badDebtBusy.value = false
  badDebtReason.value = ''
  badDebtError.value = ''
  badDebtNotice.value = ''
  try {
    const result = await api.factDetail(props.kind, id.value)
    if (current !== epoch) return
    const fact = props.kind === 'payment' ? result.payment : result.invoice
    if (result.fact_type !== props.kind || fact?.id !== id.value)
      throw new Error('单据详情响应不一致，请重试')
    detail.value = result
  } catch (caught) {
    if (current === epoch)
      error.value =
        caught instanceof ApiError && caught.status === 404
          ? '单据不存在、已删除或不在当前工作区'
          : caught instanceof Error
            ? caught.message
            : '详情加载失败'
  } finally {
    if (current === epoch) loading.value = false
  }
}
async function refreshMaterialContext(): Promise<number> {
  const current = ++epoch
  const result = await api.factDetail(props.kind, id.value)
  if (current !== epoch || result.fact_type !== 'invoice' || result.invoice?.id !== id.value)
    throw new Error('发票详情已变化，请重新刷新')
  detail.value = result
  review.value = null
  sourceBusy.value = false
  sourceError.value = ''
  page.value = 1
  return result.version
}
async function changeBadDebt() {
  if (!detail.value || badDebtBusy.value) return
  const reason = badDebtReason.value.trim()
  if (!reason || [...reason].length > 500) {
    badDebtError.value = '请填写 1～500 字理由'
    return
  }
  const current = epoch
  badDebtBusy.value = true
  badDebtError.value = ''
  try {
    const body = { marked: !badDebtMarked.value, expected_version: detail.value.version, reason }
    const fingerprint = JSON.stringify({ kind: props.kind, id: id.value, body })
    if (badDebtAttempt.fingerprint !== fingerprint)
      badDebtAttempt = { fingerprint, key: `bad-debt-${crypto.randomUUID()}` }
    const result = await api.setBadDebt(props.kind, id.value, body, badDebtAttempt.key)
    if (current !== epoch || !detail.value) return
    const fact = detail.value.payment ?? detail.value.invoice
    if (fact) fact.bad_debt = result.marked
    detail.value.version = result.version
    badDebtOpen.value = false
    badDebtReason.value = ''
    badDebtNotice.value = result.marked ? '已标记坏账，相关行程受到删除保护。' : '已取消坏账标记。'
    await nextTick()
    if (current === epoch) badDebtTrigger.value?.focus()
  } catch (caught) {
    if (current === epoch)
      badDebtError.value = caught instanceof Error ? caught.message : '坏账操作失败，理由已保留'
  } finally {
    if (current === epoch) badDebtBusy.value = false
  }
}
async function refreshBadDebt() {
  if (badDebtBusy.value) return
  const current = epoch
  badDebtBusy.value = true
  try {
    const result = await api.factDetail(props.kind, id.value)
    if (current !== epoch) return
    if (result.fact_type !== props.kind || (result.payment ?? result.invoice)?.id !== id.value)
      throw new Error('单据详情响应不一致，请重试')
    detail.value = result
    badDebtError.value = '已刷新，请核对当前坏账状态再提交；理由已保留。'
  } catch (caught) {
    if (current === epoch)
      badDebtError.value = caught instanceof Error ? caught.message : '刷新失败'
  } finally {
    if (current === epoch) badDebtBusy.value = false
  }
}
async function openBadDebt() {
  badDebtOpen.value = true
  await nextTick()
  badDebtReasonField.value?.focus()
}
async function cancelBadDebt() {
  badDebtOpen.value = false
  await nextTick()
  badDebtTrigger.value?.focus()
}
async function loadSource() {
  const source = detail.value?.source
  if (!source || sourceBusy.value) return
  const current = epoch
  sourceBusy.value = true
  sourceError.value = ''
  try {
    const result = await api.claimSet(source.claim_set_id)
    if (current === epoch) review.value = result
  } catch (caught) {
    if (current === epoch)
      sourceError.value = caught instanceof Error ? caught.message : '审核来源加载失败'
  } finally {
    if (current === epoch) sourceBusy.value = false
  }
}
async function remove() {
  if (!deleteConfirmed.value || deleting.value) return
  const current = epoch
  deleting.value = true
  deleteError.value = ''
  try {
    await api.deleteFact(props.kind, id.value)
    if (current === epoch) await router.replace(factListPath(props.kind))
  } catch (caught) {
    if (current === epoch) deleteError.value = caught instanceof Error ? caught.message : '删除失败'
  } finally {
    if (current === epoch) deleting.value = false
  }
}
function shown(value: unknown): string {
  return typeof value === 'string' ? value : (JSON.stringify(value) ?? '未填写')
}
async function cancelDelete() {
  deleteOpen.value = false
  deleteConfirmed.value = false
  await nextTick()
  deleteButton.value?.focus()
}
watch(
  () => [props.kind, id.value],
  () => void load(),
  { immediate: true },
)
onBeforeUnmount(() => {
  epoch++
})
</script>

<template>
  <div class="page-stack">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink :to="back">{{ kind === 'payment' ? '支付管理' : '发票管理' }}</RouterLink
      ><span aria-hidden="true">/</span><strong>单据详情</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>{{ title }}</h1>
        <p>正式字段与活动关系；历史审核和报销快照保留。</p>
      </div>
      <RouterLink class="button" :to="back">返回列表</RouterLink>
    </header>
    <div v-if="loading" class="panel state-layout" role="status">正在加载详情</div>
    <div v-else-if="error" class="panel state-layout" role="alert">
      <strong>{{ error }}</strong
      ><button class="button" @click="load">重试</button>
    </div>
    <template v-else-if="detail">
      <p v-if="badDebtNotice" class="notice notice-success" role="status">{{ badDebtNotice }}</p>
      <section class="panel detail-section" aria-labelledby="bad-debt-title">
        <div class="panel-heading">
          <h2 id="bad-debt-title">坏账状态</h2>
          <span :class="badDebtMarked ? 'status-pill status-warning' : 'quiet'">{{
            badDebtMarked ? '已标记坏账' : '未标记坏账'
          }}</span>
        </div>
        <div class="bad-debt-body">
          <p>坏账是业务异常标记，不改变金额、分配或报销资格；标记后当前关联行程不可删除。</p>
          <button
            v-if="allowed('allocations.manage') && !badDebtOpen"
            class="button"
            ref="badDebtTrigger"
            @click="openBadDebt"
          >
            {{ badDebtMarked ? '取消坏账标记' : '标记坏账' }}
          </button>
          <form
            v-if="badDebtOpen && allowed('allocations.manage')"
            class="form-stack"
            @submit.prevent="changeBadDebt"
          >
            <label class="field-stack"
              ><span>{{ badDebtMarked ? '取消坏账理由' : '标记坏账理由' }}</span
              ><textarea
                v-model="badDebtReason"
                ref="badDebtReasonField"
                class="input"
                maxlength="500"
                required
                :aria-describedby="badDebtError ? 'bad-debt-feedback' : undefined"
                :disabled="badDebtBusy"
              />
            </label>
            <p v-if="badDebtError" id="bad-debt-feedback" role="alert">{{ badDebtError }}</p>
            <div class="detail-actions">
              <button class="button button-primary" :disabled="badDebtBusy">
                确认{{ badDebtMarked ? '取消标记' : '标记坏账' }}</button
              ><button class="button" type="button" :disabled="badDebtBusy" @click="cancelBadDebt">
                取消</button
              ><button
                v-if="badDebtError"
                class="button"
                type="button"
                :disabled="badDebtBusy"
                @click="refreshBadDebt"
              >
                刷新当前状态
              </button>
            </div>
          </form>
        </div>
      </section>
      <section class="panel detail-section" aria-labelledby="fact-fields">
        <div class="panel-heading">
          <h2 id="fact-fields">当前正式字段</h2>
          <span class="quiet">版本 {{ detail.version }}</span>
        </div>
        <dl class="detail-fields">
          <div v-for="[label, value] in rows" :key="label">
            <dt>{{ label }}</dt>
            <dd>{{ value }}</dd>
          </div>
        </dl>
        <div class="detail-actions">
          <RouterLink
            v-if="canCorrect"
            class="button"
            :to="`/facts/${kind}/${encodeURIComponent(id)}/correction`"
            >纠正字段 / 查看历史</RouterLink
          ><RouterLink
            v-if="allowed('allocations.manage')"
            class="button"
            :to="`/allocations/${kind}/${encodeURIComponent(id)}`"
            >调整分配</RouterLink
          ><button
            v-if="allowed('resources.delete')"
            ref="deleteButton"
            class="button button-danger"
            @click="deleteOpen = true"
          >
            删除单据
          </button>
        </div>
        <div v-if="deleteOpen" class="delete-confirm" role="region" aria-label="删除确认">
          <p>
            删除后将终止此单据的活动金额分配和行程归属；原件、审核历史及既有报销快照仍保留。此处不会物理删除原件。
          </p>
          <label><input v-model="deleteConfirmed" type="checkbox" /> 我已核对单据并理解影响</label>
          <p v-if="deleteError" role="alert">{{ deleteError }}</p>
          <div class="detail-actions">
            <button
              class="button button-danger"
              :disabled="!deleteConfirmed || deleting"
              @click="remove"
            >
              {{ deleting ? '正在删除…' : '确认删除' }}</button
            ><button class="button" :disabled="deleting" @click="cancelDelete">取消</button>
          </div>
        </div>
      </section>
      <section v-if="detail.invoice" class="panel detail-section" aria-labelledby="invoice-items">
        <div class="panel-heading">
          <h2 id="invoice-items">当前发票明细</h2>
          <span class="quiet">{{ detail.invoice.item_count }} 项</span>
        </div>
        <p v-if="!detail.invoice.items?.length" class="detail-empty">票面未记录明细</p>
        <div v-else class="table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th scope="col">名称</th>
                <th scope="col">数量 / 单位</th>
                <th scope="col">单价</th>
                <th scope="col">金额</th>
                <th scope="col">税额</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in detail.invoice.items" :key="item.item_key">
                <td data-label="名称">{{ item.name }}</td>
                <td data-label="数量 / 单位">
                  {{ item.quantity ?? '未填写' }} {{ item.unit ?? '' }}
                </td>
                <td data-label="单价">
                  {{
                    item.unit_price_minor === undefined
                      ? '未填写'
                      : formatMinorUnits(item.unit_price_minor, detail.invoice.currency)
                  }}
                </td>
                <td data-label="金额">
                  {{ formatMinorUnits(item.amount_minor, detail.invoice.currency) }}
                </td>
                <td data-label="税额">
                  {{
                    item.tax_minor === undefined
                      ? '未填写'
                      : formatMinorUnits(item.tax_minor, detail.invoice.currency)
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <InvoiceMaterialsPanel
        v-if="kind === 'invoice' && canManageMaterials"
        :key="id"
        :invoice-id="id"
        :fact-version="detail.version"
        :refresh-context="refreshMaterialContext"
        @changed="load"
      />
      <section class="panel detail-section" aria-labelledby="fact-relations">
        <div class="panel-heading"><h2 id="fact-relations">关联状态</h2></div>
        <div class="detail-empty">
          <p>
            当前行程：<template v-if="detail.trip"
              >{{ detail.trip.name }}（{{ detail.trip.start_date }}—{{
                detail.trip.end_date
              }}）</template
            ><template v-else>未归属</template>
          </p>
          <p v-if="!detail.links.length">没有活动金额分配</p>
          <ul v-else>
            <li v-for="link in detail.links" :key="link.id">
              <RouterLink :to="`${factListPath(targetKind)}/${encodeURIComponent(link.target_id)}`"
                >查看关联{{ targetKind === 'invoice' ? '发票' : '支付' }}</RouterLink
              >
              · {{ formatMinorUnits(link.allocated_minor, link.currency) }} ·
              {{ link.target_business_date }}
            </li>
          </ul>
        </div>
      </section>
      <section v-if="detail.source" class="panel detail-section" aria-labelledby="fact-source">
        <div class="panel-heading">
          <h2 id="fact-source">原件与审核来源</h2>
          <a
            class="text-button"
            :href="api.documentContentURL(detail.source.document_id)"
            target="_blank"
            rel="noopener"
            >打开原件</a
          >
        </div>
        <div class="detail-empty">
          <p>
            {{ detail.source.original_name }} ·
            {{ detail.source.origin_kind === 'ai' ? 'AI 原始提取' : '显式人工录入' }} · 当前字段修订
            {{ detail.source.revision }}
          </p>
          <button class="button" :disabled="sourceBusy" @click="loadSource">
            {{ sourceBusy ? '正在加载来源…' : '展开审核来源' }}
          </button>
          <p v-if="sourceError" role="alert">{{ sourceError }}</p>
          <label class="field"
            ><span>原件页码</span
            ><select v-model="page">
              <option v-for="number in detail.source.page_count" :key="number" :value="number">
                第 {{ number }} 页
              </option>
            </select></label
          ><img
            class="source-image"
            :src="api.documentPageURL(detail.source.document_id, page)"
            :alt="`单据原件第 ${page} 页`"
          />
        </div>
        <div v-if="review" class="detail-empty">
          <dl class="source-fields">
            <div v-for="field in review.fields" :key="field.id">
              <dt>{{ fieldLabel(field.path) }}</dt>
              <dd>
                {{ field.presence === 'absent' ? '未填写' : shown(field.value)
                }}<small>来源：{{ field.source === 'user' ? '人工' : 'AI' }}</small
                ><small v-for="evidence in field.evidence" :key="evidence.id"
                  >第 {{ evidence.page }} 页：{{ evidence.quote }}</small
                >
              </dd>
            </div>
          </dl>
        </div>
      </section>
      <p v-else class="quiet">当前账号可查看正式字段，原件与审核来源需要额外权限。</p>
    </template>
  </div>
</template>

<style scoped>
.detail-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  padding: 20px;
  margin: 0;
}
.detail-fields dt,
.source-fields dt {
  font-size: 13px;
  color: var(--text-muted);
}
.detail-fields dd,
.source-fields dd {
  margin: 6px 0 0;
  overflow-wrap: anywhere;
}
.detail-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  padding: 20px;
}
.detail-empty {
  padding: 20px;
}
.bad-debt-body {
  padding: 20px;
}
.bad-debt-body > p {
  margin-top: 0;
}
.bad-debt-body .detail-actions {
  padding: 16px 0 0;
}
.detail-empty p:first-child {
  margin-top: 0;
}
.delete-confirm {
  margin: 0 20px 20px;
  padding: 16px;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
}
.delete-confirm .detail-actions {
  padding: 16px 0 0;
}
.source-image {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  object-fit: contain;
  border: 1px solid var(--border);
  margin-top: 16px;
}
.source-fields {
  display: grid;
  gap: 18px;
}
.source-fields small {
  display: block;
  color: var(--text-muted);
  margin-top: 4px;
}
.detail-empty .field {
  display: grid;
  gap: 8px;
  max-width: 240px;
  margin-top: 20px;
}
@media (max-width: 640px) {
  .detail-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  .detail-actions,
  .detail-empty,
  .detail-fields {
    padding: 16px;
  }
}
</style>
