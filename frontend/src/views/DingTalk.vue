<template>
  <div class="page">
    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>钉钉机器人配置说明</span>
        </div>
      </template>
      <template #content>
        <ul class="guide-list">
          <li>创建一个钉钉机器人，找到它的 Webhook</li>
          <li>如果开启了安全设置（如加签），请在下方填写对应字段</li>
          <li>保存后，点击 “Copy Webhook URL” 获取后台接收地址</li>
        </ul>
      </template>
    </Card>

    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>&#38025;&#38025;&#37197;&#32622;</span>
          <div class="panel-actions">
            <Button :label="'\u5237\u65B0'" icon="pi pi-refresh" class="p-button-outlined" @click="loadConfigs" />
            <Button :label="'\u6DFB\u52A0\u914D\u7F6E'" icon="pi pi-plus" @click="openModal" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable :value="configs" :loading="loading" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="name" :header="'\u914D\u7F6E\u540D\u79F0'" />
          <Column :header="'\u72B6\u6001'">
            <template #body="{ data: row }">
              <Tag :severity="row.is_active ? 'success' : 'secondary'" :value="row.is_active ? '\u542F\u7528' : '\u7981\u7528'" />
            </template>
          </Column>
          <Column :header="'Webhook'">
            <template #body="{ data: row }">
              <span class="webhook">{{ getWebhookUrl(row.id) }}</span>
            </template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '220px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  size="small"
                  class="p-button-outlined"
                  severity="secondary"
                  icon="pi pi-copy"
                  :label="'Copy Webhook URL'"
                  @click="copyWebhookUrl(row.id)"
                />
                <Button
                  size="small"
                  severity="danger"
                  class="p-button-text"
                  icon="pi pi-trash"
                  @click="confirmDelete(row.id)"
                />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>&#26368;&#36817;&#22788;&#29702;&#26085;&#24535;</span>
          <Button :label="'\u5237\u65B0'" icon="pi pi-refresh" class="p-button-outlined" @click="loadLogs" />
        </div>
      </template>
      <template #content>
        <DataTable :value="logs" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="message_type" :header="'\u7C7B\u578B'" />
          <Column field="sender_nick" :header="'\u53D1\u9001\u4EBA'" />
          <Column field="content" :header="'\u5185\u5BB9'" />
          <Column :header="'\u9644\u4EF6'">
            <template #body="{ data: row }">
              <Tag v-if="row.has_attachment" severity="info" :value="`${row.attachment_count}\u4E2A`" />
              <Tag v-else severity="secondary" :value="'\u65E0'" />
            </template>
          </Column>
          <Column :header="'\u72B6\u6001'">
            <template #body="{ data: row }">
              <Tag :severity="row.status === 'processed' ? 'success' : 'warning'" :value="row.status" />
            </template>
          </Column>
          <Column :header="'\u65F6\u95F4'">
            <template #body="{ data: row }">
              {{ row.created_at ? formatDateTime(row.created_at) : '-' }}
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="modalVisible" modal :header="'\u6DFB\u52A0\u914D\u7F6E'" :style="{ width: '620px', maxWidth: '92vw' }">
      <form class="p-fluid" @submit.prevent="handleSubmit">
        <div class="field">
          <label for="name">&#37197;&#32622;&#21517;&#31216;</label>
          <InputText id="name" v-model.trim="form.name" />
          <small v-if="errors.name" class="p-error">{{ errors.name }}</small>
        </div>

        <div class="field">
          <label for="app_key">AppKey (&#21487;&#36873;)</label>
          <InputText id="app_key" v-model.trim="form.app_key" />
        </div>

        <div class="field">
          <label for="app_secret">AppSecret (&#21487;&#36873;)</label>
          <Password id="app_secret" v-model="form.app_secret" toggleMask :feedback="false" />
        </div>

        <div class="field">
          <label for="webhook_token">Webhook Token (&#21487;&#36873;)</label>
          <Password id="webhook_token" v-model="form.webhook_token" toggleMask :feedback="false" />
          <small class="tip">&#22914;&#26524;&#26426;&#22120;&#20154;&#24320;&#21551;&#20102;&#21152;&#31614;&#65292;&#35831;&#22312;&#27492;&#22635;&#20889;&#21152;&#31614;&#23494;&#38053;</small>
        </div>

        <div class="switch-row">
          <span class="switch-label">&#21551;&#29992;&#29366;&#24577;</span>
          <InputSwitch v-model="form.is_active" />
        </div>

        <div class="footer">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" @click="modalVisible = false" />
          <Button type="submit" :label="'\u4FDD\u5B58\u914D\u7F6E'" :loading="saving" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputSwitch from 'primevue/inputswitch'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { dingtalkApi } from '@/api'
import { getBackendBaseUrl } from '@/utils/constants'
import { useNotificationStore } from '@/stores/notifications'
import type { DingtalkConfig, DingtalkLog } from '@/types'

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()

const DINGTALK_LOG_TS_KEY = 'sbm.dingtalk.lastLogTs.v1'
const getStoredTs = (key: string) => {
  try {
    return Number(window.localStorage.getItem(key) || 0) || 0
  } catch {
    return 0
  }
}

const setStoredTs = (key: string, value: number) => {
  try {
    window.localStorage.setItem(key, String(value))
  } catch {
    // ignore
  }
}

const parseTs = (v?: string) => {
  const t = v ? Date.parse(v) : NaN
  return Number.isFinite(t) ? t : 0
}

const loading = ref(false)
const saving = ref(false)
const configs = ref<DingtalkConfig[]>([])
const logs = ref<DingtalkLog[]>([])
const modalVisible = ref(false)

const form = reactive({
  name: '',
  app_key: '',
  app_secret: '',
  webhook_token: '',
  is_active: true,
})

const errors = reactive({
  name: '',
})

const resetForm = () => {
  form.name = ''
  form.app_key = ''
  form.app_secret = ''
  form.webhook_token = ''
  form.is_active = true
  errors.name = ''
}

const validate = () => {
  errors.name = ''
  if (!form.name) errors.name = '\u8BF7\u8F93\u5165\u914D\u7F6E\u540D\u79F0'
  return !errors.name
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const res = await dingtalkApi.getConfigs()
    if (res.data.success && res.data.data) configs.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u9489\u9489\u914D\u7F6E\u5931\u8D25', life: 3000 })
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  try {
    const res = await dingtalkApi.getLogs(undefined, 50)
    if (res.data.success && res.data.data) {
      logs.value = res.data.data

      const lastTs = getStoredTs(DINGTALK_LOG_TS_KEY)
      const maxTs = Math.max(0, ...logs.value.map((x) => parseTs(x.created_at)))
      if (lastTs === 0) {
        if (maxTs > 0) setStoredTs(DINGTALK_LOG_TS_KEY, maxTs)
        return
      }

      const newLogs = logs.value
        .filter((x) => parseTs(x.created_at) > lastTs)
        .sort((a, b) => parseTs(b.created_at) - parseTs(a.created_at))
        .slice(0, 3)

      for (const item of newLogs) {
        const content = (item.content || '').replace(/\s+/g, ' ').trim()
        const short = content.length > 60 ? `${content.slice(0, 60)}...` : content
        if (item.has_attachment) {
          notifications.add({
            severity: 'success',
            title: '钉钉收到发票/附件',
            detail: `附件 ${item.attachment_count || 0} 个${short ? `：${short}` : ''}`,
          })
        } else {
          notifications.add({
            severity: 'info',
            title: '钉钉收到消息',
            detail: short || item.message_type || undefined,
          })
        }
      }

      if (maxTs > lastTs) setStoredTs(DINGTALK_LOG_TS_KEY, maxTs)
    }
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
    await dingtalkApi.createConfig({
      name: form.name,
      app_key: form.app_key || undefined,
      app_secret: form.app_secret || undefined,
      webhook_token: form.webhook_token || undefined,
      is_active: form.is_active ? 1 : 0,
    })
    toast.add({ severity: 'success', summary: '\u914D\u7F6E\u521B\u5EFA\u6210\u529F', life: 2200 })
    notifications.add({ severity: 'success', title: '钉钉配置已创建', detail: form.name })
    modalVisible.value = false
    await loadConfigs()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({
      severity: 'error',
      summary: err.response?.data?.message || '\u521B\u5EFA\u914D\u7F6E\u5931\u8D25',
      life: 3500,
    })
    notifications.add({
      severity: 'error',
      title: '钉钉配置创建失败',
      detail: err.response?.data?.message || form.name,
    })
  } finally {
    saving.value = false
  }
}

const confirmDelete = (id: string) => {
  confirm.require({
    message: '\u786E\u5B9A\u5220\u9664\u8BE5\u914D\u7F6E\u5417\uFF1F',
    header: '\u5220\u9664\u786E\u8BA4',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '\u5220\u9664',
    rejectLabel: '\u53D6\u6D88',
    acceptClass: 'p-button-danger',
    accept: () => handleDelete(id),
  })
}

const handleDelete = async (id: string) => {
  try {
    await dingtalkApi.deleteConfig(id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    notifications.add({ severity: 'info', title: '钉钉配置已删除', detail: id })
    await loadConfigs()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
    notifications.add({ severity: 'error', title: '钉钉配置删除失败', detail: id })
  }
}

const getWebhookUrl = (id: string) => {
  const baseUrl = getBackendBaseUrl()
  return `${baseUrl}/api/dingtalk/webhook/${id}`
}

const copyWebhookUrl = async (id: string) => {
  const webhookUrl = getWebhookUrl(id)
  try {
    await navigator.clipboard.writeText(webhookUrl)
    toast.add({ severity: 'success', summary: 'Webhook URL \u5DF2\u590D\u5236', life: 2000 })
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
