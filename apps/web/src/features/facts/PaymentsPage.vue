<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ApiError, api, type Payment } from '../../data/client'
import { formatMinorUnits } from './money'

const items = ref<Payment[]>([])
const loading = ref(true)
const forbidden = ref(false)
const error = ref('')

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
        <h1>账单列表</h1>
        <p>只展示已经人工确认的正式 Fact。</p>
      </div>
    </header>
    <div class="facts-tabs" role="tablist" aria-label="账单类型">
      <RouterLink role="tab" aria-selected="true" to="/payments">支付</RouterLink
      ><RouterLink role="tab" aria-selected="false" to="/invoices">发票</RouterLink>
    </div>
    <section class="panel facts-panel" aria-labelledby="payments-title">
      <div class="panel-heading">
        <div>
          <h2 id="payments-title">支付</h2>
          <p>{{ items.length }} 条未删除记录</p>
        </div>
        <button class="button button-small" type="button" @click="load">刷新</button>
      </div>
      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在加载支付</strong>
      </div>
      <div v-else-if="forbidden" class="state-layout">
        <span class="state-glyph" aria-hidden="true">锁</span><strong>没有查看账单的权限</strong
        ><span>Reviewer 只能处理审核资料；请由 Owner 或 Finance 调整 Membership。</span>
      </div>
      <div v-else-if="error" class="state-layout">
        <span class="state-glyph" aria-hidden="true">!</span><strong>支付列表加载失败</strong
        ><span>{{ error }}</span
        ><button class="button" type="button" @click="load">重试</button>
      </div>
      <div v-else-if="items.length === 0" class="state-layout">
        <span class="state-glyph" aria-hidden="true">支</span><strong>还没有正式支付</strong
        ><span>在 AI 收件箱中确认一条 Payment Claim 后会显示在这里。</span
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
            </tr>
          </thead>
          <tbody>
            <tr v-for="payment in items" :key="payment.id">
              <td>
                <time :datetime="payment.transaction_time">{{
                  formatDate(payment.transaction_time)
                }}</time
                ><small>{{ payment.source_timezone }}</small>
              </td>
              <td>
                <strong>{{ payment.merchant }}</strong
                ><small>{{ payment.category || '未分类' }}</small>
              </td>
              <td>
                {{ payment.payment_method || '—'
                }}<small>{{ payment.order_number || payment.id }}</small>
              </td>
              <td>
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
              <td class="numeric amount-cell">
                {{ formatMinorUnits(payment.amount_minor, payment.currency) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
