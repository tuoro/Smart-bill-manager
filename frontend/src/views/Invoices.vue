<template>
  <div class="page">
    <div class="grid">
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#21457;&#31080;&#24635;&#25968;</div>
                <div class="stat-value">{{ stats?.totalCount || 0 }}</div>
              </div>
              <i class="pi pi-file stat-icon info" />
            </div>
          </template>
        </Card>
      </div>
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#21457;&#31080;&#24635;&#37329;&#39069;</div>
                <div class="stat-value">{{ `\u00A5${(stats?.totalAmount || 0).toFixed(2)}` }}</div>
              </div>
              <i class="pi pi-receipt stat-icon success" />
            </div>
          </template>
        </Card>
      </div>
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#26469;&#28304;&#20998;&#24067;</div>
                <div class="source-row">
                  <Tag severity="success" :value="`\u4E0A\u4F20 ${sourceStats.upload || 0}`" />
                  <Tag severity="info" :value="`\u90AE\u4EF6 ${sourceStats.email || 0}`" />
                  <Tag severity="warning" :value="`\u9489\u9489 ${sourceStats.dingtalk || 0}`" />
                </div>
              </div>
              <i class="pi pi-chart-pie stat-icon secondary" />
            </div>
          </template>
        </Card>
      </div>
    </div>

    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>&#21457;&#31080;&#21015;&#34920;</span>
          <Button :label="'\u4E0A\u4F20\u53D1\u7968'" icon="pi pi-upload" @click="openUploadModal" />
        </div>
      </template>
      <template #content>
        <DataTable
          :value="invoices"
          :loading="loading"
          :paginator="true"
          :rows="pageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          responsiveLayout="scroll"
          sortField="created_at"
          :sortOrder="-1"
        >
          <Column field="original_name" :header="'\u6587\u4EF6\u540D'">
            <template #body="{ data: row }">
              <div class="filecell">
                <i class="pi pi-file" />
                <span>{{ row.original_name }}</span>
              </div>
            </template>
          </Column>
          <Column field="invoice_number" :header="'\u53D1\u7968\u53F7'" :style="{ width: '170px' }">
            <template #body="{ data: row }">{{ row.invoice_number || '-' }}</template>
          </Column>
          <Column field="invoice_date" :header="'\u5F00\u7968\u65F6\u95F4'" sortable :style="{ width: '140px' }">
            <template #body="{ data: row }">{{ formatInvoiceDate(row.invoice_date) }}</template>
          </Column>
          <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
            <template #body="{ data: row }">{{ row.amount ? `\u00A5${row.amount.toFixed(2)}` : '-' }}</template>
          </Column>
          <Column field="seller_name" :header="'\u9500\u552E\u65B9'">
            <template #body="{ data: row }">{{ row.seller_name || '-' }}</template>
          </Column>
          <Column :header="'\u6765\u6E90'" :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <Tag :severity="getSourceSeverity(row.source)" :value="getSourceLabel(row.source)" />
            </template>
          </Column>
          <Column field="created_at" :header="'\u4E0A\u4F20\u65F6\u95F4'" sortable :style="{ width: '170px' }">
            <template #body="{ data: row }">{{ formatDateTime(row.created_at) }}</template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '160px' }">
            <template #body="{ data: row }">
              <div class="row-actions">
                <Button class="p-button-text" icon="pi pi-eye" @click="openPreview(row)" />
                <Button class="p-button-text" icon="pi pi-download" @click="downloadFile(row)" />
                <Button class="p-button-text p-button-danger" icon="pi pi-trash" @click="confirmDelete(row.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="uploadModalVisible" modal :header="'\u4E0A\u4F20\u53D1\u7968'" :style="{ width: '620px', maxWidth: '92vw' }" :closable="!uploading">
      <div class="upload-box sbm-dropzone" @click="triggerInvoiceChoose" @dragenter.prevent @dragover.prevent @drop.prevent="onInvoiceDrop">
        <div class="sbm-dropzone-hero">
          <i class="pi pi-cloud-upload" />
          <div class="sbm-dropzone-title">&#25299;&#25321; PDF/&#22270;&#29255; &#21040;&#27492;&#22788;&#65292;&#25110;&#28857;&#20987;&#36873;&#25321;</div>
          <div class="sbm-dropzone-sub">&#25903;&#25345; PDF&#12289;PNG&#12289;JPG&#65288;&#21487;&#25209;&#37327;&#65289;&#65292;&#21333;&#20010;&#25991;&#20214;&#26368;&#22823; 20MB</div>
          <Button type="button" icon="pi pi-plus" :label="'\u9009\u62E9\u6587\u4EF6'" @click.stop="chooseInvoiceFiles" />
        </div>

        <input
          ref="invoiceInput"
          class="sbm-file-input-hidden"
          type="file"
          accept="application/pdf,image/png,image/jpeg"
          multiple
          @change="onInvoiceInputChange"
        />
        <div v-if="selectedFiles.length > 0" class="file-list" @click.stop>
          <div v-for="(f, idx) in selectedFiles" :key="`${f.name}-${f.size}-${idx}`" class="file-row">
            <span class="file-row-name" :title="f.name">{{ f.name }}</span>
            <Button class="file-row-remove p-button-text" severity="secondary" icon="pi pi-times" aria-label="Remove" @click="removeSelectedFile(idx)" />
          </div>
          <div class="file-hint">&#24050;&#36873;&#25321; {{ selectedFiles.length }} &#20010;&#25991;&#20214;</div>
        </div>
      </div>
      <template #footer>
        <div class="dialog-footer-center">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" :disabled="uploading" @click="uploadModalVisible = false" />
          <Button type="button" :label="'\u4E0A\u4F20'" icon="pi pi-check" :loading="uploading" @click="handleUpload" />
        </div>
      </template>
    </Dialog>

    <Dialog v-model:visible="previewVisible" modal :header="'\u53D1\u7968\u8BE6\u60C5'" :style="{ width: '980px', maxWidth: '96vw' }">
      <div v-if="previewInvoice" class="preview">
        <div class="header-row">
          <div class="title">
            <i class="pi pi-file" />
            <span>{{ previewInvoice.original_name }}</span>
          </div>
          <div class="actions">
            <Button class="p-button-outlined" severity="secondary" icon="pi pi-external-link" :label="'\u67E5\u770B\u539F\u6587\u4EF6'" @click="downloadFile(previewInvoice)" />
            <Button class="p-button-outlined" severity="secondary" icon="pi pi-refresh" :label="'\u91CD\u65B0\u89E3\u6790'" :loading="parseStatusPending" @click="handleReparse(previewInvoice.id)" />
          </div>
        </div>

        <div class="grid">
          <div class="col-12 md:col-6">
            <div class="kv"><div class="k">&#21457;&#31080;&#21495;</div><div class="v">{{ previewInvoice.invoice_number || '-' }}</div></div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv"><div class="k">&#24320;&#31080;&#26102;&#38388;</div><div class="v">{{ formatInvoiceDate(previewInvoice.invoice_date) }}</div></div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv"><div class="k">&#37329;&#39069;</div><div class="v money">{{ previewInvoice.amount ? `\u00A5${previewInvoice.amount.toFixed(2)}` : '-' }}</div></div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">&#35299;&#26512;&#29366;&#24577;</div>
              <div class="v">
                <Tag :severity="getParseStatusSeverity(previewInvoice.parse_status)" :value="getParseStatusLabel(previewInvoice.parse_status)" />
              </div>
            </div>
          </div>
          <div class="col-12">
            <div class="kv"><div class="k">&#38144;&#21806;&#26041;</div><div class="v">{{ previewInvoice.seller_name || '-' }}</div></div>
          </div>
          <div class="col-12">
            <div class="kv"><div class="k">&#36141;&#20080;&#26041;</div><div class="v">{{ previewInvoice.buyer_name || '-' }}</div></div>
          </div>
        </div>

        <div v-if="getInvoiceItems(previewInvoice).length" class="items-section">
          <div class="items-title">&#21830;&#21697;&#26126;&#32454;</div>
          <DataTable class="items-table" :value="getInvoiceItems(previewInvoice)" scrollHeight="220px" :scrollable="true" responsiveLayout="scroll">
            <Column field="name" :header="'\u5546\u54C1\u540D\u79F0'">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.name">{{ row.name }}</span>
              </template>
            </Column>
            <Column field="spec" :header="'\u89C4\u683C\u578B\u53F7'" :style="{ width: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.spec || '-'">{{ row.spec || '-' }}</span>
              </template>
            </Column>
            <Column field="unit" :header="'\u5355\u4F4D'" :style="{ width: '120px' }">
              <template #body="{ data: row }">{{ row.unit || '-' }}</template>
            </Column>
            <Column field="quantity" :header="'\u6570\u91CF'" :style="{ width: '120px' }">
              <template #body="{ data: row }">{{ formatItemQuantity(row.quantity) }}</template>
            </Column>
          </DataTable>
        </div>

        <Divider />

        <div class="match-header">
          <div class="match-title">&#26234;&#33021;&#21305;&#37197;&#24314;&#35758;</div>
          <Button
            class="p-button-text"
            :label="'\u63A8\u8350\u5339\u914D'"
            icon="pi pi-star"
            :loading="loadingSuggestedPayments"
            @click="handleRecommendMatch"
          />
        </div>

        <Tabs v-model:value="paymentMatchTab">
          <TabList>
            <Tab value="linked">&#24050;&#20851;&#32852; ({{ linkedPayments.length }})</Tab>
            <Tab value="suggested">&#26234;&#33021;&#25512;&#33616; ({{ suggestedPayments.length }})</Tab>
          </TabList>
          <TabPanels>
          <TabPanel value="linked">
            <DataTable class="match-table" :value="linkedPayments" :loading="loadingLinkedPayments" scrollHeight="320px" :scrollable="true" responsiveLayout="scroll">
              <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
                <template #body="{ data: row }">
                  <span class="money">{{ `\u00A5${row.amount.toFixed(2)}` }}</span>
                </template>
              </Column>
              <Column :header="'\u5546\u5BB6'" :style="{ width: '260px' }">
                <template #body="{ data: row }">
                  <span class="sbm-ellipsis" :title="row.merchant || '-'">{{ row.merchant || '-' }}</span>
                </template>
              </Column>
              <Column :header="'\u4EA4\u6613\u65F6\u95F4'" :style="{ width: '170px' }">
                <template #body="{ data: row }">{{ formatDateTime(row.transaction_time) }}</template>
              </Column>
              <Column :header="'\u64CD\u4F5C'" :style="{ width: '110px' }">
                <template #body="{ data: row }">
                  <Button size="small" class="p-button-text p-button-danger" :label="'\u53D6\u6D88\u5173\u8054'" icon="pi pi-times" @click="handleUnlinkPayment(row.id)" />
                </template>
              </Column>
            </DataTable>
          </TabPanel>

          <TabPanel value="suggested">
            <DataTable class="match-table" :value="suggestedPayments" :loading="loadingSuggestedPayments" scrollHeight="320px" :scrollable="true" responsiveLayout="scroll">
              <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
                <template #body="{ data: row }">
                  <span class="money">{{ `\u00A5${row.amount.toFixed(2)}` }}</span>
                </template>
              </Column>
              <Column :header="'\u5546\u5BB6'" :style="{ width: '260px' }">
                <template #body="{ data: row }">
                  <span class="sbm-ellipsis" :title="row.merchant || '-'">{{ row.merchant || '-' }}</span>
                </template>
              </Column>
              <Column :header="'\u4EA4\u6613\u65F6\u95F4'" :style="{ width: '170px' }">
                <template #body="{ data: row }">{{ formatDateTime(row.transaction_time) }}</template>
              </Column>
              <Column :header="'\u64CD\u4F5C'" :style="{ width: '90px' }">
                <template #body="{ data: row }">
                  <Button size="small" class="p-button-text" :label="'\u5173\u8054'" :loading="linkingPayment" @click="handleLinkPayment(row.id)" />
                </template>
              </Column>
            </DataTable>

            <div v-if="!loadingSuggestedPayments && suggestedPayments.length === 0" class="no-data">
              <i class="pi pi-info-circle" />
              <span>&#26242;&#26080;&#25512;&#33616;</span>
            </div>
          </TabPanel>
          </TabPanels>
        </Tabs>

        <Divider />

        <div v-if="getInvoiceRawText(previewInvoice) || getInvoicePrettyText(previewInvoice)" class="raw-section">
          <div class="raw-title">OCR &#25991;&#26412;</div>
          <Accordion>
            <AccordionTab v-if="getInvoicePrettyText(previewInvoice)" :header="'\u70B9\u51FB\u67E5\u770B OCR \u6574\u7406\u7248\u6587\u672C'">
              <pre class="raw-text">{{ getInvoicePrettyText(previewInvoice) }}</pre>
            </AccordionTab>
            <AccordionTab v-if="getInvoiceRawText(previewInvoice)" :header="'\u70B9\u51FB\u67E5\u770B OCR \u539F\u59CB\u6587\u672C'">
              <pre class="raw-text">{{ getInvoiceRawText(previewInvoice) }}</pre>
            </AccordionTab>
          </Accordion>
        </div>
      </div>

      <template #footer>
        <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="previewVisible = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import dayjs from 'dayjs'
