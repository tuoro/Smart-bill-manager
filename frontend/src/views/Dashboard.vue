<template>
  <div v-loading="loading" element-loading-text="加载中...">
    <el-empty v-if="!loading && !data" description="暂无数据" />
    
    <template v-else-if="data">
      <!-- Statistics Cards -->
      <el-row :gutter="16" class="stats-row">
        <el-col :xs="24" :sm="12" :lg="6">
          <el-card class="stat-card gradient-purple">
            <el-statistic title="本月支出" :value="data.payments.totalThisMonth" :precision="2">
              <template #prefix>
                <el-icon><Wallet /></el-icon>
              </template>
              <template #suffix>¥</template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="12" :lg="6">
          <el-card class="stat-card gradient-pink">
            <el-statistic title="支付笔数" :value="data.payments.countThisMonth">
              <template #prefix>
                <el-icon><TrendCharts /></el-icon>
              </template>
              <template #suffix>笔</template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="12" :lg="6">
          <el-card class="stat-card gradient-blue">
            <el-statistic title="发票总数" :value="data.invoices.totalCount">
              <template #prefix>
                <el-icon><Document /></el-icon>
              </template>
              <template #suffix>张</template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :xs="24" :sm="12" :lg="6">
          <el-card class="stat-card gradient-green">
            <el-statistic title="发票金额" :value="data.invoices.totalAmount" :precision="2">
              <template #prefix>
                <el-icon><Document /></el-icon>
              </template>
              <template #suffix>¥</template>
            </el-statistic>
          </el-card>
        </el-col>
      </el-row>

      <!-- Charts Row -->
      <el-row :gutter="16" class="charts-row">
        <el-col :xs="24" :lg="16">
          <el-card>
            <template #header>
              <div class="card-header">
                <span>每日支出趋势</span>
                <el-button text @click="loadData">刷新</el-button>
              </div>
            </template>
            <div v-if="dailyData.length > 0" class="chart-container">
              <v-chart :option="lineChartOption" autoresize />
            </div>
            <el-empty v-else description="暂无数据" :image-size="100" />
          </el-card>
        </el-col>
        <el-col :xs="24" :lg="8">
          <el-card class="pie-card">
            <template #header>
              <span>支出分类</span>
            </template>
            <div v-if="categoryData.length > 0" class="chart-container">
              <v-chart :option="pieChartOption" autoresize />
            </div>
            <el-empty v-else description="暂无数据" :image-size="100" />
          </el-card>
        </el-col>
      </el-row>

      <!-- Email Status and Logs Row -->
      <el-row :gutter="16" class="status-row">
        <el-col :xs="24" :lg="12">
          <el-card>
            <template #header>
              <span><el-icon><Message /></el-icon> 邮箱监控状态</span>
            </template>
            <div v-if="data.email.monitoringStatus.length > 0">
              <div v-for="(item, index) in data.email.monitoringStatus" :key="index" class="monitor-item">
                <span class="monitor-label">邮箱 {{ index + 1 }}:</span>
                <el-tag v-if="item.status === 'running'" type="success">
                  <el-icon><CircleCheck /></el-icon> 运行中
                </el-tag>
                <el-tag v-else type="info">
                  <el-icon><CircleClose /></el-icon> 已停止
                </el-tag>
                <el-progress 
                  :percentage="item.status === 'running' ? 100 : 0"
                  :status="item.status === 'running' ? undefined : 'exception'"
                  class="monitor-progress"
                />
              </div>
            </div>
            <el-empty v-else description="暂无配置邮箱" :image-size="60" />
          </el-card>
        </el-col>
        <el-col :xs="24" :lg="12">
          <el-card>
            <template #header>
              <span>最近邮件</span>
            </template>
            <el-table :data="data.email.recentLogs" size="small" :show-header="true">
              <el-table-column prop="subject" label="主题" show-overflow-tooltip />
              <el-table-column prop="from_address" label="发件人" width="150" show-overflow-tooltip />
              <el-table-column label="附件" width="80">
                <template #default="{ row }">
                  <el-tag v-if="row.has_attachment" type="primary" size="small">{{ row.attachment_count }}个</el-tag>
                  <el-tag v-else size="small">无</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="时间" width="100">
                <template #default="{ row }">
                  {{ row.received_date ? formatDate(row.received_date) : '-' }}
                </template>
              </el-table-column>
            </el-table>
          </el-card>
        </el-col>
      </el-row>

      <!-- Invoice Source Distribution -->
      <el-row :gutter="16" class="source-row">
        <el-col :span="24">
          <el-card>
            <template #header>
              <span>发票来源分布</span>
            </template>
            <el-row :gutter="16" v-if="Object.keys(data.invoices.bySource || {}).length > 0">
              <el-col 
                v-for="(count, source, index) in data.invoices.bySource" 
                :key="source"
                :xs="12" :sm="8" :md="6"
              >
                <el-card 
                  class="source-card"
                  :style="{ background: `${COLORS[index % COLORS.length]}15` }"
                  shadow="never"
                >
                  <el-statistic 
                    :title="getSourceLabel(source as string)" 
                    :value="count"
                  >
                    <template #suffix>张</template>
                  </el-statistic>
                </el-card>
              </el-col>
            </el-row>
            <el-empty v-else description="暂无发票" :image-size="60" />
          </el-card>
        </el-col>
      </el-row>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, PieChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import VChart from 'vue-echarts'
