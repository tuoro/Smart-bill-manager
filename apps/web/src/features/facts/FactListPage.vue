<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import {
  api,
  type FactKind,
  type FactListQuery,
  type Invoice,
  type Payment,
} from '../../data/client'
import { factListPath, factListQuery } from './list-model'
import { formatMinorUnits } from './money'

const props = defineProps<{ kind: FactKind }>()
const route = useRoute(),
  router = useRouter()
const base = computed(() => factListPath(props.kind))
const title = computed(() => (props.kind === 'payment' ? '支付' : '发票'))
const items = ref<(Payment | Invoice)[]>([]),
  nextCursor = ref(''),
  loading = ref(false),
  error = ref('')
const draft = reactive({ q: '', date_from: '', date_to: '', allocation_status: 'all' })
const allowed = (capability: string) =>
  sessionStore.current.value?.capabilities.includes(capability) === true
const canRead = computed(() => allowed('facts.read'))
const canCorrect = computed(() => allowed('facts.read') && allowed('claims.review'))
const canAllocate = computed(() => allowed('allocations.manage'))
let epoch = 0
let activeQuery: FactListQuery = {}

async function load() {
  const current = ++epoch
  items.value = []
  nextCursor.value = ''
  error.value = ''
  loading.value = true
  try {
    activeQuery = factListQuery(route.query)
    draft.q = activeQuery.q ?? ''
    draft.date_from = activeQuery.date_from ?? ''
    draft.date_to = activeQuery.date_to ?? ''
    draft.allocation_status = activeQuery.allocation_status || 'all'
    if (!canRead.value) return
    const result =
      props.kind === 'payment' ? await api.payments(activeQuery) : await api.invoices(activeQuery)
    if (current !== epoch) return
    if (!Array.isArray(result.items) || typeof result.next_cursor !== 'string')
      throw new Error('分页响应不完整，请重试')
    items.value = result.items
    nextCursor.value = result.next_cursor
  } catch (caught) {
    if (current === epoch) error.value = caught instanceof Error ? caught.message : '列表加载失败'
  } finally {
    if (current === epoch) loading.value = false
  }
}