import Accordion from 'primevue/accordion'
import AccordionTab from 'primevue/accordiontab'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import Divider from 'primevue/divider'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { invoiceApi, FILE_BASE_URL } from '@/api'
import { useNotificationStore } from '@/stores/notifications'
import type { Invoice, Payment } from '@/types'

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()

const loading = ref(false)
const invoices = ref<Invoice[]>([])
const pageSize = ref(10)

const stats = ref<{ totalCount: number; totalAmount: number; bySource: Record<string, number> } | null>(null)

const uploadModalVisible = ref(false)
const uploading = ref(false)
const selectedFiles = ref<File[]>([])
const invoiceInput = ref<HTMLInputElement | null>(null)

const triggerInvoiceChoose = (event: MouseEvent) => {
  const target = event.target as HTMLElement | null
  if (!target) return
  if (target.closest('button') || target.closest('input') || target.closest('a')) return
  invoiceInput.value?.click()
}

const chooseInvoiceFiles = () => {
  invoiceInput.value?.click()
}

const removeSelectedFile = (idx: number) => {
  selectedFiles.value = selectedFiles.value.filter((_, i) => i !== idx)
  if (selectedFiles.value.length === 0) {
    if (invoiceInput.value) invoiceInput.value.value = ''
  }
}

const previewVisible = ref(false)
const previewInvoice = ref<Invoice | null>(null)
const parseStatusPending = ref(false)

