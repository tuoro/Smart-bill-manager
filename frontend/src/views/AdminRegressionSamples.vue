<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>回归样本</span>
          <div class="toolbar">
            <div class="toolbar-toggle">
              <span class="muted">脱敏导出</span>
              <InputSwitch v-model="exportRedact" />
            </div>
            <Button
              class="p-button-outlined"
              :icon="selectMode ? 'pi pi-times' : 'pi pi-check-square'"
              :label="selectMode ? '取消选择' : '选择'"
              :disabled="loading"
              @click="toggleSelectMode"
            />
            <Button
              class="p-button-outlined"
              icon="pi pi-download"
              label="下载本地所选 ZIP"
              :disabled="loading || !selectMode || selectedLocalCount === 0"
              @click="exportLocalSelectedZip"
            />
            <Button
              class="p-button-danger p-button-outlined"
              icon="pi pi-trash"
              label="删除"
              :disabled="loading || !selectMode || selected.length === 0"
              @click="onDeleteSelectedClick"
            />
            <Button class="p-button-outlined" icon="pi pi-refresh" label="刷新" :disabled="loading" @click="reload" />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="!isAdmin" severity="warn" :closable="false">仅管理员可访问</Message>

        <div v-else class="content">
          <div class="list-toolbar">
            <SelectButton v-model="kindFilter" :options="kindOptions" optionLabel="label" optionValue="value" />
            <SelectButton v-model="originFilter" :options="originOptions" optionLabel="label" optionValue="value" />
            <span class="spacer" />
          </div>

          <DataTable
            class="samples-table sbm-dt-fixed"
            :value="items"
            :loading="loading"
            responsiveLayout="scroll"
            :tableStyle="samplesTableStyle"
            :paginator="true"
            :rows="limit"
            :first="offset"
            :rowsPerPageOptions="[10, 20, 50, 100]"
            paginatorTemplate="RowsPerPageDropdown FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink CurrentPageReport"
            currentPageReportTemplate="共 {totalRecords} 条"
            :totalRecords="total"
            lazy
            dataKey="id"
            v-model:selection="selected"
            @page="onPage"
          >
            <Column v-if="selectMode" selectionMode="multiple" :style="{ width: '48px' }" />
            <Column field="origin" header="来源" :style="{ width: '10%', minWidth: '90px' }">
              <template #body="{ data: row }">
                <span class="dt-nowrap">
                  <Tag v-if="row.origin === 'repo'" severity="secondary" value="云端" />
                  <Tag v-else severity="info" value="本地" />
                </span>
              </template>
            </Column>
            <Column field="kind" header="类型" :style="{ width: '12%', minWidth: '120px' }">
              <template #body="{ data: row }">
                <span class="dt-nowrap">
                  <Tag v-if="row.kind === 'payment_screenshot'" severity="info" value="支付截图" />
                  <Tag v-else-if="row.kind === 'invoice'" severity="success" value="发票" />
                  <Tag v-else severity="secondary" :value="row.kind" />
                </span>
              </template>
            </Column>
            <Column field="name" header="名称" :style="{ width: '20%', minWidth: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.name">{{ row.name }}</span>
              </template>
            </Column>
            <Column field="source_id" header="来源ID" :style="{ width: '26%', minWidth: '240px' }">
              <template #body="{ data: row }">
                <span class="mono sbm-ellipsis" :title="String(row?.source_id || '')">{{ displaySourceId(row) }}</span>
              </template>
            </Column>
            <Column field="created_at" header="创建时间" :style="{ width: '16%', minWidth: '170px' }">
              <template #body="{ data: row }"><span class="dt-nowrap">{{ formatDateTime(row.created_at) }}</span></template>
            </Column>
            <Column field="updated_at" header="更新时间" :style="{ width: '16%', minWidth: '170px' }">
              <template #body="{ data: row }"><span class="dt-nowrap">{{ formatDateTime(row.updated_at) }}</span></template>
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
import Message from 'primevue/message'
import SelectButton from 'primevue/selectbutton'
import Tag from 'primevue/tag'
import InputSwitch from 'primevue/inputswitch'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { regressionSamplesApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { debounce } from '@/utils/debounce'
import { getApiErrorMessage, isRequestCanceled } from '@/utils/http'
import type { RegressionSample } from '@/api/regressionSamples'

const toast = useToast()
const confirm = useConfirm()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.user?.role === 'admin')

