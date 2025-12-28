<template>
  <div class="page">
    <div v-if="loading" class="loading">
      <ProgressSpinner />
      <div class="loading-text">&#21152;&#36733;&#20013;...</div>
    </div>

    <Card v-else-if="!data" class="empty-card sbm-surface">
      <template #content>
        <div class="empty">
          <i class="pi pi-inbox empty-icon" />
          <div class="empty-title">&#26242;&#26080;&#25968;&#25454;</div>
        </div>
      </template>
    </Card>

    <template v-else>
      <div class="hero sbm-surface">
        <div class="hero-left">
          <div class="hero-kicker">&#26234;&#33021;&#36134;&#21333;&#31649;&#29702;</div>
          <div class="hero-title">Smart Bill Manager</div>
          <div class="hero-subtitle">
            &#21457;&#31080;&#19982;&#25903;&#20184;&#35760;&#24405;&#33258;&#21160;&#35782;&#21035;&#65292;&#26234;&#33021;&#25512;&#33616;&#20851;&#32852;&#65292;&#21482;&#20570;&#20320;&#30495;&#27491;&#38656;&#35201;&#30340;&#20107;&#12290;
          </div>
          <div class="hero-actions">
            <Button
              :label="'\u53BB\u4E0A\u4F20\u652F\u4ED8\u622A\u56FE'"
              icon="pi pi-image"
              @click="router.push('/payments')"
            />
            <Button
              class="p-button-outlined"
              severity="secondary"
              :label="'\u53BB\u4E0A\u4F20\u53D1\u7968'"
              icon="pi pi-upload"
              @click="router.push('/invoices')"
            />
            <Button
              class="p-button-text"
              severity="secondary"
              :label="'\u67E5\u770B\u65E5\u5FD7'"
              icon="pi pi-book"
              @click="router.push('/logs')"
            />
          </div>
        </div>
        <div class="hero-right">
          <div class="hero-chip">
            <div class="chip-label">&#26412;&#26376;&#25903;&#20986;</div>
            <div class="chip-value">{{ formatMoney(data.payments.totalThisMonth) }}</div>
          </div>
          <div class="hero-chip">
            <div class="chip-label">&#21457;&#31080;&#24635;&#25968;</div>
            <div class="chip-value">{{ data.invoices.totalCount }}</div>
          </div>
        </div>
      </div>

      <div class="stats-grid">
        <Card
          class="stat-card sbm-surface"
          :style="{ '--sbm-accent': 'var(--p-primary-500, #3b82f6)', '--sbm-accent-bg': 'rgba(59, 130, 246, 0.12)' }"
        >
          <template #content>
            <div class="stat">
              <div class="stat-left">
                <div class="stat-title">&#26412;&#26376;&#25903;&#20986;</div>
                <div class="stat-value">{{ formatMoney(data.payments.totalThisMonth) }}</div>
              </div>
              <div class="stat-icon">
                <i class="pi pi-wallet" />
              </div>
            </div>
          </template>
        </Card>

        <Card
          class="stat-card sbm-surface"
          :style="{ '--sbm-accent': 'var(--p-indigo-500, #6366f1)', '--sbm-accent-bg': 'rgba(99, 102, 241, 0.12)' }"
        >
          <template #content>
            <div class="stat">
              <div class="stat-left">
                <div class="stat-title">&#25903;&#20184;&#31508;&#25968;</div>
                <div class="stat-value">{{ data.payments.countThisMonth }}</div>
              </div>
              <div class="stat-icon">
                <i class="pi pi-chart-line" />
              </div>
            </div>
          </template>
        </Card>

        <Card
          class="stat-card sbm-surface"
          :style="{ '--sbm-accent': 'var(--p-teal-500, #14b8a6)', '--sbm-accent-bg': 'rgba(20, 184, 166, 0.12)' }"
        >
          <template #content>
            <div class="stat">
              <div class="stat-left">
                <div class="stat-title">&#21457;&#31080;&#24635;&#25968;</div>
                <div class="stat-value">{{ data.invoices.totalCount }}</div>
              </div>
              <div class="stat-icon">
                <i class="pi pi-file" />
              </div>
            </div>
          </template>
        </Card>

        <Card
          class="stat-card sbm-surface"
          :style="{ '--sbm-accent': 'var(--p-orange-500, #f97316)', '--sbm-accent-bg': 'rgba(249, 115, 22, 0.12)' }"
        >
          <template #content>
            <div class="stat">
              <div class="stat-left">
                <div class="stat-title">&#21457;&#31080;&#37329;&#39069;</div>
                <div class="stat-value">{{ formatMoney(data.invoices.totalAmount) }}</div>
              </div>
              <div class="stat-icon">
                <i class="pi pi-receipt" />
              </div>
            </div>
          </template>
        </Card>
      </div>

      <div class="grid grid-equal">
        <Card class="panel sbm-surface">
          <template #title>
            <div class="panel-title">
              <span>&#27599;&#26085;&#25903;&#20986;&#36235;&#21183;</span>
              <Button
                :label="'\u5237\u65B0'"
                icon="pi pi-refresh"
                class="p-button-text"
                @click="loadData"
              />
            </div>
          </template>
          <template #content>
            <div v-if="dailyData.length > 0" class="chart">
              <v-chart :option="lineChartOption" autoresize />
            </div>
            <div v-else class="empty-mini">&#26242;&#26080;&#25968;&#25454;</div>
          </template>
        </Card>

        <Card class="panel sbm-surface">
          <template #title>
            <div class="panel-title">
              <div class="match-title">
                <span>&#25903;&#20184;&#35760;&#24405;&#19982;&#21457;&#31080;&#21305;&#37197;</span>
                <Tag
                  v-if="unlinkedRecentCount > 0"
                  severity="warn"
                  :value="`\u672A\u5173\u8054 ${unlinkedRecentCount}`"
                />
                <Tag v-else severity="success" :value="'\u5168\u90E8\u5DF2\u5173\u8054'" />
              </div>
              <div class="match-actions">
                <Button
                  class="p-button-text"
                  severity="secondary"
                  icon="pi pi-refresh"
                  :label="'\u5237\u65B0'"
                  :loading="recentPaymentsLoading"
                  @click="loadRecentPayments"
                />
                <Button
                  class="p-button-outlined"
                  severity="secondary"
                  icon="pi pi-external-link"
                  :label="'\u53BB\u652F\u4ED8\u8BB0\u5F55'"
                  @click="router.push('/payments')"
                />
              </div>
            </div>
          </template>
          <template #content>
            <div v-if="recentPaymentsLoading" class="loading-mini">
              <ProgressSpinner style="width: 26px; height: 26px" />
              <div class="loading-text-mini">&#21152;&#36733;&#20013;...</div>
            </div>
            <div v-else-if="recentPayments.length === 0" class="empty-mini">&#26242;&#26080;&#25903;&#20184;&#35760;&#24405;</div>
            <DataTable v-else :value="recentPaymentRows" size="small" responsiveLayout="scroll" :rows="6" :paginator="false">
              <Column :header="'\u4EA4\u6613\u65F6\u95F4'" :style="{ width: '160px' }">
                <template #body="{ data: row }">{{ formatDateTime(row.transaction_time) }}</template>
              </Column>
              <Column :header="'\u5546\u5BB6'">
                <template #body="{ data: row }">
                  <span class="sbm-ellipsis" :title="normalizeInlineText(row.merchant)">{{ normalizeInlineText(row.merchant) || '-' }}</span>
                </template>
              </Column>
              <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
                <template #body="{ data: row }">
                  <span class="money">{{ formatMoney(row.amount || 0) }}</span>
                </template>
              </Column>
              <Column :header="'\u53D1\u7968'" :style="{ width: '140px' }">
                <template #body="{ data: row }">
                  <Tag
                    v-if="row.invoiceCount > 0"
                    severity="success"
                    :value="`\u5DF2\u5173\u8054 ${row.invoiceCount}`"
                  />
                  <Tag v-else severity="warn" :value="'\u672A\u5173\u8054'" />
                </template>
              </Column>
              <Column :header="'\u64CD\u4F5C'" :style="{ width: '120px' }">
                <template #body="{ data: row }">
                  <Button
                    class="p-button-text"
                    size="small"
                    :label="row.invoiceCount > 0 ? '\u67E5\u770B' : '\u53BB\u5173\u8054'"
                    @click="openPaymentMatch(row.id)"
                  />
                </template>
              </Column>
            </DataTable>
          </template>
        </Card>
      </div>

      <div class="grid">
        <Card class="panel sbm-surface">
          <template #title>
            <span>&#26368;&#36817;&#37038;&#20214;</span>
          </template>
          <template #content>
            <DataTable :value="data.email.recentLogs" size="small" :rows="6" responsiveLayout="scroll">
              <Column field="subject" :header="'\u4E3B\u9898'" />
              <Column field="from_address" :header="'\u53D1\u4EF6\u4EBA'" />
              <Column :header="'\u9644\u4EF6'">
                <template #body="{ data: row }">
                  <Tag
                    v-if="row.has_attachment"
                    severity="info"
                    :value="`${row.attachment_count}\u4E2A`"
                  />
                  <Tag v-else severity="secondary" :value="'\u65E0'" />
                </template>
              </Column>
              <Column :header="'\u65F6\u95F4'">
                <template #body="{ data: row }">
                  {{ row.received_date ? formatDate(row.received_date) : '-' }}
                </template>
              </Column>
            </DataTable>
          </template>
        </Card>

        <Card class="panel sbm-surface source-panel">
          <template #title>
            <span>&#21457;&#31080;&#26469;&#28304;&#20998;&#24067;</span>
          </template>
          <template #content>
            <div v-if="Object.keys(data.invoices.bySource || {}).length > 0" class="source-grid">
              <Card
                v-for="(count, source, index) in data.invoices.bySource"
                :key="source"
                class="source-card sbm-surface"
                :style="{ '--sbm-accent': COLORS[index % COLORS.length] }"
              >
                <template #content>
                  <div class="source-title">{{ getSourceLabel(source as string) }}</div>
                  <div class="source-value">{{ count }}</div>
                </template>
              </Card>
            </div>
            <div v-else class="empty-mini">&#26242;&#26080;&#21457;&#31080;</div>
          </template>
        </Card>
      </div>

      <div class="grid">
        <Card class="panel col-span-2 sbm-surface">
          <template #title>
            <span><i class="pi pi-envelope" /> &#37038;&#31665;&#30417;&#25511;&#29366;&#24577;</span>
          </template>
          <template #content>
            <div v-if="data.email.monitoringStatus.length > 0" class="monitor-list">
              <div v-for="(item, index) in data.email.monitoringStatus" :key="item.configId || index" class="monitor-item">
                <div class="monitor-left">
                  <div class="monitor-label">&#37038;&#31665; {{ index + 1 }}</div>
                  <Tag
                    :severity="item.status === 'running' ? 'success' : 'info'"
                    :value="item.status === 'running' ? '\u8FD0\u884C\u4E2D' : '\u5DF2\u505C\u6B62'"
                  />
                </div>
                <ProgressBar
                  :value="item.status === 'running' ? 100 : 0"
                  :showValue="false"
                  style="height: 10px"
                />
              </div>
            </div>
            <div v-else class="empty-mini">&#26242;&#26080;&#37197;&#32622;&#37038;&#31665;</div>
          </template>
        </Card>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import VChart from 'vue-echarts'
