<template>
  <div>
    <!-- Info Alert -->
    <el-alert
      title="QQ邮箱配置说明"
      type="info"
      :closable="false"
      class="info-alert"
    >
      <template #default>
        <p>1. 登录QQ邮箱，进入「设置」→「账户」</p>
        <p>2. 找到「IMAP/SMTP服务」并开启</p>
        <p>3. 生成「授权码」（不是QQ密码）</p>
        <p>4. 在下方配置中使用邮箱地址和授权码</p>
      </template>
    </el-alert>

    <!-- Email Config Card -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>邮箱配置</span>
          <el-button type="primary" :icon="Plus" @click="openModal">
            添加邮箱
          </el-button>
        </div>
      </template>

      <el-table v-loading="loading" :data="configs">
        <el-table-column label="邮箱地址">
          <template #default="{ row }">
            <div class="email-cell">
              <el-icon color="#1890ff"><Message /></el-icon>
              {{ row.email }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="imap_host" label="IMAP服务器" />
        <el-table-column prop="imap_port" label="端口" width="80" />
        <el-table-column label="状态">
          <template #default="{ row }">
            <el-tag v-if="monitorStatus[row.id] === 'running'" type="success">
              <el-icon><VideoPlay /></el-icon> 监控中
            </el-tag>
            <el-tag v-else type="info">已停止</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后检查">
          <template #default="{ row }">
            {{ row.last_check ? formatDateTime(row.last_check) : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="250">
          <template #default="{ row }">
            <el-tooltip v-if="monitorStatus[row.id] === 'running'" content="停止监控">
              <el-button type="danger" link :icon="VideoPause" @click="handleStopMonitor(row.id)" />
            </el-tooltip>
            <el-tooltip v-else content="启动监控">
              <el-button type="success" link :icon="VideoPlay" @click="handleStartMonitor(row.id)" />
            </el-tooltip>
            <el-tooltip content="手动检查">
              <el-button 
                type="primary" 
                link 
                :icon="Refresh"
                :loading="checkLoading === row.id"
                @click="handleManualCheck(row.id)"
              />
            </el-tooltip>
            <el-popconfirm
              title="确定删除这个邮箱配置吗？"
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

    <!-- Email Logs Card -->
    <el-card>
      <template #header>
        <div class="card-header">
          <span>邮件处理日志</span>
          <el-button :icon="Refresh" @click="loadLogs">刷新</el-button>
        </div>
      </template>

      <el-table :data="logs">
        <el-table-column prop="subject" label="主题" show-overflow-tooltip />
        <el-table-column prop="from_address" label="发件人" width="180" show-overflow-tooltip />
        <el-table-column label="附件" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.has_attachment" type="primary" size="small">
              <el-icon><CircleCheck /></el-icon> {{ row.attachment_count }}个
            </el-tag>
            <el-tag v-else size="small">
              <el-icon><CircleClose /></el-icon> 无
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="接收时间" width="150">
          <template #default="{ row }">
            {{ row.received_date ? formatDateTime(row.received_date) : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
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
      title="添加邮箱配置"
      width="500px"
      destroy-on-close
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="100px"
      >
        <el-form-item label="快速选择">
          <el-select 
            v-model="selectedPreset" 
            placeholder="选择邮箱类型自动填充服务器配置"
            clearable
            @change="handlePresetSelect"
          >
            <el-option 
              v-for="p in EMAIL_PRESETS" 
              :key="p.name" 
              :label="p.name" 
              :value="p.name" 
            />
          </el-select>
        </el-form-item>

        <el-form-item label="邮箱地址" prop="email">
          <el-input v-model="form.email" placeholder="example@qq.com" />
        </el-form-item>

        <el-form-item label="IMAP服务器" prop="imap_host">
          <el-input v-model="form.imap_host" placeholder="imap.qq.com" />
        </el-form-item>

        <el-form-item label="IMAP端口" prop="imap_port">
          <el-input-number v-model="form.imap_port" :min="1" :max="65535" style="width: 100%" />
        </el-form-item>

        <el-form-item label="授权码/密码" prop="password">
          <el-input 
            v-model="form.password" 
            type="password" 
            placeholder="请输入授权码"
            show-password 
          />
          <template #extra>
            <span class="form-tip">QQ邮箱请使用授权码，不是QQ密码</span>
          </template>
        </el-form-item>

        <el-form-item label="启用状态">
          <el-switch v-model="form.is_active" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button :loading="testLoading" @click="handleTest">测试连接</el-button>
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
  Plus, Message, VideoPlay, VideoPause, Refresh, Delete, 
  CircleCheck, CircleClose 
} from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { emailApi } from '@/api'
import type { EmailConfig, EmailLog } from '@/types'

const EMAIL_PRESETS = [
  { name: 'QQ邮箱', host: 'imap.qq.com', port: 993 },
  { name: '163邮箱', host: 'imap.163.com', port: 993 },
  { name: '126邮箱', host: 'imap.126.com', port: 993 },
  { name: 'Gmail', host: 'imap.gmail.com', port: 993 },
  { name: 'Outlook', host: 'imap-mail.outlook.com', port: 993 },
  { name: '新浪邮箱', host: 'imap.sina.com', port: 993 },
]

const loading = ref(false)
const configs = ref<EmailConfig[]>([])
const logs = ref<EmailLog[]>([])
const monitorStatus = ref<Record<string, string>>({})
const modalVisible = ref(false)
const testLoading = ref(false)
const checkLoading = ref<string | null>(null)
const formRef = ref<FormInstance>()
const selectedPreset = ref('')

const currentPage = ref(1)
const pageSize = ref(10)

const form = reactive({
  email: '',
  imap_host: '',
  imap_port: 993,
  password: '',
  is_active: true
})

const rules: FormRules = {
  email: [
    { required: true, message: '请输入邮箱地址', trigger: 'blur' },
    { type: 'email', message: '请输入有效的邮箱地址', trigger: 'blur' }
  ],
  imap_host: [{ required: true, message: '请输入IMAP服务器地址', trigger: 'blur' }],
  imap_port: [{ required: true, message: '请输入端口号', trigger: 'blur' }],
  password: [{ required: true, message: '请输入授权码或密码', trigger: 'blur' }]
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const res = await emailApi.getConfigs()
    if (res.data.success && res.data.data) {
      configs.value = res.data.data
    }
  } catch {
    ElMessage.error('加载邮箱配置失败')
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  try {
    const res = await emailApi.getLogs(undefined, 50)
    if (res.data.success && res.data.data) {
      logs.value = res.data.data
    }
  } catch (error) {
    console.error('Load logs failed:', error)
  }
}

const loadMonitorStatus = async () => {
  try {
    const res = await emailApi.getMonitoringStatus()
    if (res.data.success && res.data.data) {
      const statusMap: Record<string, string> = {}
      res.data.data.forEach(item => {
        statusMap[item.configId] = item.status
      })
      monitorStatus.value = statusMap
    }
  } catch (error) {
    console.error('Load monitor status failed:', error)
  }
}

const openModal = () => {
  form.email = ''
  form.imap_host = ''
  form.imap_port = 993
  form.password = ''
  form.is_active = true
  selectedPreset.value = ''
  modalVisible.value = true
}

const handlePresetSelect = (preset: string) => {
  const selected = EMAIL_PRESETS.find(p => p.name === preset)
  if (selected) {
    form.imap_host = selected.host
    form.imap_port = selected.port
  }
}

const handleTest = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    testLoading.value = true
    try {
      const res = await emailApi.testConnection({
        email: form.email,
        imap_host: form.imap_host,
        imap_port: form.imap_port,
        password: form.password
      })
      if (res.data.success) {
        ElMessage.success('连接测试成功！')
      } else {
        ElMessage.error(res.data.message || '连接测试失败')
      }
    } catch {
      ElMessage.error('连接测试失败')
    } finally {
      testLoading.value = false
    }
  })
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    try {
      await emailApi.createConfig({
        email: form.email,
        imap_host: form.imap_host,
        imap_port: form.imap_port,
        password: form.password,
        is_active: form.is_active ? 1 : 0
      })
      ElMessage.success('邮箱配置创建成功')
      modalVisible.value = false
      loadConfigs()
      loadMonitorStatus()
    } catch (error: unknown) {
      const err = error as { response?: { data?: { message?: string } } }
      ElMessage.error(err.response?.data?.message || '创建配置失败')
    }
  })
}