async function applyFilters() {
  const query = { ...draft }
  const target = router.resolve({ path: base.value, query }).fullPath
  if (target === route.fullPath) await load()
  else await router.push({ path: base.value, query })
}
async function firstPage() {
  const { cursor: _cursor, ...query } = activeQuery
  if (!_cursor) await load()
  else await router.push({ path: base.value, query })
}
async function nextPage() {
  if (nextCursor.value)
    await router.push({ path: base.value, query: { ...activeQuery, cursor: nextCursor.value } })
}
function isPayment(item: Payment | Invoice): item is Payment {
  return 'merchant' in item
}
function dateLabel(item: Payment | Invoice) {
  return isPayment(item) ? item.business_date : item.invoice_date
}
function statusLabel(status: string) {
  return status === 'allocated' ? '已全部分配' : status === 'partial' ? '部分分配' : '未分配'
}
watch(
  () => route.fullPath,
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
      <span>财务数据</span><span aria-hidden="true">/</span><strong>{{ title }}管理</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>{{ title }}管理</h1>
        <p>按确认入库顺序浏览；日期、名称和单号用于筛选。</p>
      </div>
    </header>
    <section class="panel facts-panel" :aria-label="`${title}列表`">
      <div class="panel-heading">
        <div class="facts-tabs" role="tablist" aria-label="账单类型">
          <RouterLink role="tab" :aria-selected="kind === 'payment'" to="/payments"
            >支付</RouterLink
          >
          <RouterLink role="tab" :aria-selected="kind === 'invoice'" to="/invoices"
            >发票</RouterLink
          >
        </div>
        <div v-if="canRead" class="header-actions">
          <span class="quiet">本页 {{ items.length }} 条</span
          ><button class="button button-small" :disabled="loading" @click="load">刷新</button>
        </div>
      </div>
      <form v-if="canRead" class="fact-filters" @submit.prevent="applyFilters">
        <label class="field"
          ><span>{{ kind === 'payment' ? '商户 / 订单号' : '购销方 / 发票号' }}</span
          ><input v-model="draft.q" maxlength="200" placeholder="输入名称或单号"
        /></label>
        <label class="field"
          ><span>开始日期</span><input v-model="draft.date_from" type="date"
        /></label>
        <label class="field"
          ><span>结束日期</span><input v-model="draft.date_to" type="date"
        /></label>
        <label class="field"
          ><span>分配状态</span
          ><select v-model="draft.allocation_status">
            <option value="all">全部状态</option>
            <option value="unallocated">未分配</option>
            <option value="partial">部分分配</option>
            <option value="allocated">已全部分配</option>
          </select></label
        >
        <button class="button button-primary" type="submit">查询</button>
      </form>
      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner" aria-hidden="true"></span><strong>正在加载{{ title }}</strong>
      </div>
      <div v-else-if="!canRead" class="state-layout">
        <AppIcon name="lock" /><strong>没有查看账单的权限</strong>
        <span>当前账号仅可处理授权范围内的资料，请联系管理员调整权限。</span>
      </div>
      <div v-else-if="error" class="state-layout" role="alert">
        <strong>{{ title }}列表加载失败</strong><span>{{ error }}</span
        ><button class="button" @click="load">重试</button
        ><RouterLink class="button" :to="base">返回首屏</RouterLink>
      </div>
      <div v-else-if="!items.length" class="state-layout">
        <strong>当前范围没有{{ title }}记录</strong><span>请调整筛选，或上传单据后完成审核。</span
        ><RouterLink v-if="allowed('documents.process')" class="button" to="/inbox"
          >前往收件箱</RouterLink
        >
      </div>
      <div v-else class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th scope="col">{{ kind === 'payment' ? '交易日期' : '开票日期' }}</th>
              <th scope="col">{{ kind === 'payment' ? '商户' : '购销方' }}</th>
              <th scope="col">{{ kind === 'payment' ? '方式 / 订单' : '发票号码' }}</th>
              <th scope="col">状态</th>
              <th scope="col" class="numeric">金额</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.id">
              <td data-label="日期">{{ dateLabel(item) }}</td>
              <td data-label="名称">
                <strong>{{ isPayment(item) ? item.merchant : item.seller_name }}</strong
                ><small v-if="!isPayment(item)"
                  >购方：{{ item.buyer_name }} · {{ item.item_count }} 项明细</small
                >
              </td>
              <td data-label="单号">
                <template v-if="isPayment(item)"
                  >{{ item.payment_method || '未填写方式'
                  }}<small>{{ item.order_number || '无订单号' }}</small></template
                ><template v-else>{{ item.invoice_number }}</template>
                <span v-if="item.bad_debt" class="status-pill status-warning">坏账</span>
              </td>
              <td data-label="状态">
                <span
                  class="status"
                  :class="
                    item.allocation_status === 'allocated' ? 'status-success' : 'status-neutral'
                  "
                  >{{ statusLabel(item.allocation_status) }}</span
                ><small>剩余 {{ formatMinorUnits(item.remaining_minor, item.currency) }}</small>
              </td>
              <td class="numeric amount-cell" data-label="金额">
                {{
                  formatMinorUnits(
                    isPayment(item) ? item.amount_minor : item.total_minor,
                    item.currency,
                  )
                }}
              </td>
              <td data-label="操作">
                <div class="fact-row-actions">
                  <RouterLink
                    class="text-button"
                    :to="{
                      path: `${base}/${encodeURIComponent(item.id)}`,
                      query: { back: route.fullPath },
                    }"
                    >查看详情</RouterLink
                  >
                  <RouterLink
                    v-if="canCorrect"
                    class="text-button"
                    :to="`/facts/${kind}/${encodeURIComponent(item.id)}/correction`"
                    >纠正字段</RouterLink
                  >
                  <RouterLink
                    v-if="canAllocate"
                    class="text-button"
                    :to="`/allocations/${kind}/${encodeURIComponent(item.id)}`"
                    >调整分配</RouterLink
                  >
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer v-if="canRead && !error" class="fact-pagination">
        <span class="quiet">分页期间记录可能变化；刷新首屏查看最新入库记录。</span>
        <div class="header-actions">
          <button class="button button-small" :disabled="loading" @click="firstPage">
            刷新首屏</button
          ><button class="button button-small" :disabled="loading || !nextCursor" @click="nextPage">
            下一页
          </button>
        </div>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.fact-filters {
  display: grid;
  grid-template-columns: minmax(180px, 2fr) repeat(3, minmax(130px, 1fr)) auto;
  gap: 12px;
  align-items: end;
  padding: 20px;
  border-bottom: 1px solid var(--border);
}
.fact-pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 20px;
}
.fact-row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
td small {
  display: block;
  margin-top: 6px;
  color: var(--text-muted);
}
@media (max-width: 1100px) {
  .fact-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .fact-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
}
@media (max-width: 560px) {
  .fact-filters {
    grid-template-columns: minmax(0, 1fr);
    padding: 16px;
  }
}
</style>
