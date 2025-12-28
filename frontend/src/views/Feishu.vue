<template>
  <div class="page">
    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>飞书机器人配置说明</span>
        </div>
      </template>
      <template #content>
        <ul class="guide-list">
          <li>在飞书开放平台创建应用，启用机器人能力，并将机器人加入群聊/私聊</li>
          <li>配置“事件订阅”URL 为下方生成的 Webhook 地址，并订阅 <code>im.message.receive_v1</code></li>
          <li>保存 App ID / App Secret / Verification Token（可选 Encrypt Key），用于下载文件并回消息</li>
        </ul>
      </template>
    </Card>

    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>飞书配置</span>
          <div class="panel-actions">
            <Button label="刷新" icon="pi pi-refresh" class="p-button-outlined" @click="loadConfigs" />
            <Button label="添加配置" icon="pi pi-plus" @click="openModal" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable :value="configs" :loading="loading" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="name" header="配置名称" />
          <Column header="状态">
            <template #body="{ data: row }">
              <Tag :severity="row.is_active ? 'success' : 'secondary'" :value="row.is_active ? '启用' : '禁用'" />
            </template>
          </Column>
          <Column header="Webhook">
            <template #body="{ data: row }">
              <span class="webhook">{{ getWebhookUrl(row.id) }}</span>
            </template>
          </Column>
          <Column header="操作" :style="{ width: '220px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  size="small"
                  class="p-button-outlined"
                  severity="secondary"
                  icon="pi pi-copy"
                  label="Copy Webhook URL"
                  @click="copyWebhookUrl(row.id)"
                />
                <Button size="small" severity="danger" class="p-button-text" icon="pi pi-trash" @click="confirmDelete(row.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>最近处理日志</span>
          <Button label="刷新" icon="pi pi-refresh" class="p-button-outlined" @click="loadLogs" />
        </div>
      </template>
      <template #content>
        <DataTable :value="logs" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="event_type" header="事件" />
          <Column field="message_type" header="消息类型" />
          <Column field="sender_id" header="发送人" />
          <Column field="content" header="内容" />
          <Column header="附件">
            <template #body="{ data: row }">
              <Tag v-if="row.has_attachment" severity="info" :value="row.file_name || '有附件'" />
              <Tag v-else severity="secondary" value="无" />
            </template>
          </Column>
          <Column header="状态">
            <template #body="{ data: row }">
              <Tag :severity="row.status === 'processed' ? 'success' : row.status === 'skipped' ? 'secondary' : 'warning'" :value="row.status" />
            </template>
          </Column>
          <Column header="时间">
            <template #body="{ data: row }">
              {{ row.created_at ? formatDateTime(row.created_at) : '-' }}
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="modalVisible" modal header="添加配置" :style="{ width: '620px', maxWidth: '92vw' }">
      <form class="p-fluid" @submit.prevent="handleSubmit">
        <div class="field">
          <label for="name">配置名称</label>
          <InputText id="name" v-model.trim="form.name" />
          <small v-if="errors.name" class="p-error">{{ errors.name }}</small>
        </div>

        <div class="field">
          <label for="app_id">App ID（可选）</label>
          <InputText id="app_id" v-model.trim="form.app_id" />
          <small class="tip">需要用于下载文件、回消息（建议填写）</small>
        </div>

        <div class="field">
          <label for="app_secret">App Secret（可选）</label>
          <Password id="app_secret" v-model.trim="form.app_secret" toggleMask :feedback="false" />
        </div>

        <div class="field">
          <label for="verification_token">Verification Token（可选）</label>
          <Password id="verification_token" v-model.trim="form.verification_token" toggleMask :feedback="false" />
          <small class="tip">用于校验事件订阅请求来源（建议填写）</small>
        </div>

        <div class="field">
          <label for="encrypt_key">Encrypt Key（可选）</label>
          <Password id="encrypt_key" v-model.trim="form.encrypt_key" toggleMask :feedback="false" />
          <small class="tip">如果飞书事件订阅开启了加密，则需要填写；未开启可留空</small>
        </div>

        <div class="switch-row">
          <span class="switch-label">启用此配置</span>
          <InputSwitch v-model="form.is_active" />
        </div>

        <div class="footer">
          <Button label="取消" class="p-button-text" type="button" @click="modalVisible = false" />
          <Button :label="saving ? '保存中...' : '保存'" :loading="saving" type="submit" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import InputSwitch from 'primevue/inputswitch'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import dayjs from 'dayjs'
import { feishuApi } from '@/api'
import type { FeishuConfig, FeishuLog } from '@/types'
import { getBackendBaseUrl } from '@/utils/constants'
import { useNotificationStore } from '@/stores/notifications'

const confirm = useConfirm()
const toast = useToast()
const notifications = useNotificationStore()

const FEISHU_LOG_TS_KEY = 'sbm.feishu.lastLogTs.v1'

const loading = ref(false)
const modalVisible = ref(false)
const saving = ref(false)

const configs = ref<FeishuConfig[]>([])
const logs = ref<FeishuLog[]>([])

const form = reactive({
  name: '',
  app_id: '',
  app_secret: '',
  verification_token: '',
  encrypt_key: '',
  is_active: true,
})