// Linked payments state
const loadingLinkedPayments = ref(false)
const linkedPayments = ref<Payment[]>([])
const suggestedPayments = ref<Payment[]>([])
const loadingSuggestedPayments = ref(false)
const linkingPayment = ref(false)
const paymentMatchTab = ref<'linked' | 'suggested'>('linked')

const loadInvoices = async () => {
  loading.value = true
  try {
    const res = await invoiceApi.getAll()
    if (res.data.success && res.data.data) invoices.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u53D1\u7968\u5217\u8868\u5931\u8D25', life: 3000 })
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await invoiceApi.getStats()
    if (res.data.success && res.data.data) stats.value = res.data.data
  } catch (error) {
    console.error('Load stats failed:', error)
  }
}

const openUploadModal = () => {
  selectedFiles.value = []
  if (invoiceInput.value) invoiceInput.value.value = ''
  uploadModalVisible.value = true
}

const onInvoiceInputChange = (event: Event) => {
  const input = event.target as HTMLInputElement | null
  const files = input?.files ? Array.from(input.files) : []
  addInvoiceFiles(files, { replace: true })
  if (input) input.value = ''
}

const addInvoiceFiles = (files: File[], opts?: { replace?: boolean }) => {
  const allowedTypes = new Set(['application/pdf', 'image/png', 'image/jpeg'])
  const maxSize = 20 * 1024 * 1024
  const incoming = files
    .filter(f => allowedTypes.has(f.type))
    .filter(f => f.size <= maxSize)

  if (incoming.length === 0) return

  const base = opts?.replace ? [] : selectedFiles.value
  const merged = [...base]
  const seen = new Set(base.map(f => `${f.name}-${f.size}-${f.type}`))
  for (const f of incoming) {
    const key = `${f.name}-${f.size}-${f.type}`
    if (seen.has(key)) continue
    merged.push(f)
    seen.add(key)
    if (merged.length >= 10) break
  }

  if (merged.length >= 10 && (incoming.length + base.length) > 10) {
    toast.add({ severity: 'warn', summary: '最多同时选择 10 个文件', life: 2500 })
  }
  selectedFiles.value = merged
}

