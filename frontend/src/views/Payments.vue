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
            <Button
              class="p-button-outlined"
              :icon="batchDeleteMode ? 'pi pi-times' : 'pi pi-check-square'"
              :label="batchDeleteMode ? '取消选择' : '选择'"
              @click="toggleBatchDeleteMode"
            />
            <Button
              class="p-button-danger p-button-outlined"
              icon="pi pi-trash"
              label="删除"
              :disabled="!batchDeleteMode || selectedPayments.length === 0"
              @click="onBulkDeleteClick"
            />
            <DatePicker
              v-model="dateRange"
              selectionMode="range"
              :manualInput="false"
              :placeholder="'\u65E5\u671F\u8303\u56F4'"
              @update:modelValue="handleDateChange"
            />
            <Button :label="'\u4E0A\u4F20\u622A\u56FE'" icon="pi pi-image" severity="success" @click="openScreenshotModal" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable
          class="payments-table"
          :value="payments"
          :loading="loading"
          :paginator="true"
          :rows="pageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          responsiveLayout="scroll"
          sortField="transaction_time"
          :sortOrder="-1"
          dataKey="id"
          v-model:selection="selectedPayments"
        >
          <Column v-if="batchDeleteMode" selectionMode="multiple" :style="{ width: '4%' }" />
          <Column field="amount" :header="'\u91D1\u989D'" sortable :style="{ width: '10%' }">
            <template #body="{ data: row }">
              <span class="amount">{{ formatMoney(row.amount) }}</span>
            </template>
          </Column>
          <Column :header="'\u5546\u5BB6'" :style="{ width: '21%' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="normalizeInlineText(row.merchant)">{{ normalizeInlineText(row.merchant) || '-' }}</span>
            </template>
          </Column>
          <Column :header="'\u652F\u4ED8\u65B9\u5F0F'" :style="{ width: '15%' }">
            <template #body="{ data: row }">
              <Tag
                v-if="row.payment_method"
                class="sbm-tag-ellipsis"
                severity="success"
                :value="normalizePaymentMethodText(row.payment_method)"
                :title="normalizePaymentMethodText(row.payment_method)"
              />
              <span v-else>-</span>
            </template>
          </Column>
          <Column :header="'\u5907\u6CE8'" :style="{ width: '21%' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="normalizeInlineText(row.description)">{{ normalizeInlineText(row.description) || '-' }}</span>
            </template>
          </Column>
          <Column field="transaction_time" :header="'\u4EA4\u6613\u65F6\u95F4'" sortable :style="{ width: '15%' }">
            <template #body="{ data: row }">
              {{ formatDateTime(row.transaction_time) }}
            </template>
          </Column>
          <Column :header="'\u5173\u8054\u53D1\u7968'" :style="{ width: '7%' }">
            <template #body="{ data: row }">
              <Button size="small" class="p-button-text" :label="invoiceCountLabel(row.invoiceCount)" @click="viewLinkedInvoices(row)" />
            </template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '7%' }">
            <template #body="{ data: row }">
              <div class="row-actions">
                <Button class="p-button-text" icon="pi pi-eye" @click="openPaymentDetail(row)" />
                <Button class="p-button-text p-button-danger" icon="pi pi-trash" @click="confirmDelete(row.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog
      v-model:visible="uploadScreenshotModalVisible"
      modal
      :header="'\u4E0A\u4F20\u652F\u4ED8\u622A\u56FE'"
      :style="{ width: '880px', maxWidth: '96vw' }"
      :closable="!uploadingScreenshot && !savingOcrResult"
      @hide="cancelScreenshotUpload"
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
            <Message v-if="uploadDedup?.kind === 'suspected_duplicate'" severity="warn" :closable="false">
              检测到疑似重复支付记录（金额 + 时间接近）。如果确认需要保留，可点击保存后选择“仍然保存”。
            </Message>
	          <div class="grid">
            <div class="col-12 md:col-6 field">
              <label for="ocr_amount">&#37329;&#39069;</label>
              <InputNumber
                id="ocr_amount"
                v-model="ocrForm.amount"
                :minFractionDigits="2"
                :maxFractionDigits="2"
                :min="0"
                :useGrouping="false"
              />
              <small
                v-if="ocrResult?.amount_source || ocrResult?.amount_confidence"
                class="sbm-ocr-hint"
                :class="confidenceClass(ocrResult?.amount_confidence)"
              >
                &#26469;&#28304;&#65306;{{ formatSourceLabel(ocrResult?.amount_source) || '\u672a\u8bc6\u522b' }}
                <span v-if="ocrResult?.amount_confidence">（置信度：{{ confidenceLabel(ocrResult?.amount_confidence) }}）</span>
              </small>
              <small v-if="ocrErrors.amount" class="p-error">{{ ocrErrors.amount }}</small>
            </div>

            <div class="col-12 md:col-6 field">
              <label for="ocr_merchant">&#21830;&#23478;</label>
              <InputText id="ocr_merchant" v-model.trim="ocrForm.merchant" />
              <small
                v-if="ocrResult?.merchant_source || ocrResult?.merchant_confidence"
                class="sbm-ocr-hint"
                :class="confidenceClass(ocrResult?.merchant_confidence)"
              >
                &#26469;&#28304;&#65306;{{ formatSourceLabel(ocrResult?.merchant_source) || '\u672a\u8bc6\u522b' }}
                <span v-if="ocrResult?.merchant_confidence">（置信度：{{ confidenceLabel(ocrResult?.merchant_confidence) }}）</span>
              </small>
            </div>

            <div class="col-12 md:col-6 field">
              <label for="ocr_method">&#25903;&#20184;&#26041;&#24335;</label>
              <InputText id="ocr_method" v-model.trim="ocrForm.payment_method" />
              <small
                v-if="ocrResult?.payment_method_source || ocrResult?.payment_method_confidence"
                class="sbm-ocr-hint"
                :class="confidenceClass(ocrResult?.payment_method_confidence)"
              >
                &#26469;&#28304;&#65306;{{ formatSourceLabel(ocrResult?.payment_method_source) || '\u672a\u8bc6\u522b' }}
                <span v-if="ocrResult?.payment_method_confidence">（置信度：{{ confidenceLabel(ocrResult?.payment_method_confidence) }}）</span>
              </small>
            </div>

            <div class="col-12 field">
              <label for="ocr_time">&#20132;&#26131;&#26102;&#38388;</label>
              <DatePicker id="ocr_time" v-model="ocrForm.transaction_time" showTime :manualInput="false" />
              <small v-if="ocrErrors.transaction_time" class="p-error">{{ ocrErrors.transaction_time }}</small>
              <small
                v-if="ocrResult?.transaction_time_source || ocrResult?.transaction_time_confidence"
                class="sbm-ocr-hint"
                :class="confidenceClass(ocrResult?.transaction_time_confidence)"
              >
                &#26469;&#28304;&#65306;{{ formatSourceLabel(ocrResult?.transaction_time_source) || '\u672a\u8bc6\u522b' }}
                <span v-if="ocrResult?.transaction_time_confidence">（置信度：{{ confidenceLabel(ocrResult?.transaction_time_confidence) }}）</span>
              </small>
            </div>

            <div class="col-12 field">
              <label for="ocr_desc">&#22791;&#27880;</label>
              <Textarea id="ocr_desc" v-model="ocrForm.description" autoResize rows="3" />
            </div>
          </div>
        </form>

        <div v-if="ocrResult?.raw_text || ocrResult?.pretty_text" class="raw">
          <div class="raw-title">OCR &#25991;&#26412;</div>
          <Accordion>
            <AccordionTab v-if="ocrResult?.pretty_text" :header="'\u70B9\u51FB\u67E5\u770B OCR \u6574\u7406\u7248\u6587\u672C'">
              <pre class="raw-text">{{ ocrResult.pretty_text }}</pre>
            </AccordionTab>
            <AccordionTab v-if="ocrResult?.raw_text" :header="'\u70B9\u51FB\u67E5\u770B OCR \u539F\u59CB\u6587\u672C'">
              <pre class="raw-text">{{ ocrResult.raw_text }}</pre>
            </AccordionTab>
          </Accordion>
        </div>
      </div>

      <template #footer>
        <div class="dialog-footer-center">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" @click="closeScreenshotModal" />
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

      <div v-if="currentPaymentForInvoices" class="match-summary">
        <div class="match-summary-item">
          <div class="match-summary-label">已关联发票合计</div>
          <div class="match-summary-value">{{ formatMoney(linkedInvoiceAmount.sum) }}</div>
          <small v-if="linkedInvoiceAmount.unknown > 0" class="muted">另有 {{ linkedInvoiceAmount.unknown }} 张未识别金额</small>
        </div>
        <div class="match-summary-item">
          <div class="match-summary-label">支付金额</div>
          <div class="match-summary-value">{{ formatMoney(Number(currentPaymentForInvoices.amount || 0)) }}</div>
        </div>
        <div class="match-summary-item">
          <div class="match-summary-label">差额</div>
          <div class="match-summary-value">
            <Tag :severity="diffTagSeverity" :value="diffTagLabel" />
          </div>
        </div>
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
                <span class="sbm-ellipsis" :title="invoicePrimaryLabel(row)">{{ invoicePrimaryLabel(row) }}</span>
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
          <DataTable class="match-table" :value="suggestedInvoices" :loading="loadingSuggestedInvoices" scrollHeight="260px" :scrollable="true" responsiveLayout="scroll">
            <Column :header="'\u53D1\u7968\u53F7'" :style="{ width: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="invoicePrimaryLabel(row)">{{ invoicePrimaryLabel(row) }}</span>
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

          <div v-if="loadingUnlinkedInvoices || unlinkedInvoicesTotal > 0" class="match-subtitle">全部未关联发票</div>
          <DataTable
            v-if="loadingUnlinkedInvoices || unlinkedInvoicesTotal > 0"
            class="match-table match-table-secondary"
            :value="unlinkedInvoices"
            :loading="loadingUnlinkedInvoices"
            responsiveLayout="scroll"
            :paginator="true"
            :rows="unlinkedRows"
            :first="unlinkedFirst"
            :totalRecords="unlinkedInvoicesTotal"
            :rowsPerPageOptions="[10, 20, 50]"
            :lazy="true"
            @page="onUnlinkedPage"
          >
            <Column :header="'\u53D1\u7968\u53F7'" :style="{ width: '200px' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="invoicePrimaryLabel(row)">{{ invoicePrimaryLabel(row) }}</span>
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

          <div v-else class="no-data no-data-secondary">
            <i class="pi pi-info-circle" />
            <span>暂无未关联发票</span>
          </div>
        </TabPanel>

        </TabPanels>
      </Tabs>
      <template #footer>
        <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="linkedInvoicesModalVisible = false" />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="paymentDetailVisible"
      modal
      :header="'\u652F\u4ED8\u8BB0\u5F55\u8BE6\u60C5'"
      :style="{ width: '740px', maxWidth: '94vw' }"
      :breakpoints="{ '960px': '94vw', '640px': '96vw' }"
      :contentStyle="{ padding: '14px 16px' }"
      :closable="!paymentDetailEditing && !savingPaymentDetail"
      :closeOnEscape="!paymentDetailEditing && !savingPaymentDetail"
      @hide="onPaymentDetailHide"
    >
      <div v-if="detailPayment" class="detail">
        <div class="header-row">
          <div class="title">
            <i class="pi pi-image" />
            <span class="sbm-ellipsis" :title="getPaymentScreenshotTitle(detailPayment)">{{ getPaymentScreenshotTitle(detailPayment) }}</span>
          </div>
          <div class="actions">
            <Button
              v-if="isAdmin && !paymentDetailEditing"
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-verified"
              :label="'\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c'"
              :loading="markingRegressionSample"
              @click="markPaymentRegressionSample(detailPayment.id)"
            />
            <Button
              v-if="!paymentDetailEditing"
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-pencil"
              :label="'\u7F16\u8F91'"
              @click="enterPaymentEditMode"
            />
            <Button
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-refresh"
              :label="'\u91CD\u65B0\u89E3\u6790'"
              :loading="reparsingOcr"
              :disabled="paymentDetailEditing || savingPaymentDetail || !detailPayment.screenshot_path"
              @click="handleReparseOcr(detailPayment.id)"
            />
          </div>
        </div>

        <div class="grid sbm-grid-tight">
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">&#37329;&#39069;</div>
              <div class="v" :class="{ amount: !paymentDetailEditing }">
                <InputNumber
                  v-if="paymentDetailEditing"
                  v-model="paymentDetailForm.amount"
                  :minFractionDigits="2"
                  :maxFractionDigits="2"
                  :min="0"
                  :useGrouping="false"
                />
                <template v-else>{{ formatMoney(detailPayment.amount || 0) }}</template>
              </div>
            </div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">&#21830;&#23478;</div>
              <div class="v" :title="normalizeInlineText(detailPayment.merchant)">
                <InputText v-if="paymentDetailEditing" v-model.trim="paymentDetailForm.merchant" />
                <template v-else>{{ normalizeInlineText(detailPayment.merchant) || '-' }}</template>
              </div>
            </div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">&#25903;&#20184;&#26041;&#24335;</div>
              <div class="v">
                <InputText v-if="paymentDetailEditing" v-model.trim="paymentDetailForm.payment_method" />
                <template v-else>
                  <Tag
                    v-if="detailPayment.payment_method"
                    class="sbm-tag-ellipsis"
                    severity="success"
                    :value="normalizePaymentMethodText(detailPayment.payment_method)"
                    :title="normalizePaymentMethodText(detailPayment.payment_method)"
                  />
                  <span v-else>-</span>
                </template>
              </div>
            </div>
          </div>
          <div class="col-12">
            <div class="kv">
              <div class="k">&#20132;&#26131;&#26102;&#38388;</div>
              <div class="v">
                <template v-if="paymentDetailEditing">
                  <InputText
                    :modelValue="formatDateTimeDraft(paymentDetailForm.transaction_time)"
                    readonly
                    :placeholder="'请选择交易时间'"
                    @click="togglePaymentTimePanel"
                  />
                  <OverlayPanel ref="paymentTimePanel" :dismissable="true" :showCloseIcon="false" class="payment-time-panel" @show="onPaymentTimePanelShow" @hide="onPaymentTimePanelHide">
                    <DatePicker v-model="paymentDetailTimeDraft" inline showTime :manualInput="false" />
                    <div class="payment-time-panel-footer">
                      <Button type="button" class="p-button-outlined" severity="secondary" :label="'取消'" @click="cancelPaymentTimePanel" />
                      <Button type="button" :label="'确认'" icon="pi pi-check" @click="confirmPaymentTimePanel" />
                    </div>
                  </OverlayPanel>
                </template>
                <template v-else>{{ formatDateTime(detailPayment.transaction_time) }}</template>
              </div>
            </div>
          </div>
          <div class="col-12">
            <div class="kv">
              <div class="k">&#22791;&#27880;</div>
              <div class="v">
                <Textarea v-if="paymentDetailEditing" v-model="paymentDetailForm.description" autoResize rows="3" />
                <template v-else>{{ detailPayment.description || '-' }}</template>
              </div>
            </div>
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
              :imageStyle="{ width: '100%', maxWidth: '100%', height: 'auto' }"
            />
          </div>
        </div>

        <div v-if="detailPayment.extracted_data" class="section">
          <div class="section-title">OCR &#25991;&#26412;</div>
          <Accordion>
            <AccordionTab v-if="getExtractedPrettyText(detailPayment.extracted_data || null)" :header="'\u70B9\u51FB\u67E5\u770B OCR \u6574\u7406\u7248\u6587\u672C'">
              <pre class="raw-text">{{ getExtractedPrettyText(detailPayment.extracted_data || null) }}</pre>
            </AccordionTab>
            <AccordionTab :header="'\u70B9\u51FB\u67E5\u770B OCR \u539F\u59CB\u6587\u672C'">
              <pre class="raw-text">{{ getExtractedRawText(detailPayment.extracted_data || null) }}</pre>
            </AccordionTab>
          </Accordion>
        </div>
      </div>
      <template #footer>
        <div v-if="paymentDetailEditing" class="dialog-footer-center">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" :disabled="savingPaymentDetail" @click="cancelPaymentEditMode" />
          <Button type="button" :label="'\u4FDD\u5B58'" icon="pi pi-check" :loading="savingPaymentDetail" @click="savePaymentEditMode" />
        </div>
        <Button v-else type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="paymentDetailVisible = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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
