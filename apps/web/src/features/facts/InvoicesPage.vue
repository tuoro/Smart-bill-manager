<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
import { ApiError, api, type Invoice } from '../../data/client'
import { formatMinorUnits } from './money'

const items = ref<Invoice[]>([])
const loading = ref(true)
const forbidden = ref(false)
const error = ref('')
const canManageAllocations = computed(() =>
  sessionStore.current.value?.capabilities.includes('allocations.manage'),
)

function allocationLabel(status: Invoice['allocation_status']) {
  return status === 'allocated' ? '已全部分配' : status === 'partial' ? '部分分配' : '未分配'
}

async function load() {
  loading.value = true
  try {
    items.value = (await api.invoices()).items
    error.value = ''
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = caught instanceof ApiError ? caught.message : '发票列表加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <div class="page-stack">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>财务数据</span><span aria-hidden="true">/</span><strong>发票管理</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>发票管理</h1>
        <p>查看已确认的支付与发票，管理金额分配。</p>
      </div>
    </header>
    <section class="panel facts-panel" aria-labelledby="invoices-title">
      <div class="panel-heading">
        <h2 id="invoices-title" class="visually-hidden">发票</h2>
        <div class="facts-tabs" role="tablist" aria-label="账单类型">
          <RouterLink role="tab" aria-selected="false" to="/payments">支付</RouterLink>
          <RouterLink role="tab" aria-selected="true" to="/invoices">发票</RouterLink>
        </div>
        <div class="header-actions">
          <span class="quiet">{{ items.length }} 条记录</span>
          <button class="button button-small" type="button" @click="load">刷新</button>
        </div>
      </div>
      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在加载发票</strong>
      </div>
      <div v-else-if="forbidden" class="state-layout">
        <span class="state-glyph"><AppIcon name="lock" /></span><strong>没有查看账单的权限</strong
        ><span>当前账号仅可处理授权范围内的资料，请联系管理员调整权限。</span>
      </div>
      <div v-else-if="error" class="state-layout">
        <span class="state-glyph"><AppIcon name="alert" /></span><strong>发票列表加载失败</strong
        ><span>{{ error }}</span
        ><button class="button" type="button" @click="load">重试</button>
      </div>
      <div v-else-if="items.length === 0" class="state-layout">
        <span class="state-glyph"><AppIcon name="receipt" /></span><strong>还没有正式发票</strong
        ><span>上传发票并完成审核后，记录会显示在这里。</span
        ><RouterLink class="button" to="/inbox">前往收件箱</RouterLink>
      </div>
      <div v-else class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th scope="col">开票日期</th>
              <th scope="col">发票号码</th>
              <th scope="col">销售方 / 购买方</th>
              <th scope="col">明细</th>
              <th scope="col">状态</th>
              <th scope="col" class="numeric amount-column">价税合计</th>
              <th v-if="canManageAllocations" scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="invoice in items" :key="invoice.id">
              <td data-label="开票日期">
                <time :datetime="invoice.invoice_date">{{ invoice.invoice_date }}</time>
              </td>
              <td data-label="发票号码">
                <strong>{{ invoice.invoice_number }}</strong
                ><small>{{ invoice.id }}</small>
              </td>
              <td data-label="销售方 / 购买方">
                <strong>{{ invoice.seller_name }}</strong
                ><small>{{ invoice.buyer_name }}</small>
              </td>
              <td data-label="明细">{{ invoice.items.length }} 项</td>
              <td data-label="分配状态">
                <span
                  class="status"
                  :data-tone="invoice.allocation_status === 'allocated' ? 'success' : 'warning'"
                  ><span aria-hidden="true">●</span
                  >{{ allocationLabel(invoice.allocation_status) }}</span
                >
                <small>
                  已分配 {{ formatMinorUnits(invoice.allocated_minor, invoice.currency) }} · 剩余
                  {{ formatMinorUnits(invoice.remaining_minor, invoice.currency) }}
                </small>
              </td>
              <td class="numeric amount-cell" data-label="价税合计">
                {{ formatMinorUnits(invoice.total_minor, invoice.currency)
                }}<small v-if="invoice.tax_minor !== undefined"
                  >含税 {{ formatMinorUnits(invoice.tax_minor, invoice.currency) }}</small
                >
              </td>
              <td v-if="canManageAllocations" data-label="操作">
                <RouterLink
                  class="text-button"
                  :to="`/allocations/invoice/${encodeURIComponent(invoice.id)}`"
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