const samplesTableStyle = {
  minWidth: '920px',
  tableLayout: 'fixed',
} as const

const displaySourceId = (row: RegressionSample) => {
  const raw = String(row?.source_id || '')
  const origin = String(row?.origin || '')
  if (origin === 'repo' && (raw.includes('/') || raw.includes('\\'))) {
    const parts = raw.split(/[/\\\\]+/).filter(Boolean)
    return parts[parts.length - 1] || raw
  }
  return raw
}

const loading = ref(false)
const listAbort = ref<AbortController | null>(null)
const items = ref<RegressionSample[]>([])
const total = ref(0)
const selected = ref<RegressionSample[]>([])
const selectMode = ref(false)
const exportRedact = ref(true)
const selectedLocalCount = computed(() => selected.value.filter((r) => (r.origin || 'ui') === 'ui').length)

type KindValue = 'all' | 'payment_screenshot' | 'invoice'
const kindFilter = ref<KindValue>('all')
const kindOptions: Array<{ label: string; value: KindValue }> = [
  { label: '全部', value: 'all' },
  { label: '支付截图', value: 'payment_screenshot' },
  { label: '发票', value: 'invoice' },
]

type OriginValue = 'all' | 'ui' | 'repo'
const originFilter = ref<OriginValue>('all')
const originOptions: Array<{ label: string; value: OriginValue }> = [
  { label: '全部', value: 'all' },
  { label: '本地', value: 'ui' },
  { label: '云端', value: 'repo' },
]

const offset = ref(0)
const limit = ref(20)

const formatDateTime = (v?: string | null) => {
  if (!v) return '-'
  const d = dayjs(v)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : v
}

const load = async () => {
  if (!isAdmin.value) return
  listAbort.value?.abort()
  const controller = new AbortController()
  listAbort.value = controller
  loading.value = true
  try {
    const kind = kindFilter.value === 'all' ? undefined : kindFilter.value
    const origin = originFilter.value === 'all' ? undefined : originFilter.value
    const res = await regressionSamplesApi.list({ kind, origin, limit: limit.value, offset: offset.value }, { signal: controller.signal })
    if (res.data.success && res.data.data) {
      items.value = res.data.data.items || []
      total.value = res.data.data.total || 0
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '获取回归样本失败', life: 3000 })
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    toast.add({ severity: 'error', summary: getApiErrorMessage(e, '获取回归样本失败'), life: 3000 })
  } finally {
    loading.value = false
    if (listAbort.value === controller) listAbort.value = null
  }
}

const reload = async () => {
  offset.value = 0
  selected.value = []
  selectMode.value = false
  await load()
}

const onPage = async (e: any) => {
  offset.value = e.first || 0
  limit.value = e.rows || 20
  await load()
}

const toggleSelectMode = () => {
  selectMode.value = !selectMode.value
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
  }
}

const onDeleteSelectedClick = () => {
  if (!isAdmin.value) return
  if (!selectMode.value) return
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

const exportLocalSelectedZip = async () => {
  if (!isAdmin.value) return
  if (!selectMode.value) return
  if (selectedLocalCount.value === 0) {
    toast.add({ severity: 'warn', summary: '请先选择本地样本', life: 2500 })
    return
  }
  loading.value = true
  try {
    const kind = kindFilter.value === 'all' ? undefined : kindFilter.value
    const ids = selected.value.map((r) => r.id)
    const res = await regressionSamplesApi.exportSelectedZip({ ids, kind, origin: 'ui', redact: exportRedact.value })
    const blob = new Blob([res.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = parseFilename(res.headers?.['content-disposition']) || 'regression_samples.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.URL.revokeObjectURL(url)
    toast.add({ severity: 'success', summary: '已导出本地样本 ZIP', life: 2000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '导出失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
})

const reloadDebounced = debounce(() => {
  void reload()
}, 200)

watch(kindFilter, () => {
  reloadDebounced()
})

watch(originFilter, () => {
  reloadDebounced()
})

onBeforeUnmount(() => {
  reloadDebounced.cancel()
  listAbort.value?.abort()
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

.toolbar-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
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

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.dt-nowrap {
  white-space: nowrap;
}

.samples-table :deep(.p-tag),
.samples-table :deep(.p-tag-label) {
  white-space: nowrap;
}
</style>