import OverlayPanel from 'primevue/overlaypanel'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { invoiceApi, paymentApi, tasksApi, FILE_BASE_URL, regressionSamplesApi } from '@/api'
import { useNotificationStore } from '@/stores/notifications'
import { useAuthStore } from '@/stores/auth'
import type { Invoice, Payment, DedupHint } from '@/types'

interface OcrExtractedData {
  amount?: number
  merchant?: string
  transaction_time?: string
  payment_method?: string
  amount_source?: string
  amount_confidence?: number
  merchant_source?: string
  merchant_confidence?: number
  transaction_time_source?: string
  transaction_time_confidence?: number
  payment_method_source?: string
  payment_method_confidence?: number
  raw_text?: string
  pretty_text?: string
}

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()
const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()
const isAdmin = computed(() => authStore.user?.role === 'admin')

const confirmForceSave = (message: string) =>
  new Promise<boolean>(resolve => {
    confirm.require({
      header: '\u7591\u4f3c\u91cd\u590d',
      message,
      icon: 'pi pi-exclamation-triangle',
      acceptLabel: '\u4ecd\u7136\u4fdd\u5b58',
      rejectLabel: '\u53d6\u6d88',
      accept: () => resolve(true),
      reject: () => resolve(false),
    })
  })

const summarizeSampleIssues = (issues: any[]) => {
  const items = Array.isArray(issues) ? issues : []
  const parts = items.slice(0, 6).map((it: any) => {
    const level = String(it?.level || '').toLowerCase()
    const label = level === 'error' ? '\u9519\u8bef' : '\u8b66\u544a'
    return `${label}\uff1a${it?.message || it?.code || '\u672a\u77e5\u95ee\u9898'}`
  })
  const suffix = items.length > 6 ? '\u2026' : ''
  return parts.join('\uff1b') + suffix
}