const onInvoiceDrop = (event: DragEvent) => {
  const list = event.dataTransfer?.files
  if (!list || list.length === 0) return
  addInvoiceFiles(Array.from(list))
}

const handleUpload = async () => {
  if (selectedFiles.value.length === 0) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u9009\u62E9\u6587\u4EF6', life: 2200 })
    return
  }

  uploading.value = true
  try {
    let createdInvoice: Invoice | null = null
    if (selectedFiles.value.length === 1) {
      const res = await invoiceApi.upload(selectedFiles.value[0])
      createdInvoice = res.data?.data || null
    } else {
      const res = await invoiceApi.uploadMultiple(selectedFiles.value)
      const createdList = res.data?.data || []
      createdInvoice = createdList.length > 0 ? createdList[0] : null
    }
    toast.add({ severity: 'success', summary: '\u4E0A\u4F20\u6210\u529F', life: 2000 })
    notifications.add({
      severity: 'success',
      title: '\u53D1\u7968\u4E0A\u4F20\u6210\u529F',
      detail: selectedFiles.value.length === 1 ? selectedFiles.value[0]?.name : `\u5171 ${selectedFiles.value.length} \u4E2A\u6587\u4EF6`,
    })
    uploadModalVisible.value = false
    selectedFiles.value = []
    await loadInvoices()
    await loadStats()
    if (createdInvoice) openPreview(createdInvoice)
  } catch {
    toast.add({ severity: 'error', summary: '\u4E0A\u4F20\u5931\u8D25', life: 3000 })
  } finally {
    uploading.value = false
  }
}

