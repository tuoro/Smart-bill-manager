<template>
  <Dialog
    v-model:visible="visibleModel"
    modal
    :header="'\u652F\u4ED8\u8BB0\u5F55\u8BE6\u60C5'"
    :style="{ width: currentPayment?.screenshot_path ? '1060px' : '740px', maxWidth: '94vw' }"
    :breakpoints="{ '960px': '94vw', '640px': '96vw' }"
    :content-style="{ padding: '14px 16px' }"
    :closable="!editing && !saving"
    :close-on-escape="!editing && !saving"
    @hide="onDialogHide"
  >
    <div
      v-if="currentPayment"
      class="detail"
    >
      <div class="header-row">
        <div class="title">
          <i class="pi pi-image" />
          <span
            class="sbm-ellipsis"
            :title="getScreenshotTitle(currentPayment)"
          >
            {{ getScreenshotTitle(currentPayment) }}
          </span>
        </div>
        <div class="actions">
          <Button
            v-if="!editing"
            class="p-button-outlined"
            severity="secondary"
            icon="pi pi-pencil"
            :label="'\u7F16\u8F91'"
            @click="enterEditMode"
          />
          <Button
            class="p-button-outlined"
            severity="secondary"
            icon="pi pi-refresh"
            :label="'\u91CD\u65B0\u89E3\u6790'"
            :loading="reparsing"
            :disabled="editing || saving || !currentPayment.screenshot_path"
            @click="reparse(currentPayment.id)"
          />
        </div>
      </div>

      <div class="payment-detail-layout">
        <div class="payment-detail-left">
          <div class="section-title">
            &#25903;&#20184;&#25130;&#22270;
          </div>
          <div
            v-if="currentPayment.screenshot_path && screenshotSrc"
            class="screenshot-wrap"
          >
            <Image
              class="screenshot"
              :src="screenshotSrc"
              preview
              :image-style="{ width: '100%', maxWidth: '100%', height: 'auto' }"
            />
          </div>
          <Message
            v-else
            severity="secondary"
            :closable="false"
          >
            &#26242;&#26080;&#25903;&#20184;&#25130;&#22270;
          </Message>
        </div>

        <div class="payment-detail-right">
          <div class="grid sbm-grid-tight">
            <div class="col-12 md:col-6">
              <div class="kv">
                <div class="k">
                  &#37329;&#39069;
                </div>
                <div
                  class="v"
                  :class="{ amount: !editing }"
                >
                  <InputNumber
                    v-if="editing"
                    v-model="form.amount"
                    :min-fraction-digits="2"
                    :max-fraction-digits="2"
                    :min="0"
                    :use-grouping="false"
                  />
                  <template v-else>
                    {{ formatMoney(currentPayment.amount || 0) }}
                  </template>
                </div>
              </div>
            </div>
            <div class="col-12 md:col-6">
              <div class="kv">
                <div class="k">
                  &#21830;&#23478;
                </div>
                <div
                  class="v"
                  :title="normalizeInlineText(currentPayment.merchant)"
                >
                  <InputText
                    v-if="editing"
                    v-model.trim="form.merchant"
                  />
                  <template v-else>
                    {{ normalizeInlineText(currentPayment.merchant) || '-' }}
                  </template>
                </div>
              </div>
            </div>
            <div class="col-12 md:col-6">
              <div class="kv">
                <div class="k">
                  &#25903;&#20184;&#26041;&#24335;
                </div>
                <div class="v">
                  <InputText
                    v-if="editing"
                    v-model.trim="form.payment_method"
                  />
                  <template v-else>
                    <Tag
                      v-if="currentPayment.payment_method"
                      class="sbm-tag-ellipsis"
                      severity="success"
                      :value="normalizePaymentMethodText(currentPayment.payment_method)"
                      :title="normalizePaymentMethodText(currentPayment.payment_method)"
                    />
                    <span v-else>-</span>
                  </template>
                </div>
              </div>
            </div>
            <div class="col-12 md:col-6">
              <div class="kv">
                <div class="k">
                  &#20132;&#26131;&#26102;&#38388;
                </div>
                <div class="v">
                  <template v-if="editing">
                    <InputText
                      :model-value="formatDateTimeDraft(form.transaction_time)"
                      readonly
                      :placeholder="'请选择交易时间'"
                      @click="toggleTimePanel"
                    />
                    <OverlayPanel
                      ref="timePanel"
                      :dismissable="true"
                      :show-close-icon="false"
                      class="payment-time-panel"
                      @show="onTimePanelShow"
                      @hide="onTimePanelHide"
                    >
                      <DatePicker
                        v-model="timeDraft"
                        inline
                        show-time
                        :manual-input="false"
                      />
                      <div class="payment-time-panel-footer">
                        <Button
                          type="button"
                          class="p-button-outlined"
                          severity="secondary"
                          :label="'取消'"
                          @click="cancelTimePanel"
                        />
                        <Button
                          type="button"
                          :label="'确认'"
                          icon="pi pi-check"
                          @click="confirmTimePanel"
                        />
                      </div>
                    </OverlayPanel>
                  </template>
                  <template v-else>
                    {{ formatDateTime(currentPayment.transaction_time) }}
                  </template>
                </div>
              </div>
            </div>
            <div class="col-12">
              <div class="kv">
                <div class="k">
                  &#22791;&#27880;
                </div>
                <div class="v">
                  <Textarea
                    v-if="editing"
                    v-model="form.description"
                    auto-resize
                    rows="3"
                  />
                  <template v-else>
                    {{ currentPayment.description || '-' }}
                  </template>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="currentPayment.extracted_data"
        class="section"
      >
        <div class="section-title">
          OCR &#25991;&#26412;
        </div>
        <Accordion>
          <AccordionTab
            v-if="getExtractedPrettyText(currentPayment.extracted_data || null)"
            :header="'\u70B9\u51FB\u67E5\u770B OCR \u6574\u7406\u7248\u6587\u672C'"
          >
            <pre class="raw-text">{{ getExtractedPrettyText(currentPayment.extracted_data || null) }}</pre>
          </AccordionTab>
          <AccordionTab :header="'\u70B9\u51FB\u67E5\u770B OCR \u539F\u59CB\u6587\u672C'">
            <pre class="raw-text">{{ getExtractedRawText(currentPayment.extracted_data || null) }}</pre>
          </AccordionTab>
        </Accordion>
      </div>
    </div>
    <template #footer>
      <div
        v-if="editing"
        class="dialog-footer-center"
      >
        <Button
          type="button"
          class="p-button-outlined"
          severity="secondary"
          :label="'\u53D6\u6D88'"
          :disabled="saving"
          @click="cancelEditMode"
        />
        <Button
          type="button"
          :label="'\u4FDD\u5B58'"
          icon="pi pi-check"
          :loading="saving"
          @click="save"
        />
      </div>
      <Button
        v-else
        type="button"
        class="p-button-outlined"
        severity="secondary"
        :label="'\u5173\u95ED'"
        @click="visibleModel = false"
      />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import dayjs from 'dayjs'