import dayjs from 'dayjs'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import ProgressBar from 'primevue/progressbar'
import ProgressSpinner from 'primevue/progressspinner'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import { dashboardApi, paymentApi } from '@/api'
import { CHART_COLORS } from '@/utils/constants'
import type { DashboardData, Payment } from '@/types'

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent])

const router = useRouter()

const COLORS = CHART_COLORS

const loading = ref(true)
const data = ref<DashboardData | null>(null)

const recentPaymentsLoading = ref(false)
const recentPayments = ref<Payment[]>([])
const paymentInvoiceCount = ref<Record<string, number>>({})

const dailyData = computed(() => {
  if (!data.value?.payments.dailyStats) return []
  return Object.entries(data.value.payments.dailyStats)
    .map(([date, amount]) => ({
      date: dayjs(date).format('MM-DD'),
      amount,
    }))
    .sort((a, b) => a.date.localeCompare(b.date))
})

const normalizeInlineText = (value?: string | null) => (value || '').replace(/\s+/g, ' ').trim()

const formatDateTime = (date: string) => dayjs(date).format('YYYY-MM-DD HH:mm')

const unlinkedRecentCount = computed(
  () => recentPayments.value.filter((p) => (paymentInvoiceCount.value[p.id] || 0) === 0).length
)

