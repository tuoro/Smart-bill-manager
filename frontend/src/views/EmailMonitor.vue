<template>
  <div class="page">
    <Message severity="info" :closable="false" class="info">
      <div class="info-title">QQ &#37038;&#31665;&#37197;&#32622;&#35828;&#26126;</div>
      <div class="info-body">
        <p>1. &#30331;&#24405; QQ &#37038;&#31665;&#65292;&#36827;&#20837;&#8220;&#35774;&#32622;&#8221; &#8594; &#8220;&#36134;&#25143;&#8221;</p>
        <p>2. &#25214;&#21040;&#8220;IMAP/SMTP &#26381;&#21153;&#8221;&#24182;&#24320;&#21551;</p>
        <p>3. &#29983;&#25104;&#8220;&#25480;&#26435;&#30721;&#8221;&#65288;&#19981;&#26159; QQ &#23494;&#30721;&#65289;</p>
        <p>4. &#22312;&#19979;&#26041;&#37197;&#32622;&#20013;&#20351;&#29992;&#37038;&#31665;&#22320;&#22336;&#21644;&#25480;&#26435;&#30721;</p>
      </div>
    </Message>

    <Card class="panel">
      <template #title>
        <div class="panel-title">
          <span>&#37038;&#31665;&#37197;&#32622;</span>
          <div class="panel-actions">
            <Button :label="'\u5237\u65B0'" icon="pi pi-refresh" class="p-button-outlined" @click="loadAll" />
            <Button :label="'\u6DFB\u52A0\u90AE\u7BB1'" icon="pi pi-plus" @click="openModal" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable :value="configs" :loading="loading" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column :header="'\u90AE\u7BB1\u5730\u5740'">
            <template #body="{ data: row }">
              <div class="email-cell">
                <i class="pi pi-envelope" />
                <span>{{ row.email }}</span>
              </div>
            </template>
          </Column>
          <Column field="imap_host" :header="'IMAP \u670D\u52A1\u5668'" />
          <Column field="imap_port" :header="'\u7AEF\u53E3'" :style="{ width: '90px' }" />
          <Column :header="'\u72B6\u6001'" :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <Tag
                :severity="monitorStatus[row.id] === 'running' ? 'success' : 'secondary'"
                :value="monitorStatus[row.id] === 'running' ? '\u76D1\u63A7\u4E2D' : '\u5DF2\u505C\u6B62'"
              />
            </template>
          </Column>
          <Column :header="'\u6700\u540E\u68C0\u67E5'" :style="{ width: '160px' }">
            <template #body="{ data: row }">
              {{ row.last_check ? formatDateTime(row.last_check) : '-' }}
            </template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '260px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  v-if="monitorStatus[row.id] === 'running'"
                  size="small"
                  severity="danger"
                  class="p-button-outlined"
                  icon="pi pi-stop"
                  :label="'\u505C\u6B62'"
                  @click="handleStopMonitor(row.id)"
                />
                <Button
                  v-else
                  size="small"
                  severity="success"
                  class="p-button-outlined"
                  icon="pi pi-play"
                  :label="'\u542F\u52A8'"
                  @click="handleStartMonitor(row.id)"
                />

                <Button
                  size="small"
                  class="p-button-outlined"
                  icon="pi pi-bolt"
                  :label="'\u68C0\u67E5'"
                  :loading="checkLoading === row.id"
                  @click="handleManualCheck(row.id)"
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
          <span>&#37038;&#20214;&#22788;&#29702;&#26085;&#24535;</span>
          <Button :label="'\u5237\u65B0'" icon="pi pi-refresh" class="p-button-outlined" @click="loadLogs" />
        </div>
      </template>
      <template #content>
        <DataTable :value="logs" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="subject" :header="'\u4E3B\u9898'" />
          <Column field="from_address" :header="'\u53D1\u4EF6\u4EBA'" :style="{ width: '220px' }" />
          <Column :header="'\u9644\u4EF6'" :style="{ width: '110px' }">
            <template #body="{ data: row }">
              <Tag
                v-if="row.has_attachment"
                severity="info"
                :value="`${row.attachment_count}\u4E2A`"
              />
              <Tag v-else severity="secondary" :value="'\u65E0'" />
            </template>
          </Column>
          <Column :header="'\u63A5\u6536\u65F6\u95F4'" :style="{ width: '170px' }">
            <template #body="{ data: row }">
              {{ row.received_date ? formatDateTime(row.received_date) : '-' }}
            </template>
          </Column>
          <Column :header="'\u72B6\u6001'" :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <Tag
                :severity="row.status === 'processed' ? 'success' : 'warning'"
                :value="row.status === 'processed' ? '\u5DF2\u5904\u7406' : row.status"
              />
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="modalVisible" modal :header="'\u6DFB\u52A0\u90AE\u7BB1'" :style="{ width: '620px', maxWidth: '92vw' }">
      <form class="p-fluid" @submit.prevent="handleSubmit">
        <div class="grid-form">
          <div class="field col-span-2">
            <label for="email">&#37038;&#31665;&#22320;&#22336;</label>
            <InputText id="email" v-model.trim="form.email" autocomplete="email" />
            <small v-if="errors.email" class="p-error">{{ errors.email }}</small>
          </div>

          <div class="field col-span-2">
            <label for="preset">&#24555;&#25463;&#37197;&#32622;</label>
            <Dropdown
              id="preset"
              v-model="selectedPreset"
              :options="EMAIL_PRESETS"
              optionLabel="name"
              optionValue="name"
              :showClear="true"
              :placeholder="'\u9009\u62E9\u90AE\u7BB1\u63D0\u4F9B\u5546 ( \u53EF\u9009 )'"
              @change="handlePresetSelect"
            />
          </div>

          <div class="field">
            <label for="host">IMAP Host</label>
            <InputText id="host" v-model.trim="form.imap_host" />
            <small v-if="errors.imap_host" class="p-error">{{ errors.imap_host }}</small>
          </div>

          <div class="field">
            <label for="port">&#31471;&#21475;</label>
            <InputNumber id="port" v-model="form.imap_port" :min="1" :useGrouping="false" />
            <small v-if="errors.imap_port" class="p-error">{{ errors.imap_port }}</small>
          </div>

          <div class="field col-span-2">
            <label for="password">&#25480;&#26435;&#30721; / &#23494;&#30721;</label>
            <Password id="password" v-model="form.password" toggleMask :feedback="false" autocomplete="current-password" />
            <small v-if="errors.password" class="p-error">{{ errors.password }}</small>
          </div>
        </div>

        <div class="switch-row">
          <span class="switch-label">&#21551;&#29992;</span>
          <InputSwitch v-model="form.is_active" />
        </div>

        <div class="footer">
          <Button
            type="button"
            class="p-button-outlined"
            severity="secondary"
            :label="'\u6D4B\u8BD5\u8FDE\u63A5'"
            icon="pi pi-link"
            :loading="testLoading"
            @click="handleTest"
          />
          <div class="footer-right">
            <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" @click="modalVisible = false" />
            <Button type="submit" :label="'\u4FDD\u5B58'" icon="pi pi-check" :loading="saving" />
          </div>
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
import Dropdown from 'primevue/dropdown'
import InputNumber from 'primevue/inputnumber'
import InputSwitch from 'primevue/inputswitch'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { emailApi } from '@/api'
import type { EmailConfig, EmailLog } from '@/types'