const confirmForceMarkRegressionSample = (issues: any[]) =>
  new Promise<boolean>(resolve => {
    confirm.require({
      header: '\u6837\u672c\u8d28\u91cf\u63d0\u793a',
      message: `\u6837\u672c\u8d28\u91cf\u68c0\u67e5\u53d1\u73b0\uff1a${summarizeSampleIssues(issues)}\n\u4ecd\u7136\u8981\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c\u5417\uff1f`,
      icon: 'pi pi-exclamation-triangle',
      acceptLabel: '\u4ecd\u7136\u6807\u8bb0',
      rejectLabel: '\u53d6\u6d88',
      accept: () => resolve(true),
      reject: () => resolve(false),
    })
  })

const loading = ref(false)
const payments = ref<Payment[]>([])
const selectedPayments = ref<Payment[]>([])
const batchDeleteMode = ref(false)
const pageSize = ref(10)
const dateRange = ref<Date[] | null>(null)

const stats = ref<{ totalAmount: number; totalCount: number } | null>(null)
const avgAmount = computed(() => {
  const count = stats.value?.totalCount || 0
  const total = stats.value?.totalAmount || 0
  return count ? total / count : 0
})

// Screenshot upload + OCR
const uploadScreenshotModalVisible = ref(false)
const uploadingScreenshot = ref(false)
const savingOcrResult = ref(false)
const selectedScreenshotFile = ref<File | null>(null)
const selectedScreenshotName = ref('')
const screenshotError = ref('')
const ocrResult = ref<OcrExtractedData | null>(null)
const uploadDedup = ref<DedupHint | null>(null)
const uploadedPaymentId = ref<string | null>(null)
const uploadedScreenshotPath = ref<string | null>(null)
const ocrTaskId = ref<string | null>(null)
const screenshotInput = ref<HTMLInputElement | null>(null)
const screenshotUploadAttempt = ref(0)

const PENDING_PAYMENT_DRAFT_KEY = 'sbm_pending_payment_upload_draft'

const rememberPendingPaymentDraft = (draft: { paymentId?: string | null; screenshotPath?: string | null }) => {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(
    PENDING_PAYMENT_DRAFT_KEY,
    JSON.stringify({
      paymentId: draft.paymentId || null,
      screenshotPath: draft.screenshotPath || null,
      ts: Date.now(),
    }),
  )
}

const clearPendingPaymentDraft = () => {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(PENDING_PAYMENT_DRAFT_KEY)
}

const cleanupPendingPaymentDraftOnLoad = async () => {
  if (typeof window === 'undefined') return
  const raw = window.localStorage.getItem(PENDING_PAYMENT_DRAFT_KEY)
  if (!raw) return
  try {
    const parsed = JSON.parse(raw) as { paymentId?: string | null; screenshotPath?: string | null }
    const paymentId = typeof parsed?.paymentId === 'string' ? parsed.paymentId : ''
    const screenshotPath = typeof parsed?.screenshotPath === 'string' ? parsed.screenshotPath : ''
    if (paymentId) {
      const res = await paymentApi.getById(paymentId)
      const p = res.data?.data as any
      if (p?.is_draft) {
        await paymentApi.delete(paymentId)
      }
    } else if (screenshotPath) {
      await paymentApi.cancelUploadScreenshot(screenshotPath)
    }
  } catch {
    // ignore
  } finally {
    clearPendingPaymentDraft()
  }
}

