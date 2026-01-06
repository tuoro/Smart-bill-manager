<template>
  <div class="page">
    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>QQ 邮箱配置说明</span>
        </div>
      </template>
      <template #content>
        <ol class="guide-list">
          <li>登录 QQ 邮箱，进入“设置” → “账户”</li>
          <li>找到“IMAP/SMTP 服务”并开启</li>
          <li>生成“授权码”（不是 QQ 密码）</li>
          <li>在下方配置中使用邮箱地址和授权码</li>
        </ol>
      </template>
    </Card>

    <Card class="panel sbm-surface">
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
        <DataTable
          class="sbm-dt-fixed"
          :value="configs"
          :loading="loading"
          :paginator="true"
          :rows="configPageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          :tableStyle="{ minWidth: '980px', tableLayout: 'fixed' }"
          emptyMessage="暂无邮箱配置"
          responsiveLayout="scroll"
          @page="onConfigPage"
        >
          <Column :header="'\u90AE\u7BB1\u5730\u5740'" :style="{ width: '260px' }">
            <template #body="{ data: row }">
              <div class="email-cell">
                <i class="pi pi-envelope" />
                <span class="sbm-ellipsis" :title="row.email">{{ row.email }}</span>
              </div>
            </template>
          </Column>
          <Column field="imap_host" :header="'IMAP \u670D\u52A1\u5668'" :style="{ width: '220px' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="row.imap_host">{{ row.imap_host }}</span>
            </template>
          </Column>
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
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '200px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  v-if="monitorStatus[row.id] === 'running'"
                  size="small"
                  severity="danger"
                  class="p-button-outlined"
                  icon="pi pi-stop"
                  :title="'停止监控'"
                  @click="handleStopMonitor(row.id)"
                />
                <Button
                  v-else
                  size="small"
                  severity="success"
                  class="p-button-outlined"
                  icon="pi pi-play"
                  :title="'启动监控'"
                  @click="handleStartMonitor(row.id)"
                />

                <Button
                  size="small"
                  class="p-button-outlined"
                  icon="pi pi-bolt"
                  :title="'检查未读邮件'"
                  :loading="checkLoading === row.id"
                  @click="handleManualCheck(row.id)"
                />

                <Button
                  size="small"
                  class="p-button-outlined"
                  icon="pi pi-download"
                  :title="'全量同步'"
                  :loading="fullSyncLoading === row.id"
                  @click="handleManualFullSync(row.id)"
                />

                <Button
                  size="small"
                  severity="danger"
                  class="p-button-text"
                  icon="pi pi-trash"
                  :title="'删除配置'"
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
          <span>&#37038;&#20214;&#22788;&#29702;&#26085;&#24535;</span>
          <Button :label="'\u5237\u65B0'" icon="pi pi-refresh" class="p-button-outlined" @click="loadLogs" />
        </div>
      </template>
      <template #content>
        <DataTable
          class="sbm-dt-fixed"
          :value="logs"
          :paginator="true"
          :rows="logPageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          :tableStyle="{ minWidth: '1260px', tableLayout: 'fixed' }"
          emptyMessage="暂无邮件日志"
          responsiveLayout="scroll"
          @page="onLogPage"
        >
          <Column field="subject" :header="'\u4E3B\u9898'" :style="{ width: '360px' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="row.subject || ''">{{ row.subject || '-' }}</span>
            </template>
          </Column>
          <Column field="from_address" :header="'\u53D1\u4EF6\u4EBA'" :style="{ width: '240px' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="row.from_address || ''">{{ row.from_address || '-' }}</span>
            </template>
          </Column>
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
          <Column :header="'\u72B6\u6001'" :style="{ width: '280px' }">
            <template #body="{ data: row }">
              <div class="log-status">
                <Tag :severity="getLogStatusSeverity(row.status)" :value="getLogStatusLabel(row.status)" />
                <small v-if="row.parse_error" class="p-error sbm-ellipsis" :title="row.parse_error">{{ row.parse_error }}</small>
                <small v-else-if="row.parsed_invoice_id" class="muted sbm-ellipsis" :title="row.parsed_invoice_id"
                  >发票ID：{{ row.parsed_invoice_id }}</small
                >
              </div>
            </template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '160px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  size="small"
                  class="p-button-outlined"
                  icon="pi pi-cog"
                  :label="'解析'"
                  :loading="parseLoading === row.id"
                  :disabled="row.status === 'parsing' || row.status === 'parsed'"
                  @click="handleParseLog(row.id)"
                />
                <Button
                  v-if="row.parsed_invoice_id"
                  size="small"
                  class="p-button-text"
                  icon="pi pi-copy"
                  :title="'复制发票ID'"
                  @click="copyText(row.parsed_invoice_id)"
                />
              </div>
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
            <InputText id="email" v-model.trim="form.email" type="email" inputmode="email" autocomplete="email" />
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
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import Dropdown from 'primevue/dropdown'
import InputNumber from 'primevue/inputnumber'
import InputSwitch from 'primevue/inputswitch'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { emailApi } from '@/api'
import { useNotificationStore } from '@/stores/notifications'
import { isRequestCanceled } from '@/utils/http'
import type { EmailConfig, EmailLog } from '@/types'

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()