import Accordion from 'primevue/accordion'
import AccordionTab from 'primevue/accordiontab'
import Button from 'primevue/button'
import DatePicker from 'primevue/datepicker'
import Dialog from 'primevue/dialog'
import Image from 'primevue/image'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import OverlayPanel from 'primevue/overlaypanel'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { paymentApi } from '@/api/payments'
import { useNotificationStore } from '@/stores/notifications'
import type { Payment } from '@/types'

const props = defineProps<{
  visible: boolean
  payment: Payment | null
}>()

const emit = defineEmits<{
  'update:visible': [visible: boolean]
  changed: []
}>()

const visibleModel = computed({
  get: () => props.visible,
  set: (visible: boolean) => emit('update:visible', visible),
})

const toast = useToast()
const notifications = useNotificationStore()
const currentPayment = ref<Payment | null>(null)
const screenshotSrc = ref('')
const editing = ref(false)
const saving = ref(false)
const reparsing = ref(false)
const timeDraft = ref<Date | null>(null)
const timeLastTarget = ref<HTMLElement | null>(null)

type TimePanelInstance = InstanceType<typeof OverlayPanel> & {
  visible?: boolean
  container?: HTMLElement
}

const timePanel = ref<TimePanelInstance | null>(null)
const form = reactive({
  amount: 0,
  merchant: '',
  payment_method: '',
  description: '',
  transaction_time: null as Date | null,
})