import dayjs from 'dayjs'
import { Wallet, Document, TrendCharts, Message, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import { dashboardApi } from '@/api'
import { CHART_COLORS } from '@/utils/constants'
import type { DashboardData } from '@/types'

// Register ECharts components
use([CanvasRenderer, LineChart, PieChart, GridComponent, TooltipComponent, LegendComponent])

const COLORS = CHART_COLORS

const loading = ref(true)
const data = ref<DashboardData | null>(null)

const dailyData = computed(() => {
  if (!data.value?.payments.dailyStats) return []
  return Object.entries(data.value.payments.dailyStats)
    .map(([date, amount]) => ({
      date: dayjs(date).format('MM-DD'),
      amount
    }))
    .sort((a, b) => a.date.localeCompare(b.date))
})

const categoryData = computed(() => {
  if (!data.value?.payments.categoryStats) return []
  return Object.entries(data.value.payments.categoryStats).map(([name, value]) => ({
    name,
    value
  }))
})

const lineChartOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    formatter: (params: { name: string; value: number }[]) => {
      const item = params[0]
      return `${item.name}<br/>支出: ¥${item.value.toFixed(2)}`
    }
  },
  grid: {
    left: '3%',
    right: '4%',
    bottom: '3%',
    containLabel: true
  },
  xAxis: {
    type: 'category',
    data: dailyData.value.map(d => d.date),
    boundaryGap: false
  },
  yAxis: {
    type: 'value'
  },
  series: [{
    data: dailyData.value.map(d => d.amount),
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
          { offset: 1, color: 'rgba(24, 144, 255, 0.05)' }
        ]
      }
    },
    lineStyle: {
      color: '#1890ff',
      width: 2
    },
    itemStyle: {
      color: '#1890ff'
    }
  }]
}))

const pieChartOption = computed(() => ({
  tooltip: {
    trigger: 'item',
    formatter: '{b}: ¥{c} ({d}%)'
  },
  series: [{
    type: 'pie',
    radius: ['40%', '70%'],
    avoidLabelOverlap: false,
    label: {
      show: true,
      formatter: '{b} {d}%'
    },
    labelLine: {
      show: true
    },
    data: categoryData.value.map((item, index) => ({
      ...item,
      itemStyle: { color: COLORS[index % COLORS.length] }
    }))
  }]
}))

const loadData = async () => {
  loading.value = true
  try {
    const res = await dashboardApi.getSummary()
    if (res.data.success && res.data.data) {
      data.value = res.data.data
    }
  } catch (error) {
    console.error('Failed to load dashboard data:', error)
  } finally {
    loading.value = false
  }
}

const formatDate = (date: string) => {
  return dayjs(date).format('MM-DD HH:mm')
}

const getSourceLabel = (source: string) => {
  const labels: Record<string, string> = {
    upload: '手动上传',
    email: '邮件下载',
    dingtalk: '钉钉机器人'
  }
  return labels[source] || source
}

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.stats-row {
  margin-bottom: 16px;
}

.stat-card {
  color: white;
  border: none;
  transition: all var(--transition-base);
  position: relative;
  overflow: hidden;
}

.stat-card::before {
  content: '';
  position: absolute;
  top: -50%;
  right: -50%;
  width: 200%;
  height: 200%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.1), transparent 70%);
  opacity: 0;
  transition: opacity var(--transition-base);
}