const toast = useToast()
const confirm = useConfirm()

const EMAIL_PRESETS = [
  { name: 'QQ \u90AE\u7BB1', host: 'imap.qq.com', port: 993 },
  { name: '163 \u90AE\u7BB1', host: 'imap.163.com', port: 993 },
  { name: '126 \u90AE\u7BB1', host: 'imap.126.com', port: 993 },
  { name: 'Gmail', host: 'imap.gmail.com', port: 993 },
  { name: 'Outlook', host: 'imap-mail.outlook.com', port: 993 },
  { name: '\u65B0\u6D6A\u90AE\u7BB1', host: 'imap.sina.com', port: 993 },
]

const loading = ref(false)
const saving = ref(false)
const configs = ref<EmailConfig[]>([])
const logs = ref<EmailLog[]>([])
const monitorStatus = ref<Record<string, string>>({})
const modalVisible = ref(false)
const testLoading = ref(false)
const checkLoading = ref<string | null>(null)
const selectedPreset = ref<string | null>(null)

const form = reactive({
  email: '',
  imap_host: '',
  imap_port: 993,
  password: '',
  is_active: true,
})

const errors = reactive({
  email: '',
  imap_host: '',
  imap_port: '',
  password: '',
})

const resetForm = () => {
  form.email = ''
  form.imap_host = ''
  form.imap_port = 993
  form.password = ''
  form.is_active = true
  selectedPreset.value = null
  errors.email = ''
  errors.imap_host = ''
  errors.imap_port = ''
  errors.password = ''
}

const validate = () => {
  errors.email = ''
  errors.imap_host = ''
  errors.imap_port = ''
  errors.password = ''

  if (!form.email) {
    errors.email = '\u8BF7\u8F93\u5165\u90AE\u7BB1\u5730\u5740'
  } else if (!/^[^\\s@]+@[^\\s@]+\\.[^\\s@]+$/.test(form.email)) {
    errors.email = '\u8BF7\u8F93\u5165\u6709\u6548\u7684\u90AE\u7BB1\u5730\u5740'
  }
  if (!form.imap_host) errors.imap_host = '\u8BF7\u8F93\u5165 IMAP \u670D\u52A1\u5668\u5730\u5740'
  if (!form.imap_port || form.imap_port < 1) errors.imap_port = '\u8BF7\u8F93\u5165\u7AEF\u53E3\u53F7'
  if (!form.password) errors.password = '\u8BF7\u8F93\u5165\u6388\u6743\u7801\u6216\u5BC6\u7801'

  return !errors.email && !errors.imap_host && !errors.imap_port && !errors.password
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const res = await emailApi.getConfigs()
    if (res.data.success && res.data.data) configs.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u90AE\u7BB1\u914D\u7F6E\u5931\u8D25', life: 3000 })
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  try {
    const res = await emailApi.getLogs(undefined, 50)
    if (res.data.success && res.data.data) logs.value = res.data.data
  } catch (error) {
    console.error('Load logs failed:', error)
  }
}