let loadSequence = 0
let screenshotController: AbortController | null = null

const formatMoney = (value: number) => `\u00A5${(value || 0).toFixed(2)}`
const formatDateTime = (date: string) => dayjs(date).format('YYYY-MM-DD HH:mm')
const formatDateTimeDraft = (date: Date | null) => (date ? dayjs(date).format('YYYY-MM-DD HH:mm') : '')

const normalizeInlineText = (value?: string | null) => {
  const text = (value || '').replace(/\s+/g, ' ').trim()
  return text.replace(/[>鈥郝汇€夈€嬧啋]+$/g, '').trim()
}

const normalizePaymentMethodText = (value?: string | null) => {
  const text = normalizeInlineText(value)
  return text.replace(/[（）]/g, (character) => (character === '（' ? '(' : ')')).trim()
}

const fillForm = (payment: Payment) => {
  form.amount = Number(payment.amount || 0)
  form.merchant = payment.merchant || ''
  form.payment_method = normalizePaymentMethodText(payment.payment_method || '')
  form.description = payment.description || ''
  form.transaction_time = payment.transaction_time ? new Date(payment.transaction_time) : new Date()
}

const revokeScreenshot = () => {
  screenshotController?.abort()
  screenshotController = null
  if (screenshotSrc.value && typeof window !== 'undefined') URL.revokeObjectURL(screenshotSrc.value)
  screenshotSrc.value = ''
}

const loadScreenshot = async (payment: Payment, sequence: number) => {
  revokeScreenshot()
  if (typeof window === 'undefined' || !payment.screenshot_path) return

  const controller = new AbortController()
  screenshotController = controller
  try {
    const response = await paymentApi.getScreenshotBlob(payment.id, { signal: controller.signal })
    if (controller.signal.aborted || sequence !== loadSequence) return
    screenshotSrc.value = URL.createObjectURL(response.data as Blob)
  } catch (error: unknown) {
    if (!controller.signal.aborted) console.warn('加载支付截图失败:', error)
  } finally {
    if (screenshotController === controller) screenshotController = null
  }
}

const loadPayment = async () => {
  const payment = props.payment
  if (!props.visible || !payment) return

  const sequence = ++loadSequence
  currentPayment.value = payment
  editing.value = false
  saving.value = false
  fillForm(payment)

  let fullPayment = payment
  if (payment.id && !payment.extracted_data) {
    try {
      const response = await paymentApi.getById(payment.id)
      if (sequence !== loadSequence || !props.visible) return
      if (response.data.success && response.data.data) fullPayment = response.data.data
    } catch {
      // 列表数据仍可用于展示，详情补全失败不阻断打开。
    }
  }

  currentPayment.value = fullPayment
  fillForm(fullPayment)
  await loadScreenshot(fullPayment, sequence)
}

watch(
  () => [props.visible, props.payment?.id] as const,
  ([visible]) => {
    if (!visible) {
      loadSequence += 1
      revokeScreenshot()
      return
    }
    void loadPayment()
  },
  { immediate: true },
)

const isTimePanelOpen = () => Boolean(timePanel.value?.visible)

