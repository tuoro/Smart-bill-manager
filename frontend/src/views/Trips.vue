<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>行程日历</span>
          <div class="toolbar">
            <Button
              label="刷新"
              icon="pi pi-refresh"
              class="p-button-outlined"
              @click="reloadAll"
            />
            <Button
              label="新增行程"
              icon="pi pi-plus"
              @click="openTripModal()"
            />
          </div>
        </div>
      </template>
      <template #content>
        <Tabs v-model:value="activeTab">
          <TabList>
            <Tab value="trips">按行程</Tab>
            <Tab value="pending">待分配</Tab>
            <Tab value="calendar">日历</Tab>
          </TabList>

          <TabPanels>
            <TabPanel value="trips">
              <div v-if="trips.length === 0" class="empty">
                <div class="empty-title">暂无行程</div>
                <div class="empty-sub">
                  创建一个行程后，系统会自动归属该时间段的支付记录；如遇行程重叠会进入“待分配”。
                </div>
                <Button
                  label="新增行程"
                  icon="pi pi-plus"
                  @click="openTripModal()"
                />
              </div>

              <div v-else class="trip-list">
                <div class="trip-list-toolbar">
                  <small class="muted">共 {{ tripListTotal }} 个行程</small>
                </div>

                <Accordion
                  :multiple="true"
                  class="trip-accordion"
                  @tab-open="handleTripOpen"
                  @tab-close="handleTripClose"
                >
                  <AccordionTab v-for="trip in pagedTrips" :key="trip.id">
                    <template #header>
                      <div class="trip-header">
                        <div class="trip-title">
                          <div
                            class="trip-name sbm-ellipsis"
                            :title="trip.name"
                          >
                            {{ trip.name }}
                          </div>
                          <div
                            class="trip-range sbm-ellipsis"
                            :title="`${formatDateTime(trip.start_time)} ~ ${formatDateTime(trip.end_time)}`"
                          >
                            {{ formatDateTime(trip.start_time) }} ~
                            {{ formatDateTime(trip.end_time) }}
                          </div>
                        </div>
                        <div class="trip-right">
                          <div class="trip-badges">
                            <Tag
                              v-if="trip.bad_debt_locked"
                              value="坏账锁定"
                              class="sbm-lock-tag"
                            />
                            <Tag
                              :value="
                                trip.reimburse_status === 'reimbursed'
                                  ? '已报销'
                                  : '未报销'
                              "
                              :severity="
                                trip.reimburse_status === 'reimbursed'
                                  ? 'success'
                                  : 'secondary'
                              "
                            />
                            <Tag
                              :value="
                                formatMoney(
                                  summaries[trip.id]?.total_amount || 0,
                                )
                              "
                              severity="success"
                            />
                            <Tag
                              :value="`支付 ${summaries[trip.id]?.payment_count || 0}`"
                              severity="info"
                            />
                            <Tag
                              :value="`发票 ${summaries[trip.id]?.linked_invoices || 0}`"
                              severity="secondary"
                            />
                            <Tag
                              v-if="
                                (summaries[trip.id]?.unlinked_payments || 0) > 0
                              "
                              :value="`未关联 ${summaries[trip.id]?.unlinked_payments || 0}`"
                              severity="warning"
                            />
                          </div>
                          <div class="trip-toggle">
                            <span class="trip-toggle-text">{{
                              openTripIds.has(trip.id) ? "收起" : "展开"
                            }}</span>
                            <i
                              class="pi"
                              :class="
                                openTripIds.has(trip.id)
                                  ? 'pi-chevron-up'
                                  : 'pi-chevron-down'
                              "
                            />
                          </div>
                        </div>
                      </div>
                    </template>

                    <div class="trip-actions">
                      <div class="trip-actions-left">
                        <Button
                          label="编辑行程"
                          icon="pi pi-pencil"
                          class="p-button-text"
                          @click="openTripModal(trip)"
                        />
                        <Button
                          label="删除行程"
                          icon="pi pi-trash"
                          class="p-button-text p-button-danger"
                          :loading="deletingTripId === trip.id"
                          @click="confirmDeleteTrip(trip)"
                        />
                        <small class="trip-actions-hint muted"
                          >删除行程会删除“归属到该行程”的支付记录，并按规则删除/解绑关联发票</small
                        >
                      </div>
                    </div>

                    <div class="sbm-dt-hscroll">
                      <DataTable
                        :value="tripPayments[trip.id] || []"
                        :loading="loadingPaymentsTripId === trip.id"
                        responsiveLayout="scroll"
                        :paginator="true"
                        :rows="10"
                        :rowsPerPageOptions="[10, 20, 50]"
                        sortField="transaction_time"
                        :sortOrder="-1"
                        class="trip-table"
                        :pt="tableScrollPt"
                        :tableStyle="tripTableStyle"
                      >
                        <Column
                          field="amount"
                          header="金额"
                          :style="{ width: '120px' }"
                          sortable
                        >
                          <template #body="{ data: row }">
                            <span class="amount">{{
                              formatMoney(row.amount)
                            }}</span>
                          </template>
                        </Column>
                        <Column header="商家">
                          <template #body="{ data: row }">
                            <span
                              class="sbm-ellipsis"
                              :title="row.merchant || '-'"
                              >{{ row.merchant || "-" }}</span
                            >
                          </template>
                        </Column>
                        <Column header="支付方式" :style="{ width: '180px' }">
                          <template #body="{ data: row }">
                            <span
                              class="sbm-ellipsis"
                              :title="row.payment_method || '-'"
                              >{{ row.payment_method || "-" }}</span
                            >
                          </template>
                        </Column>
                        <Column
                          field="transaction_time"
                          header="支付时间"
                          :style="{ width: '180px' }"
                          sortable
                        >
                          <template #body="{ data: row }">
                            {{ formatDateTime(row.transaction_time) }}
                          </template>
                        </Column>
                        <Column header="坏账" :style="{ width: '84px' }">
                          <template #body="{ data: row }">
                            <Button
                              size="small"
                              class="p-button-text"
                              :severity="row.bad_debt ? 'danger' : 'secondary'"
                              :icon="
                                row.bad_debt ? 'pi pi-lock' : 'pi pi-lock-open'
                              "
                              :title="
                                row.bad_debt ? '已标记坏账' : '标记为坏账'
                              "
                              aria-label="坏账"
                              @click="togglePaymentBadDebt(trip, row)"
                            />
                          </template>
                        </Column>
                        <Column header="关联发票">
                          <template #body="{ data: row }">
                            <div class="invoice-chips">
                              <button
                                v-for="inv in row.invoices || []"
                                :key="inv.id"
                                type="button"
                                class="invoice-chip-btn"
                                :title="
                                  inv.bad_debt ? '点击取消坏账' : '点击标记坏账'
                                "
                                @click="toggleInvoiceBadDebt(trip, inv)"
                              >
                                <Tag
                                  class="invoice-chip"
                                  :class="{
                                    'invoice-chip--baddebt': inv.bad_debt,
                                  }"
                                  :severity="
                                    inv.bad_debt ? 'danger' : 'secondary'
                                  "
                                  :value="
                                    inv.invoice_number ||
                                    inv.seller_name ||
                                    inv.id
                                  "
                                  :title="
                                    inv.invoice_number ||
                                    inv.seller_name ||
                                    inv.id
                                  "
                                />
                              </button>
                              <span
                                v-if="
                                  !row.invoices || row.invoices.length === 0
                                "
                                class="muted"
                                >-</span
                              >
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
                    </div>
                  </AccordionTab>
                </Accordion>

                <Paginator
                  :first="tripListFirst"
                  :rows="tripListRows"
                  :totalRecords="tripListTotal"
                  :rowsPerPageOptions="[5, 10, 20, 50]"
                  @page="onTripListPage"
                />
              </div>
            </TabPanel>

            <TabPanel value="pending">
              <div class="pending-panel">
                <div class="pending-header">
                  <Button
                    label="刷新"
                    icon="pi pi-refresh"
                    class="p-button-outlined"
                    @click="loadPendingPayments"
                  />
                </div>

                <div v-if="pendingPayments.length === 0" class="empty">
                  <div class="empty-title">暂无待分配</div>
                  <div class="empty-sub">
                    当支付时间同时命中多个行程，或你手动将支付记录从行程中移除时，会出现在这里等待你选择归属。
                  </div>
                </div>

                <div v-else class="sbm-dt-hscroll">
                  <DataTable
                    :value="pendingPayments"
                    responsiveLayout="scroll"
                    :paginator="true"
                    :rows="10"
                    :rowsPerPageOptions="[10, 20, 50]"
                    class="pending-table"
                    :pt="tableScrollPt"
                    :tableStyle="tripTableStyle"
                  >
                    <Column header="金额" :style="{ width: '120px' }">
                      <template #body="{ data: row }">
                        <span class="amount">{{
                          formatMoney(row.payment.amount)
                        }}</span>
                      </template>
                    </Column>
                    <Column header="商家">
                      <template #body="{ data: row }">
                        <span
                          class="sbm-ellipsis"
                          :title="row.payment.merchant || '-'"
                          >{{ row.payment.merchant || "-" }}</span
                        >
                      </template>
                    </Column>
                    <Column header="支付时间" :style="{ width: '180px' }">
                      <template #body="{ data: row }">{{
                        formatDateTime(row.payment.transaction_time)
                      }}</template>
                    </Column>
                    <Column header="候选行程" :style="{ width: '320px' }">
                      <template #body="{ data: row }">
                        <Dropdown
                          v-model="pendingSelection[row.payment.id]"
                          :options="
                            row.candidates.map((t: any) => ({
                              label: `${t.name} · ${formatDateTime(t.start_time)}~${formatDateTime(t.end_time)}`,
                              value: t.id,
                            }))
                          "
                          optionLabel="label"
                          optionValue="value"
                          placeholder="请选择归属行程"
                          filter
                          class="pending-dropdown"
                        />
                      </template>
                    </Column>
                    <Column header="操作" :style="{ width: '220px' }">
                      <template #body="{ data: row }">
                        <div class="row-actions">
                          <Button
                            size="small"
                            icon="pi pi-check"
                            label="归属"
                            :disabled="!pendingSelection[row.payment.id]"
                            :loading="pendingWorkingId === row.payment.id"
                            @click="assignPending(row.payment.id)"
                          />
                          <Button
                            size="small"
                            class="p-button-outlined"
                            severity="secondary"
                            icon="pi pi-ban"
                            label="保持无归属"
                            :loading="pendingWorkingId === row.payment.id"
                            @click="blockPending(row.payment.id)"
                          />
                        </div>
                      </template>
                    </Column>
                  </DataTable>
                </div>
              </div>
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
                    <Button
                      label="本月"
                      icon="pi pi-calendar"
                      class="p-button-outlined"
                      @click="goToThisMonth"
                    />
                  </div>
                  <DatePicker
                    :key="calendarPickerKey"
                    v-model="calendarSelectedDate"
                    inline
                    :manualInput="false"
                    :minDate="calendarMinDate"
                    :maxDate="calendarMaxDate"
                    @month-change="handleCalendarMonthChange"
                    @year-change="handleCalendarMonthChange"
                  >
                    <template #date="{ date }">
                      <div
                        class="date-cell"
                        :class="calendarDateCellClass(date)"
                      >
                        <div class="date-day">{{ date.day }}</div>
                      </div>
                    </template>
                  </DatePicker>
                </div>

                <div class="calendar-right">
                  <Card class="sbm-surface">
                    <template #title>
                      <div class="panel-title">
                        <div class="panel-title-text">
                          <span>{{ calendarRightTitle }}</span>
                          <small
                            v-if="calendarRightRangeLabel"
                            class="panel-title-sub"
                            >{{ calendarRightRangeLabel }}</small
                          >
                        </div>
                      </div>
                    </template>
                    <template #content>
                      <div class="sbm-dt-hscroll">
                        <DataTable
                          :value="calendarDisplayPayments"
                          responsiveLayout="scroll"
                          :paginator="true"
                          :rows="10"
                          :rowsPerPageOptions="[10, 20, 50]"
                          class="calendar-table"
                          :pt="tableScrollPt"
                          :tableStyle="calendarTableStyle"
                        >
                          <Column
                            field="amount"
                            header="金额"
                            :style="{
                              width: calendarTripFilter ? '18%' : '14%',
                            }"
                          >
                            <template #body="{ data: row }">
                              <span class="amount">{{
                                formatMoney(row.amount)
                              }}</span>
                            </template>
                          </Column>
                          <Column
                            header="商家"
                            :style="{
                              width: calendarTripFilter ? '42%' : '34%',
                            }"
                          >
                            <template #body="{ data: row }">
                              <span
                                class="sbm-ellipsis"
                                :title="row.merchant || '-'"
                                >{{ row.merchant || "-" }}</span
                              >
                            </template>
                          </Column>
                          <Column
                            field="transaction_time"
                            header="时间"
                            :style="{
                              width: calendarTripFilter ? '40%' : '26%',
                            }"
                          >
                            <template #body="{ data: row }">{{
                              formatDateTime(row.transaction_time)
                            }}</template>
                          </Column>
                          <Column
                            v-if="!calendarTripFilter"
                            header="行程"
                            :style="{ width: '26%' }"
                          >
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
                      </div>
                      <small class="muted"
                        >日历视图按支付时间统计；关联发票请在“按行程”中查看。</small
                      >
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
            <small v-if="tripErrors.name" class="p-error">{{
              tripErrors.name
            }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_start">开始时间</label>
            <DatePicker
              id="trip_start"
              ref="tripStartPicker"
              v-model="tripForm.start"
              showTime
              showSeconds
              :manualInput="false"
              @show="() => onPickerShow(tripStartPicker)"
              @hide="() => onPickerHide(tripStartPicker)"
            >
              <template #footer>
                <div class="dp-footer">
                  <Button
                    type="button"
                    size="small"
                    label="确定"
                    @click="closeDatePicker(tripStartPicker)"
                  />
                </div>
              </template>
            </DatePicker>
            <small v-if="tripErrors.start" class="p-error">{{
              tripErrors.start
            }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_end">结束时间</label>
            <DatePicker
              id="trip_end"
              ref="tripEndPicker"
              v-model="tripForm.end"
              showTime
              showSeconds
              :manualInput="false"
              @show="() => onPickerShow(tripEndPicker)"
              @hide="() => onPickerHide(tripEndPicker)"
            >
              <template #footer>
                <div class="dp-footer">
                  <Button
                    type="button"
                    size="small"
                    label="确定"
                    @click="closeDatePicker(tripEndPicker)"
                  />
                </div>
              </template>
            </DatePicker>
            <small v-if="tripErrors.end" class="p-error">{{
              tripErrors.end
            }}</small>
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_reimburse_status">报销状态</label>
            <Dropdown
              id="trip_reimburse_status"
              v-model="tripForm.reimburse_status"
              :options="reimburseStatusOptions"
              optionLabel="label"
              optionValue="value"
              placeholder="请选择"
            />
          </div>
          <div class="col-12 md:col-6 field">
            <label for="trip_timezone">行程所属地/时区</label>
            <Dropdown
              id="trip_timezone"
              v-model="tripForm.timezone"
              :options="timezoneOptions"
              optionLabel="label"
              optionValue="value"
              filter
              :virtualScrollerOptions="{ itemSize: 38 }"
              placeholder="请选择时区"
            />
          </div>
          <div class="col-12 field">
            <label for="trip_note">备注（可选）</label>
            <Textarea
              id="trip_note"
              v-model.trim="tripForm.note"
              autoResize
              rows="2"
            />
          </div>
        </div>

        <div class="footer">
          <Button
            type="button"
            class="p-button-outlined"
            severity="secondary"
            label="取消"
            @click="tripModalVisible = false"
          />
          <Button
            type="submit"
            label="保存"
            icon="pi pi-check"
            :loading="savingTrip"
          />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="movePaymentModalVisible"
      modal
      header="移动支付记录"
      :style="{ width: '560px', maxWidth: '92vw' }"
    >
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
        <Button
          type="button"
          class="p-button-outlined"
          severity="secondary"
          label="取消"
          @click="closeMovePayment"
        />
        <Button
          type="button"
          label="移动"
          icon="pi pi-check"
          :disabled="!movePaymentTargetTripId"
          :loading="movingPayment"
          @click="confirmMovePayment"
        />
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import Accordion from "primevue/accordion";
import AccordionTab from "primevue/accordiontab";
import Button from "primevue/button";
import Card from "primevue/card";
import Column from "primevue/column";
import DataTable from "primevue/datatable";
import DatePicker from "primevue/datepicker";
import Dialog from "primevue/dialog";
import Dropdown from "primevue/dropdown";
import Paginator from "primevue/paginator";
import InputText from "primevue/inputtext";
import Textarea from "primevue/textarea";
import Tab from "primevue/tab";
import TabList from "primevue/tablist";
import TabPanel from "primevue/tabpanel";
import TabPanels from "primevue/tabpanels";
import Tabs from "primevue/tabs";
import Tag from "primevue/tag";
import { useConfirm } from "primevue/useconfirm";
import { useToast } from "primevue/usetoast";
import dayjs from "dayjs";
import { Temporal } from "@js-temporal/polyfill";
import { invoiceApi, paymentApi, tripsApi } from "@/api";
import type {
  Payment,
  PendingPayment,
  Trip,
  TripCascadePreview,
  TripPaymentInvoice,
  TripPaymentWithInvoices,
  TripSummary,
} from "@/types";
import { useNotificationStore } from "@/stores/notifications";

const tableScrollPt = {
  root: {
    style: {
      maxWidth: "100%",
      minWidth: "0",
    },
  },
  tableContainer: {
    style: {
      width: "100%",
      maxWidth: "100%",
      minWidth: "0",
      overflowX: "auto",
      WebkitOverflowScrolling: "touch",
    },
  },
} as const;

const tripTableStyle = {
  minWidth: "960px",
} as const;

const calendarTableStyle = {
  width: "100%",
  minWidth: "0",
} as const;

const toast = useToast();
const confirm = useConfirm();
const notifications = useNotificationStore();

const activeTab = ref<"trips" | "pending" | "calendar">("trips");

const tripStartPicker = ref<any>(null);
const tripEndPicker = ref<any>(null);

const closeDatePicker = (pickerRef: { value: any } | any) => {
  const inst = pickerRef?.value ?? pickerRef;
  if (!inst) return;
  inst.overlayVisible = false;
};

const OPEN_PICKERS = new Set<any>();
let viewportListenerAttached = false;
let viewportRaf = 0;

const resetOverlayStyle = (inst: any) => {
  const overlay = inst?.overlay as HTMLElement | undefined;
  if (!overlay) return;
  overlay.style.transform = "";
  overlay.style.transformOrigin = "";
  overlay.style.maxHeight = "";
  overlay.style.maxWidth = "";
  overlay.style.overflow = "";
};

const repositionPickerBelow = (inst: any) => {
  if (typeof window === "undefined") return;
  if (!inst?.overlayVisible) return;

  const overlay = inst?.overlay as HTMLElement | undefined;
  const root = inst?.$el as HTMLElement | undefined;
  if (!overlay || !root) return;

  const input = (root.querySelector("input") as HTMLElement | null) || root;
  const targetRect = input.getBoundingClientRect();

  const scrollX = window.scrollX || document.documentElement.scrollLeft || 0;
  const scrollY = window.scrollY || document.documentElement.scrollTop || 0;
  const viewportW =
    window.innerWidth || document.documentElement.clientWidth || 0;
  const viewportH =
    window.innerHeight || document.documentElement.clientHeight || 0;

  // Reset any previous scaling before measuring.
  overlay.style.transform = "";
  overlay.style.transformOrigin = "";
  overlay.style.maxHeight = "";
  overlay.style.maxWidth = "";
  overlay.style.overflow = "";

  const naturalRect = overlay.getBoundingClientRect();
  const naturalW = naturalRect.width || overlay.offsetWidth || 1;
  const naturalH = naturalRect.height || overlay.offsetHeight || 1;

  const margin = 8;
  const belowGap = 6;
  const availableW = Math.max(0, viewportW - margin * 2);
  const availableH = Math.max(
    0,
    viewportH - targetRect.bottom - belowGap - margin,
  );

  const scaleW = availableW > 0 ? availableW / naturalW : 1;
  const scaleH = availableH > 0 ? availableH / naturalH : 1;
  let scale = Math.min(1, scaleW, scaleH);

  const minScale = 0.75;
  if (scale < minScale) scale = minScale;

  overlay.style.transformOrigin = "top left";
  overlay.style.transform = scale < 1 ? `scale(${scale})` : "";

  const scaledW = naturalW * scale;
  const minLeft = scrollX + margin;
  const maxLeft = scrollX + viewportW - scaledW - margin;
  const left = Math.max(minLeft, Math.min(targetRect.left + scrollX, maxLeft));
  const top = targetRect.bottom + scrollY + belowGap;

  // If even after scaling the overlay is taller than available space, cap height and allow scrolling (still below).
  if (availableH > 0 && naturalH * scale > availableH) {
    overlay.style.maxHeight = `${Math.max(220, Math.floor(availableH / scale))}px`;
    overlay.style.overflow = "auto";
  }

  overlay.style.left = `${left}px`;
  overlay.style.top = `${top}px`;
  overlay.style.right = "auto";
  overlay.style.bottom = "auto";
};

const onViewportChange = () => {
  if (typeof window === "undefined") return;
  if (viewportRaf) return;
  viewportRaf = window.requestAnimationFrame(() => {
    viewportRaf = 0;
    for (const inst of OPEN_PICKERS) repositionPickerBelow(inst);
  });
};

const attachViewportListeners = () => {
  if (typeof window === "undefined") return;
  if (viewportListenerAttached) return;
  viewportListenerAttached = true;
  window.addEventListener("resize", onViewportChange, { passive: true });
  // capture=true so it also reacts when dialog scroll container scrolls
  window.addEventListener("scroll", onViewportChange, {
    passive: true,
    capture: true,
  } as any);
};

const detachViewportListeners = () => {
  if (typeof window === "undefined") return;
  if (!viewportListenerAttached) return;
  if (OPEN_PICKERS.size > 0) return;
  viewportListenerAttached = false;
  window.removeEventListener("resize", onViewportChange as any);
  window.removeEventListener("scroll", onViewportChange as any, true as any);
};

const onPickerShow = async (pickerRef: { value: any } | any) => {
  const inst = pickerRef?.value ?? pickerRef;
  if (!inst) return;
  OPEN_PICKERS.add(inst);
  attachViewportListeners();
  await nextTick();
  repositionPickerBelow(inst);
  if (
    typeof window !== "undefined" &&
    typeof window.requestAnimationFrame === "function"
  ) {
    window.requestAnimationFrame(() => repositionPickerBelow(inst));
  }
};

const onPickerHide = (pickerRef: { value: any } | any) => {
  const inst = pickerRef?.value ?? pickerRef;
  if (!inst) return;
  OPEN_PICKERS.delete(inst);
  resetOverlayStyle(inst);
  detachViewportListeners();
};

const trips = ref<Trip[]>([]);
const tripListFirst = ref(0);
const tripListRows = ref(10);
const tripListTotal = computed(() => trips.value.length);
const pagedTrips = computed(() =>
  trips.value.slice(
    tripListFirst.value,
    tripListFirst.value + tripListRows.value,
  ),
);
const onTripListPage = (e: { first: number; rows: number }) => {
  tripListFirst.value = e.first;
  tripListRows.value = e.rows;
};

const openTripIds = reactive(new Set<string>());

watch(
  () => trips.value.length,
  (n) => {
    if (n <= 0) {
      tripListFirst.value = 0;
      return;
    }
    if (tripListFirst.value >= n) tripListFirst.value = 0;
  },
);

watch(
  () => [tripListFirst.value, tripListRows.value],
  () => {
    // Accordion state resets when paging trips; keep the toggle label in sync.
    openTripIds.clear();
  },
);
const summaries = reactive<Record<string, TripSummary>>({});
const tripPayments = reactive<Record<string, TripPaymentWithInvoices[]>>({});

const loadingPaymentsTripId = ref<string | null>(null);
const deletingTripId = ref<string | null>(null);

const pendingPayments = ref<PendingPayment[]>([]);
const pendingSelection = reactive<Record<string, string | null>>({});
const pendingWorkingId = ref<string | null>(null);

const tripModalVisible = ref(false);
const savingTrip = ref(false);
const editingTrip = ref<Trip | null>(null);

const tripForm = reactive({
  name: "",
  start: null as Date | null,
  end: null as Date | null,
  reimburse_status: "unreimbursed" as "unreimbursed" | "reimbursed",
  timezone: "Asia/Shanghai",
  note: "",
});

const reimburseStatusOptions = [
  { label: "未报销", value: "unreimbursed" },
  { label: "已报销", value: "reimbursed" },
];

const AREA_CN: Record<string, string> = {
  Africa: "非洲",
  America: "美洲",
  Antarctica: "南极洲",
  Arctic: "北极",
  Asia: "亚洲",
  Atlantic: "大西洋",
  Australia: "澳洲",
  Europe: "欧洲",
  Indian: "印度洋",
  Pacific: "太平洋",
  Etc: "其他",
};

const tzLabel = (tz: string) => {
  const parts = String(tz || "").split("/");
  if (parts.length >= 2) {
    const area = AREA_CN[parts[0]] || parts[0];
    const rest = parts.slice(1).join("/").replace(/_/g, " ");
    return `${area}/${rest} (${tz})`;
  }
  return tz;
};

const timezoneOptions = computed(() => {
  let zones: string[] = [];
  const anyIntl = Intl as any;
  if (anyIntl?.supportedValuesOf) {
    try {
      zones = anyIntl.supportedValuesOf("timeZone") as string[];
    } catch {
      zones = [];
    }
  }
  if (!zones || zones.length === 0)
    zones = ["Asia/Shanghai", "Europe/Paris", "America/New_York"];
  return zones.map((z) => ({ label: tzLabel(z), value: z }));
});

const tripErrors = reactive({
  name: "",
  start: "",
  end: "",
});

const movePaymentModalVisible = ref(false);
const movePaymentId = ref<string | null>(null);
const movePaymentFromTripId = ref<string | null>(null);
const movePaymentTargetTripId = ref<string | null>(null);
const movingPayment = ref(false);

const tripNameById = computed<Record<string, string>>(() => {
  const m: Record<string, string> = {};
  for (const t of trips.value) m[t.id] = t.name;
  return m;
});

const formatDateTime = (date: string) =>
  dayjs(date).format("YYYY-MM-DD HH:mm:ss");
const formatMoney = (amount: number) => `¥${Number(amount || 0).toFixed(2)}`;

const validateTripForm = () => {
  tripErrors.name = "";
  tripErrors.start = "";
  tripErrors.end = "";
  if (!tripForm.name) tripErrors.name = "请输入行程名称";
  if (!tripForm.start) tripErrors.start = "请选择开始时间";
  if (!tripForm.end) tripErrors.end = "请选择结束时间";
  if (tripForm.start && tripForm.end && tripForm.end < tripForm.start)
    tripErrors.end = "结束时间不能早于开始时间";
  return !tripErrors.name && !tripErrors.start && !tripErrors.end;
};

const resetTripForm = () => {
  tripForm.name = "";
  tripForm.start = null;
  tripForm.end = null;
  tripForm.reimburse_status = "unreimbursed";
  tripForm.timezone = "Asia/Shanghai";
  tripForm.note = "";
  editingTrip.value = null;
  tripErrors.name = "";
  tripErrors.start = "";
  tripErrors.end = "";
};

const openTripModal = (trip?: Trip) => {
  resetTripForm();
  if (trip) {
    editingTrip.value = trip;
    tripForm.name = trip.name;
    tripForm.start = new Date(trip.start_time);
    tripForm.end = new Date(trip.end_time);
    tripForm.reimburse_status =
      trip.reimburse_status === "reimbursed" ? "reimbursed" : "unreimbursed";
    tripForm.timezone = trip.timezone || "Asia/Shanghai";
    tripForm.note = trip.note || "";
  }
  tripModalVisible.value = true;
};

const dateToParts = (d: Date) => ({
  year: d.getFullYear(),
  month: d.getMonth() + 1,
  day: d.getDate(),
  hour: d.getHours(),
  minute: d.getMinutes(),
  second: d.getSeconds(),
});

const isSameWallClock = (parts: ReturnType<typeof dateToParts>, zdt: any) => {
  const pdt = zdt.toPlainDateTime();
  return (
    pdt.year === parts.year &&
    pdt.month === parts.month &&
    pdt.day === parts.day &&
    pdt.hour === parts.hour &&
    pdt.minute === parts.minute &&
    pdt.second === parts.second
  );
};

const toUtcIsoWithDstConfirm = async (
  d: Date,
  timeZone: string,
  label: string,
): Promise<string> => {
  const parts = dateToParts(d);

  const tryMake = (disambiguation: "reject" | "earlier" | "later") =>
    Temporal.ZonedDateTime.from({ ...parts, timeZone }, {
      disambiguation,
    } as any);

  try {
    const zdt = tryMake("reject");
    const ms = Number(zdt.toInstant().epochMilliseconds);
    return new Date(ms).toISOString();
  } catch {
    // Distinguish ambiguous vs nonexistent by comparing wall-clock after disambiguation.
    const early = tryMake("earlier");
    const late = tryMake("later");
    const earlySame = isSameWallClock(parts, early);
    const lateSame = isSameWallClock(parts, late);

    if (!earlySame || !lateSame) {
      throw new Error(`${label}在所选时区不存在（夏令时切换），请重新选择`);
    }

    const earlyMs = Number(early.toInstant().epochMilliseconds);
    const lateMs = Number(late.toInstant().epochMilliseconds);
    if (earlyMs === lateMs) {
      return new Date(earlyMs).toISOString();
    }

    const earlyOffset = early.offset;
    const lateOffset = late.offset;

    return await new Promise<string>((resolve, reject) => {
      confirm.require({
        header: "夏令时时间冲突",
        message: `${label}在时区 ${timeZone} 存在两次，请选择：\n较早偏移 ${earlyOffset}\n较晚偏移 ${lateOffset}`,
        icon: "pi pi-exclamation-triangle",
        acceptLabel: `使用较早 (${earlyOffset})`,
        rejectLabel: `使用较晚 (${lateOffset})`,
        accept: () => resolve(new Date(earlyMs).toISOString()),
        reject: () => resolve(new Date(lateMs).toISOString()),
        onHide: () => reject(new Error("已取消选择")),
      });
    });
  }
};

const handleSaveTrip = async () => {
  if (!validateTripForm()) return;
  savingTrip.value = true;
  try {
    const startIso = await toUtcIsoWithDstConfirm(
      tripForm.start!,
      tripForm.timezone,
      "开始时间",
    );
    const endIso = await toUtcIsoWithDstConfirm(
      tripForm.end!,
      tripForm.timezone,
      "结束时间",
    );

    const payload = {
      name: tripForm.name,
      start_time: startIso,
      end_time: endIso,
      reimburse_status: tripForm.reimburse_status,
      timezone: tripForm.timezone,
      note: tripForm.note || undefined,
    };

    if (editingTrip.value) {
      const res = await tripsApi.update(editingTrip.value.id, payload);
      const changes = (res.data as any)?.data?.changes;
      toast.add({ severity: "success", summary: "行程已更新", life: 2000 });
      notifications.add({
        severity: "success",
        title: "行程已更新",
        detail: payload.name,
      });
      if (changes?.auto_unassigned) {
        notifications.add({
          severity: "warn",
          title: "行程重叠已打回待分配",
          detail: `自动打回 ${changes.auto_unassigned} 条；自动归属 ${changes.auto_assigned || 0} 条`,
        });
      } else if (changes?.auto_assigned) {
        notifications.add({
          severity: "info",
          title: "已自动归属支付记录",
          detail: `自动归属 ${changes.auto_assigned} 条`,
        });
      }
    } else {
      const res = await tripsApi.create(payload as any);
      const changes = (res.data as any)?.data?.changes;
      toast.add({ severity: "success", summary: "行程已创建", life: 2000 });
      notifications.add({
        severity: "success",
        title: "行程已创建",
        detail: payload.name,
      });
      if (changes?.auto_unassigned) {
        notifications.add({
          severity: "warn",
          title: "行程重叠已打回待分配",
          detail: `自动打回 ${changes.auto_unassigned} 条；自动归属 ${changes.auto_assigned || 0} 条`,
        });
      } else if (changes?.auto_assigned) {
        notifications.add({
          severity: "info",
          title: "已自动归属支付记录",
          detail: `自动归属 ${changes.auto_assigned} 条`,
        });
      }
    }

    tripModalVisible.value = false;
    await reloadAll();
  } catch (error: any) {
    const msg = error?.response?.data?.message || "保存失败";
    toast.add({ severity: "error", summary: msg, life: 3500 });
    notifications.add({
      severity: "error",
      title: "行程保存失败",
      detail: msg,
    });
  } finally {
    savingTrip.value = false;
  }
};

const loadTrips = async () => {
  const res = await tripsApi.list();
  trips.value = res.data.data || [];
};

const loadSummaries = async () => {
  const res = await tripsApi.getSummaries();
  const list = res.data.data || [];
  for (const k of Object.keys(summaries)) delete summaries[k];
  for (const s of list) summaries[s.trip_id] = s;
};

const loadTripPayments = async (tripId: string) => {
  loadingPaymentsTripId.value = tripId;
  try {
    const res = await tripsApi.getPayments(tripId, true);
    tripPayments[tripId] = res.data.data || [];
  } finally {
    loadingPaymentsTripId.value = null;
  }
};

const loadPendingPayments = async () => {
  const res = await tripsApi.pendingPayments();
  pendingPayments.value = res.data.data || [];

  // Reset selections for current list.
  const next: Record<string, string | null> = {};
  for (const row of pendingPayments.value) {
    next[row.payment.id] = pendingSelection[row.payment.id] || null;
  }
  for (const k of Object.keys(pendingSelection)) delete pendingSelection[k];
  for (const [k, v] of Object.entries(next)) pendingSelection[k] = v;
};

const assignPending = async (paymentId: string) => {
  const tripId = pendingSelection[paymentId];
  if (!tripId) return;
  pendingWorkingId.value = paymentId;
  try {
    await tripsApi.assignPendingPayment(paymentId, tripId);
    toast.add({ severity: "success", summary: "已归属", life: 2000 });
    notifications.add({
      severity: "info",
      title: "待分配支付已归属",
      detail: `${paymentId} → ${tripNameById.value[tripId] || tripId}`,
    });
    await reloadAll();
  } catch (e: any) {
    const msg = e?.response?.data?.message || "归属失败";
    toast.add({ severity: "error", summary: msg, life: 3500 });
    notifications.add({ severity: "error", title: "归属失败", detail: msg });
  } finally {
    pendingWorkingId.value = null;
  }
};

const blockPending = (paymentId: string) => {
  confirm.require({
    header: "保持无归属",
    message: "确认将该支付记录保持无归属，并阻止后续自动归属吗？",
    icon: "pi pi-exclamation-triangle",
    acceptLabel: "确认",
    rejectLabel: "取消",
    accept: async () => {
      pendingWorkingId.value = paymentId;
      try {
        await tripsApi.blockPendingPayment(paymentId);
        toast.add({ severity: "success", summary: "已保持无归属", life: 2000 });
        notifications.add({
          severity: "info",
          title: "已保持无归属",
          detail: paymentId,
        });
        await reloadAll();
      } catch (e: any) {
        const msg = e?.response?.data?.message || "操作失败";
        toast.add({ severity: "error", summary: msg, life: 3500 });
        notifications.add({
          severity: "error",
          title: "操作失败",
          detail: msg,
        });
      } finally {
        pendingWorkingId.value = null;
      }
    },
  });
};

const handleTripOpen = (e: { index: number }) => {
  const trip = pagedTrips.value[e.index];
  if (!trip) return;
  openTripIds.add(trip.id);
  if (tripPayments[trip.id]) return;
  loadTripPayments(trip.id);
};

const handleTripClose = (e: { index: number }) => {
  const trip = pagedTrips.value[e.index];
  if (!trip) return;
  openTripIds.delete(trip.id);
};

const reloadAll = async () => {
  await loadTrips();
  await loadSummaries();
  // keep payments lazy; load on demand if already opened (simple: just refresh cache)
  for (const t of trips.value) {
    if (tripPayments[t.id]) await loadTripPayments(t.id);
  }
  await loadPendingPayments();
  await refreshCalendarMonth();
};

const confirmDeleteTrip = async (trip: Trip) => {
  deletingTripId.value = trip.id;
  try {
    const res = await tripsApi.cascadePreview(trip.id);
    const preview = res.data.data as TripCascadePreview | undefined;
    deletingTripId.value = null;
    if (!preview) return;

    confirm.require({
      header: "删除行程确认",
      message: `将删除 ${preview.payments} 条支付记录，并删除/解绑关联发票（将被删除的发票：${preview.unlinked_only} 张）。此操作不可恢复，继续吗？`,
      icon: "pi pi-exclamation-triangle",
      acceptLabel: "删除",
      rejectLabel: "取消",
      acceptClass: "p-button-danger",
      accept: async () => {
        deletingTripId.value = trip.id;
        try {
          await tripsApi.deleteCascade(trip.id);
          toast.add({ severity: "success", summary: "行程已删除", life: 2200 });
          notifications.add({
            severity: "warn",
            title: "行程已删除",
            detail: `${trip.name}：删除支付 ${preview.payments} 条；删除发票 ${preview.unlinked_only} 张`,
          });
          delete tripPayments[trip.id];
          delete summaries[trip.id];
          await reloadAll();
        } catch (e: any) {
          const msg = e?.response?.data?.message || "删除失败";
          toast.add({ severity: "error", summary: msg, life: 3500 });
          notifications.add({
            severity: "error",
            title: "行程删除失败",
            detail: `${trip.name}：${msg}`,
          });
        } finally {
          deletingTripId.value = null;
        }
      },
    });
  } catch (error: any) {
    deletingTripId.value = null;
    const msg = error?.response?.data?.message || "获取删除预览失败";
    toast.add({ severity: "error", summary: msg, life: 3500 });
    notifications.add({
      severity: "error",
      title: "行程删除预览失败",
      detail: `${trip.name}：${msg}`,
    });
  }
};

const togglePaymentBadDebt = (trip: Trip, row: TripPaymentWithInvoices) => {
  const next = !row.bad_debt;
  confirm.require({
    header: next ? "标记坏账" : "取消坏账",
    message: next
      ? "将该支付记录标记为坏账，并自动锁定该行程。继续？"
      : "取消该支付记录的坏账标记。继续？",
    icon: "pi pi-exclamation-triangle",
    acceptLabel: "确认",
    rejectLabel: "取消",
    accept: async () => {
      try {
        await paymentApi.update(row.id, { bad_debt: next });
        toast.add({
          severity: "success",
          summary: next ? "已标记坏账" : "已取消坏账",
          life: 2000,
        });
        notifications.add({
          severity: next ? "warn" : "info",
          title: next ? "支付记录已标记坏账" : "支付记录已取消坏账",
          detail: `${trip.name}：${row.id}`,
        });
        await reloadAll();
      } catch (e: any) {
        const msg = e?.response?.data?.message || "操作失败";
        toast.add({ severity: "error", summary: msg, life: 3500 });
        notifications.add({
          severity: "error",
          title: "坏账操作失败",
          detail: msg,
        });
      }
    },
  });
};

const toggleInvoiceBadDebt = (trip: Trip, inv: TripPaymentInvoice) => {
  const next = !inv.bad_debt;
  confirm.require({
    header: next ? "标记坏账" : "取消坏账",
    message: next
      ? "将该发票标记为坏账，并自动锁定关联行程。继续？"
      : "取消该发票的坏账标记。继续？",
    icon: "pi pi-exclamation-triangle",
    acceptLabel: "确认",
    rejectLabel: "取消",
    accept: async () => {
      try {
        await invoiceApi.update(inv.id, { bad_debt: next });
        toast.add({
          severity: "success",
          summary: next ? "已标记坏账" : "已取消坏账",
          life: 2000,
        });
        notifications.add({
          severity: next ? "warn" : "info",
          title: next ? "发票已标记坏账" : "发票已取消坏账",
          detail: `${trip.name}：${inv.invoice_number || inv.id}`,
        });
        await reloadAll();
      } catch (e: any) {
        const msg = e?.response?.data?.message || "操作失败";
        toast.add({ severity: "error", summary: msg, life: 3500 });
        notifications.add({
          severity: "error",
          title: "坏账操作失败",
          detail: msg,
        });
      }
    },
  });
};

const unassignPayment = (paymentId: string) => {
  confirm.require({
    header: "移出行程",
    message: "确定将该支付记录移出当前行程吗？",
    icon: "pi pi-exclamation-triangle",
    acceptLabel: "移出",
    rejectLabel: "取消",
    accept: async () => {
      try {
        await paymentApi.update(paymentId, {
          trip_id: "",
          // Move to "pending" for manual re-assignment instead of blocking it forever.
          trip_assignment_source: "manual",
        });
        toast.add({
          severity: "success",
          summary: "已移出行程，已进入待分配",
          life: 2200,
        });
        notifications.add({
          severity: "info",
          title: "支付记录已移出行程",
          detail: paymentId,
        });
        await reloadAll();
      } catch (e: any) {
        const msg = e?.response?.data?.message || "操作失败";
        toast.add({ severity: "error", summary: msg, life: 3500 });
        notifications.add({
          severity: "error",
          title: "支付记录移出行程失败",
          detail: msg,
        });
      }
    },
  });
};

const openMovePayment = (paymentId: string, fromTripId: string) => {
  movePaymentId.value = paymentId;
  movePaymentFromTripId.value = fromTripId;
  movePaymentTargetTripId.value = null;
  movePaymentModalVisible.value = true;
};

const closeMovePayment = () => {
  movePaymentModalVisible.value = false;
  movePaymentId.value = null;
  movePaymentFromTripId.value = null;
  movePaymentTargetTripId.value = null;
};

const moveTargetOptions = computed(() => {
  const from = movePaymentFromTripId.value;
  return trips.value
    .filter((t) => t.id !== from)
    .map((t) => ({ label: t.name, value: t.id }));
});

const confirmMovePayment = async () => {
  if (!movePaymentId.value || !movePaymentTargetTripId.value) return;
  movingPayment.value = true;
  try {
    await paymentApi.update(movePaymentId.value, {
      trip_id: movePaymentTargetTripId.value,
      trip_assignment_source: "manual",
    });
    toast.add({ severity: "success", summary: "已移动", life: 2000 });
    notifications.add({
      severity: "info",
      title: "支付记录已移动到行程",
      detail: `${movePaymentId.value} → ${tripNameById.value[movePaymentTargetTripId.value] || movePaymentTargetTripId.value}`,
    });
    closeMovePayment();
    await reloadAll();
  } catch (e: any) {
    const msg = e?.response?.data?.message || "移动失败";
    toast.add({ severity: "error", summary: msg, life: 3500 });
    notifications.add({
      severity: "error",
      title: "支付记录移动失败",
      detail: msg,
    });
  } finally {
    movingPayment.value = false;
  }
};

// Calendar
const calendarSelectedDate = ref<Date>(new Date());
const calendarMonth = ref<{ year: number; month: number }>({
  year: dayjs().year(),
  month: dayjs().month(),
});
const calendarMonthPayments = ref<Payment[]>([]);
const calendarTripFilter = ref<string | null>(null);
const calendarMonthBase = ref<"zero" | "one" | null>(null);
const calendarPickerKey = ref(0);

const calendarActiveTrip = computed(() => {
  const id = calendarTripFilter.value;
  if (!id) return null;
  return trips.value.find((t) => t.id === id) || null;
});

const calendarTripRange = computed(() => {
  const trip = calendarActiveTrip.value;
  if (!trip) return null;
  const start = dayjs(trip.start_time).startOf("day");
  const end = dayjs(trip.end_time).endOf("day");
  if (!start.isValid() || !end.isValid()) return null;
  return { start, end };
});

const calendarMinDate = computed(() => {
  const r = calendarTripRange.value;
  return r ? r.start.toDate() : undefined;
});

const calendarMaxDate = computed(() => {
  const r = calendarTripRange.value;
  return r ? r.end.toDate() : undefined;
});

const calendarTripOptions = computed(() => [
  { label: "全部行程", value: null },
  ...trips.value.map((t) => ({ label: t.name, value: t.id })),
]);

const normalizePrimeMonthIndex = (month: number) => {
  if (calendarMonthBase.value === "zero") return month;
  if (calendarMonthBase.value === "one") return Math.max(0, month - 1);

  // Auto-detect base from the first observed value.
  if (month === 0) {
    calendarMonthBase.value = "zero";
    return month;
  }
  if (month === 12) {
    calendarMonthBase.value = "one";
    return 11;
  }

  const nowMonth0 = dayjs().month();
  if (month === nowMonth0) {
    calendarMonthBase.value = "zero";
    return month;
  }
  if (month === nowMonth0 + 1) {
    calendarMonthBase.value = "one";
    return month - 1;
  }

  calendarMonthBase.value = "zero";
  return month;
};

const handleCalendarMonthChange = (e: { month: number; year: number }) => {
  calendarMonth.value = {
    year: e.year,
    month: normalizePrimeMonthIndex(e.month),
  };
  refreshCalendarMonth();
};

const refreshCalendarMonth = async () => {
  const { year, month } = calendarMonth.value;
  const start = dayjs().year(year).month(month).startOf("month").toISOString();
  const end = dayjs().year(year).month(month).endOf("month").toISOString();
  const res = await paymentApi.getAll({
    startDate: start,
    endDate: end,
    limit: 2000,
  });
  calendarMonthPayments.value = res.data.data || [];
};

const goToThisMonth = () => {
  calendarSelectedDate.value = new Date();
  calendarMonth.value = { year: dayjs().year(), month: dayjs().month() };
  refreshCalendarMonth();
};

const calendarFilteredPayments = computed(() => {
  if (!calendarTripFilter.value) return calendarMonthPayments.value;
  return calendarMonthPayments.value.filter(
    (p) => p.trip_id === calendarTripFilter.value,
  );
});

const calendarSlotToDay = (slotDate: any) => {
  const monthIndex = normalizePrimeMonthIndex(slotDate.month);
  return dayjs().year(slotDate.year).month(monthIndex).date(slotDate.day);
};

const calendarDateCellClass = (slotDate: any) => {
  const trip = calendarActiveTrip.value;
  const range = calendarTripRange.value;
  const d = calendarSlotToDay(slotDate);

  const inRange = !!(
    trip &&
    range &&
    d.isValid() &&
    d.valueOf() >= range.start.startOf("day").valueOf() &&
    d.valueOf() <= range.end.endOf("day").valueOf()
  );
  const isStart = !!(
    trip &&
    range &&
    inRange &&
    d.format("YYYY-MM-DD") === range.start.format("YYYY-MM-DD")
  );
  const isEnd = !!(
    trip &&
    range &&
    inRange &&
    d.format("YYYY-MM-DD") === range.end.format("YYYY-MM-DD")
  );

  return {
    "is-today": !!slotDate?.today,
    "is-in-trip": inRange,
    "is-trip-start": isStart,
    "is-trip-end": isEnd,
    "is-outside-trip": !!trip && !inRange,
  };
};

const calendarSelectedKey = computed(() =>
  dayjs(calendarSelectedDate.value).format("YYYY-MM-DD"),
);

const calendarSelectedPayments = computed(() => {
  return calendarFilteredPayments.value
    .filter(
      (p) =>
        dayjs(p.transaction_time).format("YYYY-MM-DD") ===
        calendarSelectedKey.value,
    )
    .sort((a, b) => (a.transaction_time < b.transaction_time ? 1 : -1));
});

const calendarRightTitle = computed(() => {
  const trip = calendarActiveTrip.value;
  if (trip) return trip.name;
  return dayjs(calendarSelectedDate.value).format("YYYY-MM-DD");
});

const calendarRightRangeLabel = computed(() => {
  const range = calendarTripRange.value;
  if (!range) return "";
  return `${range.start.format("YYYY-MM-DD")} ~ ${range.end.format("YYYY-MM-DD")}`;
});

const calendarDisplayPayments = computed(() => {
  const tripId = calendarTripFilter.value;
  if (!tripId) return calendarSelectedPayments.value;
  const list = tripPayments[tripId] || [];
  return [...list].sort((a, b) =>
    a.transaction_time < b.transaction_time ? 1 : -1,
  );
});

watch(
  () => calendarTripFilter.value,
  async (tripId) => {
    // On trip switch, jump the calendar to the trip start date so the user sees the trip range immediately.
    if (tripId) {
      if (!tripPayments[tripId]) {
        try {
          await loadTripPayments(tripId);
        } catch {
          // ignore
        }
      }

      const trip = trips.value.find((t) => t.id === tripId);
      const fallbackStart = trip?.start_time ? dayjs(trip.start_time) : null;
      const fallbackDate =
        fallbackStart && fallbackStart.isValid()
          ? fallbackStart.toDate()
          : null;

      const target = fallbackDate;
      if (target) {
        const t = dayjs(target);
        calendarSelectedDate.value = target;
        calendarMonth.value = { year: t.year(), month: t.month() };
        calendarPickerKey.value += 1;
      }
    }

    await refreshCalendarMonth();
  },
);

onMounted(async () => {
  await reloadAll();
});
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

.trip-accordion :deep(.p-accordion-toggle-icon) {
  display: none;
}

.trip-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.trip-list-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.trip-accordion :deep(.p-accordioncontent),
.trip-accordion :deep(.p-accordioncontent-wrapper),
.trip-accordion :deep(.p-accordioncontent-content) {
  width: 100%;
  max-width: 100%;
  min-width: 0;
}

.trip-accordion :deep(.p-accordioncontent-content) {
  overflow: hidden;
}

.trip-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-width: 0;
}

.trip-right {
  display: inline-flex;
  align-items: center;
  gap: 10px;
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

.trip-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: 999px;
  border: 1px solid
    color-mix(in srgb, var(--p-text-muted-color), transparent 70%);
  color: var(--p-text-muted-color);
  background: color-mix(in srgb, var(--p-surface-0), transparent 0%);
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
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

.sbm-lock-tag {
  background: #111827;
  color: #ffffff;
  border: 1px solid rgba(255, 255, 255, 0.18);
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

.trip-actions-hint {
  flex: 0 0 100%;
  display: block;
  margin-top: 2px;
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

.invoice-chip-btn {
  border: 0;
  padding: 0;
  background: transparent;
  cursor: pointer;
  display: inline-flex;
}

.invoice-chip-btn:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--p-primary-color), transparent 40%);
  outline-offset: 2px;
  border-radius: 10px;
}

.invoice-chip--baddebt {
  background: #111827;
  color: #ffffff;
  border: 1px solid rgba(255, 255, 255, 0.18);
}

.row-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.sbm-dt-hscroll {
  min-width: 0;
  width: 100%;
  max-width: 100%;
  overflow-x: auto;
  overflow-y: hidden;
  -webkit-overflow-scrolling: touch;
  touch-action: pan-x;
  overscroll-behavior-x: contain;
}

.trip-table :deep(.p-datatable-table-container),
.pending-table :deep(.p-datatable-table-container),
.calendar-table :deep(.p-datatable-table-container) {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  touch-action: pan-x;
}

.trip-table :deep(.p-datatable-table),
.pending-table :deep(.p-datatable-table),
.calendar-table :deep(.p-datatable-table) {
  width: 100%;
  table-layout: fixed;
}

.trip-table :deep(.p-datatable-thead > tr > th),
.trip-table :deep(.p-datatable-tbody > tr > td),
.pending-table :deep(.p-datatable-thead > tr > th),
.pending-table :deep(.p-datatable-tbody > tr > td),
.calendar-table :deep(.p-datatable-thead > tr > th),
.calendar-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.calendar-table :deep(.p-datatable-thead > tr > th),
.calendar-table :deep(.p-datatable-tbody > tr > td) {
  padding: 10px 12px;
}

.calendar-table :deep(.p-datatable-thead > tr > th) {
  text-align: left;
}

.calendar-table :deep(.p-datatable-tbody > tr > td) {
  text-align: left;
}

.calendar-table :deep(.p-datatable-thead > tr > th .p-column-header-content) {
  justify-content: flex-start;
}

@media (max-width: 1100px) {
  .trip-table :deep(.p-datatable-table),
  .pending-table :deep(.p-datatable-table),
  .calendar-table :deep(.p-datatable-table) {
    width: max-content;
    min-width: max(100%, 960px);
  }
}

.pending-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pending-header {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.pending-dropdown {
  width: 100%;
  max-width: 320px;
}

.calendar-layout {
  display: grid;
  grid-template-columns: 360px 1fr;
  gap: 14px;
  align-items: start;
  width: 100%;
  max-width: 100%;
  min-width: 0;
  flex: 1 1 auto;
}

.calendar-left {
  border-radius: 16px;
  padding: 12px;
  width: 100%;
  max-width: 100%;
  min-width: 0;
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

.calendar-right {
  width: 100%;
  max-width: 100%;
  min-width: 0;
}

.calendar-left :deep(.p-dropdown),
.calendar-left :deep(.p-datepicker) {
  width: 100%;
  max-width: 100%;
}

.calendar-left :deep(.p-datepicker) {
  display: block;
}

.calendar-left :deep(.p-datepicker-panel),
.calendar-left :deep(.p-datepicker-group-container),
.calendar-left :deep(.p-datepicker-calendar-container) {
  width: 100%;
  max-width: 100%;
}

.calendar-left :deep(.p-datepicker-calendar),
.calendar-left :deep(.p-datepicker-calendar table) {
  width: 100%;
}

.calendar-left :deep(.p-datepicker-calendar table) {
  table-layout: fixed;
}

.calendar-right :deep(.p-card),
.calendar-right :deep(.p-datatable) {
  width: 100%;
  max-width: 100%;
}

.calendar-right .panel-title {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.panel-title-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.panel-title-sub {
  color: var(--p-text-muted-color);
  font-weight: 700;
  font-size: 12px;
}

.date-cell {
  width: 34px;
  height: 34px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto;
  border-radius: 999px;
}

.date-day {
  font-weight: 800;
  font-size: 12px;
  opacity: 0.95;
}

.date-cell.is-today {
  box-shadow: inset 0 0 0 1px
    color-mix(in srgb, var(--p-primary-color), transparent 35%);
}

.date-cell.is-in-trip {
  background: color-mix(in srgb, var(--p-primary-color), transparent 88%);
  color: var(--p-primary-color);
}

.date-cell.is-trip-start,
.date-cell.is-trip-end {
  background: var(--p-primary-color);
  color: var(--p-primary-contrast-color, #ffffff);
  box-shadow: none;
}

.date-cell.is-outside-trip {
  opacity: 0.45;
}

:global(.p-datepicker-day.p-datepicker-day-selected .date-cell) {
  background: var(--p-primary-color) !important;
  color: var(--p-primary-contrast-color, #ffffff) !important;
  box-shadow: none !important;
}

:global(.p-datepicker-day.p-datepicker-day-selected .date-cell .date-day) {
  color: var(--p-primary-contrast-color, #ffffff) !important;
}

.dp-footer {
  position: sticky;
  bottom: 0;
  display: flex;
  justify-content: flex-end;
  padding-top: 10px;
  padding-bottom: 2px;
  background: var(--p-surface-0, #ffffff);
  border-top: 1px solid rgba(2, 6, 23, 0.06);
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

  .calendar-toolbar {
    flex-wrap: wrap;
    justify-content: flex-start;
  }

  .calendar-dropdown {
    max-width: none;
    flex: 1 1 260px;
  }
}
</style>