const loadMonitorStatus = async () => {
  try {
    const res = await emailApi.getMonitoringStatus()
    if (res.data.success && res.data.data) {
      const statusMap: Record<string, string> = {}
      res.data.data.forEach((item: any) => {
        statusMap[item.configId] = item.status
      })
      monitorStatus.value = statusMap
    }
  } catch (error) {
    console.error('Load monitor status failed:', error)
  }
}

const loadAll = async () => {
  await Promise.all([loadConfigs(), loadLogs(), loadMonitorStatus()])
}

const openModal = () => {
  resetForm()
  modalVisible.value = true
}

const handlePresetSelect = () => {
  const name = selectedPreset.value
  if (!name) return
  const selected = EMAIL_PRESETS.find((p) => p.name === name)
  if (selected) {
    form.imap_host = selected.host
    form.imap_port = selected.port
  }
}

const handleTest = async () => {
  if (!validate()) return
  testLoading.value = true
  try {
    const res = await emailApi.testConnection({
      email: form.email,
      imap_host: form.imap_host,
      imap_port: form.imap_port,
      password: form.password,
    })
    if (res.data.success) {
      toast.add({ severity: 'success', summary: '\u8FDE\u63A5\u6D4B\u8BD5\u6210\u529F', life: 2200 })
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '\u8FDE\u63A5\u6D4B\u8BD5\u5931\u8D25', life: 3500 })
    }
  } catch {
    toast.add({ severity: 'error', summary: '\u8FDE\u63A5\u6D4B\u8BD5\u5931\u8D25', life: 3500 })
  } finally {
    testLoading.value = false
  }
}

const handleSubmit = async () => {
  if (!validate()) return
  saving.value = true
  try {
    await emailApi.createConfig({
      email: form.email,
      imap_host: form.imap_host,
      imap_port: form.imap_port,
      password: form.password,
      is_active: form.is_active ? 1 : 0,
    })
    toast.add({ severity: 'success', summary: '\u90AE\u7BB1\u914D\u7F6E\u521B\u5EFA\u6210\u529F', life: 2200 })
    modalVisible.value = false
    await loadAll()
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
    message: '\u786E\u5B9A\u5220\u9664\u8BE5\u90AE\u7BB1\u914D\u7F6E\u5417\uFF1F',
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
    await emailApi.deleteConfig(id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    await loadAll()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
  }
}

const handleStartMonitor = async (id: string) => {
  try {
    await emailApi.startMonitoring(id)
    toast.add({ severity: 'success', summary: '\u76D1\u63A7\u5DF2\u542F\u52A8', life: 2000 })
    await loadMonitorStatus()
  } catch {
    toast.add({ severity: 'error', summary: '\u542F\u52A8\u76D1\u63A7\u5931\u8D25', life: 3000 })
  }
}

const handleStopMonitor = async (id: string) => {
  try {
    await emailApi.stopMonitoring(id)
    toast.add({ severity: 'success', summary: '\u76D1\u63A7\u5DF2\u505C\u6B62', life: 2000 })
    await loadMonitorStatus()
  } catch {
    toast.add({ severity: 'error', summary: '\u505C\u6B62\u76D1\u63A7\u5931\u8D25', life: 3000 })
  }
}

const handleManualCheck = async (id: string) => {
  checkLoading.value = id
  try {
    const res = await emailApi.manualCheck(id)
    if (res.data.success) {
      toast.add({ severity: 'success', summary: res.data.message || '\u68C0\u67E5\u5B8C\u6210', life: 2200 })
      if (res.data.data && res.data.data.newEmails > 0) {
        await loadLogs()
      }
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '\u68C0\u67E5\u5931\u8D25', life: 3500 })
    }
  } catch {
    toast.add({ severity: 'error', summary: '\u68C0\u67E5\u90AE\u4EF6\u5931\u8D25', life: 3500 })
  } finally {
    checkLoading.value = null
  }
}

const formatDateTime = (date: string) => dayjs(date).format('MM-DD HH:mm')

onMounted(() => {
  loadAll()
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

.info-body p {
  margin: 6px 0;
  color: var(--color-text-secondary);
  line-height: 1.6;
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

.email-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  color: var(--color-text-primary);
}

.email-cell i {
  color: var(--color-primary);
}

.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.grid-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.col-span-2 {
  grid-column: span 2 / span 2;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(0, 0, 0, 0.02);
  margin-top: 12px;
}

.switch-label {
  font-weight: 700;
  color: var(--color-text-secondary);
}

.footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: 14px;
  flex-wrap: wrap;
}

.footer-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

@media (max-width: 540px) {
  .grid-form {
    grid-template-columns: 1fr;
  }
  .col-span-2 {
    grid-column: auto;
  }
}
</style>