const confirmDelete = (id: string) => {
  confirm.require({
    message: '\u786E\u5B9A\u5220\u9664\u8FD9\u5F20\u53D1\u7968\u5417\uFF1F',
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
    await invoiceApi.delete(id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    notifications.add({ severity: 'info', title: '\u53D1\u7968\u5DF2\u5220\u9664', detail: id })
    await loadInvoices()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
  }
}

const openPreview = (invoice: Invoice) => {
  previewInvoice.value = invoice
  previewVisible.value = true
  loadLinkedPayments(invoice.id)
}

const downloadFile = (invoice: Invoice) => {
  window.open(`${FILE_BASE_URL}/${invoice.file_path}`, '_blank')
}

const loadLinkedPayments = async (invoiceId: string) => {
  loadingLinkedPayments.value = true
  linkedPayments.value = []
  suggestedPayments.value = []
  try {
    const linkedRes = await invoiceApi.getLinkedPayments(invoiceId)
    if (linkedRes.data.success && linkedRes.data.data) linkedPayments.value = linkedRes.data.data
  } catch (error) {
    console.error('Load linked payments failed:', error)
  } finally {
    loadingLinkedPayments.value = false
  }
}

const refreshSuggestedPayments = async (invoiceId: string, opts?: { showToast?: boolean }) => {
  loadingSuggestedPayments.value = true
  try {
    const suggestedRes = await invoiceApi.getSuggestedPayments(invoiceId, { debug: true })
    suggestedPayments.value = suggestedRes.data.success && suggestedRes.data.data ? suggestedRes.data.data : []
    if (opts?.showToast) {
      if (suggestedPayments.value.length > 0) {
        toast.add({ severity: 'success', summary: `\u63A8\u8350\u5230 ${suggestedPayments.value.length} \u6761\u53EF\u5173\u8054\u7684\u652F\u4ED8\u8BB0\u5F55`, life: 2500 })
      } else if (!linkingPayment.value && linkedPayments.value.length === 0) {
        toast.add({ severity: 'warn', summary: '\u6CA1\u6709\u627E\u5230\u53EF\u63A8\u8350\u7684\u652F\u4ED8\u8BB0\u5F55', life: 2500 })
      }
    }
  } catch (error) {
    console.error('Load suggested payments failed:', error)
    suggestedPayments.value = []
    if (opts?.showToast) toast.add({ severity: 'error', summary: '\u63A8\u8350\u5339\u914D\u5931\u8D25', life: 3000 })
  } finally {
    loadingSuggestedPayments.value = false
  }
}

const handleRecommendMatch = async () => {
  if (!previewInvoice.value) return
  await refreshSuggestedPayments(previewInvoice.value.id, { showToast: true })
}

const handleLinkPayment = async (paymentId: string) => {
  if (!previewInvoice.value) return
  try {
    linkingPayment.value = true
    await invoiceApi.linkPayment(previewInvoice.value.id, paymentId)
    toast.add({ severity: 'success', summary: '\u5173\u8054\u6210\u529F', life: 2000 })
    notifications.add({
      severity: 'success',
      title: '\u53D1\u7968\u5DF2\u5173\u8054\u652F\u4ED8\u8BB0\u5F55',
      detail: `invoice=${previewInvoice.value.invoice_number || previewInvoice.value.original_name || previewInvoice.value.id} payment=${paymentId}`,
    })
    await loadLinkedPayments(previewInvoice.value.id)
    await refreshSuggestedPayments(previewInvoice.value.id, { showToast: false })
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '\u5173\u8054\u5931\u8D25', life: 3500 })
  } finally {
    linkingPayment.value = false
  }
}