const EMAIL_LOG_TS_KEY = 'sbm.email.lastLogTs.v1'
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
const parseLoading = ref<string | null>(null)
const selectedPreset = ref<string | null>(null)
const pollTimer = ref<number | null>(null)
const configPageSize = ref(10)
const logPageSize = ref(10)
const configsAbort = ref<AbortController | null>(null)
const logsAbort = ref<AbortController | null>(null)
const statusAbort = ref<AbortController | null>(null)
const pollInFlight = ref(false)
const pollErrorStreak = ref(0)
const BASE_POLL_MS = 4000
const MAX_POLL_MS = 30000

const onConfigPage = (e: any) => {
  configPageSize.value = e?.rows || configPageSize.value
}

const onLogPage = (e: any) => {
  logPageSize.value = e?.rows || logPageSize.value
}

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
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email)) {
    errors.email = '\u8BF7\u8F93\u5165\u6709\u6548\u7684\u90AE\u7BB1\u5730\u5740'
  }
  if (!form.imap_host) errors.imap_host = '\u8BF7\u8F93\u5165 IMAP \u670D\u52A1\u5668\u5730\u5740'
  if (!form.imap_port || form.imap_port < 1) errors.imap_port = '\u8BF7\u8F93\u5165\u7AEF\u53E3\u53F7'
  if (!form.password) errors.password = '\u8BF7\u8F93\u5165\u6388\u6743\u7801\u6216\u5BC6\u7801'

  return !errors.email && !errors.imap_host && !errors.imap_port && !errors.password
}

const loadConfigs = async (): Promise<boolean> => {
  configsAbort.value?.abort()
  const controller = new AbortController()
  configsAbort.value = controller
  loading.value = true
  try {
    const res = await emailApi.getConfigs({ signal: controller.signal })
    if (res.data.success && res.data.data) configs.value = res.data.data
    return Boolean(res.data.success)
  } catch (e: any) {
    if (isRequestCanceled(e)) return false
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u90AE\u7BB1\u914D\u7F6E\u5931\u8D25', life: 3000 })
    return false
  } finally {
    if (configsAbort.value === controller) configsAbort.value = null
    loading.value = false
  }
}

const loadLogs = async (): Promise<boolean> => {
  logsAbort.value?.abort()
  const controller = new AbortController()
  logsAbort.value = controller
  try {
    const res = await emailApi.getLogs(undefined, 500, { signal: controller.signal })
    if (res.data.success && res.data.data) {
      logs.value = res.data.data

      const lastTs = getStoredTs(EMAIL_LOG_TS_KEY)
      const maxTs = Math.max(0, ...logs.value.map((x) => parseTs(x.created_at)))
      if (lastTs === 0) {
        if (maxTs > 0) setStoredTs(EMAIL_LOG_TS_KEY, maxTs)
        return true
      }

      const newLogs = logs.value
        .filter((x) => parseTs(x.created_at) > lastTs)
        .sort((a, b) => parseTs(b.created_at) - parseTs(a.created_at))
        .slice(0, 3)

      for (const item of newLogs) {
        const subject = item.subject || '（无主题）'
        const detail = item.has_attachment
          ? `${subject}（附件 ${item.attachment_count || 0} 个）`
          : subject
        notifications.add({ severity: 'info', title: '邮箱收到新邮件', detail })
      }

      if (maxTs > lastTs) setStoredTs(EMAIL_LOG_TS_KEY, maxTs)
    }
    return Boolean(res.data.success)
  } catch (e: any) {
    if (isRequestCanceled(e)) return false
    console.error('Load logs failed:', e)
    return false
  } finally {
    if (logsAbort.value === controller) logsAbort.value = null
  }
}

