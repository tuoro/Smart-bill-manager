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
}

.info-alert p {
  margin: 4px 0;
}

.config-card {
  margin-bottom: 16px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.config-name {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}

.form-tip {
  color: #909399;
  font-size: 12px;
}
</style>