const handleUnlinkPayment = async (paymentId: string) => {
  if (!previewInvoice.value) return
  try {
    await invoiceApi.unlinkPayment(previewInvoice.value.id, paymentId)
    toast.add({ severity: 'success', summary: '\u53D6\u6D88\u5173\u8054\u6210\u529F', life: 2000 })
    notifications.add({
      severity: 'info',
      title: '\u53D1\u7968\u5DF2\u53D6\u6D88\u5173\u8054\u652F\u4ED8\u8BB0\u5F55',
      detail: `invoice=${previewInvoice.value.invoice_number || previewInvoice.value.original_name || previewInvoice.value.id} payment=${paymentId}`,
    })
    await loadLinkedPayments(previewInvoice.value.id)
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '\u53D6\u6D88\u5173\u8054\u5931\u8D25', life: 3500 })
  }
}

const getSourceLabel = (source?: string) => {
  const labels: Record<string, string> = {
    email: '\u90AE\u4EF6\u4E0B\u8F7D',
    dingtalk: '\u9489\u9489\u673A\u5668\u4EBA',
    upload: '\u624B\u52A8\u4E0A\u4F20',
  }
  return labels[source || ''] || source || '\u672A\u77E5'
}

const getSourceSeverity = (source?: string): 'info' | 'success' | 'warning' | 'secondary' => {
  const types: Record<string, 'info' | 'success' | 'warning' | 'secondary'> = {
    email: 'info',
    dingtalk: 'warning',
    upload: 'success',
  }
  return types[source || ''] || 'secondary'
}