const handleDelete = async (id: string) => {
  try {
    await emailApi.deleteConfig(id)
    ElMessage.success('删除成功')
    loadConfigs()
    loadMonitorStatus()
  } catch {
    ElMessage.error('删除失败')
  }
}

const handleStartMonitor = async (id: string) => {
  try {
    await emailApi.startMonitoring(id)
    ElMessage.success('监控已启动')
    loadMonitorStatus()
  } catch {
    ElMessage.error('启动监控失败')
  }
}

const handleStopMonitor = async (id: string) => {
  try {
    await emailApi.stopMonitoring(id)
    ElMessage.success('监控已停止')
    loadMonitorStatus()
  } catch {
    ElMessage.error('停止监控失败')
  }
}

const handleManualCheck = async (id: string) => {
  checkLoading.value = id
  try {
    const res = await emailApi.manualCheck(id)
    if (res.data.success) {
      ElMessage.success(res.data.message || '检查完成')
      if (res.data.data && res.data.data.newEmails > 0) {
        loadLogs()
      }
    } else {
      ElMessage.error(res.data.message || '检查失败')
    }
  } catch {
    ElMessage.error('检查邮件失败')
  } finally {
    checkLoading.value = null
  }
}

const formatDateTime = (date: string) => {
  return dayjs(date).format('MM-DD HH:mm')
}

onMounted(() => {
  loadConfigs()
  loadLogs()
  loadMonitorStatus()
})
</script>

<style scoped>
.info-alert {
  margin-bottom: 16px;
  border-radius: var(--radius-lg);
  border: none;
  background: linear-gradient(135deg, rgba(79, 172, 254, 0.08), rgba(0, 242, 254, 0.08));
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

.email-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}

.email-cell :deep(.el-icon) {
  transition: transform var(--transition-base);
}

.email-cell:hover :deep(.el-icon) {
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
  background: linear-gradient(135deg, rgba(79, 172, 254, 0.15), rgba(0, 242, 254, 0.15));
  color: #4facfe;
}

/* Tooltip buttons */
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
