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
import { Document, Upload, View, Download, Delete, UploadFilled } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { invoiceApi, FILE_BASE_URL } from '@/api'
import type { Invoice } from '@/types'

const loading = ref(false)
const invoices = ref<Invoice[]>([])
const uploadModalVisible = ref(false)
const previewVisible = ref(false)
const previewInvoice = ref<Invoice | null>(null)
const uploading = ref(false)
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

onMounted(() => {
  loadInvoices()
  loadStats()
})
</script>

<style scoped>
.stats-row {
  margin-bottom: 16px;
}

.source-stats {
  display: flex;
  justify-content: space-around;
}

.source-stats :deep(.el-statistic__number) {
  font-size: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filename {
  display: flex;
  align-items: center;
  gap: 8px;
}

.amount {
  color: #f5222d;
  font-weight: bold;
}

.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}

.upload-area {
  width: 100%;
}

.upload-area :deep(.el-upload-dragger) {
  width: 100%;
}
</style>