const triggerScreenshotChoose = (event: MouseEvent) => {
  const target = event.target as HTMLElement | null
  if (!target) return
  if (target.closest('button') || target.closest('input') || target.closest('a')) return
  screenshotInput.value?.click()
}

const chooseScreenshotFile = () => {
  screenshotInput.value?.click()
}

const setScreenshotFile = async (file?: File) => {
  screenshotError.value = ''
  if (screenshotInput.value) screenshotInput.value.value = ''

  // User canceled the file picker / no new file selected: keep current state.
  if (!file) return

  const validTypes = ['image/jpeg', 'image/jpg', 'image/png']
  if (!validTypes.includes(file.type)) {
    screenshotError.value = '\u53EA\u652F\u6301 JPG\u3001JPEG\u3001PNG \u683C\u5F0F\u7684\u56FE\u7247'
    return
  }

  const maxSize = 10 * 1024 * 1024
  if (file.size > maxSize) {
    screenshotError.value = '\u6587\u4EF6\u5927\u5C0F\u4E0D\u80FD\u8D85\u8FC7 10MB'
    return
  }

  // Replacing an existing uploaded/recognized draft should clean it up first,
  // otherwise re-uploading the same file will be considered a hash duplicate.
  await cleanupCurrentScreenshotUpload()
  resetScreenshotUploadStateKeepModalOpen()

  selectedScreenshotFile.value = file
  selectedScreenshotName.value = file.name
}

const resetScreenshotUploadStateKeepModalOpen = () => {
  screenshotUploadAttempt.value++
  uploadingScreenshot.value = false
  savingOcrResult.value = false
  uploadedPaymentId.value = null
  uploadedScreenshotPath.value = null
  ocrTaskId.value = null
  selectedScreenshotFile.value = null
  selectedScreenshotName.value = ''
  screenshotError.value = ''
  ocrResult.value = null
  uploadDedup.value = null
  ocrForm.amount = 0
  ocrForm.merchant = ''
  ocrForm.payment_method = ''
  ocrForm.description = ''
  ocrForm.transaction_time = null
  if (screenshotInput.value) screenshotInput.value.value = ''
}

const cleanupCurrentScreenshotUpload = async () => {
  const taskId = ocrTaskId.value
  const paymentId = uploadedPaymentId.value
  const screenshotPath = uploadedScreenshotPath.value

  // Stop any in-flight waitForTask loop.
  screenshotUploadAttempt.value++
  uploadingScreenshot.value = false

  if (taskId) tasksApi.cancel(taskId).catch(() => undefined)
  if (paymentId) {
    await paymentApi.delete(paymentId).catch((error) => console.error('Failed to delete payment record:', error))
  } else if (screenshotPath) {
    await paymentApi.cancelUploadScreenshot(screenshotPath).catch((error) => console.error('Failed to delete screenshot file:', error))
  }
  clearPendingPaymentDraft()
}

const onScreenshotDrop = async (event: DragEvent) => {
  const file = event.dataTransfer?.files?.[0]
  await setScreenshotFile(file)
}

const onScreenshotInputChange = async (event: Event) => {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  await setScreenshotFile(file)
  if (input) input.value = ''
}

const clearSelectedScreenshot = async () => {
  await cleanupCurrentScreenshotUpload()
  resetScreenshotUploadStateKeepModalOpen()
}

const ocrForm = reactive({
  amount: 0,
  merchant: '',
  payment_method: '',
  description: '',
  transaction_time: null as Date | null,
})
const ocrErrors = reactive({ amount: '', transaction_time: '' })

// Linked invoices
const linkedInvoicesModalVisible = ref(false)
const loadingLinkedInvoices = ref(false)
const linkedInvoices = ref<Invoice[]>([])
const currentPaymentForInvoices = ref<Payment | null>(null)
const loadingSuggestedInvoices = ref(false)
const suggestedInvoices = ref<Invoice[]>([])
const loadingUnlinkedInvoices = ref(false)
const unlinkedInvoices = ref<Invoice[]>([])
const unlinkedInvoicesTotal = ref(0)
const unlinkedFirst = ref(0)
const unlinkedRows = ref(10)
const linkingInvoiceToPayment = ref(false)
const unlinkingInvoiceFromPayment = ref(false)
const invoiceMatchTab = ref<'linked' | 'suggested'>('linked')

// Detail dialog
const paymentDetailVisible = ref(false)
const detailPayment = ref<Payment | null>(null)
const reparsingOcr = ref(false)
const paymentDetailEditing = ref(false)
const savingPaymentDetail = ref(false)
const markingRegressionSample = ref(false)
const paymentTimePanel = ref<InstanceType<typeof OverlayPanel> | null>(null)
const paymentTimeLastTarget = ref<HTMLElement | null>(null)
const paymentDetailTimeDraft = ref<Date | null>(null)
const paymentDetailForm = reactive({
  amount: 0,
  merchant: '',
  payment_method: '',
  description: '',
  transaction_time: null as Date | null,
})

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
const normalizeInlineText = (value?: string | null) => {
  const s = (value || '').replace(/\s+/g, ' ').trim()
  return s.replace(/[>鈥郝汇€夈€嬧啋]+$/g, '').trim()
}

const normalizePaymentMethodText = (value?: string | null) => {
  const s = normalizeInlineText(value)
  return s.replace(/[（）]/g, (m) => (m === '（' ? '(' : ')')).trim()
}

const invoiceCountLabel = (cnt?: number | null) => {
  const n = typeof cnt === 'number' ? cnt : 0
  if (n > 1) return `多发票 (${n})`
  return `查看 (${n})`
}

const handleDateChange = () => {
  loadPayments()
  loadStats()
}

const toggleBatchDeleteMode = () => {
  batchDeleteMode.value = !batchDeleteMode.value
  if (!batchDeleteMode.value) selectedPayments.value = []
}

const onBulkDeleteClick = () => {
  if (!batchDeleteMode.value) return
  if (selectedPayments.value.length === 0) return
  const count = selectedPayments.value.length
  confirm.require({
    message: `确定删除选中的 ${count} 条记录吗？`,
    header: '删除确认',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: () => handleBulkDelete(),
  })
}

