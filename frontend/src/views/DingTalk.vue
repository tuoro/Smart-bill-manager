<template>
  <div class="page">
    <Message severity="info" :closable="false" class="info">
      <div class="info-title">&#38025;&#38025;&#26426;&#22120;&#20154;&#37197;&#32622;&#35828;&#26126;</div>
      <ul class="info-list">
        <li>&#21019;&#24314;&#19968;&#20010;&#38025;&#38025;&#26426;&#22120;&#20154;&#65292;&#25214;&#21040;&#23427;&#30340; Webhook</li>
        <li>&#22914;&#26524;&#24320;&#21551;&#20102;&#23433;&#20840;&#35774;&#32622;&#65288;&#22914;&#21152;&#31614;&#65289;&#65292;&#35831;&#22312;&#19979;&#26041;&#22635;&#20889;&#23545;&#24212;&#23383;&#27573;</li>
        <li>&#20445;&#23384;&#21518;&#65292;&#28857;&#20987;&#8220;Copy Webhook URL&#8221; &#33719;&#21462;&#21518;&#21488;&#25509;&#25910;&#22320;&#22336;</li>
      </ul>
    </Message>

    <Card class="panel">
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

    <Card class="panel">
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
import Message from 'primevue/message'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { dingtalkApi } from '@/api'
import { getBackendBaseUrl } from '@/utils/constants'
import type { DingtalkConfig, DingtalkLog } from '@/types'

const toast = useToast()
const confirm = useConfirm()

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
    if (res.data.success && res.data.data) logs.value = res.data.data
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
    modalVisible.value = false
    await loadConfigs()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({
      severity: 'error',
      summary: err.response?.data?.message || '\u521B\u5EFA\u914D\u7F6E\u5931\u8D25',
      life: 3500,
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
    await loadConfigs()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
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
  } catch {
    toast.add({ severity: 'info', summary: `Webhook URL: ${webhookUrl}`, life: 4500 })
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

.info {
  border-radius: var(--radius-lg);
}

.info-title {
  font-weight: 800;
  margin-bottom: 6px;
}

.info-list {
  margin: 0;
  padding-left: 18px;
  color: var(--color-text-secondary);
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

