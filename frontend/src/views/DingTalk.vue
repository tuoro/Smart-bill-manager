<template>
  <div>
    <!-- Info Alert -->
    <el-alert
      title="钉钉机器人配置说明"
      type="info"
      :closable="false"
      class="info-alert"
    >
      <template #default>
        <p><strong>1. 创建钉钉机器人：</strong></p>
        <p>在钉钉群设置中添加「自定义机器人」，获取Webhook地址和安全设置</p>
        <p><strong>2. 配置机器人：</strong></p>
        <p>- 如需下载文件功能，请在钉钉开放平台创建企业内部应用获取App Key和App Secret</p>
        <p>- 如需签名验证，请配置Webhook Token（机器人安全设置中的加签密钥）</p>
        <p><strong>3. 设置回调地址：</strong></p>
        <p>创建配置后，复制Webhook URL设置到钉钉机器人的消息接收地址</p>
        <p><strong>4. 发送发票：</strong></p>
        <p>在钉钉群中@机器人并发送PDF发票文件，系统将自动解析并保存</p>
      </template>
    </el-alert>

    <!-- DingTalk Config Card -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>钉钉机器人配置</span>
          <el-button type="primary" :icon="Plus" @click="openModal">
            添加机器人
          </el-button>
        </div>
      </template>

      <el-table v-loading="loading" :data="configs">
        <el-table-column label="配置名称">
          <template #default="{ row }">
            <div class="config-name">
              <el-icon color="#1890ff"><ChatDotRound /></el-icon>
              {{ row.name }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="App Key">
          <template #default="{ row }">
            <el-text v-if="row.app_key" type="info" truncated>
              {{ row.app_key.substring(0, 8) }}...
            </el-text>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="状态">
          <template #default="{ row }">
            <el-tag v-if="row.is_active" type="success">启用</el-tag>
            <el-tag v-else type="info">禁用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间">
          <template #default="{ row }">
            {{ row.created_at ? formatDateTime(row.created_at) : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-tooltip content="复制Webhook URL">
              <el-button type="primary" link :icon="CopyDocument" @click="copyWebhookUrl(row.id)" />
            </el-tooltip>
            <el-popconfirm
              title="确定删除这个钉钉配置吗？"
              @confirm="handleDelete(row.id)"
            >
              <template #reference>
                <el-button type="danger" link :icon="Delete" />
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- Message Logs Card -->
    <el-card>
      <template #header>
        <div class="card-header">
          <span>消息处理日志</span>
          <el-button :icon="Refresh" @click="loadLogs">刷新</el-button>
        </div>
      </template>

      <el-table :data="logs">
        <el-table-column label="消息类型" width="100">
          <template #default="{ row }">
            <el-tag>{{ row.message_type || 'unknown' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sender_nick" label="发送者" width="120" />
        <el-table-column prop="content" label="内容" show-overflow-tooltip />
        <el-table-column label="附件" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.has_attachment" type="primary" size="small">
              <el-icon><CircleCheck /></el-icon> {{ row.attachment_count }}个
            </el-tag>
            <el-tag v-else size="small">
              <el-icon><CircleClose /></el-icon> 无
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="150">
          <template #default="{ row }">
            {{ row.created_at ? formatDateTime(row.created_at) : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 'processed' ? 'success' : 'warning'">
              {{ row.status === 'processed' ? '已处理' : row.status }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="logs.length"
        layout="total, sizes, prev, pager, next"
        class="pagination"
      />
    </el-card>

    <!-- Add Config Modal -->
    <el-dialog
      v-model="modalVisible"
      title="添加钉钉机器人配置"
      width="600px"
      destroy-on-close
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
      >
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="form.name" placeholder="例如：发票收集机器人" />
        </el-form-item>

        <el-form-item label="App Key">
          <el-input v-model="form.app_key" placeholder="钉钉应用App Key（可选）" />
          <template #extra>
            <span class="form-tip">可选。如需下载文件功能，请在钉钉开放平台创建应用获取</span>
          </template>
        </el-form-item>

        <el-form-item label="App Secret">
          <el-input 
            v-model="form.app_secret" 
            type="password" 
            placeholder="钉钉应用App Secret（可选）"
            show-password 
          />
          <template #extra>
            <span class="form-tip">可选。与App Key配合使用，用于获取访问令牌</span>
          </template>
        </el-form-item>

        <el-form-item label="Webhook Token">
          <el-input 
            v-model="form.webhook_token" 
            type="password" 
            placeholder="机器人加签密钥（可选）"
            show-password 
          />
          <template #extra>
            <span class="form-tip">可选。如果机器人启用了加签验证，请填写加签密钥</span>
          </template>
        </el-form-item>

        <el-form-item label="启用状态">
          <el-switch v-model="form.is_active" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="modalVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">保存配置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { 
  Plus, ChatDotRound, CopyDocument, Delete, Refresh,
  CircleCheck, CircleClose 
} from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { dingtalkApi } from '@/api'
import { getBackendBaseUrl } from '@/utils/constants'
import type { DingtalkConfig, DingtalkLog } from '@/types'

const loading = ref(false)
const configs = ref<DingtalkConfig[]>([])
const logs = ref<DingtalkLog[]>([])
const modalVisible = ref(false)
const formRef = ref<FormInstance>()

const currentPage = ref(1)
const pageSize = ref(10)

const form = reactive({
  name: '',
  app_key: '',
  app_secret: '',
  webhook_token: '',
  is_active: true
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }]
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const res = await dingtalkApi.getConfigs()
    if (res.data.success && res.data.data) {
      configs.value = res.data.data
    }
  } catch {
    ElMessage.error('加载钉钉配置失败')
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  try {
    const res = await dingtalkApi.getLogs(undefined, 50)
    if (res.data.success && res.data.data) {
      logs.value = res.data.data
    }
  } catch (error) {
    console.error('Load logs failed:', error)
  }
}

const openModal = () => {
  form.name = ''
  form.app_key = ''
  form.app_secret = ''
  form.webhook_token = ''
  form.is_active = true
  modalVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    try {
      await dingtalkApi.createConfig({
        name: form.name,
        app_key: form.app_key || undefined,
        app_secret: form.app_secret || undefined,
        webhook_token: form.webhook_token || undefined,
        is_active: form.is_active ? 1 : 0
      })
      ElMessage.success('钉钉机器人配置创建成功')
      modalVisible.value = false
      loadConfigs()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } }
      ElMessage.error(err.response?.data?.message || '创建配置失败')
    }
  })
}

const handleDelete = async (id: string) => {
  try {
    await dingtalkApi.deleteConfig(id)
    ElMessage.success('删除成功')
    loadConfigs()
  } catch {
    ElMessage.error('删除失败')
  }
}

const copyWebhookUrl = (id: string) => {
  const baseUrl = getBackendBaseUrl()
  const webhookUrl = `${baseUrl}/api/dingtalk/webhook/${id}`
  navigator.clipboard.writeText(webhookUrl).then(() => {
    ElMessage.success('Webhook URL已复制到剪贴板')
  }).catch(() => {
    ElMessage.info(`Webhook URL: ${webhookUrl}`)
  })
}

const formatDateTime = (date: string) => {
  return dayjs(date).format('YYYY-MM-DD HH:mm')
}

onMounted(() => {
  loadConfigs()
  loadLogs()
})
</script>

<style scoped>
.info-alert {
  margin-bottom: 16px;
  border-radius: var(--radius-lg);
  border: none;
  background: linear-gradient(135deg, rgba(250, 112, 154, 0.08), rgba(254, 225, 64, 0.08));
  box-shadow: var(--shadow-sm);
}

.info-alert :deep(.el-alert__content) {
  padding: 4px 0;
}

.info-alert p {
  margin: 6px 0;
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.info-alert p:first-child {
  margin-top: 0;
}

.info-alert strong {
  color: var(--color-text-primary);
  font-weight: 600;
}

.config-card {
  margin-bottom: 16px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
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

:deep(.el-table tbody tr:nth-child(even)) {
  background: rgba(0, 0, 0, 0.02);
}

:deep(.el-table tbody tr) {
  transition: all var(--transition-fast);
}

:deep(.el-table tbody tr:hover) {
  background: linear-gradient(90deg, rgba(102, 126, 234, 0.08), rgba(118, 75, 162, 0.08)) !important;
  transform: scale(1.002);
  box-shadow: 0 2px 8px rgba(102, 126, 234, 0.1);
}

.config-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}

.config-name :deep(.el-icon) {
  transition: transform var(--transition-base);
}

.config-name:hover :deep(.el-icon) {
  transform: scale(1.1);
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

:deep(.el-tag.el-tag--success) {
  background: linear-gradient(135deg, rgba(67, 233, 123, 0.15), rgba(56, 249, 215, 0.15));
  color: #43e97b;
}

:deep(.el-tag.el-tag--primary) {
  background: linear-gradient(135deg, rgba(250, 112, 154, 0.15), rgba(254, 225, 64, 0.15));
  color: #fa709a;
}

/* Tooltip buttons */
:deep(.el-button.is-link) {
  transition: all var(--transition-fast);
}

:deep(.el-button.is-link:hover) {
  transform: scale(1.1);
}

/* Text truncated */
:deep(.el-text) {
  font-family: monospace;
  font-size: 13px;
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
:deep(.el-input-number) {
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

:deep(.el-input__wrapper:hover) {
  box-shadow: var(--shadow-sm);
}

:deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

:deep(.el-input-number) {
  width: 100%;
}

.form-tip {
  color: var(--color-text-tertiary);
  font-size: 12px;
  line-height: 1.5;
  margin-top: 4px;
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
  :deep(.el-table) {
    font-size: 13px;
  }
}
</style>