const handleBulkDelete = async () => {
  const ids = Array.from(new Set(selectedPayments.value.map(x => x.id).filter(Boolean)))
  if (ids.length === 0) return
  const results = await Promise.allSettled(ids.map(id => paymentApi.delete(id)))
  const failed = results.filter(r => r.status === 'rejected').length
  const succeeded = ids.length - failed
  if (failed === 0) {
    toast.add({ severity: 'success', summary: `已删除 ${succeeded} 条记录`, life: 2200 })
  } else {
    toast.add({ severity: 'warn', summary: `已删除 ${succeeded} 条，失败 ${failed} 条`, life: 3800 })
  }
  batchDeleteMode.value = false
  selectedPayments.value = []
  await loadPayments()
  await loadStats()
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
    selectedPayments.value = selectedPayments.value.filter(p => p.id !== id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    notifications.add({ severity: 'info', title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u5220\u9664', detail: id })
    await loadPayments()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
  }
}

const openScreenshotModal = () => {
  resetScreenshotUploadState()
  uploadScreenshotModalVisible.value = true
}

const closeScreenshotModal = () => {
  uploadScreenshotModalVisible.value = false
}

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

const waitForTask = async (taskId: string, opts?: { timeoutMs?: number; shouldStop?: () => boolean }) => {
  const timeoutMs = opts?.timeoutMs ?? 120000
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    if (opts?.shouldStop?.()) return { status: 'canceled' }
    const res = await tasksApi.getById(taskId)
    const t = res.data?.data as any
    if (!t) throw new Error('任务状态获取失败')
    if (t.status === 'succeeded' || t.status === 'failed' || t.status === 'canceled') return t
    await sleep(800)
  }
  throw new Error('识别超时，请稍后重试')
}

const handleScreenshotUpload = async () => {
  if (!selectedScreenshotFile.value) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u9009\u62E9\u6587\u4EF6', life: 2200 })
    return
  }

  const attemptId = ++screenshotUploadAttempt.value
  uploadingScreenshot.value = true
  try {
    const uploadRes = await paymentApi.uploadScreenshotAsync(selectedScreenshotFile.value)
    if (uploadRes.data.success && uploadRes.data.data) {
      const { taskId, payment, screenshot_path } = uploadRes.data.data as any
      if (attemptId !== screenshotUploadAttempt.value) {
        tasksApi.cancel(taskId).catch(() => undefined)
        const draftPaymentId = payment?.id as string | undefined
        if (draftPaymentId) {
          paymentApi.delete(draftPaymentId).catch(() => undefined)
        } else if (screenshot_path) {
          paymentApi.cancelUploadScreenshot(screenshot_path).catch(() => undefined)
        }
        return
      }

      uploadedPaymentId.value = payment?.id || null
      uploadedScreenshotPath.value = screenshot_path || null
      ocrTaskId.value = taskId || null
      rememberPendingPaymentDraft({ paymentId: uploadedPaymentId.value, screenshotPath: uploadedScreenshotPath.value })

      toast.add({ severity: 'info', summary: '截图已上传，正在识别…', life: 2000 })

      const task = await waitForTask(taskId, { shouldStop: () => attemptId !== screenshotUploadAttempt.value })
      if (task.status === 'failed') {
        toast.add({ severity: 'error', summary: task?.error || '识别失败', life: 5000 })
        return
      }
      if (task.status === 'canceled') {
        toast.add({ severity: 'warn', summary: '识别已取消', life: 2500 })
        return
      }
      if (attemptId !== screenshotUploadAttempt.value) return

      const payload = task.result as any
      const { extracted, ocr_error, dedup } = payload || {}
      const nextPayment = payload?.payment || payment || null
      uploadedPaymentId.value = nextPayment?.id || uploadedPaymentId.value
      ocrResult.value = extracted
      uploadDedup.value = (dedup as DedupHint) || null

      ocrForm.amount = extracted?.amount || 0
      ocrForm.merchant = extracted?.merchant || ''
      ocrForm.payment_method = normalizePaymentMethodText(extracted?.payment_method || '')
      if (extracted?.transaction_time) {
        const t = dayjs(extracted.transaction_time)
        ocrForm.transaction_time = t.isValid() ? t.toDate() : null
      } else {
        ocrForm.transaction_time = null
      }
      ocrForm.description = ''

      if (ocr_error) {
        const hasTime = !!(extracted?.transaction_time && dayjs(extracted.transaction_time).isValid())
        toast.add({
          severity: 'warn',
          summary: hasTime
            ? `截图上传成功，但 OCR 识别不完整：${ocr_error}`
            : '截图上传成功，但未识别到交易时间，请手动选择交易时间',
          life: 5000,
        })
        notifications.add({
          severity: 'warn',
          title: hasTime ? '支付截图已识别（需校对）' : '支付截图已识别（需补全）',
          detail: selectedScreenshotName.value || undefined,
        })
      } else {
        toast.add({
          severity: 'success',
          summary: '截图识别成功，请确认或修改识别结果',
          life: 2500,
        })
        notifications.add({
          severity: 'success',
          title: '支付截图已识别',
          detail: selectedScreenshotName.value || undefined,
        })
      }
    }
  } catch (error: unknown) {
    const err = error as any
    const resp = err?.response
    const data = resp?.data
    const dup = data?.data as DedupHint | undefined
    if (resp?.status === 409 && dup?.kind === 'hash_duplicate') {
      toast.add({ severity: 'warn', summary: '\u6587\u4EF6\u5185\u5BB9\u91CD\u590D\uFF0C\u5DF2\u5B58\u5728\u8BB0\u5F55', life: 4500 })
      return
    }
    const message = data?.message || '\u622A\u56FE\u8BC6\u522B\u5931\u8D25'
    const detail = data?.error
    toast.add({ severity: 'error', summary: detail ? `${message}\uFF1A${detail}` : message, life: 5000 })
  } finally {
    if (attemptId === screenshotUploadAttempt.value) uploadingScreenshot.value = false
  }
}

const handleSaveOcrResult = async () => {
  if (!validateOcrForm()) return
  savingOcrResult.value = true
  try {
    const payload: any = {
      amount: Number(ocrForm.amount),
      merchant: ocrForm.merchant,
      payment_method: normalizePaymentMethodText(ocrForm.payment_method),
      description: ocrForm.description,
      transaction_time: dayjs(ocrForm.transaction_time as Date).toISOString(),
    }

    if (uploadedPaymentId.value) {
      const saveCurrent = async (force: boolean) => {
        await paymentApi.update(uploadedPaymentId.value as string, {
          ...payload,
          confirm: true,
          force_duplicate_save: force ? true : undefined,
        })
      }
      try {
        await saveCurrent(false)
      } catch (error: unknown) {
        const err = error as any
        const resp = err?.response
        const dup = resp?.data?.data as DedupHint | undefined
        if (resp?.status === 409 && dup?.kind === 'suspected_duplicate') {
          const ok = await confirmForceSave('检测到疑似重复支付记录（金额 + 时间接近），是否仍然保存？')
          if (!ok) return
          await saveCurrent(true)
        } else {
          throw error
        }
      }
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u66F4\u65B0\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u4FDD\u5B58',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    } else {
      if (uploadedScreenshotPath.value) payload.screenshot_path = uploadedScreenshotPath.value
      if (ocrResult.value) payload.extracted_data = JSON.stringify(ocrResult.value)
      await paymentApi.create(payload)
      toast.add({ severity: 'success', summary: '\u652F\u4ED8\u8BB0\u5F55\u521B\u5EFA\u6210\u529F', life: 2000 })
      notifications.add({
        severity: 'success',
        title: '\u652F\u4ED8\u8BB0\u5F55\u5DF2\u4FDD\u5B58',
        detail: `${formatMoney(payload.amount)} ${payload.merchant || ''}`.trim(),
      })
    }

    clearPendingPaymentDraft()
    resetScreenshotUploadState()
    await loadPayments()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u4FDD\u5B58\u5931\u8D25', life: 3000 })
  } finally {
    savingOcrResult.value = false
  }
}

const resetScreenshotUploadState = () => {
  screenshotUploadAttempt.value++
  uploadingScreenshot.value = false
  savingOcrResult.value = false
  uploadedPaymentId.value = null
  uploadedScreenshotPath.value = null
  ocrTaskId.value = null
  selectedScreenshotFile.value = null
  selectedScreenshotName.value = ''
  screenshotError.value = ''
  ocrResult.value = null
  uploadDedup.value = null
  ocrForm.amount = 0
  ocrForm.merchant = ''
  ocrForm.payment_method = ''
  ocrForm.description = ''
  ocrForm.transaction_time = null
  if (screenshotInput.value) screenshotInput.value.value = ''
  uploadScreenshotModalVisible.value = false
}