const getTimeOverlayElement = (): HTMLElement | null => {
  if (timePanel.value?.container) return timePanel.value.container
  return (
    (document.querySelector('.p-popover.payment-time-panel') as HTMLElement | null) ||
    (document.querySelector('.p-overlaypanel.payment-time-panel') as HTMLElement | null)
  )
}

const forceTimePanelBelow = () => {
  if (typeof window === 'undefined' || !timeLastTarget.value) return
  const overlay = getTimeOverlayElement()
  if (!overlay) return

  const rect = timeLastTarget.value.getBoundingClientRect()
  const scrollX = window.scrollX || document.documentElement.scrollLeft || 0
  const scrollY = window.scrollY || document.documentElement.scrollTop || 0
  const gap = 6
  const width = overlay.getBoundingClientRect().width || overlay.offsetWidth
  const minLeft = scrollX + 8
  const maxLeft = scrollX + window.innerWidth - width - 8
  const desiredLeft = rect.left + scrollX
  const left = Number.isFinite(width) && width > 0 ? Math.max(minLeft, Math.min(desiredLeft, maxLeft)) : desiredLeft

  overlay.style.top = ''
  overlay.style.bottom = ''
  overlay.style.left = ''
  overlay.style.right = ''
  overlay.style.insetBlockStart = `${rect.bottom + scrollY + gap}px`
  overlay.style.insetBlockEnd = 'auto'
  overlay.style.insetInlineStart = `${left}px`
  overlay.style.insetInlineEnd = 'auto'

  const available = window.innerHeight - rect.bottom - gap - 16
  const content =
    (overlay.querySelector('.p-popover-content') as HTMLElement | null) ||
    (overlay.querySelector('.p-overlaypanel-content') as HTMLElement | null)
  if (content) {
    content.style.maxHeight = `${Math.max(240, Math.floor(available))}px`
    content.style.overflow = 'auto'
  }
}

const realignTimePanel = async () => {
  await nextTick()
  if (!timePanel.value || !isTimePanelOpen()) return
  const align = () => {
    timePanel.value?.alignOverlay()
    forceTimePanelBelow()
  }
  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    window.requestAnimationFrame(align)
  } else {
    align()
  }
}

const toggleTimePanel = (event: MouseEvent) => {
  if (!editing.value) return
  timeLastTarget.value = event.currentTarget as HTMLElement | null
  timeDraft.value = form.transaction_time ? new Date(form.transaction_time) : new Date()
  timePanel.value?.toggle(event)
  void realignTimePanel()
}

const onTimePanelShow = async () => {
  await realignTimePanel()
  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    window.requestAnimationFrame(forceTimePanelBelow)
  } else {
    forceTimePanelBelow()
  }
}

const onTimePanelHide = () => {
  timeLastTarget.value = null
}

const cancelTimePanel = () => {
  timeDraft.value = form.transaction_time ? new Date(form.transaction_time) : new Date()
  timePanel.value?.hide()
}

const confirmTimePanel = () => {
  if (!timeDraft.value) return
  form.transaction_time = new Date(timeDraft.value)
  timePanel.value?.hide()
}

const enterEditMode = () => {
  if (!currentPayment.value) return
  editing.value = true
  timePanel.value?.hide()
  timeLastTarget.value = null
  timeDraft.value = null
  fillForm(currentPayment.value)
}

const cancelEditMode = () => {
  timePanel.value?.hide()
  timeLastTarget.value = null
  timeDraft.value = null
  if (currentPayment.value) fillForm(currentPayment.value)
  editing.value = false
}

