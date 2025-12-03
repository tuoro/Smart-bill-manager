<template>
  <div>
    <!-- Statistics Cards -->
    <el-row :gutter="16" class="stats-row">
      <el-col :xs="24" :sm="8">
        <el-card>
          <el-statistic title="总支出" :value="stats?.totalAmount || 0" :precision="2">
            <template #prefix>
              <el-icon color="#cf1322"><Wallet /></el-icon>
            </template>
            <template #suffix>¥</template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card>
          <el-statistic title="交易笔数" :value="stats?.totalCount || 0">
            <template #prefix>
              <el-icon color="#3f8600"><ShoppingCart /></el-icon>
            </template>
            <template #suffix>笔</template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card>
          <el-statistic 
            title="平均每笔" 
            :value="stats?.totalCount ? (stats.totalAmount / stats.totalCount) : 0"
            :precision="2"
          >
            <template #suffix>¥</template>
          </el-statistic>
        </el-card>
      </el-col>
    </el-row>

    <!-- Payment List Card -->
    <el-card>
      <template #header>
        <div class="card-header">
          <span>支付记录</span>
          <div class="header-controls">
            <el-date-picker
              v-model="dateRange"
              type="daterange"
              range-separator="至"
              start-placeholder="开始日期"
              end-placeholder="结束日期"
              value-format="YYYY-MM-DD"
              @change="handleDateChange"
            />
            <el-select 
              v-model="categoryFilter" 
              placeholder="选择分类" 
              clearable
              style="width: 120px"
              @change="handleFilterChange"
            >
              <el-option v-for="c in CATEGORIES" :key="c" :label="c" :value="c" />
            </el-select>
            <el-button type="primary" :icon="Plus" @click="openModal()">
              添加记录
            </el-button>
          </div>
        </div>
      </template>

      <el-table 
        v-loading="loading"
        :data="payments"
        :default-sort="{ prop: 'transaction_time', order: 'descending' }"
      >
        <el-table-column label="金额" sortable prop="amount">
          <template #default="{ row }">
            <span class="amount">¥{{ row.amount.toFixed(2) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="merchant" label="商家" show-overflow-tooltip />
        <el-table-column label="分类">
          <template #default="{ row }">
            <el-tag v-if="row.category" type="primary">{{ row.category }}</el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="支付方式">
          <template #default="{ row }">
            <el-tag v-if="row.payment_method" type="success">{{ row.payment_method }}</el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="备注" show-overflow-tooltip />
        <el-table-column label="交易时间" sortable prop="transaction_time">
          <template #default="{ row }">
            {{ formatDateTime(row.transaction_time) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button type="primary" link :icon="Edit" @click="openModal(row)" />
            <el-popconfirm
              title="确定删除这条记录吗？"
              @confirm="handleDelete(row.id)"
            >
              <template #reference>
                <el-button type="danger" link :icon="Delete" />
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="payments.length"
        layout="total, sizes, prev, pager, next"
        class="pagination"
      />
    </el-card>

    <!-- Add/Edit Modal -->
    <el-dialog
      v-model="modalVisible"
      :title="editingPayment ? '编辑支付记录' : '添加支付记录'"
      width="500px"
      destroy-on-close
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="80px"
      >
        <el-form-item label="金额" prop="amount">
          <el-input-number
            v-model="form.amount"
            :min="0"
            :precision="2"
            :controls="false"
            style="width: 100%"
            placeholder="请输入金额"
          >
            <template #prefix>¥</template>
          </el-input-number>
        </el-form-item>

        <el-form-item label="商家" prop="merchant">
          <el-input v-model="form.merchant" placeholder="请输入商家名称" />
        </el-form-item>

        <el-form-item label="分类" prop="category">
          <el-select v-model="form.category" placeholder="请选择分类" clearable style="width: 100%">
            <el-option v-for="c in CATEGORIES" :key="c" :label="c" :value="c" />
          </el-select>
        </el-form-item>

        <el-form-item label="支付方式" prop="payment_method">
          <el-select v-model="form.payment_method" placeholder="请选择支付方式" clearable style="width: 100%">
            <el-option v-for="m in PAYMENT_METHODS" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>

        <el-form-item label="备注" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="请输入备注" />
        </el-form-item>

        <el-form-item label="交易时间" prop="transaction_time">
          <el-date-picker
            v-model="form.transaction_time"
            type="datetime"
            placeholder="请选择交易时间"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="modalVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">
          {{ editingPayment ? '更新' : '添加' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Edit, Delete, Wallet, ShoppingCart } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { paymentApi } from '@/api'
import type { Payment } from '@/types'

const CATEGORIES = ['餐饮', '交通', '购物', '娱乐', '住房', '医疗', '教育', '通讯', '其他']
const PAYMENT_METHODS = ['微信支付', '支付宝', '银行卡', '现金', '信用卡', '其他']

const loading = ref(false)
const payments = ref<Payment[]>([])
const modalVisible = ref(false)
const editingPayment = ref<Payment | null>(null)
const formRef = ref<FormInstance>()

const currentPage = ref(1)
const pageSize = ref(10)
const dateRange = ref<[string, string] | null>(null)
const categoryFilter = ref<string | null>(null)

const stats = ref<{
  totalAmount: number
  totalCount: number
  categoryStats: Record<string, number>
} | null>(null)

const form = reactive({
  amount: 0,
  merchant: '',
  category: '',
  payment_method: '',
  description: '',
  transaction_time: new Date()
})

const rules: FormRules = {
  amount: [{ required: true, message: '请输入金额', trigger: 'blur' }],
  transaction_time: [{ required: true, message: '请选择交易时间', trigger: 'change' }]
}

const loadPayments = async () => {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (dateRange.value) {
      params.startDate = dayjs(dateRange.value[0]).startOf('day').toISOString()
      params.endDate = dayjs(dateRange.value[1]).endOf('day').toISOString()
    }
    if (categoryFilter.value) {
      params.category = categoryFilter.value
    }
    const res = await paymentApi.getAll(params)
    if (res.data.success && res.data.data) {
      payments.value = res.data.data
    }
  } catch {
    ElMessage.error('加载支付记录失败')
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const startDate = dateRange.value ? dayjs(dateRange.value[0]).startOf('day').toISOString() : undefined
    const endDate = dateRange.value ? dayjs(dateRange.value[1]).endOf('day').toISOString() : undefined
    const res = await paymentApi.getStats(startDate, endDate)
    if (res.data.success && res.data.data) {
      stats.value = res.data.data
    }
  } catch (error) {
    console.error('Load stats failed:', error)
  }
}

const openModal = (payment?: Payment) => {
  if (payment) {
    editingPayment.value = payment
    form.amount = payment.amount
    form.merchant = payment.merchant || ''
    form.category = payment.category || ''
    form.payment_method = payment.payment_method || ''
    form.description = payment.description || ''
    form.transaction_time = new Date(payment.transaction_time)
  } else {
    editingPayment.value = null
    form.amount = 0
    form.merchant = ''
    form.category = ''
    form.payment_method = ''
    form.description = ''
    form.transaction_time = new Date()
  }
  modalVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    try {
      const payload = {
        amount: form.amount,
        merchant: form.merchant,
        category: form.category,
        payment_method: form.payment_method,
        description: form.description,
        transaction_time: dayjs(form.transaction_time).toISOString()
      }

      if (editingPayment.value) {
        await paymentApi.update(editingPayment.value.id, payload)
        ElMessage.success('支付记录更新成功')
      } else {
        await paymentApi.create(payload)
        ElMessage.success('支付记录创建成功')
      }

      modalVisible.value = false
      loadPayments()
      loadStats()
    } catch {
      ElMessage.error('操作失败')
    }
  })
}

const handleDelete = async (id: string) => {
  try {
    await paymentApi.delete(id)
    ElMessage.success('删除成功')
    loadPayments()
    loadStats()
  } catch {
    ElMessage.error('删除失败')
  }
}

const handleDateChange = () => {
  loadPayments()
  loadStats()
}

const handleFilterChange = () => {
  loadPayments()
}

const formatDateTime = (date: string) => {
  return dayjs(date).format('YYYY-MM-DD HH:mm')
}

onMounted(() => {
  loadPayments()
  loadStats()
})
</script>

<style scoped>
.stats-row {
  margin-bottom: 16px;
}

.stats-row :deep(.el-card) {
  transition: all var(--transition-base);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}

.stats-row :deep(.el-card:hover) {
  transform: translateY(-2px);
  box-shadow: var(--shadow-md);
}

.stats-row :deep(.el-statistic__head) {
  font-weight: 500;
  color: var(--color-text-secondary);
  margin-bottom: 8px;
}

.stats-row :deep(.el-statistic__content) {
  font-weight: 700;
}

.stats-row :deep(.el-icon) {
  transition: transform var(--transition-base);
}

.stats-row :deep(.el-card:hover .el-icon) {
  transform: scale(1.1);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.header-controls {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.header-controls :deep(.el-date-editor) {
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-base);
}

.header-controls :deep(.el-date-editor:hover) {
  box-shadow: var(--shadow-md);
}

.header-controls :deep(.el-select) {
  border-radius: var(--radius-md);
}

/* Enhanced table styles */
:deep(.el-table) {
  border-radius: var(--radius-lg);
  overflow: hidden;
}

:deep(.el-table thead) {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.06), rgba(118, 75, 162, 0.06));
}

:deep(.el-table th) {
  background: transparent !important;
  font-weight: 600;
  color: var(--color-text-primary);
  border-bottom: 2px solid rgba(102, 126, 234, 0.1);
}

:deep(.el-table td) {
  border-bottom: 1px solid rgba(0, 0, 0, 0.04);
}

/* Zebra striping */
:deep(.el-table tbody tr:nth-child(even)) {
  background: rgba(0, 0, 0, 0.02);
}

/* Row hover effect */
:deep(.el-table tbody tr) {
  transition: all var(--transition-fast);
}

:deep(.el-table tbody tr:hover) {
  background: linear-gradient(90deg, rgba(102, 126, 234, 0.08), rgba(118, 75, 162, 0.08)) !important;
  transform: scale(1.002);
  box-shadow: 0 2px 8px rgba(102, 126, 234, 0.1);
}

.amount {
  color: #f5222d;
  font-weight: bold;
  font-size: 15px;
  font-family: var(--font-mono);
}

/* Enhanced tags */
:deep(.el-tag) {
  border-radius: var(--radius-sm);
  font-weight: 500;
  border: none;
  padding: 4px 10px;
  transition: all var(--transition-base);
}

:deep(.el-tag:hover) {
  transform: translateY(-1px);
  box-shadow: var(--shadow-sm);
}

:deep(.el-tag.el-tag--primary) {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.15), rgba(118, 75, 162, 0.15));
  color: #667eea;
}