const cancelScreenshotUpload = () => {
  const taskId = ocrTaskId.value
  if (taskId) tasksApi.cancel(taskId).catch(() => undefined)
  if (uploadedPaymentId.value) {
    paymentApi.delete(uploadedPaymentId.value).catch((error) => console.error('Failed to delete payment record:', error))
  } else if (uploadedScreenshotPath.value) {
    paymentApi.cancelUploadScreenshot(uploadedScreenshotPath.value).catch((error) => console.error('Failed to delete screenshot file:', error))
  }
  clearPendingPaymentDraft()
  resetScreenshotUploadState()
}

const formatSourceLabel = (src?: string) => {
  if (!src) return ''
  const map: Record<string, string> = {
    wechat_amount_label: '微信金额标签',
    alipay_amount_label: '支付宝金额标签',
    bank_amount_label: '银行金额标签',
    generic_amount: '通用金额匹配',
    wechat_label: '微信字段标签',
    alipay_label: '支付宝字段标签',
    bank_label: '银行字段标签',
    alipay_bill_detail: '支付宝账单详情',
    unionpay_bill_detail: '云闪付账单详情',
    generic_merchant_suffix: '通用商家匹配',
    wechat_time_label: '微信时间标签',
    alipay_time_label: '支付宝时间标签',
    bank_time_label: '银行时间标签',
    unionpay_time_label: '云闪付时间标签',
    alipay_transfer_time: '支付宝转账时间标签',
    jd_time: '京东时间标签',
    wechat_order: '微信单号标签',
    alipay_order: '支付宝单号标签',
    alipay_transfer_voucher_no: '支付宝转账凭证编号',
    bank_order_label: '银行单号标签',
    jd_trade_no: '京东交易号标签',
    jd_merchant_order: '京东商户单号标签',
    jd_total_order: '京东总订单编号',
    jd_order: '京东订单编号',
    unionpay_order: '云闪付订单编号',
    unionpay_merchant_order: '云闪付商户订单号',
    wechat_method_label: '微信支付方式标签',
    alipay_method_label: '支付宝支付方式标签',
    bank_method_label: '银行支付方式标签',
    unionpay_method_label: '云闪付支付方式标签',
    jd_method: '京东支付方式标签',
    alipay_transfer: '支付宝转账',
    wechat_infer: '微信推断',
    alipay_infer: '支付宝推断',
    bank_infer: '银行推断',
    alipay_transfer_payee: '支付宝转账收款方',
    jd_title: '京东账单详情标题',
    unionpay_amount_label: '云闪付金额标签',
    unionpay_amount: '云闪付金额匹配',
    jd_amount: '京东金额匹配',
  }
  return map[src] || src
}

const confidenceLabel = (c?: number) => {
  if (c === undefined || c === null) return ''
  if (c >= 0.8) return '\u9ad8' // 高
  if (c >= 0.6) return '\u4e2d' // 中
  return '\u4f4e' // 低
}

const confidenceClass = (c?: number) => {
  const label = confidenceLabel(c)
  if (label === '\u4f4e') return 'sbm-ocr-hint-low'
  if (label === '\u4e2d') return 'sbm-ocr-hint-mid'
  return ''
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
  void fetchUnlinkedInvoices({ reset: true })
}

const fetchUnlinkedInvoices = async (opts?: { reset?: boolean }) => {
  if (opts?.reset) unlinkedFirst.value = 0
  loadingUnlinkedInvoices.value = true
  try {
    const res = await invoiceApi.getUnlinked({ limit: unlinkedRows.value, offset: unlinkedFirst.value })
    const payload = res.data?.data as any
    unlinkedInvoices.value = Array.isArray(payload?.items) ? (payload.items as Invoice[]) : []
    unlinkedInvoicesTotal.value = typeof payload?.total === 'number' ? payload.total : 0
  } catch {
    unlinkedInvoices.value = []
    unlinkedInvoicesTotal.value = 0
  } finally {
    loadingUnlinkedInvoices.value = false
  }
}

const onUnlinkedPage = (event: { first: number; rows: number }) => {
  unlinkedFirst.value = event.first
  unlinkedRows.value = event.rows
  void fetchUnlinkedInvoices()
}

const linkedInvoiceAmount = computed(() => {
  let sum = 0
  let unknown = 0
  for (const inv of linkedInvoices.value) {
    if (typeof inv.amount === 'number') {
      sum += Number(inv.amount || 0)
    } else {
      unknown += 1
    }
  }
  return { sum, unknown }
})

const diffTagLabel = computed(() => {
  if (!currentPaymentForInvoices.value) return '-'
  if (linkedInvoiceAmount.value.unknown > 0) return '差额未知'
  const pay = Number(currentPaymentForInvoices.value.amount || 0)
  const diff = linkedInvoiceAmount.value.sum - pay
  const sign = diff > 0 ? '+' : ''
  return `${sign}${diff.toFixed(2)}`
})

const diffTagSeverity = computed(() => {
  if (!currentPaymentForInvoices.value) return 'secondary'
  if (linkedInvoiceAmount.value.unknown > 0) return 'secondary'
  const pay = Number(currentPaymentForInvoices.value.amount || 0)
  const diffAbs = Math.abs(linkedInvoiceAmount.value.sum - pay)
  return diffAbs < 0.01 ? 'success' : 'warn'
})

const invoicePrimaryLabel = (inv: Invoice | null | undefined) => {
  if (!inv) return '-'
  const no = (inv.invoice_number || '').trim()
  if (no) return no
  const name = (inv.original_name || inv.filename || '').trim()
  return name || '-'
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
  invoiceMatchTab.value = 'suggested'
  await refreshSuggestedInvoices({ showToast: true })
}

const handleLinkInvoiceToPayment = async (invoiceId: string) => {
  if (!currentPaymentForInvoices.value) return
  try {
    linkingInvoiceToPayment.value = true
    await invoiceApi.linkPayment(invoiceId, currentPaymentForInvoices.value.id)
    toast.add({ severity: 'success', summary: '\u5173\u8054\u6210\u529F', life: 2000 })
    await viewLinkedInvoices(currentPaymentForInvoices.value)
    await loadPayments()
    void fetchUnlinkedInvoices()
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
    await loadPayments()
    void fetchUnlinkedInvoices()
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string } } }
    toast.add({ severity: 'error', summary: err.response?.data?.message || '\u53D6\u6D88\u5173\u8054\u5931\u8D25', life: 3500 })
  } finally {
    unlinkingInvoiceFromPayment.value = false
  }
}

const openPaymentDetail = (payment: Payment) => {
  detailPayment.value = payment
  paymentDetailEditing.value = false
  savingPaymentDetail.value = false
  paymentTimePanel.value?.hide?.()
  paymentTimeLastTarget.value = null
  paymentDetailTimeDraft.value = null
  paymentDetailForm.amount = Number(payment.amount || 0)
  paymentDetailForm.merchant = payment.merchant || ''
  paymentDetailForm.payment_method = normalizePaymentMethodText(payment.payment_method || '')
  paymentDetailForm.description = payment.description || ''
  paymentDetailForm.transaction_time = payment.transaction_time ? new Date(payment.transaction_time) : new Date()
  paymentDetailVisible.value = true
}