.stat-card:hover::before {
  opacity: 1;
}

.stat-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 12px 24px rgba(0, 0, 0, 0.2);
}

.stat-card :deep(.el-statistic__head) {
  color: rgba(255, 255, 255, 0.9);
  font-weight: 500;
  font-size: 14px;
  margin-bottom: 8px;
}

.stat-card :deep(.el-statistic__content) {
  color: white;
  font-weight: 700;
}

.stat-card :deep(.el-icon) {
  transition: transform var(--transition-base);
  font-size: 20px;
}

.stat-card:hover :deep(.el-icon) {
  transform: scale(1.1) rotate(5deg);
}

.gradient-purple {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  box-shadow: 0 4px 15px rgba(102, 126, 234, 0.4);
}

.gradient-purple:hover {
  box-shadow: 0 8px 25px rgba(102, 126, 234, 0.5);
}

.gradient-pink {
  background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
  box-shadow: 0 4px 15px rgba(240, 147, 251, 0.4);
}

.gradient-pink:hover {
  box-shadow: 0 8px 25px rgba(240, 147, 251, 0.5);
}

.gradient-blue {
  background: linear-gradient(135deg, #4facfe 0%, #00f2fe 100%);
  box-shadow: 0 4px 15px rgba(79, 172, 254, 0.4);
}

.gradient-blue:hover {
  box-shadow: 0 8px 25px rgba(79, 172, 254, 0.5);
}

.gradient-green {
  background: linear-gradient(135deg, #43e97b 0%, #38f9d7 100%);
  box-shadow: 0 4px 15px rgba(67, 233, 123, 0.4);
}

.gradient-green:hover {
  box-shadow: 0 8px 25px rgba(67, 233, 123, 0.5);
}

.charts-row {
  margin-bottom: 16px;
}

.el-card {
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-base);
}

.el-card:not(.stat-card):hover {
  box-shadow: var(--shadow-md);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: var(--color-text-primary);
}

.card-header span {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-header :deep(.el-icon) {
  color: var(--color-primary);
}

.chart-container {
  height: 300px;
  position: relative;
}

.pie-card {
  height: 100%;
}

.status-row {
  margin-bottom: 16px;
}

.monitor-item {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
  padding: 12px;
  background: rgba(0, 0, 0, 0.02);
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

.monitor-item:hover {
  background: rgba(0, 0, 0, 0.04);
  transform: translateX(4px);
}

.monitor-label {
  margin-right: 8px;
  font-weight: 500;
  color: var(--color-text-secondary);
}

.monitor-progress {
  flex: 1;
  margin-left: 16px;
}

.source-row {
  margin-bottom: 16px;
}

.source-card {
  text-align: center;
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

.source-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-sm);
}

.source-card :deep(.el-statistic__number) {
  font-size: 20px;
  font-weight: 700;
}

.source-card :deep(.el-statistic__head) {
  font-weight: 500;
  margin-bottom: 8px;
}

/* Table enhancements */
:deep(.el-table) {
  border-radius: var(--radius-md);
  overflow: hidden;
}

:deep(.el-table thead) {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.05), rgba(118, 75, 162, 0.05));
}

:deep(.el-table th) {
  background: transparent !important;
  font-weight: 600;
  color: var(--color-text-primary);
}

:deep(.el-table tbody tr) {
  transition: all var(--transition-fast);
}

:deep(.el-table tbody tr:hover > td) {
  background: rgba(102, 126, 234, 0.05);
}

/* Tag enhancements */
:deep(.el-tag) {
  border-radius: var(--radius-sm);
  font-weight: 500;
  transition: all var(--transition-base);
}

:deep(.el-tag:hover) {
  transform: scale(1.05);
}

/* Progress bar enhancements */
:deep(.el-progress__text) {
  font-weight: 600;
}

/* Empty state */
:deep(.el-empty) {
  padding: 40px 0;
}

:deep(.el-empty__description) {
  color: var(--color-text-tertiary);
  font-weight: 500;
}

/* Loading animation */
:deep(.el-loading-spinner) {
  font-size: 32px;
}

/* Responsive */
@media (max-width: 768px) {
  .chart-container {
    height: 250px;
  }
  
  .stat-card :deep(.el-statistic__content) {
    font-size: 20px;
  }
}
</style>
