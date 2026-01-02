<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>回归样本</span>
          <div class="toolbar">
            <div class="autosync">
              <span class="muted">自动同步</span>
              <InputSwitch v-model="autoSyncEnabled" :disabled="loading" />
              <Dropdown
                v-model="autoSyncIntervalMs"
                :options="autoSyncIntervalOptions"
                optionLabel="label"
                optionValue="value"
                class="interval-dropdown"
                :disabled="loading || !autoSyncEnabled"
              />
            </div>
            <Button class="p-button-outlined" icon="pi pi-sync" label="同步" :disabled="loading" @click="syncFromRepo" />
            <Button class="p-button-outlined" icon="pi pi-download" label="导出 ZIP" :disabled="loading || total === 0" @click="exportZip" />
            <Button
              class="p-button-danger p-button-outlined"
              icon="pi pi-trash"
              :label="batchDeleteMode ? `删除所选（${selected.length}）` : '删除所选'"
              :disabled="loading || (batchDeleteMode && selected.length === 0)"
              @click="onBulkDeleteClick"
            />
            <Button v-if="batchDeleteMode" class="p-button-text" label="取消" :disabled="loading" @click="exitBatchDeleteMode" />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="!isAdmin" severity="warn" :closable="false">仅管理员可访问</Message>

        <div v-else class="content">
          <div class="list-toolbar">
            <SelectButton v-model="kindFilter" :options="kindOptions" optionLabel="label" optionValue="value" />
            <span class="spacer" />
            <InputText v-model.trim="search" class="search" placeholder="搜索名称/来源ID" @keydown.enter="reload" />
            <Button class="p-button-outlined" icon="pi pi-refresh" label="刷新" :disabled="loading" @click="reload" />
          </div>

          <DataTable
            class="samples-table"
            :value="items"
            :loading="loading"
            responsiveLayout="scroll"
            :paginator="true"
            :rows="20"
            :totalRecords="total"
            lazy
            dataKey="id"
            v-model:selection="selected"
            @page="onPage"
          >
            <Column v-if="batchDeleteMode" selectionMode="multiple" :style="{ width: '48px' }" />
            <Column field="kind" header="类型" :style="{ width: '140px' }">
              <template #body="{ data: row }">
                <Tag v-if="row.kind === 'payment_screenshot'" severity="info" value="支付截图" />
                <Tag v-else-if="row.kind === 'invoice'" severity="success" value="发票" />
                <Tag v-else severity="secondary" :value="row.kind" />
              </template>
            </Column>
            <Column field="name" header="名称" :style="{ width: '28%' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.name">{{ row.name }}</span>
              </template>
            </Column>
            <Column field="source_id" header="来源ID" :style="{ width: '34%' }">
              <template #body="{ data: row }">
                <span class="mono sbm-ellipsis" :title="row.source_id">{{ row.source_id }}</span>
              </template>
            </Column>
            <Column field="created_at" header="创建时间" :style="{ width: '180px' }">
              <template #body="{ data: row }">{{ formatDateTime(row.created_at) }}</template>
            </Column>
            <Column field="updated_at" header="更新时间" :style="{ width: '180px' }">
              <template #body="{ data: row }">{{ formatDateTime(row.updated_at) }}</template>
            </Column>
          </DataTable>
        </div>
      </template>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import SelectButton from 'primevue/selectbutton'
import Tag from 'primevue/tag'
import InputSwitch from 'primevue/inputswitch'
import Dropdown from 'primevue/dropdown'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { regressionSamplesApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import type { RegressionSample } from '@/api/regressionSamples'

const toast = useToast()
const confirm = useConfirm()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.user?.role === 'admin')

const loading = ref(false)
const items = ref<RegressionSample[]>([])
const total = ref(0)
const selected = ref<RegressionSample[]>([])
const batchDeleteMode = ref(false)

const autoSyncEnabled = ref<boolean>(localStorage.getItem('sbm_regression_auto_sync_enabled') === '1')
const autoSyncIntervalMs = ref<number>(Number(localStorage.getItem('sbm_regression_auto_sync_interval_ms') || 15 * 60_000))
const autoSyncIntervalOptions = [
  { label: '5 分钟', value: 5 * 60_000 },
  { label: '15 分钟', value: 15 * 60_000 },
  { label: '60 分钟', value: 60 * 60_000 },
]
let autoSyncTimer: number | null = null

type KindValue = 'all' | 'payment_screenshot' | 'invoice'
const kindFilter = ref<KindValue>('all')
const kindOptions: Array<{ label: string; value: KindValue }> = [
  { label: '全部', value: 'all' },
  { label: '支付截图', value: 'payment_screenshot' },
  { label: '发票', value: 'invoice' },
]

