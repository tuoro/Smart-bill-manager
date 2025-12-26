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
            <el-button type="success" :icon="Upload" @click="uploadScreenshotModalVisible = true">
              上传截图
            </el-button>
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
        <el-table-column label="关联发票" width="100">
          <template #default="{ row }">
            <el-button 
              type="primary" 
              link 
              @click="viewLinkedInvoices(row)"
            >
              查看 ({{ linkedInvoicesCount[row.id] || 0 }})
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button type="primary" link :icon="View" @click="openPaymentDetail(row)" />
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

    <!-- Upload Screenshot Modal -->
    <el-dialog
      v-model="uploadScreenshotModalVisible"
      title="上传支付截图"
      width="600px"
      destroy-on-close
    >
      <el-upload
        ref="uploadRef"
        v-model:file-list="screenshotFileList"
        class="upload-area"
        drag
        accept=".jpg,.jpeg,.png"
        :auto-upload="false"
        :limit="1"
        :on-change="handleScreenshotChange"
        :before-upload="beforeScreenshotUpload"
      >
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text">
          点击或拖拽文件到此区域上传
        </div>
        <template #tip>
          <div class="el-upload__tip">
            支持格式：JPG、JPEG、PNG，最大文件大小：10MB
          </div>
        </template>
      </el-upload>

      <!-- OCR Result Preview -->
      <el-card v-if="ocrResult" class="ocr-result" shadow="never">
        <template #header>
          <span>识别结果（请确认或修改）</span>
        </template>
        <el-form
          ref="ocrFormRef"
          :model="ocrForm"
          :rules="rules"
          label-width="80px"
        >
          <el-form-item label="金额" prop="amount">
            <el-input-number
              v-model="ocrForm.amount"
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
            <el-input v-model="ocrForm.merchant" placeholder="请输入商家名称" />
          </el-form-item>

          <el-form-item label="支付方式" prop="payment_method">
            <el-select v-model="ocrForm.payment_method" placeholder="请选择支付方式" clearable style="width: 100%">
              <el-option v-for="m in PAYMENT_METHODS" :key="m" :label="m" :value="m" />
            </el-select>
          </el-form-item>

          <el-form-item label="订单号">
            <el-input v-model="ocrForm.order_number" placeholder="订单号" disabled />
          </el-form-item>

          <el-form-item label="交易时间" prop="transaction_time">
            <el-date-picker
              v-model="ocrForm.transaction_time"
              type="datetime"
              placeholder="请选择交易时间"
              style="width: 100%"
            />
          </el-form-item>

          <el-form-item label="分类" prop="category">
            <el-select v-model="ocrForm.category" placeholder="请选择分类" clearable style="width: 100%">
              <el-option v-for="c in CATEGORIES" :key="c" :label="c" :value="c" />
            </el-select>
          </el-form-item>

          <el-form-item label="备注" prop="description">
            <el-input v-model="ocrForm.description" type="textarea" :rows="2" placeholder="请输入备注" />
          </el-form-item>
        </el-form>

        <!-- OCR Raw Text Section -->
        <el-divider v-if="ocrResult?.raw_text" />
        <div v-if="ocrResult?.raw_text" class="ocr-raw-text-section">
          <el-collapse>
            <el-collapse-item title="点击查看 OCR 识别的原始文本" name="1">
              <pre class="raw-text">{{ ocrResult.raw_text }}</pre>
            </el-collapse-item>
          </el-collapse>
        </div>
      </el-card>

      <template #footer>
        <el-button @click="cancelScreenshotUpload">取消</el-button>
        <el-button 
          v-if="!ocrResult"
          type="primary" 
          :loading="uploadingScreenshot"
          :disabled="screenshotFileList.length === 0"
          @click="handleScreenshotUpload"
        >
          识别
        </el-button>
        <el-button 
          v-else
          type="primary" 
          :loading="savingOcrResult"
          @click="handleSaveOcrResult"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    <!-- Linked Invoices Modal -->
    <el-dialog
      v-model="linkedInvoicesModalVisible"
      title="关联的发票"
      width="800px"
      destroy-on-close
    >
      <el-table 
        v-loading="loadingLinkedInvoices"
        :data="linkedInvoices"
        max-height="400px"
      >
        <el-table-column label="文件名" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="filename">
              <el-icon color="#1890ff"><Document /></el-icon>
              {{ row.original_name }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="invoice_number" label="发票号码">
          <template #default="{ row }">
            {{ row.invoice_number || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="金额">
          <template #default="{ row }">
            <span v-if="row.amount" class="amount">¥{{ row.amount.toFixed(2) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="seller_name" label="销售方" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.seller_name || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="开票日期">
          <template #default="{ row }">
            {{ row.invoice_date || '-' }}
          </template>
        </el-table-column>
      </el-table>

      <template #footer>
        <el-button @click="linkedInvoicesModalVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- Payment Detail Modal -->
    <el-dialog
      v-model="paymentDetailVisible"
      title="支付记录详情"
      width="700px"
      destroy-on-close
    >
      <el-descriptions v-if="detailPayment" :column="2" border>
        <el-descriptions-item label="金额" :span="2">
          <span class="amount">¥{{ detailPayment.amount?.toFixed(2) || '0.00' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="商家">
          {{ detailPayment.merchant || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="支付方式">
          <el-tag v-if="detailPayment.payment_method" type="success">
            {{ detailPayment.payment_method }}
          </el-tag>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="分类">
          <el-tag v-if="detailPayment.category" type="primary">
            {{ detailPayment.category }}
          </el-tag>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="交易时间">
          {{ formatDateTime(detailPayment.transaction_time) }}
        </el-descriptions-item>
        <el-descriptions-item label="备注" :span="2">
          {{ detailPayment.description || '-' }}
        </el-descriptions-item>
        
        <!-- 截图预览 -->
        <el-descriptions-item v-if="detailPayment.screenshot_path" label="支付截图" :span="2">
          <el-image 
            :src="`${FILE_BASE_URL}/${detailPayment.screenshot_path}`"
            :preview-src-list="[`${FILE_BASE_URL}/${detailPayment.screenshot_path}`]"
            fit="contain"
            style="max-width: 300px; max-height: 400px; cursor: pointer;"
          />
        </el-descriptions-item>
        
        <!-- OCR 原始文本 -->
        <el-descriptions-item v-if="detailPayment.extracted_data" label="OCR原始文本" :span="2">
          <div class="ocr-section">
            <el-button 
              type="primary" 
              link 
              :icon="Refresh"
              :loading="reparsingOcr"
              @click="handleReparseOcr(detailPayment.id)"
            >
              重新解析
            </el-button>
            <el-collapse>
              <el-collapse-item title="点击查看 OCR 识别的原始文本" name="1">
                <pre class="raw-text">{{ getExtractedRawText(detailPayment.extracted_data) }}</pre>
              </el-collapse-item>
            </el-collapse>
          </div>
        </el-descriptions-item>
      </el-descriptions>

      <template #footer>
        <el-button @click="paymentDetailVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance, type FormRules, type UploadFile, type UploadRawFile } from 'element-plus'
import { Plus, Edit, Delete, Wallet, ShoppingCart, Upload, UploadFilled, Document, View, Refresh } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { paymentApi, FILE_BASE_URL } from '@/api'
import type { Payment, Invoice } from '@/types'

// Interface for OCR extracted data
interface OcrExtractedData {
  amount?: number
  merchant?: string
  transaction_time?: string
  payment_method?: string
  order_number?: string
  raw_text?: string
}

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

// Screenshot upload state
const uploadScreenshotModalVisible = ref(false)
const uploadingScreenshot = ref(false)
const savingOcrResult = ref(false)
const screenshotFileList = ref<UploadFile[]>([])
const ocrResult = ref<OcrExtractedData | null>(null)
const ocrFormRef = ref<FormInstance>()
const uploadedPaymentId = ref<string | null>(null)

// Linked invoices state
const linkedInvoicesModalVisible = ref(false)
const loadingLinkedInvoices = ref(false)
const linkedInvoices = ref<Invoice[]>([])
const linkedInvoicesCount = ref<Record<string, number>>({})
const currentPaymentForInvoices = ref<Payment | null>(null)

// Payment detail state
const paymentDetailVisible = ref(false)
const detailPayment = ref<Payment | null>(null)
const reparsingOcr = ref(false)

const form = reactive({
  amount: 0,
  merchant: '',
  category: '',
  payment_method: '',
  description: '',
  transaction_time: new Date()
})

const ocrForm = reactive({
  amount: 0,
  merchant: '',
  category: '',
  payment_method: '',
  description: '',
  transaction_time: new Date(),
  order_number: ''
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

// Load linked invoices count for all payments
const loadLinkedInvoicesCount = async () => {
  try {
    const counts: Record<string, number> = {}
    for (const payment of payments.value) {
      try {
        const res = await paymentApi.getPaymentInvoices(payment.id)
        if (res.data.success && res.data.data) {
          counts[payment.id] = res.data.data.length
        }
      } catch {
        counts[payment.id] = 0
      }
    }
    linkedInvoicesCount.value = counts
  } catch (error) {
    console.error('Load linked invoices count failed:', error)
  }
}

// Screenshot upload functions
const handleScreenshotChange = (_file: UploadFile, uploadFiles: UploadFile[]) => {
  screenshotFileList.value = uploadFiles
}

const beforeScreenshotUpload = (rawFile: UploadRawFile) => {
  const validTypes = ['image/jpeg', 'image/jpg', 'image/png']
  if (!validTypes.includes(rawFile.type)) {
    ElMessage.error('只支持 JPG、JPEG、PNG 格式的图片')
    return false
  }
  const maxSize = 10 * 1024 * 1024 // 10MB
  if (rawFile.size > maxSize) {
    ElMessage.error('文件大小不能超过 10MB')
    return false
  }
  return true
}

const handleScreenshotUpload = async () => {
  if (screenshotFileList.value.length === 0) {
    ElMessage.warning('请选择文件')
    return
  }

  uploadingScreenshot.value = true
  try {
    const file = screenshotFileList.value[0].raw as File
    const res = await paymentApi.uploadScreenshot(file)
    if (res.data.success && res.data.data) {
      const { payment, extracted } = res.data.data
      
      // Save payment ID for later update
      uploadedPaymentId.value = payment.id
      
      ocrResult.value = extracted
      
      // Fill OCR form with extracted data
      ocrForm.amount = extracted.amount || 0
      ocrForm.merchant = extracted.merchant || ''
      ocrForm.payment_method = extracted.payment_method || ''
      ocrForm.order_number = extracted.order_number || ''
      ocrForm.transaction_time = extracted.transaction_time 
        ? new Date(extracted.transaction_time) 
        : new Date()
      ocrForm.category = ''
      ocrForm.description = ''
      
      ElMessage.success('截图识别成功，请确认或修改识别结果')
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    ElMessage.error(err.response?.data?.message || '截图识别失败')
  } finally {
    uploadingScreenshot.value = false
  }
}

const handleSaveOcrResult = async () => {
  if (!ocrFormRef.value) return

  await ocrFormRef.value.validate(async (valid) => {
    if (!valid) return

    savingOcrResult.value = true
    try {
      const payload = {
        amount: ocrForm.amount,
        merchant: ocrForm.merchant,
        category: ocrForm.category,
        payment_method: ocrForm.payment_method,
        description: ocrForm.description,
        transaction_time: dayjs(ocrForm.transaction_time).toISOString()
      }

      // Update the existing payment record instead of creating a new one
      if (uploadedPaymentId.value) {
        await paymentApi.update(uploadedPaymentId.value, payload)
        ElMessage.success('支付记录更新成功')
      } else {
        await paymentApi.create(payload)
        ElMessage.success('支付记录创建成功')
      }
      
      // Important: do not delete the payment record after saving
      resetScreenshotUploadState()
      loadPayments()
      loadStats()
      loadLinkedInvoicesCount()
    } catch {
      ElMessage.error('保存失败')
    } finally {
      savingOcrResult.value = false
    }
  })
}

const resetScreenshotUploadState = () => {
  uploadedPaymentId.value = null
  screenshotFileList.value = []
  ocrResult.value = null
  ocrForm.amount = 0
  ocrForm.merchant = ''
  ocrForm.category = ''
  ocrForm.payment_method = ''
  ocrForm.description = ''
  ocrForm.transaction_time = new Date()
  ocrForm.order_number = ''
  uploadScreenshotModalVisible.value = false
}

const cancelScreenshotUpload = () => {
  // If user cancels after uploading, delete the created payment record
  if (uploadedPaymentId.value) {
    paymentApi.delete(uploadedPaymentId.value).catch((error) => {
      // Log deletion errors for debugging, but don't show to user
      console.error('Failed to delete payment record:', error)
    })
  }
  resetScreenshotUploadState()
}

// Linked invoices functions
const viewLinkedInvoices = async (payment: Payment) => {
  currentPaymentForInvoices.value = payment
  linkedInvoicesModalVisible.value = true
  loadingLinkedInvoices.value = true
  
  try {
    const res = await paymentApi.getPaymentInvoices(payment.id)
    if (res.data.success && res.data.data) {
      linkedInvoices.value = res.data.data
    }
  } catch {
    ElMessage.error('加载关联发票失败')
  } finally {
    loadingLinkedInvoices.value = false
  }
}

// Payment detail functions
const openPaymentDetail = (payment: Payment) => {
  detailPayment.value = payment
  paymentDetailVisible.value = true
}

const getExtractedRawText = (extractedData: string | null): string => {
  if (!extractedData) return ''
  try {
    const data = JSON.parse(extractedData)
    return data.raw_text || ''
  } catch {
    return extractedData
  }
}

const handleReparseOcr = async (paymentId: string) => {
  reparsingOcr.value = true
  try {
    const res = await paymentApi.reparseScreenshot(paymentId)
    if (res.data.success) {
      ElMessage.success('重新解析成功')
      // Refresh detail data
      const detailRes = await paymentApi.getById(paymentId)
      if (detailRes.data.success && detailRes.data.data) {
        detailPayment.value = detailRes.data.data
      }
      // Refresh list
      loadPayments()
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    ElMessage.error(err.response?.data?.message || '重新解析失败')
  } finally {
    reparsingOcr.value = false
  }
}

// Load linked invoices count when payments are loaded
const loadPaymentsWithCount = async () => {
  await loadPayments()
  await loadLinkedInvoicesCount()
}

onMounted(() => {
  loadPaymentsWithCount()
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

/* Upload area styles */
.upload-area {
  width: 100%;
}

.upload-area :deep(.el-upload) {
  width: 100%;
}

.upload-area :deep(.el-upload-dragger) {
  width: 100%;
  border-radius: var(--radius-lg);
  border: 2px dashed rgba(102, 126, 234, 0.3);
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.03), rgba(118, 75, 162, 0.03));
  transition: all var(--transition-base);
}

.upload-area :deep(.el-upload-dragger:hover) {
  border-color: #667eea;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.08), rgba(118, 75, 162, 0.08));
  transform: translateY(-2px);
}

.upload-area :deep(.el-upload-dragger .el-icon) {
  color: #667eea;
  font-size: 48px;
  transition: all var(--transition-base);
}

.upload-area :deep(.el-upload-dragger:hover .el-icon) {
  transform: scale(1.1);
}

.upload-area :deep(.el-upload__text) {
  color: var(--color-text-secondary);
  font-size: 14px;
}

.upload-area :deep(.el-upload__tip) {
  margin-top: 8px;
  color: var(--color-text-tertiary);
  font-size: 13px;
}

/* OCR result card */
.ocr-result {
  margin-top: 16px;
  border: 1px solid rgba(102, 126, 234, 0.2);
}

.ocr-result :deep(.el-card__header) {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.05), rgba(118, 75, 162, 0.05));
  font-weight: 600;
  color: #667eea;
}

.filename {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
  color: var(--color-text-primary);
}

.filename :deep(.el-icon) {
  transition: transform var(--transition-base);
}

.filename:hover :deep(.el-icon) {
  transform: scale(1.1);
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

/* OCR raw text section */
.ocr-raw-text-section {
  margin-top: 16px;
}

.raw-text {
  white-space: pre-wrap;
  word-wrap: break-word;
  max-height: 300px;
  overflow-y: auto;
  background: #f5f5f5;
  padding: 12px;
  border-radius: var(--radius-md);
  font-size: 12px;
  line-height: 1.6;
  font-family: var(--font-mono);
}

.ocr-section {
  width: 100%;
}

.ocr-section .el-button {
  margin-bottom: 8px;
}
</style>