const loadMonitorStatus = async (): Promise<boolean> => {
  statusAbort.value?.abort()
  const controller = new AbortController()
  statusAbort.value = controller
  try {
    const res = await emailApi.getMonitoringStatus({ signal: controller.signal })
    if (res.data.success && res.data.data) {
      const statusMap: Record<string, string> = {}
      res.data.data.forEach((item: any) => {
        statusMap[item.configId] = item.status
      })
      monitorStatus.value = statusMap
    }
    return Boolean(res.data.success)
  } catch (e: any) {
    if (isRequestCanceled(e)) return false
    console.error('Load monitor status failed:', e)
    return false
  } finally {
    if (statusAbort.value === controller) statusAbort.value = null
  }
}

const loadAll = async () => {
  await Promise.all([loadConfigs(), loadLogs(), loadMonitorStatus()])
}

const getLogStatusSeverity = (status: string) => {
  switch (status) {
    case 'parsed':
      return 'success'
    case 'parsing':
      return 'info'
    case 'error':
      return 'danger'
    case 'received':
      return 'warning'
    default:
      return 'secondary'
  }
}

const getLogStatusLabel = (status: string) => {
  switch (status) {
    case 'parsed':
      return '已解析'
    case 'parsing':
      return '解析中'
    case 'error':
      return '解析失败'
    case 'received':
      return '待解析'
    default:
      return status || '-'
  }
}

const copyText = async (text: string) => {
  const value = String(text || '').trim()
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    toast.add({ severity: 'success', summary: '已复制', life: 1500 })
  } catch {
    toast.add({ severity: 'warn', summary: '复制失败（浏览器限制）', life: 2200 })
  }
}

const handleParseLog = async (id: string) => {
  parseLoading.value = id
  try {
    const res = await emailApi.parseLog(id)
    if (res.data.success && res.data.data) {
      toast.add({ severity: 'success', summary: '解析成功', life: 2200 })
      notifications.add({ severity: 'success', title: '邮件已解析为发票', detail: res.data.data.id })
      await loadLogs()
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '解析失败', life: 3500 })
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '解析失败', life: 3500 })
  } finally {
    parseLoading.value = null
  }
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
      notifications.add({ severity: 'success', title: '邮箱连接测试成功', detail: form.email })
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '\u8FDE\u63A5\u6D4B\u8BD5\u5931\u8D25', life: 3500 })
      notifications.add({ severity: 'error', title: '邮箱连接测试失败', detail: res.data.message || form.email })
    }
  } catch {
    toast.add({ severity: 'error', summary: '\u8FDE\u63A5\u6D4B\u8BD5\u5931\u8D25', life: 3500 })
    notifications.add({ severity: 'error', title: '邮箱连接测试失败', detail: form.email })
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
    notifications.add({ severity: 'success', title: '邮箱配置已创建', detail: form.email })
    modalVisible.value = false
    await loadAll()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({
      severity: 'error',
      summary: err.response?.data?.message || '\u521B\u5EFA\u914D\u7F6E\u5931\u8D25',
      life: 3500,
    })
    notifications.add({
      severity: 'error',
      title: '邮箱配置创建失败',
      detail: err.response?.data?.message || form.email,
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
    notifications.add({ severity: 'info', title: '邮箱配置已删除', detail: id })
    await loadAll()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
    notifications.add({ severity: 'error', title: '邮箱配置删除失败', detail: id })
  }
}

const handleStartMonitor = async (id: string) => {
  try {
    await emailApi.startMonitoring(id)
    toast.add({ severity: 'success', summary: '\u76D1\u63A7\u5DF2\u542F\u52A8', life: 2000 })
    notifications.add({ severity: 'success', title: '邮箱监控已启动', detail: id })
    await loadMonitorStatus()
  } catch {
    toast.add({ severity: 'error', summary: '\u542F\u52A8\u76D1\u63A7\u5931\u8D25', life: 3000 })
    notifications.add({ severity: 'error', title: '邮箱监控启动失败', detail: id })
  }
}

const handleStopMonitor = async (id: string) => {
  try {
    await emailApi.stopMonitoring(id)
    toast.add({ severity: 'success', summary: '\u76D1\u63A7\u5DF2\u505C\u6B62', life: 2000 })
    notifications.add({ severity: 'info', title: '邮箱监控已停止', detail: id })
    await loadMonitorStatus()
  } catch {
    toast.add({ severity: 'error', summary: '\u505C\u6B62\u76D1\u63A7\u5931\u8D25', life: 3000 })
    notifications.add({ severity: 'error', title: '邮箱监控停止失败', detail: id })
  }
}

const handleManualCheck = async (id: string) => {
  checkLoading.value = id
  try {
    const res = await emailApi.manualCheck(id)
    if (res.data.success) {
      toast.add({ severity: 'success', summary: res.data.message || '\u68C0\u67E5\u5B8C\u6210', life: 2200 })
      const newEmails = res.data.data?.newEmails || 0
      notifications.add({
        severity: newEmails > 0 ? 'success' : 'info',
        title: '邮箱检查完成',
        detail: newEmails > 0 ? `新邮件 ${newEmails} 封（已尝试下载附件）` : '暂无新邮件',
      })
      if (res.data.data && res.data.data.newEmails > 0) {
        await loadLogs()
      }
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '\u68C0\u67E5\u5931\u8D25', life: 3500 })
      notifications.add({ severity: 'error', title: '邮箱检查失败', detail: res.data.message || id })
    }
  } catch {
    toast.add({ severity: 'error', summary: '\u68C0\u67E5\u90AE\u4EF6\u5931\u8D25', life: 3500 })
    notifications.add({ severity: 'error', title: '邮箱检查失败', detail: id })
  } finally {
    checkLoading.value = null
  }
}

