<template>
  <div>
    <!-- Statistics Cards -->
    <el-row :gutter="16" class="stats-row">
      <el-col :xs="24" :sm="8">
        <el-card>
          <el-statistic title="发票总数" :value="stats?.totalCount || 0">
            <template #prefix>
              <el-icon color="#1890ff"><Document /></el-icon>
            </template>
            <template #suffix>张</template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card>
          <el-statistic title="发票总金额" :value="stats?.totalAmount || 0" :precision="2">
            <template #suffix>¥</template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="8">
        <el-card>
          <div class="source-stats">
            <el-statistic title="手动上传" :value="stats?.bySource?.upload || 0">
              <template #suffix>张</template>
            </el-statistic>
            <el-statistic title="邮件下载" :value="stats?.bySource?.email || 0">
              <template #suffix>张</template>
            </el-statistic>
            <el-statistic title="钉钉机器人" :value="stats?.bySource?.dingtalk || 0">
              <template #suffix>张</template>
            </el-statistic>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Invoice List Card -->
    <el-card>
      <template #header>
        <div class="card-header">
          <span>发票列表</span>
          <el-button type="primary" :icon="Upload" @click="uploadModalVisible = true">
            上传发票
          </el-button>
        </div>
      </template>

      <el-table 
        v-loading="loading"
        :data="invoices"
        :default-sort="{ prop: 'created_at', order: 'descending' }"
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
        <el-table-column label="来源">
          <template #default="{ row }">
            <el-tag :type="getSourceType(row.source)">{{ getSourceLabel(row.source) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="上传时间" sortable prop="created_at">
          <template #default="{ row }">
            {{ formatDateTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button type="primary" link :icon="View" @click="openPreview(row)" />
            <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
            <el-popconfirm
              title="确定删除这张发票吗？"
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
        :total="invoices.length"
        layout="total, sizes, prev, pager, next"
        class="pagination"
      />
    </el-card>

    <!-- Upload Modal -->
    <el-dialog
      v-model="uploadModalVisible"
      title="上传发票"
      width="500px"
      destroy-on-close
    >
      <el-upload
        ref="uploadRef"
        v-model:file-list="fileList"
        class="upload-area"
        drag
        multiple
        accept=".pdf"
        :auto-upload="false"
        :on-change="handleFileChange"
        :before-upload="beforeUpload"
      >
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text">
          点击或拖拽文件到此区域上传
        </div>
        <template #tip>
          <div class="el-upload__tip">
            支持单个或批量上传PDF发票文件，系统将自动解析发票信息
          </div>
        </template>
      </el-upload>

      <template #footer>
        <el-button @click="cancelUpload">取消</el-button>
        <el-button 
          type="primary" 
          :loading="uploading"
          :disabled="fileList.length === 0"
          @click="handleUpload"
        >
          上传
        </el-button>
      </template>
    </el-dialog>

    <!-- Preview Modal -->
    <el-dialog
      v-model="previewVisible"
      title="发票详情"
      width="700px"
      destroy-on-close
    >
      <el-descriptions v-if="previewInvoice" :column="2" border>
        <el-descriptions-item label="文件名" :span="2">
          {{ previewInvoice.original_name }}
        </el-descriptions-item>
        <el-descriptions-item label="解析状态" :span="2">
          <el-tag 
            :type="getParseStatusType(previewInvoice.parse_status)"
            :icon="(previewInvoice.parse_status === 'parsing' || parseStatusPending) ? Loading : undefined"
          >
            {{ getParseStatusLabel(previewInvoice.parse_status) }}
          </el-tag>
          <el-button 
            v-if="previewInvoice.parse_status !== 'parsing'"
            type="primary" 
            link 
            :icon="Refresh"
            :loading="parseStatusPending"
            :disabled="parseStatusPending"
            @click="handleReparse(previewInvoice.id)"
            style="margin-left: 8px"
          >
            重新解析
          </el-button>
        </el-descriptions-item>
        <el-descriptions-item v-if="previewInvoice.parse_error" label="解析错误" :span="2">
          <el-text type="danger">{{ previewInvoice.parse_error }}</el-text>
        </el-descriptions-item>
        <el-descriptions-item label="发票号码">
          {{ previewInvoice.invoice_number || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="开票日期">
          {{ previewInvoice.invoice_date || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="金额">
          {{ previewInvoice.amount ? `¥${previewInvoice.amount.toFixed(2)}` : '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="文件大小">
          {{ previewInvoice.file_size ? `${(previewInvoice.file_size / 1024).toFixed(2)} KB` : '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="销售方" :span="2">
          {{ previewInvoice.seller_name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="购买方" :span="2">
          {{ previewInvoice.buyer_name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="来源">
          <el-tag :type="getSourceType(previewInvoice.source)">
            {{ getSourceLabel(previewInvoice.source) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="上传时间">
          {{ formatDateTime(previewInvoice.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item v-if="previewInvoice.raw_text" label="OCR原始文本" :span="2">
          <el-collapse>
            <el-collapse-item title="点击查看提取的原始文本" name="1">
              <pre class="raw-text">{{ previewInvoice.raw_text }}</pre>
            </el-collapse-item>
          </el-collapse>
        </el-descriptions-item>
        <el-descriptions-item label="预览" :span="2">
          <el-button type="primary" @click="downloadFile(previewInvoice)">
            查看PDF文件
          </el-button>
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, type UploadInstance, type UploadFile, type UploadRawFile } from 'element-plus'
import { Document, Upload, View, Download, Delete, UploadFilled, Refresh, Loading } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { invoiceApi, FILE_BASE_URL } from '@/api'
import type { Invoice } from '@/types'

const loading = ref(false)
const invoices = ref<Invoice[]>([])
const uploadModalVisible = ref(false)
const previewVisible = ref(false)
const previewInvoice = ref<Invoice | null>(null)
const uploading = ref(false)
const parseStatusPending = ref(false)
const fileList = ref<UploadFile[]>([])
const uploadRef = ref<UploadInstance>()

const currentPage = ref(1)
const pageSize = ref(10)

const stats = ref<{
  totalCount: number
  totalAmount: number
  bySource: Record<string, number>
} | null>(null)

const loadInvoices = async () => {
  loading.value = true
  try {
    const res = await invoiceApi.getAll()
    if (res.data.success && res.data.data) {
      invoices.value = res.data.data
    }
  } catch {
    ElMessage.error('加载发票列表失败')
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await invoiceApi.getStats()
    if (res.data.success && res.data.data) {
      stats.value = res.data.data
    }
  } catch (error) {
    console.error('Load stats failed:', error)
  }
}

const handleFileChange = (_file: UploadFile, uploadFiles: UploadFile[]) => {
  fileList.value = uploadFiles
}

const beforeUpload = (rawFile: UploadRawFile) => {
  if (rawFile.type !== 'application/pdf') {
    ElMessage.error('只支持PDF文件')
    return false
  }
  return true
}

const handleUpload = async () => {
  if (fileList.value.length === 0) {
    ElMessage.warning('请选择文件')
    return
  }

  uploading.value = true
  try {
    const files = fileList.value.map(f => f.raw as File)
    if (files.length === 1) {
      await invoiceApi.upload(files[0])
    } else {
      await invoiceApi.uploadMultiple(files)
    }
    ElMessage.success('上传成功')
    cancelUpload()
    loadInvoices()
    loadStats()
  } catch {
    ElMessage.error('上传失败')
  } finally {
    uploading.value = false
  }
}

const cancelUpload = () => {
  fileList.value = []
  uploadModalVisible.value = false
}

const handleDelete = async (id: string) => {
  try {
    await invoiceApi.delete(id)
    ElMessage.success('删除成功')
    loadInvoices()
    loadStats()
  } catch {
    ElMessage.error('删除失败')
  }
}

const openPreview = (invoice: Invoice) => {
  previewInvoice.value = invoice
  previewVisible.value = true
}

const downloadFile = (invoice: Invoice) => {
  window.open(`${FILE_BASE_URL}/${invoice.file_path}`, '_blank')
}

const getSourceLabel = (source?: string) => {
  const labels: Record<string, string> = {
    email: '邮件下载',
    dingtalk: '钉钉机器人',
    upload: '手动上传'
  }
  return labels[source || ''] || source || '未知'
}

const getSourceType = (source?: string): 'primary' | 'success' | 'warning' | 'info' => {
  const types: Record<string, 'primary' | 'success' | 'warning' | 'info'> = {
    email: 'primary',
    dingtalk: 'warning',
    upload: 'success'
  }
  return types[source || ''] || 'info'
}

const formatDateTime = (date?: string) => {
  if (!date) return '-'
  return dayjs(date).format('YYYY-MM-DD HH:mm')
}

const getParseStatusLabel = (status?: string) => {
  const labels: Record<string, string> = {
    pending: '待解析',
    parsing: '解析中',
    success: '解析成功',
    failed: '解析失败'
  }
  return labels[status || 'pending'] || '未知'
}

const getParseStatusType = (status?: string): 'info' | 'warning' | 'success' | 'danger' => {
  const types: Record<string, 'info' | 'warning' | 'success' | 'danger'> = {
    pending: 'info',
    parsing: 'warning',
    success: 'success',
    failed: 'danger'
  }
  return types[status || 'pending'] || 'info'
}

const handleReparse = async (id: string) => {
  parseStatusPending.value = true
  try {
    const res = await invoiceApi.parse(id)
    if (res.data.success && res.data.data) {
      ElMessage.success('发票解析完成')
      // Update the preview invoice with new data
      previewInvoice.value = res.data.data
      // Reload the invoices list
      loadInvoices()
    }
  } catch {
    ElMessage.error('发票解析失败')
  } finally {
    parseStatusPending.value = false
  }
}

onMounted(() => {
  loadInvoices()
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

.source-stats {
  display: flex;
  justify-content: space-around;
  gap: 16px;
}

.source-stats :deep(.el-statistic) {
  flex: 1;
  text-align: center;
}

.source-stats :deep(.el-statistic__number) {
  font-size: 20px;
  font-weight: 700;
}

.source-stats :deep(.el-statistic__head) {
  font-size: 13px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
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
  background: linear-gradient(135deg, rgba(79, 172, 254, 0.15), rgba(0, 242, 254, 0.15));
  color: #4facfe;
}

:deep(.el-tag.el-tag--success) {
  background: linear-gradient(135deg, rgba(67, 233, 123, 0.15), rgba(56, 249, 215, 0.15));
  color: #43e97b;
}

:deep(.el-tag.el-tag--warning) {
  background: linear-gradient(135deg, rgba(250, 112, 154, 0.15), rgba(254, 225, 64, 0.15));
  color: #fa709a;
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

/* Upload Modal */
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

/* Upload list */
:deep(.el-upload-list) {
  margin-top: 16px;
}

:deep(.el-upload-list__item) {
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
  border: 1px solid rgba(0, 0, 0, 0.06);
  margin-bottom: 8px;
}

:deep(.el-upload-list__item:hover) {
  background: rgba(102, 126, 234, 0.05);
  border-color: rgba(102, 126, 234, 0.2);
}

/* Preview dialog */
:deep(.el-dialog.preview-dialog) {
  max-width: 90vw;
  max-height: 90vh;
}

:deep(.el-dialog.preview-dialog .el-dialog__body) {
  max-height: calc(90vh - 120px);
  overflow: auto;
  padding: 0;
}

:deep(.el-dialog.preview-dialog iframe) {
  display: block;
  border: none;
  border-radius: var(--radius-md);
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

@media (max-width: 768px) {
  .source-stats {
    flex-direction: column;
    gap: 12px;
  }
  
  :deep(.el-table) {
    font-size: 13px;
  }
  
  .amount {
    font-size: 14px;
  }
  
  .filename {
    font-size: 13px;
  }
}
</style>