:deep(.el-tag.el-tag--success) {
  background: linear-gradient(135deg, rgba(67, 233, 123, 0.15), rgba(56, 249, 215, 0.15));
  color: #43e97b;
}

/* Button enhancements */
:deep(.el-button) {
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

:deep(.el-button.is-link) {
  transition: all var(--transition-fast);
}

:deep(.el-button.is-link:hover) {
  transform: scale(1.1);
}

.pagination {
  margin-top: 20px;
  justify-content: flex-end;
}

:deep(.el-pagination) {
  gap: 8px;
}

:deep(.el-pagination button),
:deep(.el-pagination .el-pager li) {
  border-radius: var(--radius-sm);
  transition: all var(--transition-base);
}

:deep(.el-pagination button:hover),
:deep(.el-pagination .el-pager li:hover) {
  transform: translateY(-1px);
}

:deep(.el-pagination .el-pager li.is-active) {
  background: linear-gradient(135deg, #667eea, #764ba2);
  color: white;
  box-shadow: 0 2px 8px rgba(102, 126, 234, 0.4);
}

/* Modal enhancements */
:deep(.el-dialog) {
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-xl);
}

:deep(.el-dialog__header) {
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  padding: 20px 24px;
}

:deep(.el-dialog__title) {
  font-weight: 600;
  font-size: 18px;
  background: linear-gradient(135deg, #667eea, #764ba2);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

:deep(.el-dialog__body) {
  padding: 24px;
}

:deep(.el-dialog__footer) {
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  padding: 16px 24px;
}

/* Form enhancements */
:deep(.el-form-item__label) {
  font-weight: 500;
  color: var(--color-text-primary);
}

:deep(.el-input__wrapper),
:deep(.el-textarea__inner),
:deep(.el-input-number),
:deep(.el-select .el-input__wrapper) {
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

:deep(.el-input__wrapper:hover),
:deep(.el-textarea__inner:hover) {
  box-shadow: var(--shadow-sm);
}

:deep(.el-input__wrapper.is-focus),
:deep(.el-textarea__inner:focus) {
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

:deep(.el-input-number) {
  width: 100%;
}

:deep(.el-input-number .el-input__wrapper) {
  padding-left: 30px;
}

/* Popconfirm enhancement */
:deep(.el-popconfirm__main) {
  margin-bottom: 12px;
}

/* Card enhancement */
:deep(.el-card) {
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-base);
}

:deep(.el-card__header) {
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  padding: 18px 20px;
  font-weight: 600;
}

/* Loading state */
:deep(.el-loading-mask) {
  border-radius: var(--radius-lg);
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
}

@media (max-width: 768px) {
  .header-controls {
    width: 100%;
  }
  
  .header-controls > * {
    flex: 1;
    min-width: 0;
  }
  
  :deep(.el-table) {
    font-size: 13px;
  }
  
  .amount {
    font-size: 14px;
  }
}
</style>