const formatDateTimeDraft = (date: Date | null) => {
  if (!date) return ''
  return dayjs(date).format('YYYY-MM-DD HH:mm')
}

const isPaymentTimePanelOpen = () => {
  const p = paymentTimePanel.value as any
  return !!p?.visible
}

const getPaymentTimeOverlayEl = (): HTMLElement | null => {
  const p = paymentTimePanel.value as any
  const container = p?.container as HTMLElement | undefined
  if (container) return container
  return (
    (document.querySelector('.p-popover.payment-time-panel') as HTMLElement | null) ||
    (document.querySelector('.p-overlaypanel.payment-time-panel') as HTMLElement | null)
  )
}

const forcePaymentTimeBelow = () => {
  if (typeof window === 'undefined') return
  const target = paymentTimeLastTarget.value
  if (!target) return

  const overlay = getPaymentTimeOverlayEl()
  if (!overlay) return

  const rect = target.getBoundingClientRect()
  const scrollX = window.scrollX || document.documentElement.scrollLeft || 0
  const scrollY = window.scrollY || document.documentElement.scrollTop || 0
  const gap = 6
  const top = rect.bottom + scrollY + gap

  const w = overlay.getBoundingClientRect().width || overlay.offsetWidth
  const minLeft = scrollX + 8
  const maxLeft = scrollX + window.innerWidth - w - 8
  const desiredLeft = rect.left + scrollX
  const left = Number.isFinite(w) && w > 0 ? Math.max(minLeft, Math.min(desiredLeft, maxLeft)) : desiredLeft

  overlay.style.top = ''
  overlay.style.bottom = ''
  overlay.style.left = ''
  overlay.style.right = ''
  overlay.style.insetBlockStart = `${top}px`
  overlay.style.insetBlockEnd = 'auto'
  overlay.style.insetInlineStart = `${left}px`
  overlay.style.insetInlineEnd = 'auto'

  const available = window.innerHeight - rect.bottom - gap - 16
  const content =
    (overlay.querySelector('.p-popover-content') as HTMLElement | null) ||
    (overlay.querySelector('.p-overlaypanel-content') as HTMLElement | null)
  if (content) {
    const maxH = Math.max(240, Math.floor(available))
    content.style.maxHeight = `${maxH}px`
    content.style.overflow = 'auto'
  }
}

const realignPaymentTimePanel = async () => {
  await nextTick()
  const p = paymentTimePanel.value
  if (!p) return
  if (!isPaymentTimePanelOpen()) return

  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    window.requestAnimationFrame(() => {
      p.alignOverlay()
      forcePaymentTimeBelow()
    })
    return
  }
  p.alignOverlay()
  forcePaymentTimeBelow()
}

const togglePaymentTimePanel = (event: MouseEvent) => {
  if (!paymentDetailEditing.value) return
  paymentTimeLastTarget.value = event.currentTarget as HTMLElement | null
  paymentDetailTimeDraft.value = paymentDetailForm.transaction_time ? new Date(paymentDetailForm.transaction_time) : new Date()
  paymentTimePanel.value?.toggle(event)
  void realignPaymentTimePanel()
}

const onPaymentTimePanelShow = async () => {
  await realignPaymentTimePanel()
  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    window.requestAnimationFrame(() => forcePaymentTimeBelow())
  } else {
    forcePaymentTimeBelow()
  }
}

const onPaymentTimePanelHide = () => {
  paymentTimeLastTarget.value = null
}

const cancelPaymentTimePanel = () => {
  paymentDetailTimeDraft.value = paymentDetailForm.transaction_time ? new Date(paymentDetailForm.transaction_time) : new Date()
  paymentTimePanel.value?.hide?.()
}

const confirmPaymentTimePanel = () => {
  if (!paymentDetailTimeDraft.value) return
  paymentDetailForm.transaction_time = new Date(paymentDetailTimeDraft.value)
  paymentTimePanel.value?.hide?.()
}

const onPaymentDetailHide = () => {
  paymentTimePanel.value?.hide?.()
  paymentTimeLastTarget.value = null
  paymentDetailTimeDraft.value = null
}

const markPaymentRegressionSample = async (id: string) => {
  if (!isAdmin.value) return
  if (!id) return
  markingRegressionSample.value = true
  try {
    const res = await regressionSamplesApi.markPayment(id)
    if (res.data.success) {
      const issues = (res.data as any)?.data?.issues as any[] | undefined
      const hasWarn = Array.isArray(issues) && issues.some((it) => String(it?.level || '').toLowerCase() === 'warn')
      toast.add({
        severity: hasWarn ? 'warn' : 'success',
        summary: hasWarn ? '\u5df2\u6807\u8bb0\u56de\u5f52\u6837\u672c\uff08\u6709\u8b66\u544a\uff09' : '\u5df2\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c',
        life: 2500,
      })
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '\u6807\u8bb0\u5931\u8d25', life: 3000 })
  } catch (e: any) {
    if (e?.response?.status === 422) {
      const issues = e?.response?.data?.data?.issues as any[] | undefined
      const ok = await confirmForceMarkRegressionSample(issues || [])
      if (!ok) return
      const res = await regressionSamplesApi.markPayment(id, { force: true })
      if (res.data.success) {
        toast.add({ severity: 'success', summary: '\u5df2\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c', life: 2500 })
        return
      }
    }
    toast.add({ severity: 'error', summary: e.response?.data?.message || '\u6807\u8bb0\u5931\u8d25', life: 3000 })
  } finally {
    markingRegressionSample.value = false
  }
}

const handlePaymentTimeViewportChange = () => {
  if (!isPaymentTimePanelOpen()) return
  void realignPaymentTimePanel()
}

const enterPaymentEditMode = () => {
  if (!detailPayment.value) return
  paymentDetailEditing.value = true
  paymentTimePanel.value?.hide?.()
  paymentTimeLastTarget.value = null
  paymentDetailTimeDraft.value = null
  paymentDetailForm.amount = Number(detailPayment.value.amount || 0)
  paymentDetailForm.merchant = detailPayment.value.merchant || ''
  paymentDetailForm.payment_method = normalizePaymentMethodText(detailPayment.value.payment_method || '')
  paymentDetailForm.description = detailPayment.value.description || ''
  paymentDetailForm.transaction_time = detailPayment.value.transaction_time ? new Date(detailPayment.value.transaction_time) : new Date()
}

const cancelPaymentEditMode = () => {
  if (!detailPayment.value) {
    paymentDetailEditing.value = false
    return
  }
  paymentTimePanel.value?.hide?.()
  paymentTimeLastTarget.value = null
  paymentDetailTimeDraft.value = null
  paymentDetailForm.amount = Number(detailPayment.value.amount || 0)
  paymentDetailForm.merchant = detailPayment.value.merchant || ''
  paymentDetailForm.payment_method = normalizePaymentMethodText(detailPayment.value.payment_method || '')
  paymentDetailForm.description = detailPayment.value.description || ''
  paymentDetailForm.transaction_time = detailPayment.value.transaction_time ? new Date(detailPayment.value.transaction_time) : new Date()
  paymentDetailEditing.value = false
}

