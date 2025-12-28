<template>
  <div class="page">
    <div class="grid">
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#24635;&#25903;&#20986;</div>
                <div class="stat-value">{{ formatMoney(stats?.totalAmount || 0) }}</div>
              </div>
              <i class="pi pi-wallet stat-icon danger" />
            </div>
          </template>
        </Card>
      </div>
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#20132;&#26131;&#31508;&#25968;</div>
                <div class="stat-value">{{ stats?.totalCount || 0 }}</div>
              </div>
              <i class="pi pi-shopping-cart stat-icon success" />
            </div>
          </template>
        </Card>
      </div>
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#24179;&#22343;&#27599;&#31508;</div>
                <div class="stat-value">{{ formatMoney(avgAmount) }}</div>
              </div>
              <i class="pi pi-chart-line stat-icon info" />
            </div>
          </template>
        </Card>
      </div>
    </div>

    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>&#25903;&#20184;&#35760;&#24405;</span>
          <div class="toolbar">
            <DatePicker
              v-model="dateRange"
              selectionMode="range"
              :manualInput="false"
              :placeholder="'\u65E5\u671F\u8303\u56F4'"
              @update:modelValue="handleDateChange"
            />
            <Button :label="'\u4E0A\u4F20\u622A\u56FE'" icon="pi pi-image" severity="success" @click="openScreenshotModal" />
            <Button :label="'\u6DFB\u52A0\u8BB0\u5F55'" icon="pi pi-plus" @click="openModal()" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable
          :value="payments"
          :loading="loading"
          :paginator="true"
          :rows="pageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          responsiveLayout="scroll"
          sortField="transaction_time"
          :sortOrder="-1"
        >
          <Column field="amount" :header="'\u91D1\u989D'" sortable :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <span class="amount">{{ formatMoney(row.amount) }}</span>
            </template>
          </Column>
          <Column field="merchant" :header="'\u5546\u5BB6'" />
          <Column :header="'\u652F\u4ED8\u65B9\u5F0F'" :style="{ width: '160px' }">
            <template #body="{ data: row }">
              <Tag
                v-if="row.payment_method"
                class="sbm-tag-ellipsis"
                severity="success"
                :value="normalizeInlineText(row.payment_method)"
                :title="normalizeInlineText(row.payment_method)"
              />
              <span v-else>-</span>
            </template>
          </Column>
          <Column field="description" :header="'\u5907\u6CE8'" />
          <Column field="transaction_time" :header="'\u4EA4\u6613\u65F6\u95F4'" sortable :style="{ width: '170px' }">
            <template #body="{ data: row }">
              {{ formatDateTime(row.transaction_time) }}
            </template>
          </Column>
          <Column :header="'\u5173\u8054\u53D1\u7968'" :style="{ width: '130px' }">
            <template #body="{ data: row }">
              <Button class="p-button-text" :label="`\u67E5\u770B (${linkedInvoicesCount[row.id] || 0})`" @click="viewLinkedInvoices(row)" />
            </template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '170px' }">
            <template #body="{ data: row }">
              <div class="row-actions">
                <Button class="p-button-text" icon="pi pi-eye" @click="openPaymentDetail(row)" />
                <Button class="p-button-text" icon="pi pi-pencil" @click="openModal(row)" />
                <Button class="p-button-text p-button-danger" icon="pi pi-trash" @click="confirmDelete(row.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog
      v-model:visible="modalVisible"
      modal
      :header="editingPayment ? '\u7F16\u8F91\u652F\u4ED8\u8BB0\u5F55' : '\u6DFB\u52A0\u652F\u4ED8\u8BB0\u5F55'"
      :style="{ width: '620px', maxWidth: '92vw' }"
    >
      <form class="p-fluid" @submit.prevent="handleSubmit">
        <div class="grid">
          <div class="col-12 md:col-6 field">
            <label for="amount">&#37329;&#39069;</label>
            <InputNumber id="amount" v-model="form.amount" :minFractionDigits="2" :maxFractionDigits="2" :min="0" />
            <small v-if="errors.amount" class="p-error">{{ errors.amount }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="merchant">&#5546;&#23478;</label>
            <InputText id="merchant" v-model.trim="form.merchant" />
          </div>
          <div class="col-12 md:col-6 field">
            <label for="method">&#25903;&#20184;&#26041;&#24335;</label>
            <InputText id="method" v-model.trim="form.payment_method" />
          </div>
          <div class="col-12 field">
            <label for="time">&#20132;&#26131;&#26102;&#38388;</label>
            <DatePicker id="time" v-model="form.transaction_time" showTime :manualInput="false" />
            <small v-if="errors.transaction_time" class="p-error">{{ errors.transaction_time }}</small>
          </div>
          <div class="col-12 field">
            <label for="desc">&#22791;&#27880;</label>
            <Textarea id="desc" v-model="form.description" autoResize rows="3" />
          </div>
        </div>
        <div class="footer">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" @click="modalVisible = false" />
          <Button type="submit" :label="'\u4FDD\u5B58'" icon="pi pi-check" />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="uploadScreenshotModalVisible"
      modal
      :header="'\u4E0A\u4F20\u652F\u4ED8\u622A\u56FE'"
      :style="{ width: '880px', maxWidth: '96vw' }"
      :closable="!uploadingScreenshot && !savingOcrResult"
    >
      <div class="upload-screenshot-layout">
        <div
          class="upload-box sbm-dropzone"
          @click="triggerScreenshotChoose"
          @dragenter.prevent
          @dragover.prevent
          @drop.prevent="onScreenshotDrop"
        >
          <div class="sbm-dropzone-hero">
            <i class="pi pi-cloud-upload" />
            <div class="sbm-dropzone-title">&#25299;&#25321;&#22270;&#29255;&#21040;&#27492;&#22788;&#65292;&#25110;&#28857;&#20987;&#36873;&#25321;</div>
            <div class="sbm-dropzone-sub">&#25903;&#25345; PNG/JPG&#65292;&#26368;&#22823; 10MB</div>
            <Button type="button" icon="pi pi-plus" :label="'\u9009\u62E9\u622A\u56FE'" @click.stop="chooseScreenshotFile" />
          </div>

          <input
            ref="screenshotInput"
            class="sbm-file-input-hidden"
            type="file"
            accept="image/png,image/jpeg"
            @change="onScreenshotInputChange"
          />

          <div v-if="selectedScreenshotName" class="file-row" @click.stop>
            <span class="file-row-name" :title="selectedScreenshotName">{{ selectedScreenshotName }}</span>
            <Button
              class="file-row-remove p-button-text"
              severity="secondary"
              icon="pi pi-times"
              aria-label="Remove"
              @click="clearSelectedScreenshot"
            />
          </div>
          <small v-if="screenshotError" class="p-error">{{ screenshotError }}</small>
        </div>

        <Message v-if="!ocrResult" severity="info" :closable="false">
          &#35831;&#36873;&#25321;&#25130;&#22270;&#65292;&#28857;&#20987;&#8220;&#35782;&#21035;&#8221;&#29983;&#25104;&#24405;&#20837;&#24314;&#35758;&#12290;
        </Message>

        <form v-else class="p-fluid" @submit.prevent="handleSaveOcrResult">
          <div class="grid">
            <div class="col-12 md:col-6 field">
              <label for="ocr_amount">&#37329;&#39069;</label>
              <InputNumber id="ocr_amount" v-model="ocrForm.amount" :minFractionDigits="2" :maxFractionDigits="2" :min="0" />
              <small v-if="ocrErrors.amount" class="p-error">{{ ocrErrors.amount }}</small>
            </div>
            <div class="col-12 md:col-6 field">
              <label for="ocr_merchant">&#5546;&#23478;</label>
              <InputText id="ocr_merchant" v-model.trim="ocrForm.merchant" />
            </div>
            <div class="col-12 md:col-6 field">
              <label for="ocr_method">&#25903;&#20184;&#26041;&#24335;</label>
              <InputText id="ocr_method" v-model.trim="ocrForm.payment_method" />
            </div>
            <div class="col-12 field">
              <label for="ocr_time">&#20132;&#26131;&#26102;&#38388;</label>
              <DatePicker id="ocr_time" v-model="ocrForm.transaction_time" showTime :manualInput="false" />
              <small v-if="ocrErrors.transaction_time" class="p-error">{{ ocrErrors.transaction_time }}</small>
            </div>
            <div class="col-12 field">
              <label for="ocr_order">&#20132;&#26131;&#21333;&#21495; (&#21487;&#36873;)</label>
              <InputText id="ocr_order" v-model.trim="ocrForm.order_number" />
            </div>
            <div class="col-12 field">
              <label for="ocr_desc">&#22791;&#27880;</label>
              <Textarea id="ocr_desc" v-model="ocrForm.description" autoResize rows="3" />
            </div>
          </div>
        </form>

        <div v-if="ocrResult?.raw_text" class="raw">
          <div class="raw-title">OCR &#21407;&#22987;&#25991;&#26412;</div>
          <pre class="raw-text">{{ ocrResult.raw_text }}</pre>
        </div>
      </div>

      <template #footer>
        <div class="dialog-footer-center">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" @click="cancelScreenshotUpload" />
          <Button
            v-if="!ocrResult"
            type="button"
            :label="'\u8BC6\u522B'"
            icon="pi pi-search"
            :loading="uploadingScreenshot"
            :disabled="!selectedScreenshotFile"
            @click="handleScreenshotUpload"
          />
          <Button v-else type="button" :label="'\u4FDD\u5B58'" icon="pi pi-check" :loading="savingOcrResult" @click="handleSaveOcrResult" />
        </div>
      </template>
    </Dialog>

    <Dialog v-model:visible="linkedInvoicesModalVisible" modal :header="'\u5173\u8054\u7684\u53D1\u7968'" :style="{ width: '980px', maxWidth: '96vw' }">
      <div class="match-header">
        <div class="match-title">&#26234;&#33021;&#21305;&#37197;&#24314;&#35758;</div>
        <Button
          class="p-button-text"
          :label="'\u63A8\u8350\u5339\u914D'"
          icon="pi pi-star"
          :loading="loadingSuggestedInvoices"
          @click="handleRecommendInvoices"
        />
      </div>

      <Tabs v-model:value="invoiceMatchTab">
        <TabList>
          <Tab value="linked">&#24050;&#20851;&#32852; ({{ linkedInvoices.length }})</Tab>
          <Tab value="suggested">&#26234;&#33021;&#25512;&#33616; ({{ suggestedInvoices.length }})</Tab>
        </TabList>
        <TabPanels>
        <TabPanel value="linked">
          <DataTable class="match-table" :value="linkedInvoices" :loading="loadingLinkedInvoices" scrollHeight="360px" :scrollable="true" responsiveLayout="scroll">
            <Column :header="'\u53D1\u7968\u53F7'" :style="{ width: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.invoice_number || '-'">{{ row.invoice_number || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
              <template #body="{ data: row }">{{ row.amount ? formatMoney(row.amount) : '-' }}</template>
            </Column>
            <Column :header="'\u9500\u552E\u65B9'" :style="{ width: '320px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.seller_name || '-'">{{ row.seller_name || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u5F00\u7968\u65F6\u95F4'" :style="{ width: '150px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.invoice_date || '-'">{{ row.invoice_date || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u64CD\u4F5C'" :style="{ width: '110px' }">
              <template #body="{ data: row }">
                <Button
                  size="small"
                  class="p-button-text p-button-danger"
                  :label="'\u53D6\u6D88\u5173\u8054'"
                  :loading="unlinkingInvoiceFromPayment"
                  @click="handleUnlinkInvoiceFromPayment(row.id)"
                />
              </template>
            </Column>
          </DataTable>
        </TabPanel>

        <TabPanel value="suggested">
          <DataTable class="match-table" :value="suggestedInvoices" :loading="loadingSuggestedInvoices" scrollHeight="360px" :scrollable="true" responsiveLayout="scroll">
            <Column :header="'\u53D1\u7968\u53F7'" :style="{ width: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.invoice_number || '-'">{{ row.invoice_number || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u91D1\u989D'" :style="{ width: '120px' }">
              <template #body="{ data: row }">{{ row.amount ? formatMoney(row.amount) : '-' }}</template>
            </Column>
            <Column :header="'\u9500\u552E\u65B9'" :style="{ width: '320px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.seller_name || '-'">{{ row.seller_name || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u5F00\u7968\u65F6\u95F4'" :style="{ width: '150px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.invoice_date || '-'">{{ row.invoice_date || '-' }}</span>
              </template>
            </Column>
            <Column :header="'\u64CD\u4F5C'" :style="{ width: '90px' }">
              <template #body="{ data: row }">
                <Button size="small" class="p-button-text" :label="'\u5173\u8054'" :loading="linkingInvoiceToPayment" @click="handleLinkInvoiceToPayment(row.id)" />
              </template>
            </Column>
          </DataTable>

          <div v-if="!loadingSuggestedInvoices && suggestedInvoices.length === 0" class="no-data">
            <i class="pi pi-info-circle" />
            <span>&#26242;&#26080;&#25512;&#33616;</span>
          </div>
        </TabPanel>
        </TabPanels>
      </Tabs>
      <template #footer>
        <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="linkedInvoicesModalVisible = false" />
      </template>
    </Dialog>

    <Dialog v-model:visible="paymentDetailVisible" modal :header="'\u652F\u4ED8\u8BB0\u5F55\u8BE6\u60C5'" :style="{ width: '820px', maxWidth: '96vw' }">
      <div v-if="detailPayment">
        <div class="grid">
          <div class="col-12 md:col-6">
            <div class="kv"><div class="k">&#37329;&#39069;</div><div class="v amount">{{ formatMoney(detailPayment.amount || 0) }}</div></div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv"><div class="k">&#5546;&#23478;</div><div class="v">{{ detailPayment.merchant || '-' }}</div></div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">&#25903;&#20184;&#26041;&#24335;</div>
              <div class="v">
                <Tag
                  v-if="detailPayment.payment_method"
                  class="sbm-tag-ellipsis"
                  severity="success"
                  :value="normalizeInlineText(detailPayment.payment_method)"
                  :title="normalizeInlineText(detailPayment.payment_method)"
                />
                <span v-else>-</span>
              </div>
            </div>
          </div>
          <div class="col-12">
            <div class="kv"><div class="k">&#20132;&#26131;&#26102;&#38388;</div><div class="v">{{ formatDateTime(detailPayment.transaction_time) }}</div></div>
          </div>
          <div class="col-12">
            <div class="kv"><div class="k">&#22791;&#27880;</div><div class="v">{{ detailPayment.description || '-' }}</div></div>
          </div>
        </div>

        <Divider />

        <div v-if="detailPayment.screenshot_path" class="section">
          <div class="section-title">&#25903;&#20184;&#25130;&#22270;</div>
          <div class="screenshot-wrap">
            <Image
              class="screenshot"
              :src="`${FILE_BASE_URL}/${detailPayment.screenshot_path}`"
              preview
              :imageStyle="{ maxWidth: '100%', height: 'auto' }"
            />
          </div>
        </div>

        <div v-if="detailPayment.extracted_data" class="section">
          <div class="section-title-row">
            <div class="section-title">OCR &#21407;&#22987;&#25991;&#26412;</div>
            <Button class="p-button-text" icon="pi pi-refresh" :label="'\u91CD\u65B0\u89E3\u6790'" :loading="reparsingOcr" @click="handleReparseOcr(detailPayment.id)" />
          </div>
          <Accordion>
            <AccordionTab :header="'\u70B9\u51FB\u67E5\u770B OCR \u539F\u59CB\u6587\u672C'">
              <pre class="raw-text">{{ getExtractedRawText(detailPayment.extracted_data || null) }}</pre>
            </AccordionTab>
          </Accordion>
        </div>
      </div>
      <template #footer>
        <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="paymentDetailVisible = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import dayjs from 'dayjs'
import Accordion from 'primevue/accordion'
import AccordionTab from 'primevue/accordiontab'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import DatePicker from 'primevue/datepicker'
import Dialog from 'primevue/dialog'
import Divider from 'primevue/divider'
import Image from 'primevue/image'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { invoiceApi, paymentApi, FILE_BASE_URL } from '@/api'
import { useNotificationStore } from '@/stores/notifications'
import type { Invoice, Payment } from '@/types'

interface OcrExtractedData {
  amount?: number
  merchant?: string
  transaction_time?: string
  payment_method?: string
  order_number?: string
  raw_text?: string
}

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()

const loading = ref(false)
const payments = ref<Payment[]>([])
const pageSize = ref(10)
const dateRange = ref<Date[] | null>(null)

const stats = ref<{ totalAmount: number; totalCount: number } | null>(null)
const avgAmount = computed(() => {
  const count = stats.value?.totalCount || 0
  const total = stats.value?.totalAmount || 0
  return count ? total / count : 0
})

// Payment CRUD dialog
const modalVisible = ref(false)
const editingPayment = ref<Payment | null>(null)

const form = reactive({
  amount: 0,
  merchant: '',
  payment_method: '',
  description: '',
  transaction_time: new Date(),
})
const errors = reactive({ amount: '', transaction_time: '' })

// Screenshot upload + OCR
const uploadScreenshotModalVisible = ref(false)
const uploadingScreenshot = ref(false)
const savingOcrResult = ref(false)
const selectedScreenshotFile = ref<File | null>(null)
const selectedScreenshotName = ref('')
const screenshotError = ref('')
const ocrResult = ref<OcrExtractedData | null>(null)
const uploadedPaymentId = ref<string | null>(null)
const screenshotInput = ref<HTMLInputElement | null>(null)

const triggerScreenshotChoose = (event: MouseEvent) => {
  const target = event.target as HTMLElement | null
  if (!target) return
  if (target.closest('button') || target.closest('input') || target.closest('a')) return
  screenshotInput.value?.click()
}

const chooseScreenshotFile = () => {
  screenshotInput.value?.click()
}

const setScreenshotFile = (file?: File) => {
  screenshotError.value = ''
  if (screenshotInput.value) screenshotInput.value.value = ''

  if (!file) {
    selectedScreenshotFile.value = null
    selectedScreenshotName.value = ''
    return
  }

  const validTypes = ['image/jpeg', 'image/jpg', 'image/png']
  if (!validTypes.includes(file.type)) {
    screenshotError.value = '\u53EA\u652F\u6301 JPG\u3001JPEG\u3001PNG \u683C\u5F0F\u7684\u56FE\u7247'
    selectedScreenshotFile.value = null
    selectedScreenshotName.value = ''
    return
  }

  const maxSize = 10 * 1024 * 1024
  if (file.size > maxSize) {
    screenshotError.value = '\u6587\u4EF6\u5927\u5C0F\u4E0D\u80FD\u8D85\u8FC7 10MB'
    selectedScreenshotFile.value = null
    selectedScreenshotName.value = ''
    return
  }

  selectedScreenshotFile.value = file
  selectedScreenshotName.value = file.name
  ocrResult.value = null
  uploadedPaymentId.value = null
}

const onScreenshotDrop = (event: DragEvent) => {
  const file = event.dataTransfer?.files?.[0]
  setScreenshotFile(file)
}

const onScreenshotInputChange = (event: Event) => {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  setScreenshotFile(file)
  if (input) input.value = ''
}

const clearSelectedScreenshot = () => {
  selectedScreenshotFile.value = null
  selectedScreenshotName.value = ''
  screenshotError.value = ''
  ocrResult.value = null
  uploadedPaymentId.value = null
  if (screenshotInput.value) screenshotInput.value.value = ''
}

const ocrForm = reactive({
  amount: 0,
  merchant: '',
  payment_method: '',
  description: '',
  transaction_time: new Date(),
  order_number: '',
})
const ocrErrors = reactive({ amount: '', transaction_time: '' })

// Linked invoices
const linkedInvoicesModalVisible = ref(false)
const loadingLinkedInvoices = ref(false)
const linkedInvoices = ref<Invoice[]>([])
const linkedInvoicesCount = ref<Record<string, number>>({})
const currentPaymentForInvoices = ref<Payment | null>(null)
const loadingSuggestedInvoices = ref(false)
const suggestedInvoices = ref<Invoice[]>([])
const linkingInvoiceToPayment = ref(false)
const unlinkingInvoiceFromPayment = ref(false)
const invoiceMatchTab = ref<'linked' | 'suggested'>('linked')

// Detail dialog
const paymentDetailVisible = ref(false)
const detailPayment = ref<Payment | null>(null)
const reparsingOcr = ref(false)

const validatePaymentForm = () => {
  errors.amount = ''
  errors.transaction_time = ''
  if (form.amount === null || Number.isNaN(Number(form.amount)) || Number(form.amount) <= 0) errors.amount = '\u8BF7\u8F93\u5165\u91D1\u989D'
  if (!form.transaction_time) errors.transaction_time = '\u8BF7\u9009\u62E9\u4EA4\u6613\u65F6\u95F4'
  return !errors.amount && !errors.transaction_time
}

const validateOcrForm = () => {
  ocrErrors.amount = ''
  ocrErrors.transaction_time = ''
  if (ocrForm.amount === null || Number.isNaN(Number(ocrForm.amount)) || Number(ocrForm.amount) <= 0) ocrErrors.amount = '\u8BF7\u8F93\u5165\u91D1\u989D'
  if (!ocrForm.transaction_time) ocrErrors.transaction_time = '\u8BF7\u9009\u62E9\u4EA4\u6613\u65F6\u95F4'
  return !ocrErrors.amount && !ocrErrors.transaction_time
}

const loadPayments = async () => {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (dateRange.value && dateRange.value[0] && dateRange.value[1]) {
      params.startDate = dayjs(dateRange.value[0]).startOf('day').toISOString()
      params.endDate = dayjs(dateRange.value[1]).endOf('day').toISOString()
    }
    const res = await paymentApi.getAll(params)
    if (res.data.success && res.data.data) payments.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u652F\u4ED8\u8BB0\u5F55\u5931\u8D25', life: 3000 })
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const startDate = dateRange.value?.[0] ? dayjs(dateRange.value[0]).startOf('day').toISOString() : undefined
    const endDate = dateRange.value?.[1] ? dayjs(dateRange.value[1]).endOf('day').toISOString() : undefined
    const res = await paymentApi.getStats(startDate, endDate)
    if (res.data.success && res.data.data) stats.value = res.data.data
  } catch (error) {
    console.error('Load stats failed:', error)
  }
}

const formatDateTime = (date: string) => dayjs(date).format('YYYY-MM-DD HH:mm')
const formatMoney = (value: number) => `\u00A5${(value || 0).toFixed(2)}`
const normalizeInlineText = (value?: string | null) => (value || '').replace(/\s+/g, ' ').trim()

const loadLinkedInvoicesCount = async () => {
  try {
    const counts: Record<string, number> = {}
    for (const payment of payments.value) {
      try {
        const res = await paymentApi.getPaymentInvoices(payment.id)
        if (res.data.success && res.data.data) counts[payment.id] = res.data.data.length
      } catch {
        counts[payment.id] = 0
      }
    }
    linkedInvoicesCount.value = counts
  } catch (error) {
    console.error('Load linked invoices count failed:', error)
  }
}

const loadPaymentsWithCount = async () => {
  await loadPayments()
  await loadLinkedInvoicesCount()
}

const handleDateChange = () => {
  loadPaymentsWithCount()
  loadStats()
}

const openModal = (payment?: Payment) => {
  errors.amount = ''
  errors.transaction_time = ''
  if (payment) {
    editingPayment.value = payment
    form.amount = payment.amount
    form.merchant = payment.merchant || ''
    form.payment_method = payment.payment_method || ''
    form.description = payment.description || ''
    form.transaction_time = new Date(payment.transaction_time)
  } else {
    editingPayment.value = null
    form.amount = 0
    form.merchant = ''
    form.payment_method = ''
    form.description = ''
    form.transaction_time = new Date()
  }
  modalVisible.value = true
}

const handleSubmit = async () => {
  if (!validatePaymentForm()) return
  try {
    const payload = {
      amount: Number(form.amount),
      merchant: form.merchant,
      payment_method: form.payment_method,
      description: form.description,
      transaction_time: dayjs(form.transaction_time).toISOString(),
    }
    if (editingPayment.value) {
      await paymentApi.update(editingPayment.value.id, payload)
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u66F4\u65B0\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u66F4\u65B0',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    } else {
      await paymentApi.create(payload)
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u521B\u5EFA\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u521B\u5EFA',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    }
    modalVisible.value = false
    await loadPaymentsWithCount()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u64CD\u4F5C\u5931\u8D25', life: 3000 })
  }
}

const confirmDelete = (id: string) => {
  confirm.require({
    message: '\u786E\u5B9A\u5220\u9664\u8BE5\u6761\u8BB0\u5F55\u5417\uFF1F',
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
    await paymentApi.delete(id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    notifications.add({ severity: 'info', title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u5220\u9664', detail: id })
    await loadPaymentsWithCount()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
  }
}

const openScreenshotModal = () => {
  resetScreenshotUploadState()
  uploadScreenshotModalVisible.value = true
}

const handleScreenshotUpload = async () => {
  if (!selectedScreenshotFile.value) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u9009\u62E9\u6587\u4EF6', life: 2200 })
    return
  }

  uploadingScreenshot.value = true
  try {
    const res = await paymentApi.uploadScreenshot(selectedScreenshotFile.value)
    if (res.data.success && res.data.data) {
      const { payment, extracted, ocr_error } = res.data.data as any
      uploadedPaymentId.value = payment.id
      ocrResult.value = extracted

      ocrForm.amount = extracted.amount || 0
      ocrForm.merchant = extracted.merchant || ''
      ocrForm.payment_method = extracted.payment_method || ''
      ocrForm.order_number = extracted.order_number || ''
      ocrForm.transaction_time = extracted.transaction_time ? new Date(extracted.transaction_time) : new Date()
      ocrForm.description = ''

      if (ocr_error) {
        toast.add({
          severity: 'warn',
          summary: `\u622A\u56FE\u4E0A\u4F20\u6210\u529F\uFF0C\u4F46 OCR \u8BC6\u522B\u5931\u8D25\uFF1A${ocr_error}`,
          life: 5000,
        })
        notifications.add({
          severity: 'warn',
          title: '\u652F\u4ED8\u622A\u56FE\u4E0A\u4F20\u6210\u529F\uFF0COCR \u5931\u8D25',
          detail: selectedScreenshotName.value || undefined,
        })
      } else {
        toast.add({
          severity: 'success',
          summary: '\u622A\u56FE\u8BC6\u522B\u6210\u529F\uFF0C\u8BF7\u786E\u8BA4\u6216\u4FEE\u6539\u8BC6\u522B\u7ED3\u679C',
          life: 2500,
        })
        notifications.add({
          severity: 'success',
          title: '\u652F\u4ED8\u622A\u56FE\u5DF2\u8BC6\u522B',
          detail: selectedScreenshotName.value || undefined,
        })
      }
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string; error?: string } } }
    const message = err.response?.data?.message || '\u622A\u56FE\u8BC6\u522B\u5931\u8D25'
    const detail = err.response?.data?.error
    toast.add({ severity: 'error', summary: detail ? `${message}\uFF1A${detail}` : message, life: 5000 })
  } finally {
    uploadingScreenshot.value = false
  }
}

const handleSaveOcrResult = async () => {
  if (!validateOcrForm()) return
  savingOcrResult.value = true
  try {
    const payload = {
      amount: Number(ocrForm.amount),
      merchant: ocrForm.merchant,
      payment_method: ocrForm.payment_method,
      description: ocrForm.description,
      transaction_time: dayjs(ocrForm.transaction_time).toISOString(),
    }

    if (uploadedPaymentId.value) {
      await paymentApi.update(uploadedPaymentId.value, payload)
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u66F4\u65B0\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u4FDD\u5B58',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    } else {
      await paymentApi.create(payload)
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u521B\u5EFA\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u4FDD\u5B58',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    }

    resetScreenshotUploadState()
    await loadPaymentsWithCount()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u4FDD\u5B58\u5931\u8D25', life: 3000 })
  } finally {
    savingOcrResult.value = false
  }
}

const resetScreenshotUploadState = () => {
  uploadedPaymentId.value = null
  selectedScreenshotFile.value = null
  selectedScreenshotName.value = ''
  screenshotError.value = ''
  ocrResult.value = null
  ocrForm.amount = 0
  ocrForm.merchant = ''
  ocrForm.payment_method = ''
  ocrForm.description = ''
  ocrForm.transaction_time = new Date()
  ocrForm.order_number = ''
  uploadScreenshotModalVisible.value = false
}

const cancelScreenshotUpload = () => {
  if (uploadedPaymentId.value) {
    paymentApi.delete(uploadedPaymentId.value).catch((error) => console.error('Failed to delete payment record:', error))
  }
  resetScreenshotUploadState()
}

const viewLinkedInvoices = async (payment: Payment) => {
  currentPaymentForInvoices.value = payment
  linkedInvoicesModalVisible.value = true
  loadingLinkedInvoices.value = true
  suggestedInvoices.value = []
  try {
    const res = await paymentApi.getPaymentInvoices(payment.id)
    if (res.data.success && res.data.data) linkedInvoices.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '\u52A0\u8F7D\u5173\u8054\u53D1\u7968\u5931\u8D25', life: 3000 })
  } finally {
    loadingLinkedInvoices.value = false
  }
  await refreshSuggestedInvoices({ showToast: false })
}

const refreshSuggestedInvoices = async (opts?: { showToast?: boolean }) => {
  if (!currentPaymentForInvoices.value) return
  loadingSuggestedInvoices.value = true
  try {
    const res = await paymentApi.getSuggestedInvoices(currentPaymentForInvoices.value.id, { debug: true })
    suggestedInvoices.value = res.data.success && res.data.data ? res.data.data : []
    if (opts?.showToast) {
      if (suggestedInvoices.value.length > 0) {
        toast.add({ severity: 'success', summary: `\u63A8\u8350\u5230 ${suggestedInvoices.value.length} \u5F20\u53EF\u5173\u8054\u7684\u53D1\u7968`, life: 2500 })
      } else if (!linkingInvoiceToPayment.value && linkedInvoices.value.length === 0) {
        toast.add({ severity: 'warn', summary: '\u6CA1\u6709\u627E\u5230\u53EF\u63A8\u8350\u7684\u53D1\u7968', life: 2500 })
      }
    }
  } catch {
    suggestedInvoices.value = []
    if (opts?.showToast) toast.add({ severity: 'error', summary: '\u63A8\u8350\u5339\u914D\u5931\u8D25', life: 3000 })
  } finally {
    loadingSuggestedInvoices.value = false
  }
}

const handleRecommendInvoices = async () => {
  await refreshSuggestedInvoices({ showToast: true })
}

const handleLinkInvoiceToPayment = async (invoiceId: string) => {
  if (!currentPaymentForInvoices.value) return
  try {
    linkingInvoiceToPayment.value = true
    await invoiceApi.linkPayment(invoiceId, currentPaymentForInvoices.value.id)
    toast.add({ severity: 'success', summary: '\u5173\u8054\u6210\u529F', life: 2000 })
    await viewLinkedInvoices(currentPaymentForInvoices.value)
    await loadLinkedInvoicesCount()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '\u5173\u8054\u5931\u8D25', life: 3500 })
  } finally {
    linkingInvoiceToPayment.value = false
  }
}

const handleUnlinkInvoiceFromPayment = async (invoiceId: string) => {
  if (!currentPaymentForInvoices.value) return
  try {
    unlinkingInvoiceFromPayment.value = true
    await invoiceApi.unlinkPayment(invoiceId, currentPaymentForInvoices.value.id)
    toast.add({ severity: 'success', summary: '\u53D6\u6D88\u5173\u8054\u6210\u529F', life: 2000 })
    await viewLinkedInvoices(currentPaymentForInvoices.value)
    await loadLinkedInvoicesCount()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '\u53D6\u6D88\u5173\u8054\u5931\u8D25', life: 3500 })
  } finally {
    unlinkingInvoiceFromPayment.value = false
  }
}

const openPaymentDetail = (payment: Payment) => {
  detailPayment.value = payment
  paymentDetailVisible.value = true
}

const getExtractedRawText = (extractedData: string | null): string => {
  if (!extractedData) return ''
  try {
    const data = JSON.parse(extractedData)
    return data.raw_text || ''
  } catch {
    return extractedData
  }
}

const handleReparseOcr = async (paymentId: string) => {
  reparsingOcr.value = true
  try {
    const res = await paymentApi.reparseScreenshot(paymentId)
    if (res.data.success) {
      toast.add({ severity: 'success', summary: '\u91CD\u65B0\u89E3\u6790\u6210\u529F', life: 2000 })
      notifications.add({ severity: 'success', title: '支付截图已重新解析', detail: paymentId })
      const detailRes = await paymentApi.getById(paymentId)
      if (detailRes.data.success && detailRes.data.data) detailPayment.value = detailRes.data.data
      await loadPaymentsWithCount()
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string; error?: string } } }
    const message = err.response?.data?.message || '\u91CD\u65B0\u89E3\u6790\u5931\u8D25'
    const detail = err.response?.data?.error
    toast.add({ severity: 'error', summary: detail ? `${message}\uFF1A${detail}` : message, life: 5000 })
    notifications.add({ severity: 'error', title: '支付截图重新解析失败', detail: detail || message })
  } finally {
    reparsingOcr.value = false
  }
}

onMounted(() => {
  loadPaymentsWithCount()
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

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.field {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.field label {
  display: block;
  font-weight: 700;
  color: var(--color-text-secondary);
}

.field :deep(.p-inputtext),
.field :deep(.p-inputnumber),
.field :deep(.p-datepicker),
.field :deep(.p-textarea),
.field :deep(.p-inputtextarea) {
  width: 100%;
}

.field :deep(.p-datepicker-input) {
  width: 100%;
}

.row-actions {
  display: flex;
  gap: 6px;
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.sbm-tag-ellipsis {
  max-width: 100%;
}

.sbm-tag-ellipsis :deep(.p-tag-label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 180px;
}

.amount {
  font-weight: 900;
  color: var(--p-red-600, #dc2626);
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

.stat-icon.danger {
  background: rgba(220, 38, 38, 0.12);
  color: var(--p-red-600, #dc2626);
}

.stat-icon.success {
  background: rgba(22, 163, 74, 0.12);
  color: var(--p-green-600, #16a34a);
}

.stat-icon.info {
  background: rgba(59, 130, 246, 0.12);
  color: var(--p-primary-600, #2563eb);
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 10px;
}

.upload-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border-radius: var(--radius-md);
  border: 1px dashed rgba(59, 130, 246, 0.35);
  border: 1px dashed color-mix(in srgb, var(--p-primary-400, #60a5fa), transparent 25%);
  background: rgba(59, 130, 246, 0.03);
  background: color-mix(in srgb, var(--p-primary-50, #eff6ff), transparent 55%);
}

.upload-screenshot-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
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
  min-height: clamp(96px, 14vh, 132px);
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

.raw-title {
  font-weight: 800;
  color: var(--color-text-secondary);
  margin: 12px 0 6px;
}

.raw-text {
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow: auto;
  background: rgba(0, 0, 0, 0.03);
  padding: 10px;
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
}

.match-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.match-title {
  font-weight: 800;
  color: var(--p-text-color);
}

.match-table :deep(.p-datatable-thead > tr > th),
.match-table :deep(.p-datatable-tbody > tr > td) {
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

.section {
  margin-top: 12px;
}

.section-title {
  font-weight: 900;
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.section-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
}

.screenshot-wrap {
  width: 100%;
  max-width: 100%;
  overflow: hidden;
}

.screenshot :deep(img) {
  display: block;
  max-width: 100%;
  height: auto;
  max-height: 420px;
  object-fit: contain;
  border-radius: var(--radius-md);
}
</style>
