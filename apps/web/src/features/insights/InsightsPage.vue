<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import {
  ApiError,
  api,
  type InsightAggregate,
  type InsightFact,
  type InsightFilter,
  type Trip,
} from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import {
  appendInsightItems,
  buildInsightFilter,
  defaultInsightFilterDraft,
  groupInsightAggregates,
  insightAllocationLabels,
  insightFactTypeLabel,
  insightFactTypeLabels,
  insightTripScopeLabels,
} from './model'

const draft = ref(defaultInsightFilterDraft())
const appliedFilter = ref<InsightFilter>()
const trips = ref<Trip[]>([])
const groups = ref<InsightAggregate[]>([])
const items = ref<InsightFact[]>([])
const nextCursor = ref('')
const loading = ref(true)
const loadingMore = ref(false)
const forbidden = ref(false)
const offline = ref(!navigator.onLine)
const filterError = ref('')
const error = ref('')
let requestVersion = 0

const canRead = computed(() => sessionStore.current.value?.capabilities.includes('insights.read'))
const groupedSummaries = computed(() => groupInsightAggregates(groups.value))

async function loadInitial() {
  if (!canRead.value) {
    forbidden.value = true
    loading.value = false
    return
  }
  loading.value = true
  forbidden.value = false
  error.value = ''
  const decision = buildInsightFilter(draft.value)
  if (!decision.filter) {
    filterError.value = decision.error ?? '筛选条件不完整'
    loading.value = false
    return
  }
  try {
    const [tripResult] = await Promise.all([api.trips(), fetchInsights(decision.filter, false)])
    trips.value = tripResult.items
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = forbidden.value
      ? ''
      : caught instanceof ApiError
        ? caught.message
        : '数据洞察加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function applyFilters() {
  const decision = buildInsightFilter(draft.value)
  filterError.value = decision.error ?? ''
  if (!decision.filter || offline.value) return
  loading.value = true
  error.value = ''
  try {
    await fetchInsights(decision.filter, false)
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = forbidden.value
      ? ''
      : caught instanceof ApiError
        ? caught.message
        : '筛选查询失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (!appliedFilter.value || !nextCursor.value || offline.value) return
  loadingMore.value = true
  error.value = ''
  try {
    await fetchInsights(appliedFilter.value, true)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : errorMessage(caught)
  } finally {
    loadingMore.value = false
  }
}

async function fetchInsights(filter: InsightFilter, append: boolean) {
  const version = ++requestVersion
  const cursor = append ? nextCursor.value : ''
  const page = await api.insights(filter, cursor, 50)
  if (version !== requestVersion) return
  items.value = append ? appendInsightItems(items.value, page.items) : page.items
  groups.value = page.groups
  nextCursor.value = page.next_cursor ?? ''
  appliedFilter.value = page.filter
}

function clearFilters() {
  draft.value = defaultInsightFilterDraft()
  filterError.value = ''
  void applyFilters()
}

function changeTripScope() {
  if (draft.value.trip_scope !== 'assigned') draft.value.trip_id = ''
}

function aggregateFactLabel(factType: InsightAggregate['fact_type']) {
  return factType === 'payment' ? '支付' : '发票'
}

function errorMessage(caught: unknown) {
  return caught instanceof Error ? caught.message : '更多洞察明细加载失败，请重新应用筛选'
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value && appliedFilter.value) void applyFilters()
}

onMounted(() => {
  void loadInitial()
  window.addEventListener('online', setOnlineState)
  window.addEventListener('offline', setOnlineState)
})

onUnmounted(() => {
  window.removeEventListener('online', setOnlineState)
  window.removeEventListener('offline', setOnlineState)
})
</script>

<template>
  <div class="page-stack insight-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>财务数据</span><span aria-hidden="true">/</span><strong>数据洞察</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>数据洞察</h1>
        <p>查看已确认单据的金额与分配情况，按币种和单据类型分别汇总。</p>
      </div>
      <button v-if="canRead" class="button" type="button" :disabled="offline" @click="applyFilters">
        刷新
      </button>
    </header>

    <div v-if="offline" class="notice notice-warning" role="status">
      <AppIcon name="alert" />
      <span>当前离线。已加载结果会保留，恢复网络后可重新查询。</span>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <AppIcon name="alert" /><span>{{ error }}</span>
      <button class="text-button" type="button" :disabled="offline" @click="applyFilters">
        重试
      </button>
    </div>

    <div v-if="loading && items.length === 0" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在汇总单据</strong>
      <span>正在读取金额与分配情况。</span>
    </div>
    <div v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph"><AppIcon name="lock" /></span
      ><strong>没有查看数据洞察的权限</strong>
      <span>请联系管理员开通查看权限。</span>
    </div>

    <template v-else-if="canRead">
      <section class="panel insight-filter-panel" aria-labelledby="insight-filter-title">
        <div class="panel-heading">
          <div>
            <h2 id="insight-filter-title">筛选单据</h2>
            <p>不填写日期即查询全部时间。</p>
          </div>
        </div>
        <form
          class="insight-filter-grid"
          :aria-describedby="filterError ? 'insight-filter-error' : undefined"
          @submit.prevent="applyFilters"
        >
          <div class="insight-filter-field">
            <label for="insight-fact-type">单据类型</label>
            <select id="insight-fact-type" v-model="draft.fact_type">
              <option v-for="(label, value) in insightFactTypeLabels" :key="value" :value="value">
                {{ label }}
              </option>
            </select>
          </div>
          <div class="insight-filter-field">
            <label for="insight-date-from">起始日期</label>
            <input
              id="insight-date-from"
              v-model="draft.date_from"
              type="date"
              :aria-describedby="filterError ? 'insight-filter-error' : undefined"
            />
          </div>
          <div class="insight-filter-field">
            <label for="insight-date-to">结束日期</label>
            <input
              id="insight-date-to"
              v-model="draft.date_to"
              type="date"
              :aria-describedby="filterError ? 'insight-filter-error' : undefined"
            />
          </div>
          <div class="insight-filter-field">
            <label for="insight-currency">币种</label>
            <select id="insight-currency" v-model="draft.currency">
              <option value="">全部币种</option>
              <option value="CNY">CNY</option>
              <option value="USD">USD</option>
              <option value="EUR">EUR</option>
              <option value="JPY">JPY</option>
            </select>
          </div>
          <div class="insight-filter-field">
            <label for="insight-allocation-status">分配状态</label>
            <select id="insight-allocation-status" v-model="draft.allocation_status">
              <option v-for="(label, value) in insightAllocationLabels" :key="value" :value="value">
                {{ label }}
              </option>
            </select>
          </div>
          <div class="insight-filter-field">
            <label for="insight-trip-scope">行程范围</label>
            <select id="insight-trip-scope" v-model="draft.trip_scope" @change="changeTripScope">
              <option v-for="(label, value) in insightTripScopeLabels" :key="value" :value="value">
                {{ label }}
              </option>
            </select>
          </div>
          <div v-if="draft.trip_scope === 'assigned'" class="insight-filter-field">
            <label for="insight-trip-id">具体行程（可选）</label>
            <select id="insight-trip-id" v-model="draft.trip_id">
              <option value="">全部已归属行程</option>
              <option v-for="trip in trips" :key="trip.id" :value="trip.id">
                {{ trip.name }} · {{ trip.start_date }} 至 {{ trip.end_date }}
              </option>
            </select>
          </div>
          <div class="insight-filter-actions">
            <button class="button button-primary" type="submit" :disabled="offline || loading">
              {{ loading ? '查询中…' : '应用筛选' }}
            </button>
            <button
              class="button button-quiet"
              type="button"
              :disabled="loading"
              @click="clearFilters"
            >
              清除筛选
            </button>
          </div>
          <p v-if="filterError" id="insight-filter-error" class="field-error" role="alert">
            {{ filterError }}
          </p>
        </form>
      </section>

      <section class="panel insight-summary-panel" aria-labelledby="insight-summary-title">
        <div class="panel-heading">
          <div>
            <h2 id="insight-summary-title">金额概览</h2>
            <p>不同币种及支付、发票分别统计，不合并计算。</p>
          </div>
        </div>
        <div v-if="groupedSummaries.length === 0" class="state-layout">
          <span class="state-glyph"><AppIcon name="chart" /></span><strong>当前筛选没有汇总</strong>
          <span>调整筛选条件，或先在收件箱完成单据审核。</span>
        </div>
        <div v-else class="insight-currency-groups">
          <section v-for="currencyGroup in groupedSummaries" :key="currencyGroup.currency">
            <h3>{{ currencyGroup.currency }}</h3>
            <div class="insight-aggregate-grid">
              <article v-for="aggregate in currencyGroup.facts" :key="aggregate.fact_type">
                <header>
                  <strong>{{ aggregateFactLabel(aggregate.fact_type) }}</strong>
                  <span>{{ aggregate.count }} 条</span>
                </header>
                <dl>
                  <div>
                    <dt>总额</dt>
                    <dd>{{ formatMinorUnits(aggregate.total_minor, aggregate.currency) }}</dd>
                  </div>
                  <div>
                    <dt>已分配</dt>
                    <dd>{{ formatMinorUnits(aggregate.allocated_minor, aggregate.currency) }}</dd>
                  </div>
                  <div>
                    <dt>剩余</dt>
                    <dd>{{ formatMinorUnits(aggregate.remaining_minor, aggregate.currency) }}</dd>
                  </div>
                </dl>
                <p>
                  未分配 {{ aggregate.unallocated_count }} · 部分 {{ aggregate.partial_count }} ·
                  已分配 {{ aggregate.allocated_count }}
                </p>
              </article>
            </div>
          </section>
        </div>
      </section>

      <section class="panel insight-list-panel" aria-labelledby="insight-list-title">
        <div class="panel-heading">
          <div>
            <h2 id="insight-list-title">单据明细</h2>
            <p>按业务日期从新到旧排列。</p>
          </div>
          <span>{{ items.length }} 条已加载</span>
        </div>
        <div v-if="items.length === 0" class="state-layout">
          <span class="state-glyph"><AppIcon name="search" /></span
          ><strong>当前筛选没有单据</strong>
          <span>清除筛选可查看全部当前支付与发票。</span>
        </div>
        <ul v-else class="insight-fact-list">
          <li v-for="item in items" :key="`${item.fact_type}:${item.fact_id}`">
            <article>
              <header>
                <div>
                  <span
                    class="status"
                    :data-tone="
                      item.allocation_status === 'allocated'
                        ? 'success'
                        : item.allocation_status === 'partial'
                          ? 'warning'
                          : 'neutral'
                    "
                  >
                    <span aria-hidden="true">●</span
                    >{{ insightAllocationLabels[item.allocation_status] }}
                  </span>
                  <strong
                    >{{ insightFactTypeLabel(item.fact_type) }} · {{ item.display_name }}</strong
                  >
                  <small>{{ item.business_date }} · 记录编号 {{ item.fact_id }}</small>
                </div>
                <strong class="numeric">{{
                  formatMinorUnits(item.amount_minor, item.currency)
                }}</strong>
              </header>
              <dl>
                <div>
                  <dt>已分配</dt>
                  <dd>{{ formatMinorUnits(item.allocated_minor, item.currency) }}</dd>
                </div>
                <div>
                  <dt>剩余</dt>
                  <dd>{{ formatMinorUnits(item.remaining_minor, item.currency) }}</dd>
                </div>
                <div>
                  <dt>当前行程</dt>
                  <dd>
                    {{
                      item.trip
                        ? `${item.trip.name} · ${item.trip.start_date} 至 ${item.trip.end_date}`
                        : '未归属'
                    }}
                  </dd>
                </div>
              </dl>
            </article>
          </li>
        </ul>
        <div v-if="items.length > 0" class="insight-pagination">
          <button
            v-if="nextCursor"
            class="button"
            type="button"
            :disabled="offline || loadingMore"
            @click="loadMore"
          >
            {{ loadingMore ? '加载中…' : '加载更多' }}
          </button>
          <span v-else>已到达当前筛选结果末尾</span>
        </div>
      </section>
    </template>
  </div>
</template>