const formatDateTime = (date?: string) => {
  if (!date) return '-'
  return dayjs(date).format('YYYY-MM-DD HH:mm')
}

type InvoiceLineItem = { name: string; spec?: string; unit?: string; quantity?: number }

const getInvoiceItems = (invoice: Invoice | null): InvoiceLineItem[] => {
  if (!invoice?.extracted_data) return []
  try {
    const data = JSON.parse(invoice.extracted_data) as { items?: unknown }
    if (!Array.isArray(data.items)) return []
    return data.items
      .map((it: unknown) => {
        const obj = (it ?? {}) as Record<string, unknown>
        return {
          name: typeof obj.name === 'string' ? obj.name : '',
          spec: typeof obj.spec === 'string' ? obj.spec : '',
          unit: typeof obj.unit === 'string' ? obj.unit : '',
          quantity: typeof obj.quantity === 'number' ? obj.quantity : undefined,
        }
      })
      .filter((it: InvoiceLineItem) => it.name.trim().length > 0)
  } catch {
    return []
  }
}

const formatItemQuantity = (qty?: number) => {
  if (qty == null) return '-'
  if (Number.isFinite(qty) && Number.isInteger(qty)) return String(qty)
  return String(qty)
}

const formatInvoiceDate = (date?: string) => {
  if (!date) return '-'
  const parsed = dayjs(date)
  if (parsed.isValid()) return parsed.format('YYYY-MM-DD')
  const m = String(date).match(/(\\d{4})\\D+(\\d{1,2})\\D+(\\d{1,2})/)
  if (m) {
    const y = m[1]
    const mm = m[2].padStart(2, '0')
    const dd = m[3].padStart(2, '0')
    return `${y}-${mm}-${dd}`
  }
  return date
}

const getParseStatusLabel = (status?: string) => {
  const labels: Record<string, string> = {
    pending: '\u5F85\u89E3\u6790',
    parsing: '\u89E3\u6790\u4E2D',
    success: '\u89E3\u6790\u6210\u529F',
    failed: '\u89E3\u6790\u5931\u8D25',
  }
  return labels[status || 'pending'] || '\u672A\u77E5'
}

const getParseStatusSeverity = (status?: string): 'info' | 'warning' | 'success' | 'danger' => {
  const types: Record<string, 'info' | 'warning' | 'success' | 'danger'> = {
    pending: 'info',
    parsing: 'warning',
    success: 'success',
    failed: 'danger',
  }
  return types[status || 'pending'] || 'info'
}

const getInvoiceRawText = (invoice: Invoice | null) => {
  if (!invoice) return ''
  if (invoice.raw_text) return invoice.raw_text
  if (!invoice.extracted_data) return ''
  try {
    const data = JSON.parse(invoice.extracted_data)
    return data.raw_text || ''
  } catch {
    return invoice.extracted_data || ''
  }
}

const getInvoicePrettyText = (invoice: Invoice | null) => {
  if (!invoice) return ''
  if (!invoice.extracted_data) return ''
  try {
    const data = JSON.parse(invoice.extracted_data)
    return data.pretty_text || ''
  } catch {
    return ''
  }
}

const handleReparse = async (id: string) => {
  parseStatusPending.value = true
  try {
    const res = await invoiceApi.parse(id)
    if (res.data.success && res.data.data) {
      toast.add({ severity: 'success', summary: '\u53D1\u7968\u89E3\u6790\u5B8C\u6210', life: 2200 })
      notifications.add({
        severity: 'success',
        title: '\u53D1\u7968\u5DF2\u91CD\u65B0\u89E3\u6790',
        detail: res.data.data.invoice_number || res.data.data.original_name || id,
      })
      previewInvoice.value = res.data.data
      await loadInvoices()
    }
  } catch {
    toast.add({ severity: 'error', summary: '\u53D1\u7968\u89E3\u6790\u5931\u8D25', life: 3000 })
    notifications.add({ severity: 'error', title: '\u53D1\u7968\u91CD\u65B0\u89E3\u6790\u5931\u8D25', detail: id })
  } finally {
    parseStatusPending.value = false
  }
}