const recentPaymentRows = computed(() =>
  recentPayments.value.map((p) => ({
    ...p,
    invoiceCount: paymentInvoiceCount.value[p.id] || 0,
  }))
)

const loadRecentPayments = async () => {
  recentPaymentsLoading.value = true
  try {
    const res = await paymentApi.getAll({ limit: 6, offset: 0 })
    const list = res.data.success && res.data.data ? res.data.data : []
    const sorted = [...list].sort((a, b) => dayjs(b.transaction_time).valueOf() - dayjs(a.transaction_time).valueOf())
    recentPayments.value = sorted.slice(0, 6)

    const entries = await Promise.all(
      recentPayments.value.map(async (p) => {
        try {
          const r = await paymentApi.getPaymentInvoices(p.id)
          const count = r.data.success && r.data.data ? r.data.data.length : 0
          return [p.id, count] as const
        } catch {
          return [p.id, 0] as const
        }
      })
    )
    paymentInvoiceCount.value = Object.fromEntries(entries)
  } catch (error) {
    console.error('Failed to load recent payments:', error)
    recentPayments.value = []
    paymentInvoiceCount.value = {}
  } finally {
    recentPaymentsLoading.value = false
  }
}

const openPaymentMatch = (paymentId: string) => {
  router.push({ path: '/payments', query: { match: paymentId } })
}

const lineChartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    formatter: (params: { name: string; value: number }[]) => {
      const item = params[0]
      return `${item.name}<br/>\u652F\u51FA: \u00A5${item.value.toFixed(2)}`
    },
  },
  grid: {
    left: '3%',
    right: '4%',
    bottom: '3%',
    containLabel: true,
  },
  xAxis: {
    type: 'category',
    data: dailyData.value.map((d) => d.date),
    boundaryGap: false,
  },
  yAxis: {
    type: 'value',
  },
  series: [
    {
      data: dailyData.value.map((d) => d.amount),
      type: 'line',
      smooth: true,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(24, 144, 255, 0.3)' },
            { offset: 1, color: 'rgba(24, 144, 255, 0.05)' },
          ],
        },
      },
      lineStyle: {
        color: '#1890ff',
        width: 2,
      },
      itemStyle: {
        color: '#1890ff',
      },
    },
  ],
}))

const loadData = async () => {
  loading.value = true
  try {
    const [res] = await Promise.all([dashboardApi.getSummary(), loadRecentPayments()])
    data.value = res.data.success && res.data.data ? res.data.data : null
  } catch (error) {
    console.error('Failed to load dashboard data:', error)
    data.value = null
  } finally {
    loading.value = false
  }
}

const formatDate = (date: string) => dayjs(date).format('MM-DD HH:mm')

const getSourceLabel = (source: string) => {
  const labels: Record<string, string> = {
    upload: '\u624B\u52A8\u4E0A\u4F20',
    email: '\u90AE\u4EF6\u4E0B\u8F7D',
    feishu: '\u98DE\u4E66\u673A\u5668\u4EBA',
    dingtalk: '\u9489\u9489(\u5DF2\u79FB\u9664)',
  }
  return labels[source] || source
}

const formatMoney = (value: number) => `\u00A5${(value || 0).toFixed(2)}`

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.hero {
  display: grid;
  grid-template-columns: 1.4fr 0.6fr;
  gap: 18px;
  padding: 22px 22px 20px;
  border-radius: 18px;
}