const save = async () => {
  if (!currentPayment.value) return
  if (!Number.isFinite(Number(form.amount)) || Number(form.amount) <= 0) {
    toast.add({ severity: 'warn', summary: '请填写金额', life: 2200 })
    return
  }
  if (!form.transaction_time) {
    toast.add({ severity: 'warn', summary: '请选择交易时间', life: 2200 })
    return
  }

  saving.value = true
  try {
    timePanel.value?.hide()
    const payload = {
      amount: Number(form.amount),
      merchant: form.merchant,
      payment_method: normalizePaymentMethodText(form.payment_method),
      description: form.description,
      transaction_time: dayjs(form.transaction_time).toISOString(),
    }
    await paymentApi.update(currentPayment.value.id, payload)
    const refreshed = await paymentApi.getById(currentPayment.value.id)
    currentPayment.value =
      refreshed.data.success && refreshed.data.data
        ? refreshed.data.data
        : ({ ...currentPayment.value, ...payload } as Payment)
    toast.add({ severity: 'success', summary: '已保存', life: 2000 })
    editing.value = false
    emit('changed')
  } catch {
    toast.add({ severity: 'error', summary: '保存失败', life: 3000 })
  } finally {
    saving.value = false
  }
}

const getExtractedRawText = (extractedData: string | null) => {
  if (!extractedData) return ''
  try {
    const data = JSON.parse(extractedData) as { raw_text?: string }
    return data.raw_text || ''
  } catch {
    return extractedData
  }
}

const getExtractedPrettyText = (extractedData: string | null) => {
  if (!extractedData) return ''
  try {
    const data = JSON.parse(extractedData) as { pretty_text?: string }
    return data.pretty_text || ''
  } catch {
    return ''
  }
}

const getScreenshotTitle = (payment: Payment) => {
  const path = payment.screenshot_path || ''
  if (path) return path.split('/').pop() || path
  return normalizeInlineText(payment.merchant) || payment.id
}

const reparse = async (paymentId: string) => {
  reparsing.value = true
  try {
    const response = await paymentApi.reparseScreenshot(paymentId)
    if (response.data.success) {
      toast.add({ severity: 'success', summary: '重新解析成功', life: 2000 })
      notifications.add({ severity: 'success', title: '支付截图已重新解析', detail: paymentId })
      const detailResponse = await paymentApi.getById(paymentId)
      if (detailResponse.data.success && detailResponse.data.data) currentPayment.value = detailResponse.data.data
      emit('changed')
    }
  } catch (error: unknown) {
    const requestError = error as { response?: { data?: { message?: string; error?: string } } }
    const message = requestError.response?.data?.message || '重新解析失败'
    const detail = requestError.response?.data?.error
    toast.add({ severity: 'error', summary: detail ? `${message}：${detail}` : message, life: 5000 })
    notifications.add({ severity: 'error', title: '支付截图重新解析失败', detail: detail || message })
  } finally {
    reparsing.value = false
  }
}

const handleViewportChange = () => {
  if (isTimePanelOpen()) void realignTimePanel()
}

const onDialogHide = () => {
  loadSequence += 1
  timePanel.value?.hide()
  timeLastTarget.value = null
  timeDraft.value = null
  editing.value = false
  revokeScreenshot()
}

onMounted(() => {
  window.addEventListener('resize', handleViewportChange, { passive: true })
  window.addEventListener('orientationchange', handleViewportChange, { passive: true })
  window.addEventListener('scroll', handleViewportChange, true)
})

onBeforeUnmount(() => {
  loadSequence += 1
  revokeScreenshot()
  window.removeEventListener('resize', handleViewportChange)
  window.removeEventListener('orientationchange', handleViewportChange)
  window.removeEventListener('scroll', handleViewportChange, true)
})
</script>

<style scoped>
.kv {
  border: 1px solid color-mix(in srgb, var(--p-surface-200), transparent 35%);
  background: color-mix(in srgb, var(--p-surface-0), transparent 10%);
  border-radius: var(--radius-md);
  padding: 8px 10px;
}

.kv :deep(.p-inputtext),
.kv :deep(.p-inputnumber),
.kv :deep(.p-datepicker),
.kv :deep(.p-textarea),
.kv :deep(.p-inputtextarea),
.kv :deep(.p-datepicker-input) {
  width: 100%;
}