const sourceStats = computed(() => stats.value?.bySource || {})

onMounted(() => {
  loadInvoices()
  loadStats()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.row-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.filecell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--color-text-primary);
}

.filecell i {
  color: var(--color-primary);
}

.stat {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.stat-title {
  font-weight: 700;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.stat-value {
  margin-top: 6px;
  font-size: 22px;
  font-weight: 900;
}

.stat-icon {
  font-size: 20px;
  padding: 12px;
  border-radius: 14px;
}

.stat-icon.info {
  background: rgba(59, 130, 246, 0.12);
  color: var(--p-primary-600, #2563eb);
}

.stat-icon.success {
  background: rgba(22, 163, 74, 0.12);
  color: var(--p-green-600, #16a34a);
}

.stat-icon.secondary {
  background: rgba(0, 0, 0, 0.06);
  color: rgba(0, 0, 0, 0.55);
}

.source-row {
  margin-top: 8px;
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.upload-box {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border-radius: var(--radius-md);
  border: 1px dashed rgba(59, 130, 246, 0.35);
  border: 1px dashed color-mix(in srgb, var(--p-primary-400, #60a5fa), transparent 25%);
  background: rgba(59, 130, 246, 0.03);
  background: color-mix(in srgb, var(--p-primary-50, #eff6ff), transparent 55%);
}

.sbm-dropzone {
  cursor: pointer;
  position: relative;
}

.sbm-file-input-hidden {
  position: absolute;
  inset: 0;
  width: 1px;
  height: 1px;
  overflow: hidden;
  opacity: 0;
  pointer-events: none;
}

.sbm-dropzone-hero {
  width: 100%;
  min-height: clamp(104px, 16vh, 150px);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--color-text-secondary);
  user-select: none;
}

.sbm-dropzone-hero i {
  font-size: 22px;
  color: var(--p-primary-600, #2563eb);
}

.sbm-dropzone-title {
  font-weight: 900;
  color: var(--color-text-primary);
}

.sbm-dropzone-sub {
  font-size: 12px;
  color: var(--color-text-tertiary);
}

.dialog-footer-center {
  width: 100%;
  display: flex;
  justify-content: center;
  gap: 10px;
}

.file-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.file-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(0, 0, 0, 0.02);
}

.file-row-name {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.file-row-remove {
  flex: 0 0 auto;
}

.file-hint {
  color: var(--color-text-secondary);
  font-weight: 700;
}

.preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 900;
  color: var(--color-text-primary);
}

.actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.kv {
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(0, 0, 0, 0.02);
  border-radius: var(--radius-md);
  padding: 10px 12px;
}

.k {
  font-size: 12px;
  font-weight: 800;
  color: var(--color-text-tertiary);
}

.v {
  margin-top: 6px;
  font-weight: 700;
  color: var(--color-text-primary);
}

.money {
  color: var(--p-red-600, #dc2626);
  font-weight: 900;
}

.match-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.match-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: 2px;
  margin-bottom: 10px;
}

.match-title {
  font-weight: 800;
  color: var(--p-text-color);
}

.match-table :deep(.p-datatable-thead > tr > th),
.match-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.items-section {
  margin-top: 10px;
}

.items-title {
  font-weight: 900;
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.items-table :deep(.p-datatable-thead > tr > th),
.items-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
}

.no-data {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--color-text-tertiary);
  font-weight: 700;
}

.raw-section {
  margin-top: 4px;
}

.raw-title {
  font-weight: 900;
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.raw-text {
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 320px;
  overflow: auto;
  background: rgba(0, 0, 0, 0.03);
  padding: 10px;
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
}
</style>