const errors = reactive<{ name?: string }>({})

const resetForm = () => {
  form.name = ''
  form.app_id = ''
  form.app_secret = ''
  form.verification_token = ''
  form.encrypt_key = ''
  form.is_active = true
  errors.name = undefined
}

const validate = () => {
  errors.name = form.name ? undefined : '配置名称不能为空'
  return !errors.name
}

const getStoredTs = (key: string) => {
  try {
    const v = localStorage.getItem(key)
    return v ? Number(v) : 0
  } catch {
    return 0
  }
}

const setStoredTs = (key: string, value: number) => {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    // ignore
  }
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const res = await feishuApi.getConfigs()
    configs.value = res.data.data || []
  } catch (error) {
    console.error('Load configs failed:', error)
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  try {
    const res = await feishuApi.getLogs(undefined, 50)
    const data = res.data.data || []
    logs.value = data

    const lastTs = getStoredTs(FEISHU_LOG_TS_KEY)
    let maxTs = lastTs
    for (const item of data) {
      const ts = item.created_at ? dayjs(item.created_at).valueOf() : 0
      if (ts > maxTs) maxTs = ts
      if (ts <= lastTs) continue

      const title = item.status === 'processed' ? '飞书收到文件' : '飞书事件'
      const detail = item.file_name || item.content || item.event_type || undefined
      notifications.add({
        severity: item.status === 'failed' ? 'error' : item.status === 'processed' ? 'success' : 'info',
        title,
        detail,
      })
    }
    if (maxTs > lastTs) setStoredTs(FEISHU_LOG_TS_KEY, maxTs)
  } catch (error) {
    console.error('Load logs failed:', error)
  }
}

const openModal = () => {
  resetForm()
  modalVisible.value = true
}

const handleSubmit = async () => {
  if (!validate()) return
  saving.value = true
  try {
    await feishuApi.createConfig({
      name: form.name,
      app_id: form.app_id || undefined,
      app_secret: form.app_secret || undefined,
      verification_token: form.verification_token || undefined,
      encrypt_key: form.encrypt_key || undefined,
      is_active: form.is_active ? 1 : 0,
    })
    toast.add({ severity: 'success', summary: '配置创建成功', life: 2200 })
    notifications.add({ severity: 'success', title: '飞书配置已创建', detail: form.name })
    modalVisible.value = false
    await loadConfigs()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({
      severity: 'error',
      summary: err.response?.data?.message || '创建配置失败',
      life: 3500,
    })
    notifications.add({
      severity: 'error',
      title: '飞书配置创建失败',
      detail: err.response?.data?.message || form.name,
    })
  } finally {
    saving.value = false
  }
}

const confirmDelete = (id: string) => {
  confirm.require({
    message: '确定删除该配置吗？',
    header: '删除确认',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: () => handleDelete(id),
  })
}

const handleDelete = async (id: string) => {
  try {
    await feishuApi.deleteConfig(id)
    toast.add({ severity: 'success', summary: '删除成功', life: 2000 })
    notifications.add({ severity: 'info', title: '飞书配置已删除', detail: id })
    await loadConfigs()
  } catch {
    toast.add({ severity: 'error', summary: '删除失败', life: 3000 })
    notifications.add({ severity: 'error', title: '飞书配置删除失败', detail: id })
  }
}

const getWebhookUrl = (id: string) => {
  const baseUrl = getBackendBaseUrl()
  return `${baseUrl}/api/feishu/webhook/${id}`
}

const copyWebhookUrl = async (id: string) => {
  const webhookUrl = getWebhookUrl(id)
  try {
    await navigator.clipboard.writeText(webhookUrl)
    toast.add({ severity: 'success', summary: 'Webhook URL 已复制', life: 2000 })
    notifications.add({ severity: 'success', title: 'Webhook URL 已复制', detail: webhookUrl })
  } catch {
    toast.add({ severity: 'info', summary: `Webhook URL: ${webhookUrl}`, life: 4500 })
    notifications.add({ severity: 'info', title: 'Webhook URL', detail: webhookUrl })
  }
}

const formatDateTime = (date: string) => dayjs(date).format('YYYY-MM-DD HH:mm')

onMounted(() => {
  loadConfigs()
  loadLogs()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.field {
  margin: 0 0 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.field label {
  display: block;
  font-weight: 700;
  color: var(--p-text-color);
}

.field :deep(.p-inputtext),
.field :deep(.p-password) {
  width: 100%;
}

.field :deep(.p-password input) {
  width: 100%;
}

.guide-list {
  margin: 0;
  padding-left: 18px;
  color: var(--p-text-muted-color);
}

.guide-list li {
  margin: 6px 0;
  line-height: 1.55;
}

.panel {
  border-radius: var(--radius-lg);
}

.panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.webhook {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text-secondary);
  word-break: break-all;
}

.tip {
  display: block;
  margin-top: 6px;
  color: var(--color-text-tertiary);
  font-size: 12px;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(0, 0, 0, 0.02);
  margin-top: 10px;
}

.switch-label {
  font-weight: 700;
  color: var(--color-text-secondary);
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}
</style>

