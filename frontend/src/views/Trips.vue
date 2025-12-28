<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>行程日历</span>
          <div class="toolbar">
            <Button label="刷新" icon="pi pi-refresh" class="p-button-outlined" @click="reloadAll" />
            <Button label="新增行程" icon="pi pi-plus" @click="openTripModal()" />
          </div>
        </div>
      </template>
      <template #content>
        <Tabs v-model:value="activeTab">
          <TabList>
            <Tab value="trips">按行程</Tab>
            <Tab value="calendar">日历</Tab>
          </TabList>

          <TabPanels>
            <TabPanel value="trips">
              <div v-if="trips.length === 0" class="empty">
                <div class="empty-title">暂无行程</div>
                <div class="empty-sub">创建一个行程后，可同步归属该时间段的支付记录，并查看关联发票。</div>
                <Button label="新增行程" icon="pi pi-plus" @click="openTripModal()" />
              </div>

              <Accordion v-else :multiple="true" class="trip-accordion" @tab-open="handleTripOpen">
                <AccordionTab v-for="trip in trips" :key="trip.id">
                  <template #header>
                    <div class="trip-header">
                      <div class="trip-title">
                        <div class="trip-name sbm-ellipsis" :title="trip.name">{{ trip.name }}</div>
                        <div class="trip-range sbm-ellipsis" :title="`${formatDateTime(trip.start_time)} ~ ${formatDateTime(trip.end_time)}`">
                          {{ formatDateTime(trip.start_time) }} ~ {{ formatDateTime(trip.end_time) }}
                        </div>
                      </div>
                      <div class="trip-badges">
                        <Tag :value="formatMoney(summaries[trip.id]?.total_amount || 0)" severity="success" />
                        <Tag :value="`支付 ${summaries[trip.id]?.payment_count || 0}`" severity="info" />
                        <Tag :value="`发票 ${summaries[trip.id]?.linked_invoices || 0}`" severity="secondary" />
                        <Tag
                          v-if="(summaries[trip.id]?.unlinked_payments || 0) > 0"
                          :value="`未关联 ${summaries[trip.id]?.unlinked_payments || 0}`"
                          severity="warning"
                        />
                      </div>
                    </div>
                  </template>

                  <div class="trip-actions">
                    <div class="trip-actions-left">
                      <Button
                        label="同步该时间段支付记录"
                        icon="pi pi-sync"
                        class="p-button-outlined"
                        :loading="syncingTripId === trip.id"
                        @click="confirmSync(trip)"
                      />
                      <Button label="编辑行程" icon="pi pi-pencil" class="p-button-text" @click="openTripModal(trip)" />
                      <Button
                        label="删除行程"
                        icon="pi pi-trash"
                        class="p-button-text p-button-danger"
                        :loading="deletingTripId === trip.id"
                        @click="confirmDeleteTrip(trip)"
                      />
                    </div>
                    <div class="trip-actions-right">
                      <small class="muted">
                        删除行程会删除“归属到该行程”的支付记录，并按规则删除/解绑关联发票
                      </small>
                    </div>
                  </div>

                  <DataTable
                    :value="tripPayments[trip.id] || []"
                    :loading="loadingPaymentsTripId === trip.id"
                    responsiveLayout="scroll"
                    sortField="transaction_time"
                    :sortOrder="-1"
                    class="trip-table"
                  >
                    <Column field="amount" header="金额" :style="{ width: '120px' }" sortable>
                      <template #body="{ data: row }">
                        <span class="amount">{{ formatMoney(row.amount) }}</span>
                      </template>
                    </Column>
                    <Column header="商家">
                      <template #body="{ data: row }">
                        <span class="sbm-ellipsis" :title="row.merchant || '-'">{{ row.merchant || '-' }}</span>
                      </template>
                    </Column>
                    <Column header="支付方式" :style="{ width: '180px' }">
                      <template #body="{ data: row }">
                        <span class="sbm-ellipsis" :title="row.payment_method || '-'">{{ row.payment_method || '-' }}</span>
                      </template>
                    </Column>
                    <Column field="transaction_time" header="支付时间" :style="{ width: '180px' }" sortable>
                      <template #body="{ data: row }">
                        {{ formatDateTime(row.transaction_time) }}
                      </template>
                    </Column>
                    <Column header="关联发票">
                      <template #body="{ data: row }">
                        <div class="invoice-chips">
                          <Tag
                            v-for="inv in row.invoices || []"
                            :key="inv.id"
                            class="invoice-chip"
                            severity="secondary"
                            :value="inv.invoice_number || inv.seller_name || inv.id"
                            :title="inv.invoice_number || inv.seller_name || inv.id"
                          />
                          <span v-if="!row.invoices || row.invoices.length === 0" class="muted">-</span>
                        </div>
                      </template>
                    </Column>
                    <Column header="操作" :style="{ width: '220px' }">
                      <template #body="{ data: row }">
                        <div class="row-actions">
                          <Button
                            size="small"
                            class="p-button-outlined"
                            severity="secondary"
                            icon="pi pi-times"
                            label="移出行程"
                            @click="unassignPayment(row.id)"
                          />
                          <Button
                            size="small"
                            class="p-button-text"
                            icon="pi pi-external-link"
                            label="移动到..."
                            @click="openMovePayment(row.id, trip.id)"
                          />
                        </div>
                      </template>
                    </Column>
                  </DataTable>
                </AccordionTab>
              </Accordion>
            </TabPanel>

            <TabPanel value="calendar">
              <div class="calendar-layout">
                <div class="calendar-left sbm-surface">
                  <div class="calendar-toolbar">
                    <Dropdown
                      v-model="calendarTripFilter"
                      :options="calendarTripOptions"
                      optionLabel="label"
                      optionValue="value"
                      placeholder="按行程筛选"
                      class="calendar-dropdown"
                    />
                    <Button label="本月" icon="pi pi-calendar" class="p-button-outlined" @click="goToThisMonth" />
                  </div>
                  <DatePicker
                    v-model="calendarSelectedDate"
                    inline
                    :manualInput="false"
                    @month-change="handleCalendarMonthChange"
                    @year-change="handleCalendarMonthChange"
                  >
                    <template #date="{ date }">
                      <div class="date-cell" :class="{ 'is-today': date.today }">
                        <div class="date-day">{{ date.day }}</div>
                        <div v-if="getCalendarDayTotal(date) !== 0" class="date-total">
                          {{ formatMoneyCompact(getCalendarDayTotal(date)) }}
                        </div>
                      </div>
                    </template>
                  </DatePicker>
                </div>

                <div class="calendar-right">
                  <Card class="sbm-surface">
                    <template #title>
                      <div class="panel-title">
                        <span>{{ calendarRightTitle }}</span>
                        <Tag v-if="calendarSelectedTotal !== 0" :value="formatMoney(calendarSelectedTotal)" severity="success" />
                      </div>
                    </template>
                    <template #content>
                      <DataTable :value="calendarSelectedPayments" responsiveLayout="scroll">
                        <Column field="amount" header="金额" :style="{ width: '120px' }">
                          <template #body="{ data: row }">
                            <span class="amount">{{ formatMoney(row.amount) }}</span>
                          </template>
                        </Column>
                        <Column header="商家">
                          <template #body="{ data: row }">
                            <span class="sbm-ellipsis" :title="row.merchant || '-'">{{ row.merchant || '-' }}</span>
                          </template>
                        </Column>
                        <Column field="transaction_time" header="时间" :style="{ width: '180px' }">
                          <template #body="{ data: row }">{{ formatDateTime(row.transaction_time) }}</template>
                        </Column>
                        <Column header="行程" :style="{ width: '180px' }">
                          <template #body="{ data: row }">
                            <Tag
                              v-if="row.trip_id && tripNameById[row.trip_id]"
                              severity="secondary"
                              class="sbm-tag-ellipsis"
                              :value="tripNameById[row.trip_id]"
                              :title="tripNameById[row.trip_id]"
                            />
                            <span v-else class="muted">-</span>
                          </template>
                        </Column>
                      </DataTable>
                      <small class="muted">日历视图按支付时间统计；关联发票请在“按行程”中查看。</small>
                    </template>
                  </Card>
                </div>
              </div>
            </TabPanel>
          </TabPanels>
        </Tabs>
      </template>
    </Card>

    <Dialog
      v-model:visible="tripModalVisible"
      modal
      :header="editingTrip ? '编辑行程' : '新增行程'"
      :style="{ width: '760px', maxWidth: '96vw' }"
    >
      <form class="p-fluid" @submit.prevent="handleSaveTrip">
        <div class="grid">
          <div class="col-12 field">
            <label for="trip_name">行程名称</label>
            <InputText id="trip_name" v-model.trim="tripForm.name" />
            <small v-if="tripErrors.name" class="p-error">{{ tripErrors.name }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_start">开始时间</label>
            <DatePicker id="trip_start" v-model="tripForm.start" showTime showSeconds :manualInput="false" />
            <small v-if="tripErrors.start" class="p-error">{{ tripErrors.start }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_end">结束时间</label>
            <DatePicker id="trip_end" v-model="tripForm.end" showTime showSeconds :manualInput="false" />
            <small v-if="tripErrors.end" class="p-error">{{ tripErrors.end }}</small>
          </div>
          <div class="col-12 field">
            <label for="trip_note">备注（可选）</label>
            <Textarea id="trip_note" v-model.trim="tripForm.note" autoResize rows="2" />
          </div>
        </div>

        <div class="footer">
          <Button type="button" class="p-button-outlined" severity="secondary" label="取消" @click="tripModalVisible = false" />
          <Button type="submit" label="保存" icon="pi pi-check" :loading="savingTrip" />
        </div>
      </form>
    </Dialog>

    <Dialog v-model:visible="movePaymentModalVisible" modal header="移动支付记录" :style="{ width: '560px', maxWidth: '92vw' }">
      <div class="field">
        <label>选择目标行程</label>
        <Dropdown
          v-model="movePaymentTargetTripId"
          :options="moveTargetOptions"
          optionLabel="label"
          optionValue="value"
          placeholder="请选择行程"
        />
      </div>
      <div class="footer">
        <Button type="button" class="p-button-outlined" severity="secondary" label="取消" @click="closeMovePayment" />
        <Button type="button" label="移动" icon="pi pi-check" :disabled="!movePaymentTargetTripId" :loading="movingPayment" @click="confirmMovePayment" />
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Accordion from 'primevue/accordion'
import AccordionTab from 'primevue/accordiontab'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import DatePicker from 'primevue/datepicker'
import Dialog from 'primevue/dialog'
import Dropdown from 'primevue/dropdown'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import dayjs from 'dayjs'
import { paymentApi, tripsApi } from '@/api'
import type { Payment, Trip, TripAssignPreview, TripCascadePreview, TripPaymentWithInvoices, TripSummary } from '@/types'
import { useNotificationStore } from '@/stores/notifications'

const toast = useToast()
const confirm = useConfirm()
const notifications = useNotificationStore()

const activeTab = ref<'trips' | 'calendar'>('trips')

const trips = ref<Trip[]>([])
const summaries = reactive<Record<string, TripSummary>>({})
const tripPayments = reactive<Record<string, TripPaymentWithInvoices[]>>({})

const loadingPaymentsTripId = ref<string | null>(null)
const syncingTripId = ref<string | null>(null)
const deletingTripId = ref<string | null>(null)

const tripModalVisible = ref(false)
const savingTrip = ref(false)
const editingTrip = ref<Trip | null>(null)

const tripForm = reactive({
  name: '',
  start: null as Date | null,
  end: null as Date | null,
  note: '',
})

const tripErrors = reactive({
  name: '',
  start: '',
  end: '',
})

const movePaymentModalVisible = ref(false)
const movePaymentId = ref<string | null>(null)
const movePaymentFromTripId = ref<string | null>(null)
const movePaymentTargetTripId = ref<string | null>(null)
const movingPayment = ref(false)

const tripNameById = computed<Record<string, string>>(() => {
  const m: Record<string, string> = {}
  for (const t of trips.value) m[t.id] = t.name
  return m
})

const formatDateTime = (date: string) => dayjs(date).format('YYYY-MM-DD HH:mm:ss')
const formatMoney = (amount: number) => `¥${Number(amount || 0).toFixed(2)}`
const formatMoneyCompact = (amount: number) => {
  const v = Number(amount || 0)
  if (Math.abs(v) >= 10000) return `${(v / 10000).toFixed(1)}w`
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}k`
  return `${v.toFixed(0)}`
}

const validateTripForm = () => {
  tripErrors.name = ''
  tripErrors.start = ''
  tripErrors.end = ''
  if (!tripForm.name) tripErrors.name = '请输入行程名称'
  if (!tripForm.start) tripErrors.start = '请选择开始时间'
  if (!tripForm.end) tripErrors.end = '请选择结束时间'
  if (tripForm.start && tripForm.end && tripForm.end < tripForm.start) tripErrors.end = '结束时间不能早于开始时间'
  return !tripErrors.name && !tripErrors.start && !tripErrors.end
}

const resetTripForm = () => {
  tripForm.name = ''
  tripForm.start = null
  tripForm.end = null
  tripForm.note = ''
  editingTrip.value = null
  tripErrors.name = ''
  tripErrors.start = ''
  tripErrors.end = ''
}

const openTripModal = (trip?: Trip) => {
  resetTripForm()
  if (trip) {
    editingTrip.value = trip
    tripForm.name = trip.name
    tripForm.start = new Date(trip.start_time)
    tripForm.end = new Date(trip.end_time)
    tripForm.note = trip.note || ''
  }
  tripModalVisible.value = true
}

const handleSaveTrip = async () => {
  if (!validateTripForm()) return
  savingTrip.value = true
  try {
    const payload = {
      name: tripForm.name,
      start_time: dayjs(tripForm.start!).toISOString(),
      end_time: dayjs(tripForm.end!).toISOString(),
      note: tripForm.note || undefined,
    }

    if (editingTrip.value) {
      await tripsApi.update(editingTrip.value.id, payload)
      toast.add({ severity: 'success', summary: '行程已更新', life: 2000 })
      notifications.add({ severity: 'success', title: '行程已更新', detail: payload.name })
    } else {
      await tripsApi.create(payload as any)
      toast.add({ severity: 'success', summary: '行程已创建', life: 2000 })
      notifications.add({ severity: 'success', title: '行程已创建', detail: payload.name })
    }

    tripModalVisible.value = false
    await reloadAll()
  } catch (error: any) {
    const msg = error?.response?.data?.message || '保存失败'
    toast.add({ severity: 'error', summary: msg, life: 3500 })
    notifications.add({ severity: 'error', title: '行程保存失败', detail: msg })
  } finally {
    savingTrip.value = false
  }
}

const loadTrips = async () => {
  const res = await tripsApi.list()
  trips.value = res.data.data || []
}

const loadSummary = async (tripId: string) => {
  const res = await tripsApi.getSummary(tripId)
  if (res.data.data) summaries[tripId] = res.data.data
}

const loadTripPayments = async (tripId: string) => {
  loadingPaymentsTripId.value = tripId
  try {
    const res = await tripsApi.getPayments(tripId, true)
    tripPayments[tripId] = res.data.data || []
  } finally {
    loadingPaymentsTripId.value = null
  }
}

const handleTripOpen = (e: { index: number }) => {
  const trip = trips.value[e.index]
  if (!trip) return
  if (tripPayments[trip.id]) return
  loadTripPayments(trip.id)
}

const reloadAll = async () => {
  await loadTrips()
  await Promise.all(trips.value.map((t) => loadSummary(t.id)))
  // keep payments lazy; load on demand if already opened (simple: just refresh cache)
  for (const t of trips.value) {
    if (tripPayments[t.id]) await loadTripPayments(t.id)
  }
  await refreshCalendarMonth()
}

const confirmSync = async (trip: Trip) => {
  syncingTripId.value = trip.id
  try {
    const res = await tripsApi.assignByTimePreview(trip.id)
    const preview = res.data.data as TripAssignPreview | undefined
    syncingTripId.value = null
    if (!preview) return

    confirm.require({
      header: '同步确认',
      message: `匹配到 ${preview.matched_payments} 条支付，预计同步 ${preview.will_assign} 条（已归属其他行程 ${preview.assigned_other_trip} 条将跳过）。继续吗？`,
      icon: 'pi pi-exclamation-triangle',
      acceptLabel: '同步',
      rejectLabel: '取消',
      accept: async () => {
        syncingTripId.value = trip.id
        try {
          await tripsApi.assignByTime(trip.id)
          toast.add({ severity: 'success', summary: '同步完成', life: 2200 })
          notifications.add({
            severity: 'success',
            title: '行程同步完成',
            detail: `${trip.name}：同步 ${preview.will_assign} 条（匹配 ${preview.matched_payments} 条）`,
          })
          await loadSummary(trip.id)
          await loadTripPayments(trip.id)
        } catch (e: any) {
          const msg = e?.response?.data?.message || '同步失败'
          toast.add({ severity: 'error', summary: msg, life: 3500 })
          notifications.add({ severity: 'error', title: '行程同步失败', detail: `${trip.name}：${msg}` })
        } finally {
          syncingTripId.value = null
        }
      },
    })
  } catch (error: any) {
    syncingTripId.value = null
    const msg = error?.response?.data?.message || '获取预览失败'
    toast.add({ severity: 'error', summary: msg, life: 3500 })
    notifications.add({ severity: 'error', title: '行程同步预览失败', detail: `${trip.name}：${msg}` })
  }
}

const confirmDeleteTrip = async (trip: Trip) => {
  deletingTripId.value = trip.id
  try {
    const res = await tripsApi.cascadePreview(trip.id)
    const preview = res.data.data as TripCascadePreview | undefined
    deletingTripId.value = null
    if (!preview) return

    confirm.require({
      header: '删除行程确认',
      message: `将删除 ${preview.payments} 条支付记录，并删除/解绑关联发票（将被删除的发票：${preview.unlinked_only} 张）。此操作不可恢复，继续吗？`,
      icon: 'pi pi-exclamation-triangle',
      acceptLabel: '删除',
      rejectLabel: '取消',
      acceptClass: 'p-button-danger',
      accept: async () => {
        deletingTripId.value = trip.id
        try {
          await tripsApi.deleteCascade(trip.id)
          toast.add({ severity: 'success', summary: '行程已删除', life: 2200 })
          notifications.add({
            severity: 'warn',
            title: '行程已删除',
            detail: `${trip.name}：删除支付 ${preview.payments} 条；删除发票 ${preview.unlinked_only} 张`,
          })
          delete tripPayments[trip.id]
          delete summaries[trip.id]
          await reloadAll()
        } catch (e: any) {
          const msg = e?.response?.data?.message || '删除失败'
          toast.add({ severity: 'error', summary: msg, life: 3500 })
          notifications.add({ severity: 'error', title: '行程删除失败', detail: `${trip.name}：${msg}` })
        } finally {
          deletingTripId.value = null
        }
      },
    })
  } catch (error: any) {
    deletingTripId.value = null
    const msg = error?.response?.data?.message || '获取删除预览失败'
    toast.add({ severity: 'error', summary: msg, life: 3500 })
    notifications.add({ severity: 'error', title: '行程删除预览失败', detail: `${trip.name}：${msg}` })
  }
}

const unassignPayment = (paymentId: string) => {
  confirm.require({
    header: '移出行程',
    message: '确定将该支付记录移出当前行程吗？',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '移出',
    rejectLabel: '取消',
    accept: async () => {
      try {
        await paymentApi.update(paymentId, { trip_id: '' })
        toast.add({ severity: 'success', summary: '已移出行程', life: 2000 })
        notifications.add({ severity: 'info', title: '支付记录已移出行程', detail: paymentId })
        await reloadAll()
      } catch (e: any) {
        const msg = e?.response?.data?.message || '操作失败'
        toast.add({ severity: 'error', summary: msg, life: 3500 })
        notifications.add({ severity: 'error', title: '支付记录移出行程失败', detail: msg })
      }
    },
  })
}

const openMovePayment = (paymentId: string, fromTripId: string) => {
  movePaymentId.value = paymentId
  movePaymentFromTripId.value = fromTripId
  movePaymentTargetTripId.value = null
  movePaymentModalVisible.value = true
}

const closeMovePayment = () => {
  movePaymentModalVisible.value = false
  movePaymentId.value = null
  movePaymentFromTripId.value = null
  movePaymentTargetTripId.value = null
}

const moveTargetOptions = computed(() => {
  const from = movePaymentFromTripId.value
  return trips.value
    .filter((t) => t.id !== from)
    .map((t) => ({ label: t.name, value: t.id }))
})

const confirmMovePayment = async () => {
  if (!movePaymentId.value || !movePaymentTargetTripId.value) return
  movingPayment.value = true
  try {
    await paymentApi.update(movePaymentId.value, { trip_id: movePaymentTargetTripId.value })
    toast.add({ severity: 'success', summary: '已移动', life: 2000 })
    notifications.add({
      severity: 'info',
      title: '支付记录已移动到行程',
      detail: `${movePaymentId.value} → ${tripNameById.value[movePaymentTargetTripId.value] || movePaymentTargetTripId.value}`,
    })
    closeMovePayment()
    await reloadAll()
  } catch (e: any) {
    const msg = e?.response?.data?.message || '移动失败'
    toast.add({ severity: 'error', summary: msg, life: 3500 })
    notifications.add({ severity: 'error', title: '支付记录移动失败', detail: msg })
  } finally {
    movingPayment.value = false
  }
}

// Calendar
const calendarSelectedDate = ref<Date>(new Date())
const calendarMonth = ref<{ year: number; month: number }>({ year: dayjs().year(), month: dayjs().month() })
const calendarMonthPayments = ref<Payment[]>([])
const calendarTripFilter = ref<string | null>(null)
const calendarMonthBase = ref<'zero' | 'one' | null>(null)

const calendarTripOptions = computed(() => [
  { label: '全部支付', value: null },
  ...trips.value.map((t) => ({ label: t.name, value: t.id })),
])

const normalizePrimeMonthIndex = (month: number) => {
  if (calendarMonthBase.value === 'zero') return month
  if (calendarMonthBase.value === 'one') return Math.max(0, month - 1)

  // Auto-detect base from the first observed value.
  if (month === 0) {
    calendarMonthBase.value = 'zero'
    return month
  }
  if (month === 12) {
    calendarMonthBase.value = 'one'
    return 11
  }

  const nowMonth0 = dayjs().month()
  if (month === nowMonth0) {
    calendarMonthBase.value = 'zero'
    return month
  }
  if (month === nowMonth0 + 1) {
    calendarMonthBase.value = 'one'
    return month - 1
  }

  calendarMonthBase.value = 'zero'
  return month
}

const handleCalendarMonthChange = (e: { month: number; year: number }) => {
  calendarMonth.value = { year: e.year, month: normalizePrimeMonthIndex(e.month) }
  refreshCalendarMonth()
}

const refreshCalendarMonth = async () => {
  const { year, month } = calendarMonth.value
  const start = dayjs().year(year).month(month).startOf('month').toISOString()
  const end = dayjs().year(year).month(month).endOf('month').toISOString()
  const res = await paymentApi.getAll({ startDate: start, endDate: end, limit: 2000 })
  calendarMonthPayments.value = res.data.data || []
}

const goToThisMonth = () => {
  calendarSelectedDate.value = new Date()
  calendarMonth.value = { year: dayjs().year(), month: dayjs().month() }
  refreshCalendarMonth()
}

const calendarFilteredPayments = computed(() => {
  if (!calendarTripFilter.value) return calendarMonthPayments.value
  return calendarMonthPayments.value.filter((p) => p.trip_id === calendarTripFilter.value)
})

const dailyTotals = computed<Record<string, number>>(() => {
  const totals: Record<string, number> = {}
  for (const p of calendarFilteredPayments.value) {
    const key = p.transaction_time ? dayjs(p.transaction_time).format('YYYY-MM-DD') : ''
    if (!key) continue
    totals[key] = (totals[key] || 0) + Number(p.amount || 0)
  }
  return totals
})

const getCalendarDayTotal = (slotDate: any) => {
  // slotDate: {day, month, year, today, selectable}
  const monthIndex = normalizePrimeMonthIndex(slotDate.month)
  const key = dayjs().year(slotDate.year).month(monthIndex).date(slotDate.day).format('YYYY-MM-DD')
  return dailyTotals.value[key] || 0
}

const calendarSelectedKey = computed(() => dayjs(calendarSelectedDate.value).format('YYYY-MM-DD'))

const calendarSelectedPayments = computed(() => {
  return calendarFilteredPayments.value
    .filter((p) => dayjs(p.transaction_time).format('YYYY-MM-DD') === calendarSelectedKey.value)
    .sort((a, b) => (a.transaction_time < b.transaction_time ? 1 : -1))
})

const calendarSelectedTotal = computed(() => dailyTotals.value[calendarSelectedKey.value] || 0)

const calendarRightTitle = computed(() => {
  const base = dayjs(calendarSelectedDate.value).format('YYYY-MM-DD')
  const trip = calendarTripFilter.value ? tripNameById.value[calendarTripFilter.value] : ''
  return trip ? `${base} · ${trip}` : base
})

onMounted(async () => {
  await reloadAll()
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
  gap: 12px;
  flex-wrap: wrap;
}

.toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
}

.empty {
  padding: 28px 14px;
  text-align: center;
  display: grid;
  gap: 8px;
  justify-items: center;
}

.empty-title {
  font-weight: 800;
  font-size: 18px;
  color: var(--p-text-color);
}

.empty-sub {
  color: var(--p-text-muted-color);
  max-width: 560px;
}

.trip-accordion :deep(.p-accordion-header-link) {
  gap: 10px;
}

.trip-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-width: 0;
}

.trip-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.trip-name {
  font-weight: 900;
  color: var(--p-text-color);
}

.trip-range {
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.trip-badges {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.trip-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin: 4px 0 12px;
}

.trip-actions-left {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.muted {
  color: var(--p-text-muted-color);
}

.amount {
  font-weight: 800;
  color: var(--p-text-color);
}

.invoice-chips {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  align-items: center;
}

.invoice-chip {
  max-width: 240px;
}

.row-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.calendar-layout {
  display: grid;
  grid-template-columns: 360px 1fr;
  gap: 14px;
  align-items: start;
}

.calendar-left {
  border-radius: 16px;
  padding: 12px;
}

.calendar-toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.calendar-dropdown {
  width: 100%;
  max-width: 220px;
}

.calendar-right .panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.date-cell {
  display: grid;
  gap: 2px;
  justify-items: center;
}

.date-day {
  font-weight: 800;
  font-size: 12px;
  opacity: 0.95;
}

.date-cell.is-today {
  border-radius: 10px;
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--p-primary-color), transparent 30%);
}

.date-total {
  font-size: 10px;
  color: var(--p-text-muted-color);
  line-height: 1;
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
  color: var(--p-text-muted-color);
  line-height: 1.6;
  padding-left: 4px;
}

.field :deep(.p-inputtext),
.field :deep(.p-inputnumber),
.field :deep(.p-datepicker),
.field :deep(.p-textarea),
.field :deep(.p-inputtextarea),
.field :deep(.p-dropdown) {
  width: 100%;
}

.field :deep(.p-datepicker-input) {
  width: 100%;
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}

@media (max-width: 980px) {
  .calendar-layout {
    grid-template-columns: 1fr;
  }
}
</style>