.header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}

.detail .sbm-grid-tight {
  margin: 0;
}

.detail .sbm-grid-tight > [class*='col-'] {
  padding: 0.35rem;
}

.title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 900;
  color: var(--color-text-primary);
  min-width: 0;
}

.actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.k {
  font-size: 12px;
  font-weight: 800;
  color: var(--color-text-tertiary);
  display: flex;
  align-items: center;
  min-height: 18px;
  line-height: 1.6;
  padding-bottom: 1px;
}

.v {
  margin-top: 6px;
  font-weight: 700;
  color: var(--color-text-primary);
  line-height: 1.6;
}

.amount {
  font-weight: 900;
  color: var(--p-red-600, #dc2626);
}

.section {
  margin-top: 10px;
}

.section-title {
  font-weight: 900;
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.payment-detail-layout {
  display: grid;
  grid-template-columns: minmax(320px, 38%) 1fr;
  gap: 14px;
  align-items: start;
}

.payment-detail-left,
.payment-detail-right {
  min-width: 0;
}

.screenshot-wrap {
  width: 100%;
  max-width: 100%;
  overflow: hidden;
}

.screenshot :deep(img) {
  display: block;
  max-width: 100%;
  width: 100%;
  height: auto;
  max-height: 60vh;
  object-fit: contain;
  border-radius: var(--radius-md);
}

.sbm-tag-ellipsis {
  display: inline-flex;
  flex: 0 1 auto;
  width: fit-content;
  max-width: 100%;
  min-width: 0;
}

.sbm-tag-ellipsis :deep(.p-tag-label) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.raw-text {
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow: auto;
  background: color-mix(in srgb, var(--p-surface-0), transparent 8%);
  padding: 10px;
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
}

.dialog-footer-center {
  width: 100%;
  display: flex;
  justify-content: center;
  gap: 10px;
}

@media (max-width: 900px) {
  .payment-detail-layout {
    grid-template-columns: 1fr;
  }
}

:global(.p-popover.payment-time-panel),
:global(.p-overlaypanel.payment-time-panel) {
  width: auto;
  max-width: calc(100vw - 16px);
  border-radius: 16px;
  box-shadow: var(--shadow-xl);
  overflow: hidden;
}

:global(.p-popover.payment-time-panel .p-popover-arrow),
:global(.p-overlaypanel.payment-time-panel .p-overlaypanel-arrow) {
  display: none;
}

:global(.p-popover.payment-time-panel .p-popover-content),
:global(.p-overlaypanel.payment-time-panel .p-overlaypanel-content) {
  padding: 10px 12px 8px;
}

:global(.payment-time-panel .p-datepicker) {
  display: inline-block;
  font-size: 0.92rem;
}

:global(.payment-time-panel .p-datepicker),
:global(.payment-time-panel .p-datepicker-panel) {
  width: auto !important;
}

:global(.payment-time-panel .p-datepicker-panel-inline) {
  display: flex;
  align-items: center;
  gap: 8px;
}

:global(.payment-time-panel .p-datepicker-calendar-container) {
  padding: 0 0.4rem;
}

:global(.payment-time-panel .p-datepicker-header) {
  padding: 0.55rem 0.65rem;
}

:global(.payment-time-panel .p-datepicker-time-picker) {
  padding: 0.55rem 0.65rem;
  border-block-start: 0 none !important;
  border-top: 0 none !important;
  border-left: 0 none !important;
}

.payment-time-panel-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 10px;
}

@media (max-width: 640px) {
  :global(.payment-time-panel .p-datepicker-panel-inline) {
    flex-direction: column;
    align-items: stretch;
  }

  :global(.payment-time-panel .p-datepicker-time-picker) {
    border-left: 0;
    border-top: 1px solid rgba(0, 0, 0, 0.06);
  }
}
</style>