const search = ref('')
const offset = ref(0)
const limit = ref(20)

const formatDateTime = (v?: string | null) => {
  if (!v) return '-'
  const d = dayjs(v)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : v
}

const load = async () => {
  if (!isAdmin.value) return
  loading.value = true
  try {
    const kind = kindFilter.value === 'all' ? undefined : kindFilter.value
    const res = await regressionSamplesApi.list({ kind, search: search.value || undefined, limit: limit.value, offset: offset.value })
    if (res.data.success && res.data.data) {
      items.value = res.data.data.items || []
      total.value = res.data.data.total || 0
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '获取回归样本失败', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '获取回归样本失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

const reload = async () => {
  offset.value = 0
  selected.value = []
  await load()
}

const onPage = async (e: any) => {
  offset.value = e.first || 0
  limit.value = e.rows || 20
  await load()
}

const exitBatchDeleteMode = () => {
  batchDeleteMode.value = false
  selected.value = []
}

const doDeleteSelected = async (rows: RegressionSample[]) => {
  if (!isAdmin.value) return
  if (rows.length === 0) return
  loading.value = true
  try {
    const res = await regressionSamplesApi.bulkDelete(rows.map((r) => r.id))
    if (res.data.success) {
      toast.add({ severity: 'success', summary: `已删除 ${res.data.data?.deleted || rows.length} 个样本`, life: 2000 })
    } else {
      toast.add({ severity: 'error', summary: res.data.message || '删除失败', life: 3000 })
    }
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '删除失败', life: 3000 })
  } finally {
    loading.value = false
    await reload()
    batchDeleteMode.value = false
  }
}

const onBulkDeleteClick = () => {
  if (!isAdmin.value) return
  if (!batchDeleteMode.value) {
    batchDeleteMode.value = true
    return
  }
  if (selected.value.length === 0) return
  confirm.require({
    message: `确定删除选中的 ${selected.value.length} 个回归样本吗？`,
    header: '删除确认',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: () => void doDeleteSelected(selected.value),
  })
}

const parseFilename = (disposition?: string) => {
  if (!disposition) return ''
  const m = disposition.match(/filename=\"?([^\";]+)\"?/i)
  return m?.[1] || ''
}

const syncFromRepo = async () => {
  if (!isAdmin.value) return
  if (loading.value) return
  loading.value = true
  try {
    const res = await regressionSamplesApi.syncFromRepo('repo_only')
    if (res.data.success && res.data.data) {
      const d = res.data.data
      const summary = `扫描 ${d.files}，新增 ${d.inserted}，更新 ${d.updated}，跳过 ${d.skipped}，错误 ${d.errors}`
      toast.add({ severity: d.errors > 0 ? 'warn' : 'success', summary: '同步完成', detail: summary, life: 3500 })
      await reload()
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '同步失败', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '同步失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

const exportZip = async () => {
  if (!isAdmin.value) return
  loading.value = true
  try {
    const kind = kindFilter.value === 'all' ? undefined : kindFilter.value
    const res = await regressionSamplesApi.exportZip(kind)
    const blob = new Blob([res.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = parseFilename(res.headers?.['content-disposition']) || 'regression_samples.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.URL.revokeObjectURL(url)
    toast.add({ severity: 'success', summary: '已导出 ZIP', life: 2000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '导出失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
  setupAutoSync()
})

watch(kindFilter, () => {
  void reload()
})

watch(autoSyncEnabled, (v) => {
  localStorage.setItem('sbm_regression_auto_sync_enabled', v ? '1' : '0')
  setupAutoSync()
})

watch(autoSyncIntervalMs, (v) => {
  localStorage.setItem('sbm_regression_auto_sync_interval_ms', String(v || 0))
  setupAutoSync()
})

const setupAutoSync = () => {
  if (autoSyncTimer) {
    window.clearInterval(autoSyncTimer)
    autoSyncTimer = null
  }
  if (!isAdmin.value || !autoSyncEnabled.value) return
  const ms = Number(autoSyncIntervalMs.value || 0)
  if (!ms || ms < 60_000) return
  autoSyncTimer = window.setInterval(() => {
    void syncFromRepo()
  }, ms)
}

onBeforeUnmount(() => {
  if (autoSyncTimer) window.clearInterval(autoSyncTimer)
})
</script>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.autosync {
  display: flex;
  align-items: center;
  gap: 8px;
}

.interval-dropdown {
  min-width: 120px;
}

.muted {
  color: var(--text-color-secondary, rgba(0, 0, 0, 0.55));
  font-size: 12px;
}

.content {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.list-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.spacer {
  flex: 1;
}

.search {
  min-width: 240px;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}
</style>