const fullSyncLoading = ref<string | null>(null)

const handleManualFullSync = async (id: string) => {
  fullSyncLoading.value = id
  try {
    const res = await emailApi.manualFullSync(id)
    if (res.data.success) {
      toast.add({ severity: 'success', summary: res.data.message || '全量同步完成', life: 2500 })
      const newEmails = res.data.data?.newEmails || 0
      notifications.add({
        severity: newEmails > 0 ? 'success' : 'info',
        title: '邮箱全量同步完成',
        detail: newEmails > 0 ? `新增记录 ${newEmails} 封` : '没有新增记录',
      })
      await loadLogs()
      await loadMonitorStatus()
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '全量同步失败', life: 3500 })
      notifications.add({ severity: 'error', title: '邮箱全量同步失败', detail: res.data.message || id })
    }
  } catch {
    toast.add({ severity: 'error', summary: '全量同步失败', life: 3500 })
    notifications.add({ severity: 'error', title: '邮箱全量同步失败', detail: id })
  } finally {
    fullSyncLoading.value = null
  }
}

const formatDateTime = (date: string) => dayjs(date).format('MM-DD HH:mm')

const pollTick = async () => {
  if (document.visibilityState !== 'visible') return
  if (pollInFlight.value) return
  pollInFlight.value = true
  try {
    const [okLogs, okStatus] = await Promise.all([loadLogs(), loadMonitorStatus()])
    if (okLogs && okStatus) pollErrorStreak.value = 0
    else pollErrorStreak.value = Math.min(6, pollErrorStreak.value + 1)
  } finally {
    pollInFlight.value = false
  }
}

const stopPolling = () => {
  if (pollTimer.value) {
    window.clearTimeout(pollTimer.value)
    pollTimer.value = null
  }
}

const nextPollDelayMs = () => {
  const exp = Math.min(3, pollErrorStreak.value)
  return Math.min(MAX_POLL_MS, BASE_POLL_MS * Math.pow(2, exp))
}

const scheduleNextPoll = (delayMs: number) => {
  stopPolling()
  pollTimer.value = window.setTimeout(async () => {
    await pollTick()
    scheduleNextPoll(nextPollDelayMs())
  }, delayMs)
}

const handleVisibilityChange = () => {
  if (document.visibilityState !== 'visible') {
    stopPolling()
    return
  }
  pollErrorStreak.value = 0
  void pollTick()
  scheduleNextPoll(BASE_POLL_MS)
}

onMounted(() => {
  void loadAll()
  void pollTick()
  scheduleNextPoll(BASE_POLL_MS)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onBeforeUnmount(() => {
  stopPolling()
  configsAbort.value?.abort()
  logsAbort.value?.abort()
  statusAbort.value?.abort()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.grid-form .field {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.grid-form .field label {
  display: block;
  font-weight: 700;
  color: var(--p-text-color);
}

.grid-form .field :deep(.p-inputtext),
.grid-form .field :deep(.p-dropdown),
.grid-form .field :deep(.p-inputnumber),
.grid-form .field :deep(.p-password) {
  width: 100%;
}

.grid-form .field :deep(.p-inputnumber-input),
.grid-form .field :deep(.p-password input) {
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
  flex-wrap: nowrap;
}

.log-status {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.muted {
  color: var(--p-text-muted-color);
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
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
  border: 1px solid color-mix(in srgb, var(--p-surface-200), transparent 35%);
  background: color-mix(in srgb, var(--p-surface-0), transparent 10%);
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