.hero-left {
  min-width: 0;
}

.hero-kicker {
  font-weight: 900;
  color: var(--p-text-muted-color);
  letter-spacing: 0.2px;
}

.hero-title {
  margin-top: 6px;
  font-size: 30px;
  font-weight: 950;
  letter-spacing: -0.7px;
  color: var(--p-text-color);
}

.hero-subtitle {
  margin-top: 10px;
  color: var(--p-text-muted-color);
  font-weight: 600;
  line-height: 1.7;
}

.hero-actions {
  margin-top: 14px;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.hero-right {
  display: grid;
  gap: 10px;
  align-content: start;
}

.hero-chip {
  border-radius: 16px;
  padding: 14px 14px;
  border: 1px solid color-mix(in srgb, var(--p-surface-200), transparent 20%);
  background: color-mix(in srgb, var(--p-surface-0), transparent 10%);
}

.chip-label {
  font-size: 12px;
  font-weight: 900;
  color: var(--p-text-muted-color);
}

.chip-value {
  margin-top: 6px;
  font-size: 18px;
  font-weight: 950;
  color: var(--p-text-color);
}

.loading {
  display: grid;
  place-items: center;
  padding: 60px 0;
  gap: 10px;
}

.loading-text {
  color: var(--color-text-tertiary);
  font-weight: 600;
}

.empty-card {
  border-radius: var(--radius-lg);
}

.empty {
  display: grid;
  place-items: center;
  padding: 24px;
  gap: 8px;
}

.empty-icon {
  font-size: 34px;
  color: var(--color-text-tertiary);
}

.empty-title {
  font-weight: 700;
  color: var(--color-text-secondary);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.stat-card {
  border-left: 4px solid var(--sbm-accent, var(--p-primary-500));
  overflow: hidden;
  border-radius: 16px;
}

.stat {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.stat-title {
  font-weight: 800;
  font-size: 13px;
  color: var(--p-text-muted-color);
}

.stat-value {
  margin-top: 6px;
  font-size: 24px;
  font-weight: 950;
  letter-spacing: -0.2px;
  color: var(--p-text-color);
}

.stat-icon {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--sbm-accent-bg, rgba(59, 130, 246, 0.12));
  color: var(--sbm-accent, var(--p-primary-500));
}

.stat-icon i {
  font-size: 20px;
}

.grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
}

.grid > * {
  min-width: 0;
}

.grid-equal {
  grid-template-columns: 1fr 1fr;
}

.col-span-2 {
  grid-column: span 2 / span 2;
}

.panel {
  border-radius: 16px;
  min-width: 0;
  overflow: hidden;
}

.panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.match-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.match-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.loading-mini {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
}

.loading-text-mini {
  color: var(--color-text-tertiary);
  font-weight: 600;
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.money {
  font-weight: 950;
  color: var(--p-red-600, #dc2626);
}

.chart {
  height: 320px;
}

.empty-mini {
  padding: 18px 0;
  color: var(--color-text-tertiary);
  font-weight: 600;
}

.monitor-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.monitor-item {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border-radius: var(--radius-md);
  background: rgba(2, 6, 23, 0.02);
  border: 1px solid rgba(2, 6, 23, 0.06);
}

.monitor-left {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.monitor-label {
  font-weight: 700;
  color: var(--color-text-secondary);
}

.source-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.source-panel .source-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.source-card {
  border-radius: var(--radius-md);
  border-top: 3px solid var(--sbm-accent, var(--p-primary-500));
}

.source-title {
  font-weight: 700;
  color: var(--color-text-secondary);
  margin-bottom: 6px;
}

.source-value {
  font-size: 20px;
  font-weight: 900;
  color: var(--color-text-primary);
}

@media (max-width: 1100px) {
  .hero {
    grid-template-columns: 1fr;
  }

  .stats-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .grid {
    grid-template-columns: 1fr;
  }
  .col-span-2 {
    grid-column: auto;
  }
  .source-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