const savePaymentEditMode = async () => {
  if (!detailPayment.value) return
  if (paymentDetailForm.amount === null || Number.isNaN(Number(paymentDetailForm.amount)) || Number(paymentDetailForm.amount) <= 0) {
    toast.add({ severity: 'warn', summary: '请填写金额', life: 2200 })
    return
  }
  if (!paymentDetailForm.transaction_time) {
    toast.add({ severity: 'warn', summary: '请选择交易时间', life: 2200 })
    return
  }

  savingPaymentDetail.value = true
  try {
    paymentTimePanel.value?.hide?.()
    const payload = {
      amount: Number(paymentDetailForm.amount),
      merchant: paymentDetailForm.merchant,
      payment_method: normalizePaymentMethodText(paymentDetailForm.payment_method),
      description: paymentDetailForm.description,
      transaction_time: dayjs(paymentDetailForm.transaction_time).toISOString(),
    }
    await paymentApi.update(detailPayment.value.id, payload)
    const refreshed = await paymentApi.getById(detailPayment.value.id)
    if (refreshed.data.success && refreshed.data.data) {
      detailPayment.value = refreshed.data.data
    } else {
      detailPayment.value = { ...detailPayment.value, ...payload } as Payment
    }
    toast.add({ severity: 'success', summary: '已保存', life: 2000 })
    paymentDetailEditing.value = false
    await loadPayments()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '保存失败', life: 3000 })
  } finally {
    savingPaymentDetail.value = false
  }
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

const getExtractedPrettyText = (extractedData: string | null): string => {
  if (!extractedData) return ''
  try {
    const data = JSON.parse(extractedData)
    return data.pretty_text || ''
  } catch {
    return ''
  }
}

const getPaymentScreenshotTitle = (payment: Payment) => {
  const path = payment.screenshot_path || ''
  if (path) {
    const parts = path.split('/')
    return parts[parts.length - 1] || path
  }
  return normalizeInlineText(payment.merchant) || payment.id
}

const handleReparseOcr = async (paymentId: string) => {
  reparsingOcr.value = true
  try {
    const res = await paymentApi.reparseScreenshot(paymentId)
    if (res.data.success) {
      toast.add({ severity: 'success', summary: '重新解析成功', life: 2000 })
      notifications.add({ severity: 'success', title: '支付截图已重新解析', detail: paymentId })
      const detailRes = await paymentApi.getById(paymentId)
      if (detailRes.data.success && detailRes.data.data) {
        // 刷新当前详情数据，避免重新打开才能看到最新字段
        detailPayment.value = detailRes.data.data
      }
      await loadPayments()
    }
  } catch (error: unknown) {
    const err = error as { response?: { data?: { message?: string; error?: string } } }
    const message = err.response?.data?.message || '重新解析失败'
    const detail = err.response?.data?.error
    toast.add({ severity: 'error', summary: detail ? `${message}：${detail}` : message, life: 5000 })
    notifications.add({ severity: 'error', title: '支付截图重新解析失败', detail: detail || message })
  } finally {
    reparsingOcr.value = false
  }
}

const tryOpenMatchFromRoute = async () => {
  const match = route.query.match
  if (typeof match !== 'string' || !match) return
  if (linkedInvoicesModalVisible.value && currentPaymentForInvoices.value?.id === match) return

  try {
    const local = payments.value.find((p) => p.id === match)
    if (local) {
      await viewLinkedInvoices(local)
      return
    }
    const res = await paymentApi.getById(match)
    if (res.data.success && res.data.data) await viewLinkedInvoices(res.data.data)
  } finally {
    const query = { ...route.query }
    delete (query as any).match
    router.replace({ query })
  }
}

onMounted(() => {
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', handlePaymentTimeViewportChange, { passive: true })
    window.addEventListener('orientationchange', handlePaymentTimeViewportChange, { passive: true } as any)
    window.addEventListener('scroll', handlePaymentTimeViewportChange, true)
  }
  void (async () => {
    await cleanupPendingPaymentDraftOnLoad()
    await loadPayments()
    await loadStats()
    await tryOpenMatchFromRoute()
  })()
})

onBeforeUnmount(() => {
  if (typeof window === 'undefined') return
  window.removeEventListener('resize', handlePaymentTimeViewportChange as any)
  window.removeEventListener('orientationchange', handlePaymentTimeViewportChange as any)
  window.removeEventListener('scroll', handlePaymentTimeViewportChange as any, true)
})

watch(
  () => route.query.match,
  () => {
    void tryOpenMatchFromRoute()
  }
)
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
  line-height: 1.6;
  padding-left: 4px;
  overflow: visible;
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
  justify-content: flex-start;
}

.payments-table :deep(.p-datatable-table) {
  width: 100%;
  table-layout: fixed;
}

.payments-table :deep(.p-datatable-thead > tr > th),
.payments-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

@media (max-width: 768px) {
  .payments-table :deep(.p-datatable-table) {
    width: max-content;
    min-width: 100%;
  }
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
  line-height: 1.5;
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
  border: 1px solid color-mix(in srgb, var(--p-surface-200), transparent 35%);
  background: color-mix(in srgb, var(--p-surface-0), transparent 10%);
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
  background: color-mix(in srgb, var(--p-surface-0), transparent 8%);
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

.match-summary {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--p-content-border-color);
  background: var(--p-content-background);
  border-radius: 10px;
  margin-top: 10px;
  margin-bottom: 12px;
}

.match-summary-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.match-summary-label {
  font-size: 12px;
  color: var(--text-color-secondary, var(--p-text-muted-color));
}

.match-summary-value {
  font-weight: 700;
  color: var(--p-text-color);
}

.muted {
  color: var(--text-color-secondary, var(--p-text-muted-color));
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

.match-subtitle {
  margin-top: 14px;
  margin-bottom: 6px;
  font-weight: 800;
  color: var(--p-text-color);
}

.match-table-secondary {
  margin-top: 6px;
}

.no-data-secondary {
  margin-top: 6px;
}

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
.kv :deep(.p-inputtextarea) {
  width: 100%;
}

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

.detail :deep(.p-divider.p-divider-horizontal) {
  margin: 10px 0;
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
  padding-top: 0;
  padding-bottom: 1px;
}

.v {
  margin-top: 6px;
  font-weight: 700;
  color: var(--color-text-primary);
  line-height: 1.6;
}

.section {
  margin-top: 10px;
}

.section-title {
  font-weight: 900;
  color: var(--color-text-primary);
  margin-bottom: 6px;
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
  width: 100%;
  height: auto;
  max-height: 320px;
  object-fit: contain;
  border-radius: var(--radius-md);
}

.sbm-ocr-hint {
  display: block;
  color: var(--text-color-secondary);
  margin-top: 2px;
}

.sbm-ocr-hint-low {
  color: #d97706;
  font-weight: 800;
}

.sbm-ocr-hint-mid {
  color: var(--color-text-secondary);
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
  /* PrimeVue 默认会给 time picker 加一条上边框；横向布局时会变成你截图里的“黑线” */
  border-block-start: 0 none !important;
  border-top: 0 none !important;
  border-left: 0 none !important;
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

.payment-time-panel-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 10px;
}
</style>




