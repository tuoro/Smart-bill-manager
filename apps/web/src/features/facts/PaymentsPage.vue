<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import { ApiError, api, type Payment } from '../../data/client'
import { formatMinorUnits } from './money'

const items = ref<Payment[]>([])
const loading = ref(true)
const forbidden = ref(false)
const error = ref('')
const canManageAllocations = computed(() =>
  sessionStore.current.value?.capabilities.includes('allocations.manage'),
)

async function load() {
  loading.value = true
  try {
    items.value = (await api.payments()).items
    error.value = ''
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = caught instanceof ApiError ? caught.message : '支付列表加载失败'
  } finally {
    loading.value = false
  }
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}

function allocationLabel(status: Payment['allocation_status']) {
  return status === 'allocated' ? '已全部分配' : status === 'partial' ? '部分分配' : '未分配'
}

onMounted(() => void load())
</script>

<template>
  <div class="page-stack">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>财务数据</span><span aria-hidden="true">/</span><strong>支付管理</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>支付管理</h1>
        <p>查看已确认的支付与发票，管理金额分配。</p>
      </div>
    </header>
    <section class="panel facts-panel" aria-labelledby="payments-title">
      <div class="panel-heading">
        <h2 id="payments-title" class="visually-hidden">支付</h2>
        <div class="facts-tabs" role="tablist" aria-label="账单类型">
          <RouterLink role="tab" aria-selected="true" to="/payments">支付</RouterLink>
          <RouterLink role="tab" aria-selected="false" to="/invoices">发票</RouterLink>
        </div>
        <div class="header-actions">
          <span class="quiet">{{ items.length }} 条记录</span>
          <button class="button button-small" type="button" @click="load">刷新</button>
        </div>
      </div>
      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在加载支付</strong>
      </div>
      <div v-else-if="forbidden" class="state-layout">
        <span class="state-glyph"><AppIcon name="lock" /></span><strong>没有查看账单的权限</strong
        ><span>当前账号仅可处理授权范围内的资料，请联系管理员调整权限。</span>
      </div>
      <div v-else-if="error" class="state-layout">
        <span class="state-glyph"><AppIcon name="alert" /></span><strong>支付列表加载失败</strong
        ><span>{{ error }}</span
        ><button class="button" type="button" @click="load">重试</button>
      </div>
      <div v-else-if="items.length === 0" class="state-layout">
        <span class="state-glyph"><AppIcon name="payment" /></span><strong>还没有正式支付</strong
        ><span>上传支付凭证并完成审核后，记录会显示在这里。</span
        ><RouterLink class="button" to="/inbox">前往收件箱</RouterLink>
      </div>
      <div v-else class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th scope="col">交易时间</th>
              <th scope="col">商户</th>
              <th scope="col">方式 / 订单</th>
              <th scope="col">状态</th>
              <th scope="col" class="numeric amount-column">金额</th>
              <th v-if="canManageAllocations" scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="payment in items" :key="payment.id">
              <td data-label="交易时间">
                <time :datetime="payment.transaction_time">{{
                  formatDate(payment.transaction_time)
                }}</time
                ><small>{{ payment.source_timezone }}</small>
              </td>
              <td data-label="商户">
                <strong>{{ payment.merchant }}</strong
                ><small>{{ payment.category || '未分类' }}</small>
              </td>
              <td data-label="方式 / 订单">
                {{ payment.payment_method || '—'
                }}<small>{{ payment.order_number || payment.id }}</small>
              </td>
              <td data-label="分配状态">
                <span
                  class="status"
                  :data-tone="payment.allocation_status === 'allocated' ? 'success' : 'warning'"
                  ><span aria-hidden="true">●</span
                  >{{ allocationLabel(payment.allocation_status) }}</span
                >
                <small>
                  已分配 {{ formatMinorUnits(payment.allocated_minor, payment.currency) }} · 剩余
                  {{ formatMinorUnits(payment.remaining_minor, payment.currency) }}
                </small>
              </td>
              <td class="numeric amount-cell" data-label="金额">
                {{ formatMinorUnits(payment.amount_minor, payment.currency) }}
              </td>
              <td v-if="canManageAllocations" data-label="操作">
                <RouterLink
                  class="text-button"
                  :to="`/allocations/payment/${encodeURIComponent(payment.id)}`"
                  >调整分配</RouterLink
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
