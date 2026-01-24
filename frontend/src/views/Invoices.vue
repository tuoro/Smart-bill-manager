<template>
  <div class="page">
    <div class="grid">
      <div class="col-12 md:col-4">
        <Card class="sbm-surface">
          <template #content>
            <div class="stat">
              <div>
                <div class="stat-title">&#21457;&#31080;&#24635;&#25968;</div>
                <div class="stat-value">{{ displayStats.totalCount }}</div>
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
                <div class="stat-value">{{ `\u00A5${displayStats.totalAmount.toFixed(2)}` }}</div>
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
                  <Tag severity="secondary" :value="`\u5176\u4ED6 ${otherSourceCount}`" />
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
              :disabled="!batchDeleteMode || selectedInvoices.length === 0"
              @click="onBulkDeleteClick"
            />
            <DatePicker
              v-model="dateRange"
              selectionMode="range"
              :manualInput="false"
              dateFormat="yy-mm-dd"
              :placeholder="'开票日期范围'"
              @update:modelValue="handleDateChange"
            />
            <Button :label="'\u4E0A\u4F20\u53D1\u7968'" icon="pi pi-upload" @click="openUploadModal" />
          </div>
        </div>
      </template>
      <template #content>
        <DataTable
          class="invoices-table sbm-dt-fixed"
          :value="invoices"
          :loading="loading"
          :paginator="true"
          :lazy="true"
          :rows="pageSize"
          :first="first"
          :totalRecords="totalRecords"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          responsiveLayout="scroll"
          sortField="created_at"
          :sortOrder="-1"
          dataKey="id"
          v-model:selection="selectedInvoices"
          @page="onPage"
        >
          <Column v-if="batchDeleteMode" selectionMode="multiple" :style="{ width: '4%' }" />
          <Column field="original_name" :header="'\u6587\u4EF6\u540D'" :style="{ width: '16%' }">
            <template #body="{ data: row }">
              <div class="filecell">
                <i class="pi pi-file" />
                <span class="filecell-name sbm-ellipsis" :title="row.original_name">{{ row.original_name }}</span>
              </div>
            </template>
          </Column>
          <Column field="invoice_number" :header="'\u53D1\u7968\u53F7'" :style="{ width: '16%' }">
            <template #body="{ data: row }">{{ row.invoice_number || '-' }}</template>
          </Column>
          <Column field="invoice_date" :header="'\u5F00\u7968\u65F6\u95F4'" sortable :style="{ width: '10%' }">
            <template #body="{ data: row }">{{ formatInvoiceDate(row.invoice_date) }}</template>
          </Column>
          <Column :header="'\u91D1\u989D'" :style="{ width: '10%' }">
            <template #body="{ data: row }">{{ row.amount ? `\u00A5${row.amount.toFixed(2)}` : '-' }}</template>
          </Column>
          <Column field="seller_name" :header="'\u9500\u552E\u65B9'" :style="{ width: '18%' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="row.seller_name || ''">{{ row.seller_name || '-' }}</span>
            </template>
          </Column>
          <Column :header="'\u6765\u6E90'" :style="{ width: '8%' }">
            <template #body="{ data: row }">
              <Tag :severity="getSourceSeverity(row.source)" :value="getSourceLabel(row.source)" />
            </template>
          </Column>
          <Column field="created_at" :header="'\u4E0A\u4F20\u65F6\u95F4'" sortable :style="{ width: '10%' }">
            <template #body="{ data: row }">{{ formatDateTime(row.created_at) }}</template>
          </Column>
          <Column :header="'\u64CD\u4F5C'" :style="{ width: '8%' }">
            <template #body="{ data: row }">
              <div class="row-actions">
                <Button class="p-button-text" icon="pi pi-eye" @click="openPreview(row)" />
                <Button class="p-button-text p-button-danger" icon="pi pi-trash" @click="confirmDelete(row.id)" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog
      v-model:visible="uploadModalVisible"
      modal
      :header="'\u4E0A\u4F20\u53D1\u7968'"
      :style="{ width: uploadedInvoiceId ? '1060px' : '880px', maxWidth: '96vw' }"
      :closable="!uploading && !savingUploadOcr"
    >
      <div class="upload-screenshot-layout">
        <div class="upload-box sbm-dropzone" @click="triggerInvoiceChoose" @dragenter.prevent @dragover.prevent @drop.prevent="onInvoiceDrop">
          <div class="sbm-dropzone-hero">
            <i class="pi pi-cloud-upload" />
            <div class="sbm-dropzone-title">拖拽文件到此处，或者点击选择</div>
            <div class="sbm-dropzone-sub">支持 PDF/PNG/JPG，可多选（最多 10 个），最大 20MB</div>
            <Button type="button" icon="pi pi-plus" :label="'\u9009\u62E9\u6587\u4EF6'" :disabled="uploading || savingUploadOcr" @click.stop="chooseInvoiceFiles" />
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
              <Button
                class="file-row-remove p-button-text"
                severity="secondary"
                icon="pi pi-times"
                aria-label="Remove"
                :disabled="uploading || savingUploadOcr"
                @click="removeSelectedFile(idx)"
              />
            </div>
            <div class="file-hint">已选择 {{ selectedFiles.length }} 个文件</div>
          </div>
        </div>

        <Message v-if="!uploadedInvoiceId" severity="info" :closable="false">
          请选择文件，点击“上传”解析后可在下方修改识别结果。
        </Message>
        <Message v-else-if="uploadedInvoiceId && !uploadOcrResult" severity="warn" :closable="false">
          已上传，但未解析出可用的 OCR 摘要（可能仍在解析中或解析失败）。你仍可手动填写后保存，也可以稍后在发票详情里点击“重新解析”。
        </Message>
        
        <Message v-if="uploadDedup?.kind === 'suspected_duplicate'" severity="warn" :closable="false">
          检测到疑似重复发票（发票号码重复）。如果确认需要保留，可点击保存后选择“仍然保存”。
        </Message>

        <div v-if="uploadedInvoiceId" class="upload-invoice-layout">
          <div class="upload-invoice-left">
            <div class="invoice-file-preview">
              <div class="raw-title">发票预览</div>
              <div class="invoice-file-box">
                <template v-if="uploadedInvoice?.file_path && uploadedInvoiceFileSrc">
                  <Image
                    v-if="isInvoiceImageFile(uploadedInvoice.file_path)"
                    class="invoice-image"
                    :src="uploadedInvoiceFileSrc"
                    preview
                    :imageStyle="{ width: '100%', maxWidth: '100%', height: 'auto' }"
                  />
                  <iframe
                    v-else-if="isInvoicePdfFile(uploadedInvoice.file_path)"
                    class="invoice-pdf"
                    :src="uploadedInvoiceFileSrc"
                    loading="lazy"
                    title="Invoice PDF Preview"
                  />
                  <Message v-else severity="secondary" :closable="false">该文件暂不支持预览，请点击“查看原文件”。</Message>
                </template>
                <Message v-else severity="secondary" :closable="false">暂无可预览的发票文件。</Message>
              </div>
            </div>
          </div>

          <div class="upload-invoice-right">
            <form class="p-fluid ocr-form" @submit.prevent="handleSaveUploadedInvoice">
              <div class="grid">
                <div class="col-12 md:col-6 field">
                  <label for="inv_num">发票号码</label>
                  <InputText id="inv_num" v-model.trim="uploadOcrForm.invoice_number" />
                  <small
                    v-if="isAdmin && (uploadOcrResult?.invoice_number_source || uploadOcrResult?.invoice_number_confidence)"
                    class="ocr-hint"
                    :class="confidenceClass(uploadOcrResult?.invoice_number_confidence)"
                  >
                    来源：{{ formatSourceLabel(uploadOcrResult?.invoice_number_source) || '\u672a\u8bc6\u522b' }}
                    <span v-if="uploadOcrResult?.invoice_number_confidence">（置信度：{{ confidenceLabel(uploadOcrResult?.invoice_number_confidence) }}）</span>
                  </small>
                </div>
                <div class="col-12 md:col-6 field">
                  <label for="inv_date">开票日期</label>
                  <DatePicker id="inv_date" v-model="uploadOcrForm.invoice_date" :manualInput="false" dateFormat="yy-mm-dd" :placeholder="'开票日期'" />
                  <small
                    v-if="isAdmin && (uploadOcrResult?.invoice_date_source || uploadOcrResult?.invoice_date_confidence)"
                    class="ocr-hint"
                    :class="confidenceClass(uploadOcrResult?.invoice_date_confidence)"
                  >
                    来源：{{ formatSourceLabel(uploadOcrResult?.invoice_date_source) || '\u672a\u8bc6\u522b' }}
                    <span v-if="uploadOcrResult?.invoice_date_confidence">（置信度：{{ confidenceLabel(uploadOcrResult?.invoice_date_confidence) }}）</span>
                  </small>
                </div>

                <div class="col-12 md:col-6 field">
                  <label for="inv_amount">价税合计</label>
                  <InputNumber id="inv_amount" v-model="uploadOcrForm.amount" :minFractionDigits="2" :maxFractionDigits="2" :min="0" :useGrouping="false" />
                  <small
                    v-if="isAdmin && (uploadOcrResult?.amount_source || uploadOcrResult?.amount_confidence)"
                    class="ocr-hint"
                    :class="confidenceClass(uploadOcrResult?.amount_confidence)"
                  >
                    来源：{{ formatSourceLabel(uploadOcrResult?.amount_source) || '\u672a\u8bc6\u522b' }}
                    <span v-if="uploadOcrResult?.amount_confidence">（置信度：{{ confidenceLabel(uploadOcrResult?.amount_confidence) }}）</span>
                  </small>
                </div>
                <div class="col-12 md:col-6 field">
                  <label for="inv_seller">销售方</label>
                  <InputText id="inv_seller" v-model.trim="uploadOcrForm.seller_name" />
                  <small
                    v-if="isAdmin && (uploadOcrResult?.seller_name_source || uploadOcrResult?.seller_name_confidence)"
                    class="ocr-hint"
                    :class="confidenceClass(uploadOcrResult?.seller_name_confidence)"
                  >
                    来源：{{ formatSourceLabel(uploadOcrResult?.seller_name_source) || '\u672a\u8bc6\u522b' }}
                    <span v-if="uploadOcrResult?.seller_name_confidence">（置信度：{{ confidenceLabel(uploadOcrResult?.seller_name_confidence) }}）</span>
                  </small>
                </div>
                <div class="col-12 md:col-6 field">
                  <label for="inv_buyer">购买方</label>
                  <InputText id="inv_buyer" v-model.trim="uploadOcrForm.buyer_name" />
                  <small
                    v-if="isAdmin && (uploadOcrResult?.buyer_name_source || uploadOcrResult?.buyer_name_confidence)"
                    class="ocr-hint"
                    :class="confidenceClass(uploadOcrResult?.buyer_name_confidence)"
                  >
                    来源：{{ formatSourceLabel(uploadOcrResult?.buyer_name_source) || '\u672a\u8bc6\u522b' }}
                    <span v-if="uploadOcrResult?.buyer_name_confidence">（置信度：{{ confidenceLabel(uploadOcrResult?.buyer_name_confidence) }}）</span>
                  </small>
                </div>
              </div>
            </form>
          </div>
        </div>

        <div v-if="isAdmin && uploadedInvoice && (getInvoiceRawText(uploadedInvoice) || getInvoicePrettyText(uploadedInvoice))" class="raw-section">
          <div class="raw-title">
            OCR 文本
            <span v-if="getInvoiceRawTextSource(uploadedInvoice)" class="raw-source">（来源：{{ getInvoiceRawTextSource(uploadedInvoice) }}）</span>
          </div>
          <Accordion>
            <AccordionTab v-if="getInvoicePrettyText(uploadedInvoice)" :header="'点击查看 OCR 整理版文本'">
              <pre class="raw-text">{{ getInvoicePrettyText(uploadedInvoice) }}</pre>
            </AccordionTab>
            <AccordionTab v-if="getInvoiceRawText(uploadedInvoice)" :header="'点击查看 OCR 原始文本'">
              <pre class="raw-text">{{ getInvoiceRawText(uploadedInvoice) }}</pre>
            </AccordionTab>
          </Accordion>
        </div>
      </div>

      <template #footer>
        <div class="dialog-footer-center">
          <Button
            type="button"
            class="p-button-outlined"
            severity="secondary"
            :label="'\u53D6\u6D88'"
            :disabled="uploading || savingUploadOcr"
            @click="handleCancelUpload"
          />
          <Button
            v-if="!uploadedInvoiceId"
            type="button"
            :label="'\u4E0A\u4F20'"
            icon="pi pi-check"
            :loading="uploading"
            :disabled="uploading || selectedFiles.length === 0"
            @click="handleUpload"
          />
          <Button v-else type="button" :label="'\u4FDD\u5B58'" icon="pi pi-check" :loading="savingUploadOcr" @click="handleSaveUploadedInvoice" />
        </div>
      </template>
    </Dialog>

    <Dialog
      v-model:visible="previewVisible"
      modal
      :header="'\u53D1\u7968\u8BE6\u60C5'"
      :style="{ width: previewInvoice?.file_path ? '1060px' : '860px', maxWidth: '94vw' }"
      :breakpoints="{ '960px': '94vw', '640px': '96vw' }"
      :contentStyle="{ padding: '14px 16px' }"
      :closable="!invoiceDetailEditing && !savingInvoiceDetail"
      :closeOnEscape="!invoiceDetailEditing && !savingInvoiceDetail"
    >
      <div v-if="previewInvoice" class="preview">
        <div class="header-row">
          <div class="title">
            <i class="pi pi-file" />
            <span>{{ previewInvoice.original_name }}</span>
          </div>
          <div class="actions">
            <Button
              v-if="isAdmin && !invoiceDetailEditing"
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-verified"
              :label="'\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c'"
              :loading="markingRegressionSample"
              @click="markInvoiceRegressionSample(previewInvoice.id)"
            />
            <Button
              v-if="!invoiceDetailEditing"
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-pencil"
              :label="'\u7F16\u8F91'"
              @click="enterInvoiceEditMode"
            />
            <Button class="p-button-outlined" severity="secondary" icon="pi pi-external-link" :label="'\u67E5\u770B\u539F\u6587\u4EF6'" @click="downloadFile(previewInvoice)" />
            <Button
              class="p-button-outlined"
              severity="secondary"
              icon="pi pi-refresh"
              :label="'\u91CD\u65B0\u89E3\u6790'"
              :loading="parseStatusPending"
              :disabled="invoiceDetailEditing || savingInvoiceDetail"
              @click="handleReparse(previewInvoice.id)"
            />
          </div>
        </div>

        <div class="invoice-detail-layout">
          <div class="invoice-detail-left">
            <div class="invoice-file-preview">
              <div class="raw-title">发票原件</div>
              <div class="invoice-file-box">
                <template v-if="previewInvoice.file_path && previewInvoiceFileSrc">
                  <Image
                    v-if="isInvoiceImageFile(previewInvoice.file_path)"
                    class="invoice-image"
                    :src="previewInvoiceFileSrc"
                    preview
                    :imageStyle="{ width: '100%', maxWidth: '100%', height: 'auto' }"
                  />
                  <iframe
                    v-else-if="isInvoicePdfFile(previewInvoice.file_path)"
                    class="invoice-pdf"
                    :src="previewInvoiceFileSrc"
                    loading="lazy"
                    title="Invoice PDF Preview"
                  />
                  <Message v-else severity="secondary" :closable="false">该文件暂不支持预览，请点击“查看原文件”。</Message>
                </template>
                <Message v-else severity="secondary" :closable="false">暂无可预览的发票文件。</Message>
              </div>
            </div>

            <div v-if="previewInvoice" class="invoice-attachments">
              <div class="raw-title">附件</div>
              <div class="invoice-attachments-box">
                <div v-if="invoiceDetailEditing" class="invoice-attachments-toolbar">
                  <Button
                    class="p-button-outlined p-button-sm"
                    icon="pi pi-paperclip"
                    :label="'添加附件'"
                    :loading="uploadingInvoiceAttachment"
                    :disabled="savingInvoiceDetail || uploadingInvoiceAttachment"
                    @click="chooseInvoiceAttachmentFiles"
                  />
                  <input
                    ref="invoiceAttachmentInput"
                    class="sbm-file-input-hidden"
                    type="file"
                    accept="application/pdf,image/png,image/jpeg"
                    multiple
                    @change="onInvoiceAttachmentInputChange"
                  />
                </div>

                <Message v-if="!previewInvoice.attachments?.length" severity="secondary" :closable="false">暂无附件</Message>

                <div v-for="a in previewInvoice.attachments" :key="a.id" class="invoice-attachment-row">
                  <div class="invoice-attachment-name">
                    <i class="pi pi-paperclip" />
                    <span class="sbm-ellipsis" :title="a.original_name || a.filename">{{ a.original_name || a.filename }}</span>
                  </div>
                  <div class="invoice-attachment-actions">
                    <Tag v-if="a.kind" severity="secondary" :value="formatAttachmentKind(a.kind)" />
                    <Button
                      class="p-button-outlined p-button-sm"
                      severity="secondary"
                      icon="pi pi-download"
                      :label="'下载'"
                      :disabled="uploadingInvoiceAttachment"
                      @click="downloadAttachment(previewInvoice, a)"
                    />
                    <Button
                      v-if="invoiceDetailEditing"
                      class="p-button-text p-button-sm"
                      severity="danger"
                      icon="pi pi-trash"
                      aria-label="Delete"
                      :disabled="savingInvoiceDetail || uploadingInvoiceAttachment"
                      @click="confirmDeleteAttachment(previewInvoice, a)"
                    />
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div class="invoice-detail-right">
            <div class="grid sbm-grid-tight">
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">发票号</div>
              <div class="v">
                <InputText v-if="invoiceDetailEditing" v-model.trim="invoiceDetailForm.invoice_number" />
                <template v-else>{{ previewInvoice.invoice_number || '-' }}</template>
              </div>
            </div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">开票时间</div>
              <div class="v">
                <DatePicker
                  v-if="invoiceDetailEditing"
                  v-model="invoiceDetailForm.invoice_date"
                  :manualInput="false"
                  dateFormat="yy-mm-dd"
                  :placeholder="'开票日期'"
                />
                <template v-else>{{ formatInvoiceDate(previewInvoice.invoice_date) }}</template>
              </div>
            </div>
          </div>
          <div class="col-12 md:col-6">
            <div class="kv">
              <div class="k">金额</div>
              <div class="v" :class="{ money: !invoiceDetailEditing }">
                <InputNumber
                  v-if="invoiceDetailEditing"
                  v-model="invoiceDetailForm.amount"
                  :minFractionDigits="2"
                  :maxFractionDigits="2"
                  :min="0"
                  :useGrouping="false"
                />
                <template v-else>{{ previewInvoice.amount ? `\u00A5${previewInvoice.amount.toFixed(2)}` : '-' }}</template>
              </div>
            </div>
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
            <div class="kv">
              <div class="k">销售方</div>
              <div class="v">
                <InputText v-if="invoiceDetailEditing" v-model.trim="invoiceDetailForm.seller_name" />
                <template v-else>{{ previewInvoice.seller_name || '-' }}</template>
              </div>
            </div>
          </div>
          <div class="col-12">
            <div class="kv">
              <div class="k">购买方</div>
              <div class="v">
                <InputText v-if="invoiceDetailEditing" v-model.trim="invoiceDetailForm.buyer_name" />
                <template v-else>{{ previewInvoice.buyer_name || '-' }}</template>
              </div>
            </div>
          </div>
            </div>
          </div>
        </div>

        <div v-if="getInvoiceItems(previewInvoice).length" class="items-section">
          <div class="items-title">&#21830;&#21697;&#26126;&#32454;</div>
          <DataTable class="items-table" :value="getInvoiceItems(previewInvoice)" responsiveLayout="scroll">
            <Column field="name" :header="'\u5546\u54C1\u540D\u79F0'" :style="{ width: '48%' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.name">{{ row.name }}</span>
              </template>
            </Column>
            <Column field="spec" :header="'\u89C4\u683C\u578B\u53F7'" :style="{ width: '32%' }">
              <template #body="{ data: row }">
                <span class="sbm-ellipsis" :title="row.spec || '-'">{{ row.spec || '-' }}</span>
              </template>
            </Column>
            <Column field="unit" :header="'\u5355\u4F4D'" :style="{ width: '10%' }">
              <template #body="{ data: row }">{{ row.unit || '-' }}</template>
            </Column>
            <Column field="quantity" :header="'\u6570\u91CF'" :style="{ width: '10%' }">
              <template #body="{ data: row }">{{ formatItemQuantity(row.quantity) }}</template>
            </Column>
          </DataTable>
        </div>

        <Divider />

        <div class="match-header">
          <div class="match-title">已关联支付记录 ({{ linkedPayments.length }})</div>
          <small class="muted">关联/取消关联请在支付记录页面操作</small>
        </div>

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
        </DataTable>

        <div v-if="!loadingLinkedPayments && linkedPayments.length === 0" class="no-data">
          <i class="pi pi-info-circle" />
          <span>暂无关联</span>
        </div>

        <Divider />

        <Accordion v-if="getInvoiceExtracted(previewInvoice)" class="ocr-fields">
          <AccordionTab header="OCR 摘要">
            <div class="field-row">
              <span>发票号码</span>
              <span>
                {{ getInvoiceExtracted(previewInvoice)?.invoice_number || '-' }}
                <small
                  v-if="getInvoiceExtracted(previewInvoice)?.invoice_number_source || getInvoiceExtracted(previewInvoice)?.invoice_number_confidence"
                  class="ocr-hint"
                  :class="confidenceClass(getInvoiceExtracted(previewInvoice)?.invoice_number_confidence)"
                >
                  来源：{{ formatSourceLabel(getInvoiceExtracted(previewInvoice)?.invoice_number_source) || '未识别' }}
                  <span v-if="getInvoiceExtracted(previewInvoice)?.invoice_number_confidence">
                    （置信度：{{ confidenceLabel(getInvoiceExtracted(previewInvoice)?.invoice_number_confidence) }}）
                  </span>
                </small>
              </span>
            </div>
            <div class="field-row">
              <span>开票日期</span>
              <span>
                {{ getInvoiceExtracted(previewInvoice)?.invoice_date || '-' }}
                <small
                  v-if="getInvoiceExtracted(previewInvoice)?.invoice_date_source || getInvoiceExtracted(previewInvoice)?.invoice_date_confidence"
                  class="ocr-hint"
                  :class="confidenceClass(getInvoiceExtracted(previewInvoice)?.invoice_date_confidence)"
                >
                  来源：{{ formatSourceLabel(getInvoiceExtracted(previewInvoice)?.invoice_date_source) || '未识别' }}
                  <span v-if="getInvoiceExtracted(previewInvoice)?.invoice_date_confidence">
                    （置信度：{{ confidenceLabel(getInvoiceExtracted(previewInvoice)?.invoice_date_confidence) }}）
                  </span>
                </small>
              </span>
            </div>
            <div class="field-row">
              <span>价税合计</span>
              <span>
                {{ getInvoiceExtracted(previewInvoice)?.amount ?? '-' }}
                <small
                  v-if="getInvoiceExtracted(previewInvoice)?.amount_source || getInvoiceExtracted(previewInvoice)?.amount_confidence"
                  class="ocr-hint"
                  :class="confidenceClass(getInvoiceExtracted(previewInvoice)?.amount_confidence)"
                >
                  来源：{{ formatSourceLabel(getInvoiceExtracted(previewInvoice)?.amount_source) || '未识别' }}
                  <span v-if="getInvoiceExtracted(previewInvoice)?.amount_confidence">
                    （置信度：{{ confidenceLabel(getInvoiceExtracted(previewInvoice)?.amount_confidence) }}）
                  </span>
                </small>
              </span>
            </div>
            <div class="field-row">
              <span>销售方</span>
              <span>
                {{ getInvoiceExtracted(previewInvoice)?.seller_name || '-' }}
                <small
                  v-if="getInvoiceExtracted(previewInvoice)?.seller_name_source || getInvoiceExtracted(previewInvoice)?.seller_name_confidence"
                  class="ocr-hint"
                  :class="confidenceClass(getInvoiceExtracted(previewInvoice)?.seller_name_confidence)"
                >
                  来源：{{ formatSourceLabel(getInvoiceExtracted(previewInvoice)?.seller_name_source) || '未识别' }}
                  <span v-if="getInvoiceExtracted(previewInvoice)?.seller_name_confidence">
                    （置信度：{{ confidenceLabel(getInvoiceExtracted(previewInvoice)?.seller_name_confidence) }}）
                  </span>
                </small>
              </span>
            </div>
            <div class="field-row">
              <span>购买方</span>
              <span>
                {{ getInvoiceExtracted(previewInvoice)?.buyer_name || '-' }}
                <small
                  v-if="getInvoiceExtracted(previewInvoice)?.buyer_name_source || getInvoiceExtracted(previewInvoice)?.buyer_name_confidence"
                  class="ocr-hint"
                  :class="confidenceClass(getInvoiceExtracted(previewInvoice)?.buyer_name_confidence)"
                >
                  来源：{{ formatSourceLabel(getInvoiceExtracted(previewInvoice)?.buyer_name_source) || '未识别' }}
                  <span v-if="getInvoiceExtracted(previewInvoice)?.buyer_name_confidence">
                    （置信度：{{ confidenceLabel(getInvoiceExtracted(previewInvoice)?.buyer_name_confidence) }}）
                  </span>
                </small>
              </span>
            </div>
          </AccordionTab>
        </Accordion>

        <div v-if="getInvoiceRawText(previewInvoice) || getInvoicePrettyText(previewInvoice)" class="raw-section">
          <div class="raw-title">
            OCR &#25991;&#26412;
            <span v-if="getInvoiceRawTextSource(previewInvoice)" class="raw-source">（&#26469;&#28304;&#65306;{{ getInvoiceRawTextSource(previewInvoice) }}）</span>
          </div>
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
        <div v-if="invoiceDetailEditing" class="dialog-footer-center">
          <Button type="button" class="p-button-outlined" severity="secondary" :label="'\u53D6\u6D88'" :disabled="savingInvoiceDetail" @click="cancelInvoiceEditMode" />
          <Button type="button" :label="'\u4FDD\u5B58'" icon="pi pi-check" :loading="savingInvoiceDetail" @click="saveInvoiceEditMode" />
        </div>
        <Button v-else type="button" class="p-button-outlined" severity="secondary" :label="'\u5173\u95ED'" @click="previewVisible = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import dayjs from 'dayjs'
import Accordion from 'primevue/accordion'
import AccordionTab from 'primevue/accordiontab'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import Divider from 'primevue/divider'
import DatePicker from 'primevue/datepicker'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Image from 'primevue/image'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { invoiceApi, tasksApi, regressionSamplesApi } from '@/api'
import { useNotificationStore } from '@/stores/notifications'
import { useAuthStore } from '@/stores/auth'
import { debounce } from '@/utils/debounce'
import { getApiErrorMessage, isRequestCanceled } from '@/utils/http'
import type { Invoice, Payment, DedupHint, InvoiceAttachment } from '@/types'

interface InvoiceExtractedData {
  invoice_number?: string
  invoice_number_source?: string
  invoice_number_confidence?: number
  invoice_date?: string
  invoice_date_source?: string
  invoice_date_confidence?: number
  amount?: number
  amount_source?: string
  amount_confidence?: number
  tax_amount?: number
  tax_amount_source?: string
  tax_amount_confidence?: number
  seller_name?: string
  seller_name_source?: string
  seller_name_confidence?: number
  buyer_name?: string
  buyer_name_source?: string
  buyer_name_confidence?: number
  raw_text?: string
  raw_text_source?: string
  pretty_text?: string
}

const formatSourceLabel = (src?: string) => {
  if (!src) return ''
  const map: Record<string, string> = {
    label: '标签',
    standalone: '独立行匹配',
    spaced_label: '空格分隔标签',
    tax_total_label: '价税合计标签',
    chinese_amount: '大写金额附近',
    standalone_amount: '独立金额',
    max_currency: '最大金额',
    tax_label: '税额标签',
    position: '版面位置',
    buyer_label: '购买方标签',
    buyer_section: '购买方区块',
    buyer_individual: '个人',
    seller_label: '销售方标签',
    seller_section: '销售方区块',
  }
  return map[src] || src
}

const confidenceLabel = (c?: number) => {
  if (c === undefined || c === null) return ''
  if (c >= 0.8) return '高'
  if (c >= 0.6) return '中'
  return '低'
}

const confidenceClass = (c?: number) => {
  const label = confidenceLabel(c)
  if (label === '低') return 'ocr-hint-low'
  if (label === '中') return 'ocr-hint-mid'
  return ''
}

const isInvoiceImageFile = (p?: string) => /\.(png|jpe?g|gif|webp|bmp)$/i.test(String(p || ''))
const isInvoicePdfFile = (p?: string) => /\.pdf$/i.test(String(p || ''))
const uploadedInvoiceFileSrc = ref<string>('')
const previewInvoiceFileSrc = ref<string>('')

const revokeObjectUrl = (url: string) => {
  if (typeof window === 'undefined') return
  const u = String(url || '')
  if (u) URL.revokeObjectURL(u)
}

const loadInvoiceFileBlobUrl = async (invoiceId: string) => {
  if (typeof window === 'undefined') return ''
  const res = await invoiceApi.getFileBlob(invoiceId)
  return URL.createObjectURL(res.data as Blob)
}

const toast = useToast()
const notifications = useNotificationStore()
const confirm = useConfirm()
const authStore = useAuthStore()
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
const invoices = ref<Invoice[]>([])
const selectedInvoices = ref<Invoice[]>([])
const batchDeleteMode = ref(false)
const pageSize = ref(10)
const first = ref(0)
const totalRecords = ref(0)
const dateRange = ref<Date[] | null>(null)

const stats = ref<{ totalCount: number; totalAmount: number; bySource: Record<string, number> } | null>(null)

const uploadModalVisible = ref(false)
const uploading = ref(false)
const savingUploadOcr = ref(false)
const selectedFiles = ref<File[]>([])
const invoiceInput = ref<HTMLInputElement | null>(null)
const uploadedInvoiceIds = ref<string[]>([])
const uploadedInvoiceId = ref<string | null>(null)
const uploadedInvoice = ref<Invoice | null>(null)

watch(
  () => uploadedInvoice.value?.id,
  async (id) => {
    revokeObjectUrl(uploadedInvoiceFileSrc.value)
    uploadedInvoiceFileSrc.value = ''
    if (!id) return
    try {
      uploadedInvoiceFileSrc.value = await loadInvoiceFileBlobUrl(id)
    } catch (err) {
      console.warn('Load uploaded invoice file failed:', err)
    }
  },
)
const uploadOcrResult = ref<InvoiceExtractedData | null>(null)
const uploadDedup = ref<DedupHint | null>(null)
const uploadConfirmed = ref(false)
const uploadOcrForm = reactive({
  invoice_number: '',
  invoice_date: null as Date | null,
  amount: null as number | null,
  seller_name: '',
  buyer_name: '',
})

const PENDING_INVOICE_DRAFT_KEY = 'sbm_pending_invoice_upload_draft'

const rememberPendingInvoiceDraft = (ids: string[]) => {
  if (typeof window === 'undefined') return
  const uniq = Array.from(new Set(ids.filter(Boolean)))
  window.localStorage.setItem(PENDING_INVOICE_DRAFT_KEY, JSON.stringify({ ids: uniq, ts: Date.now() }))
}

const clearPendingInvoiceDraft = () => {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(PENDING_INVOICE_DRAFT_KEY)
}

const cleanupPendingInvoiceDraftOnLoad = async () => {
  if (typeof window === 'undefined') return
  const raw = window.localStorage.getItem(PENDING_INVOICE_DRAFT_KEY)
  if (!raw) return
  try {
    const parsed = JSON.parse(raw) as { ids?: unknown }
    const ids = Array.isArray(parsed?.ids) ? (parsed.ids as unknown[]) : []
    for (const v of ids) {
      if (typeof v !== 'string' || !v) continue
      try {
        const res = await invoiceApi.getById(v)
        const inv = res.data?.data as any
        if (inv?.is_draft) {
          await invoiceApi.delete(v)
        }
      } catch {
        // ignore per-item
      }
    }
  } catch {
    // ignore
  } finally {
    clearPendingInvoiceDraft()
  }
}

const triggerInvoiceChoose = (event: MouseEvent) => {
  const target = event.target as HTMLElement | null
  if (!target) return
  if (target.closest('button') || target.closest('input') || target.closest('a')) return
  invoiceInput.value?.click()
}

const chooseInvoiceFiles = () => {
  invoiceInput.value?.click()
}

const removeSelectedFile = async (idx: number) => {
  // If OCR/upload already happened, removing a file must also discard created draft invoices,
  // otherwise re-uploading the same file will be treated as a hash duplicate.
  if (uploadedInvoiceIds.value.length > 0 || uploadedInvoiceId.value) {
    await discardUploadedInvoices()
    resetUploadDraftState()
    return
  }

  selectedFiles.value = selectedFiles.value.filter((_, i) => i !== idx)
  if (selectedFiles.value.length === 0) {
    if (invoiceInput.value) invoiceInput.value.value = ''
  }
}

const previewVisible = ref(false)
const previewInvoice = ref<Invoice | null>(null)
const uploadingInvoiceAttachment = ref(false)
const invoiceAttachmentInput = ref<HTMLInputElement | null>(null)

watch(
  () => previewInvoice.value?.id,
  async (id) => {
    revokeObjectUrl(previewInvoiceFileSrc.value)
    previewInvoiceFileSrc.value = ''
    if (!id) return
    try {
      previewInvoiceFileSrc.value = await loadInvoiceFileBlobUrl(id)
    } catch (err) {
      console.warn('Load preview invoice file failed:', err)
    }
  },
)
const parseStatusPending = ref(false)
const invoiceDetailEditing = ref(false)
const savingInvoiceDetail = ref(false)
const markingRegressionSample = ref(false)
const invoiceDetailForm = reactive({
  invoice_number: '',
  invoice_date: null as Date | null,
  amount: null as number | null,
  seller_name: '',
  buyer_name: '',
})

// Linked payments state
const loadingLinkedPayments = ref(false)
const linkedPayments = ref<Payment[]>([])

const invoicesAbort = ref<AbortController | null>(null)
const statsAbort = ref<AbortController | null>(null)

const loadInvoices = async () => {
  invoicesAbort.value?.abort()
  const controller = new AbortController()
  invoicesAbort.value = controller
  loading.value = true
  try {
    for (let attempt = 0; attempt < 2; attempt += 1) {
      const params: Record<string, any> = {
        limit: pageSize.value,
        offset: first.value,
      }
      if (dateRange.value && dateRange.value[0] && dateRange.value[1]) {
        params.startDate = dayjs(dateRange.value[0]).format('YYYY-MM-DD')
        params.endDate = dayjs(dateRange.value[1]).format('YYYY-MM-DD')
      }
      const res = await invoiceApi.getAll(params, { signal: controller.signal })
      const data = res.data.data
      if (res.data.success && data) {
        invoices.value = data.items || []
        totalRecords.value = typeof data.total === 'number' ? data.total : 0
        if (invoices.value.length === 0 && totalRecords.value > 0 && first.value > 0) {
          first.value = Math.max(0, first.value - pageSize.value)
          continue
        }
      }
      break
    }
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    toast.add({ severity: 'error', summary: getApiErrorMessage(e, '\u52A0\u8F7D\u53D1\u7968\u5217\u8868\u5931\u8D25'), life: 3000 })
  } finally {
    if (invoicesAbort.value === controller) {
      invoicesAbort.value = null
      loading.value = false
    }
  }
}

const loadStats = async () => {
  try {
    statsAbort.value?.abort()
    const controller = new AbortController()
    statsAbort.value = controller
    const startDate =
      dateRange.value?.[0] && dateRange.value?.[1] ? dayjs(dateRange.value[0]).format('YYYY-MM-DD') : undefined
    const endDate =
      dateRange.value?.[0] && dateRange.value?.[1] ? dayjs(dateRange.value[1]).format('YYYY-MM-DD') : undefined
    const res = await invoiceApi.getStats({ startDate, endDate }, { signal: controller.signal })
    if (res.data.success && res.data.data) stats.value = res.data.data
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    console.error('Load stats failed:', e)
  } finally {
    statsAbort.value = null
  }
}

const reloadDebounced = debounce(() => {
  first.value = 0
  selectedInvoices.value = []
  void loadInvoices()
  void loadStats()
}, 250)

const handleDateChange = () => {
  reloadDebounced()
}

const onPage = (event: any) => {
  first.value = typeof event?.first === 'number' ? event.first : 0
  pageSize.value = typeof event?.rows === 'number' ? event.rows : pageSize.value
  selectedInvoices.value = []
  void loadInvoices()
}

const toggleBatchDeleteMode = () => {
  batchDeleteMode.value = !batchDeleteMode.value
  if (!batchDeleteMode.value) selectedInvoices.value = []
}

const onBulkDeleteClick = () => {
  if (!batchDeleteMode.value) return
  if (selectedInvoices.value.length === 0) return
  const count = selectedInvoices.value.length
  confirm.require({
    message: `确定删除选中的 ${count} 张发票吗？`,
    header: '删除确认',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: () => handleBulkDelete(),
  })
}

const handleBulkDelete = async () => {
  const ids = Array.from(new Set(selectedInvoices.value.map(x => x.id).filter(Boolean)))
  if (ids.length === 0) return
  const results = await Promise.allSettled(ids.map(id => invoiceApi.delete(id)))
  const failed = results.filter(r => r.status === 'rejected').length
  const succeeded = ids.length - failed
  if (failed === 0) {
    toast.add({ severity: 'success', summary: `已删除 ${succeeded} 张发票`, life: 2200 })
  } else {
    toast.add({ severity: 'warn', summary: `已删除 ${succeeded} 张，失败 ${failed} 张`, life: 3800 })
  }
  batchDeleteMode.value = false
  selectedInvoices.value = []
  await loadInvoices()
  await loadStats()
}

const computeStatsFromInvoices = (list: Invoice[]) => {
  let totalAmount = 0
  const bySource: Record<string, number> = {}
  for (const inv of list) {
    if (typeof inv.amount === 'number') totalAmount += inv.amount
    const src = inv.source || 'unknown'
    bySource[src] = (bySource[src] || 0) + 1
  }
  return { totalCount: list.length, totalAmount, bySource }
}

const displayStats = computed(() => {
  if (stats.value) return stats.value
  return computeStatsFromInvoices(invoices.value)
})

const openUploadModal = () => {
  selectedFiles.value = []
  if (invoiceInput.value) invoiceInput.value.value = ''
  uploadedInvoiceIds.value = []
  uploadedInvoiceId.value = null
  uploadedInvoice.value = null
  uploadOcrResult.value = null
  uploadDedup.value = null
  uploadConfirmed.value = false
  uploadOcrForm.invoice_number = ''
  uploadOcrForm.invoice_date = null
  uploadOcrForm.amount = null
  uploadOcrForm.seller_name = ''
  uploadOcrForm.buyer_name = ''
  uploadModalVisible.value = true
}

const resetUploadDraftState = () => {
  selectedFiles.value = []
  if (invoiceInput.value) invoiceInput.value.value = ''
  uploadedInvoiceIds.value = []
  uploadedInvoiceId.value = null
  uploadedInvoice.value = null
  uploadOcrResult.value = null
  uploadConfirmed.value = false
  uploadOcrForm.invoice_number = ''
  uploadOcrForm.invoice_date = null
  uploadOcrForm.amount = null
  uploadOcrForm.seller_name = ''
  uploadOcrForm.buyer_name = ''
  clearPendingInvoiceDraft()
}

const discardUploadedInvoices = async () => {
  const ids = uploadedInvoiceIds.value.filter(Boolean)
  if (ids.length === 0) return
  const results = await Promise.allSettled(ids.map(id => invoiceApi.delete(id)))
  const failed = results.filter(r => r.status === 'rejected').length
  if (failed === 0) {
    toast.add({ severity: 'info', summary: '已取消并删除本次上传的发票', life: 2200 })
  } else {
    toast.add({ severity: 'warn', summary: `已取消，但有 ${failed} 条上传记录删除失败（可在列表中手动删除）`, life: 4000 })
  }
  await loadInvoices()
  await loadStats()
}

const handleCancelUpload = () => {
  uploadModalVisible.value = false
}

const onInvoiceInputChange = async (event: Event) => {
  const input = event.target as HTMLInputElement | null
  const files = input?.files ? Array.from(input.files) : []

  // If user selects new files after an upload/parse, discard the previous draft(s) first.
  if (uploadedInvoiceIds.value.length > 0 || uploadedInvoiceId.value) {
    await discardUploadedInvoices()
    resetUploadDraftState()
  }
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

const onInvoiceDrop = async (event: DragEvent) => {
  const list = event.dataTransfer?.files
  if (!list || list.length === 0) return

  // If user drags new files after an upload/parse, discard the previous draft(s) first.
  if (uploadedInvoiceIds.value.length > 0 || uploadedInvoiceId.value) {
    await discardUploadedInvoices()
    resetUploadDraftState()
  }
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
    const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))
    const waitForTask = async (taskId: string, opts?: { timeoutMs?: number }) => {
      const timeoutMs = opts?.timeoutMs ?? 180000
      const start = Date.now()
      while (Date.now() - start < timeoutMs) {
        const res = await tasksApi.getById(taskId)
        const t = res.data?.data as any
        if (!t) throw new Error('任务状态获取失败')
        if (t.status === 'succeeded' || t.status === 'failed' || t.status === 'canceled') return t
        await sleep(900)
      }
      throw new Error('识别超时，请稍后重试')
    }

    if (selectedFiles.value.length === 1) {
      const res = await invoiceApi.uploadAsync(selectedFiles.value[0])
      const payload = (res.data?.data || null) as any
      const taskId = payload?.taskId as string
      const draftInvoice = payload?.invoice as Invoice
      createdInvoice = draftInvoice || null
      uploadedInvoiceIds.value = createdInvoice ? [createdInvoice.id] : []

      if (taskId) {
        const task = await waitForTask(taskId)
        if (task.status === 'failed') throw new Error(task?.error || '识别失败')
        if (task.status === 'canceled') throw new Error('识别已取消')
        const result = task.result as any
        createdInvoice = result?.invoice || createdInvoice
        uploadDedup.value = (result?.dedup as any) || null
      }
    } else {
      const res = await invoiceApi.uploadMultipleAsync(selectedFiles.value)
      const items = (res.data?.data || []) as Array<{ taskId: string; invoice: Invoice }>
      const createdList = items.map(it => it.invoice)
      createdInvoice = createdList.length > 0 ? createdList[0] : null
      uploadedInvoiceIds.value = createdList.map(it => it.id)
      uploadDedup.value = null

      // Wait until all invoices are parsed before allowing confirm-all.
      for (let i = 0; i < items.length; i++) {
        const it = items[i]
        if (!it?.taskId) continue
        const task = await waitForTask(it.taskId)
        if (task.status === 'failed') throw new Error(task?.error || '识别失败')
        if (task.status === 'canceled') throw new Error('识别已取消')
        if (i === 0) {
          const result = task.result as any
          createdInvoice = result?.invoice || createdInvoice
          uploadDedup.value = (result?.dedup as any) || null
        }
      }
    }
    if (createdInvoice) {
      uploadedInvoiceId.value = createdInvoice.id
      uploadedInvoice.value = createdInvoice
      const extracted = getInvoiceExtracted(createdInvoice)
      uploadOcrResult.value = extracted
      uploadOcrForm.invoice_number = extracted?.invoice_number || ''
      uploadOcrForm.invoice_date = parseInvoiceDateToDate(extracted?.invoice_date)
      uploadOcrForm.amount = extracted?.amount ?? null
      uploadOcrForm.seller_name = extracted?.seller_name || ''
      uploadOcrForm.buyer_name = extracted?.buyer_name || ''
    }
    rememberPendingInvoiceDraft(uploadedInvoiceIds.value)
    toast.add({ severity: 'success', summary: '\u4E0A\u4F20\u6210\u529F\uFF0C\u8BF7\u786E\u8BA4\u8BC6\u522B\u7ED3\u679C', life: 2200 })
    notifications.add({
      severity: 'success',
      title: '\u53D1\u7968\u4E0A\u4F20\u6210\u529F',
      detail: selectedFiles.value.length === 1 ? selectedFiles.value[0]?.name : `\u5171 ${selectedFiles.value.length} \u4E2A\u6587\u4EF6`,
    })
    // 不立即关闭，等待用户确认并保存
  } catch (error: unknown) {
    const err = error as any
    const resp = err?.response
    const data = resp?.data
    const dup = data?.data as DedupHint | undefined
    if (resp?.status === 409 && dup?.kind === 'hash_duplicate') {
      toast.add({ severity: 'warn', summary: '\u6587\u4EF6\u5185\u5BB9\u91CD\u590D\uFF0C\u5DF2\u5B58\u5728\u8BB0\u5F55', life: 4500 })
    } else {
      toast.add({ severity: 'error', summary: '\u4E0A\u4F20\u5931\u8D25', life: 3000 })
    }
  } finally {
    uploading.value = false
  }
}

const handleSaveUploadedInvoice = async () => {
  if (!uploadedInvoiceId.value) {
    uploadModalVisible.value = false
    return
  }
  savingUploadOcr.value = true
  try {
    const payload: Partial<Invoice> = {
      invoice_number: uploadOcrForm.invoice_number || undefined,
      invoice_date: uploadOcrForm.invoice_date ? dayjs(uploadOcrForm.invoice_date).format('YYYY-MM-DD') : undefined,
      amount: uploadOcrForm.amount === null ? undefined : Number(uploadOcrForm.amount),
      seller_name: uploadOcrForm.seller_name || undefined,
      buyer_name: uploadOcrForm.buyer_name || undefined,
    }
    const saveCurrent = async (force: boolean) => {
      await invoiceApi.update(uploadedInvoiceId.value as string, {
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
        const ok = await confirmForceSave('检测到疑似重复发票（发票号码重复），是否仍然保存？')
        if (!ok) return
        await saveCurrent(true)
      } else {
        throw error
      }
    }

    const restIds = uploadedInvoiceIds.value.filter(id => id && id !== uploadedInvoiceId.value)
    if (restIds.length > 0) {
      const results = await Promise.allSettled(restIds.map(id => invoiceApi.update(id, { confirm: true })))
      const failed = results.filter(r => r.status === 'rejected').length
      if (failed > 0) {
        toast.add({
          severity: 'warn',
          summary: `已保存，但有 ${failed} 个发票确认失败（可在列表中重新编辑/保存）`,
          life: 4500,
        })
      }
    }
    clearPendingInvoiceDraft()
    toast.add({ severity: 'success', summary: '\u53D1\u7968\u4FE1\u606F\u5DF2\u66F4\u65B0', life: 2000 })
    uploadConfirmed.value = true
    uploadModalVisible.value = false
    await loadInvoices()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u4FDD\u5B58\u5931\u8D25', life: 3000 })
  } finally {
    savingUploadOcr.value = false
  }
}

watch(
  () => uploadModalVisible.value,
  async visible => {
    if (visible) return
    if (uploading.value || savingUploadOcr.value) return
    if (!uploadConfirmed.value) {
      await discardUploadedInvoices()
    }
    resetUploadDraftState()
  },
)

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
    selectedInvoices.value = selectedInvoices.value.filter(inv => inv.id !== id)
    toast.add({ severity: 'success', summary: '\u5220\u9664\u6210\u529F', life: 2000 })
    notifications.add({ severity: 'info', title: '\u53D1\u7968\u5DF2\u5220\u9664', detail: id })
    await loadInvoices()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '\u5220\u9664\u5931\u8D25', life: 3000 })
  }
}

const openPreview = async (invoice: Invoice) => {
  let full = invoice
  if (invoice?.id) {
    try {
      const res = await invoiceApi.getById(invoice.id)
      if (res.data?.success && res.data?.data) full = res.data.data
    } catch {
      // ignore, fallback to list item
    }
  }
  previewInvoice.value = full
  invoiceDetailEditing.value = false
  savingInvoiceDetail.value = false
  invoiceDetailForm.invoice_number = full.invoice_number || ''
  invoiceDetailForm.invoice_date = parseInvoiceDateToDate(full.invoice_date)
  invoiceDetailForm.amount = full.amount ?? null
  invoiceDetailForm.seller_name = full.seller_name || ''
  invoiceDetailForm.buyer_name = full.buyer_name || ''
  previewVisible.value = true
  loadLinkedPayments(full.id)
}

const downloadFile = async (invoice: Invoice) => {
  if (typeof window === 'undefined') return
  if (!invoice?.id) return
  try {
    const res = await invoiceApi.getFileBlob(invoice.id)
    const url = URL.createObjectURL(res.data as Blob)
    window.open(url, '_blank')
    window.setTimeout(() => revokeObjectUrl(url), 60_000)
  } catch (err) {
    console.warn('Download invoice file failed:', err)
  }
}

const formatAttachmentKind = (kind?: string) => {
  const k = String(kind || '').trim().toLowerCase()
  if (!k) return ''
  if (k === 'itinerary') return '电子行程单'
  return kind || ''
}

const downloadAttachment = async (invoice: Invoice, attachment: InvoiceAttachment) => {
  if (typeof window === 'undefined') return
  if (!invoice?.id || !attachment?.id) return
  try {
    const res = await invoiceApi.getAttachmentBlob(invoice.id, attachment.id)
    const url = URL.createObjectURL(res.data as Blob)
    window.open(url, '_blank')
    window.setTimeout(() => revokeObjectUrl(url), 60_000)
  } catch (err) {
    console.warn('Download invoice attachment failed:', err)
  }
}

const chooseInvoiceAttachmentFiles = () => {
  invoiceAttachmentInput.value?.click()
}

const onInvoiceAttachmentInputChange = async (e: Event) => {
  const input = e.target as HTMLInputElement | null
  const inv = previewInvoice.value
  if (!input || !inv?.id) return
  const files = Array.from(input.files || [])
  if (files.length === 0) return

  uploadingInvoiceAttachment.value = true
  try {
    for (const f of files) {
      const res = await invoiceApi.uploadAttachment(inv.id, f)
      const att = res.data?.data?.attachment
      if (res.data.success && att) {
        if (!Array.isArray(inv.attachments)) inv.attachments = []
        if (!inv.attachments.some((x) => x.id === att.id)) {
          inv.attachments.push(att)
        }
        toast.add({ severity: 'success', summary: '附件已添加', life: 1800 })
      } else {
        toast.add({ severity: 'error', summary: res.data.message || '附件上传失败', life: 2500 })
      }
    }
  } catch (err: any) {
    toast.add({ severity: 'error', summary: err?.response?.data?.message || '附件上传失败', life: 2500 })
  } finally {
    uploadingInvoiceAttachment.value = false
    input.value = ''
  }
}

const confirmDeleteAttachment = async (invoice: Invoice, attachment: InvoiceAttachment) => {
  if (!invoice?.id || !attachment?.id) return
  confirm.require({
    header: '删除附件',
    message: `确认删除附件：${attachment.original_name || attachment.filename}？`,
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    accept: async () => {
      uploadingInvoiceAttachment.value = true
      try {
        const res = await invoiceApi.deleteAttachment(invoice.id, attachment.id)
        if (res.data.success) {
          const inv = previewInvoice.value
          if (inv?.attachments) {
            inv.attachments = inv.attachments.filter((x) => x.id !== attachment.id)
          }
          toast.add({ severity: 'success', summary: '附件已删除', life: 1800 })
        } else {
          toast.add({ severity: 'error', summary: res.data.message || '删除失败', life: 2500 })
        }
      } catch (err: any) {
        toast.add({ severity: 'error', summary: err?.response?.data?.message || '删除失败', life: 2500 })
      } finally {
        uploadingInvoiceAttachment.value = false
      }
    },
  })
}

const enterInvoiceEditMode = () => {
  if (!previewInvoice.value) return
  invoiceDetailEditing.value = true
  invoiceDetailForm.invoice_number = previewInvoice.value.invoice_number || ''
  invoiceDetailForm.invoice_date = parseInvoiceDateToDate(previewInvoice.value.invoice_date)
  invoiceDetailForm.amount = previewInvoice.value.amount ?? null
  invoiceDetailForm.seller_name = previewInvoice.value.seller_name || ''
  invoiceDetailForm.buyer_name = previewInvoice.value.buyer_name || ''
}

const cancelInvoiceEditMode = () => {
  if (!previewInvoice.value) {
    invoiceDetailEditing.value = false
    return
  }
  invoiceDetailForm.invoice_number = previewInvoice.value.invoice_number || ''
  invoiceDetailForm.invoice_date = parseInvoiceDateToDate(previewInvoice.value.invoice_date)
  invoiceDetailForm.amount = previewInvoice.value.amount ?? null
  invoiceDetailForm.seller_name = previewInvoice.value.seller_name || ''
  invoiceDetailForm.buyer_name = previewInvoice.value.buyer_name || ''
  invoiceDetailEditing.value = false
}

const markInvoiceRegressionSample = async (id: string) => {
  if (!isAdmin.value) return
  if (!id) return
  markingRegressionSample.value = true
  try {
    const res = await regressionSamplesApi.markInvoice(id)
    if (res.data.success) {
      const issues = (res.data as any)?.data?.issues as any[] | undefined
      const warnIssues = (Array.isArray(issues) ? issues : []).filter((it) => String(it?.level || '').toLowerCase() === 'warn')
      const nonPiiWarnIssues = warnIssues.filter((it) => !String(it?.code || '').toLowerCase().startsWith('pii_'))
      const hasWarn = nonPiiWarnIssues.length > 0
      const hasOnlyPiiWarn = warnIssues.length > 0 && nonPiiWarnIssues.length === 0
      toast.add({
        severity: hasWarn ? 'warn' : hasOnlyPiiWarn ? 'info' : 'success',
        summary: hasWarn
          ? '\u5df2\u6807\u8bb0\u56de\u5f52\u6837\u672c\uff08\u6709\u8b66\u544a\uff09'
          : hasOnlyPiiWarn
            ? '\u5df2\u6807\u8bb0\u56de\u5f52\u6837\u672c\uff08\u542b\u9690\u79c1\u5b57\u6bb5\u63d0\u793a\uff09'
            : '\u5df2\u6807\u8bb0\u4e3a\u56de\u5f52\u6837\u672c',
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
      const res = await regressionSamplesApi.markInvoice(id, { force: true })
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

const saveInvoiceEditMode = async () => {
  if (!previewInvoice.value) return
  savingInvoiceDetail.value = true
  try {
    const payload: Partial<Invoice> = {
      invoice_number: invoiceDetailForm.invoice_number || undefined,
      invoice_date: invoiceDetailForm.invoice_date ? dayjs(invoiceDetailForm.invoice_date).format('YYYY-MM-DD') : undefined,
      amount: invoiceDetailForm.amount === null ? undefined : Number(invoiceDetailForm.amount),
      seller_name: invoiceDetailForm.seller_name || undefined,
      buyer_name: invoiceDetailForm.buyer_name || undefined,
    }
    await invoiceApi.update(previewInvoice.value.id, payload)
    const refreshed = await invoiceApi.getById(previewInvoice.value.id)
    if (refreshed.data.success && refreshed.data.data) {
      previewInvoice.value = refreshed.data.data
    } else {
      previewInvoice.value = { ...previewInvoice.value, ...payload } as Invoice
    }
    toast.add({ severity: 'success', summary: '已保存', life: 2000 })
    invoiceDetailEditing.value = false
    await loadInvoices()
    await loadStats()
  } catch {
    toast.add({ severity: 'error', summary: '保存失败', life: 3000 })
  } finally {
    savingInvoiceDetail.value = false
  }
}

const loadLinkedPayments = async (invoiceId: string) => {
  loadingLinkedPayments.value = true
  linkedPayments.value = []
  try {
    const linkedRes = await invoiceApi.getLinkedPayments(invoiceId)
    if (linkedRes.data.success && linkedRes.data.data) linkedPayments.value = linkedRes.data.data
  } catch (error) {
    console.error('Load linked payments failed:', error)
  } finally {
    loadingLinkedPayments.value = false
  }
}

const getSourceLabel = (source?: string) => {
  const labels: Record<string, string> = {
    email: '\u90AE\u4EF6\u4E0B\u8F7D',
    feishu: '\u5916\u90E8\u5BFC\u5165',
    dingtalk: '\u5916\u90E8\u5BFC\u5165',
    upload: '\u624B\u52A8\u4E0A\u4F20',
  }
  return labels[source || ''] || source || '\u672A\u77E5'
}

const getSourceSeverity = (source?: string): 'info' | 'success' | 'warning' | 'secondary' => {
  const types: Record<string, 'info' | 'success' | 'warning' | 'secondary'> = {
    email: 'info',
    feishu: 'secondary',
    dingtalk: 'secondary',
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
  const m = String(date).match(/(\d{4})\D+(\d{1,2})\D+(\d{1,2})/)
  if (m) {
    const y = m[1]
    const mm = m[2].padStart(2, '0')
    const dd = m[3].padStart(2, '0')
    return `${y}-${mm}-${dd}`
  }
  return date
}

const parseInvoiceDateToDayjs = (date?: string | null) => {
  const raw = String(date || '').trim()
  if (!raw) return null
  const direct = dayjs(raw)
  if (direct.isValid()) return direct
  const m = raw.match(/(\d{4})\D+(\d{1,2})\D+(\d{1,2})/)
  if (!m) return null
  const y = m[1]
  const mm = m[2].padStart(2, '0')
  const dd = m[3].padStart(2, '0')
  const parsed = dayjs(`${y}-${mm}-${dd}`)
  return parsed.isValid() ? parsed : null
}

const parseInvoiceDateToDate = (date?: string | null) => {
  const parsed = parseInvoiceDateToDayjs(date)
  return parsed ? parsed.toDate() : null
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

const rawTextSourceLabel = (src?: string) => {
  const s = String(src || '').trim().toLowerCase()
  if (!s) return ''
  if (s === 'pymupdf') return 'PyMuPDF'
  if (s === 'pymupdf_zones') return 'PyMuPDF（分区）'
  if (s === 'rapidocr') return 'RapidOCR'
  if (s === 'merged') return 'Merged'
  return src || ''
}

const getInvoiceRawTextSource = (invoice: Invoice | null) => {
  const extracted = getInvoiceExtracted(invoice)
  return rawTextSourceLabel(extracted?.raw_text_source)
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

const getInvoiceExtracted = (invoice: Invoice | null): InvoiceExtractedData | null => {
  if (!invoice?.extracted_data) return null
  try {
    const data = JSON.parse(invoice.extracted_data)
    return data as InvoiceExtractedData
  } catch {
    return null
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

const sourceStats = computed(() => displayStats.value.bySource || {})
const otherSourceCount = computed(() => {
  const m = sourceStats.value as Record<string, unknown>
  let total = 0
  for (const [k, v] of Object.entries(m)) {
    if (k === 'upload' || k === 'email') continue
    const n = Number(v || 0)
    if (Number.isFinite(n)) total += n
  }
  return total
})

onMounted(() => {
  void (async () => {
    await cleanupPendingInvoiceDraftOnLoad()
    await loadInvoices()
    await loadStats()
  })()
})

onBeforeUnmount(() => {
  reloadDebounced.cancel()
  invoicesAbort.value?.abort()
  statsAbort.value?.abort()
  revokeObjectUrl(previewInvoiceFileSrc.value)
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

.row-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: flex-start;
}

.invoices-table :deep(.p-datatable-table) {
  width: 100%;
  table-layout: fixed;
}

.invoices-table :deep(.p-datatable-thead > tr > th),
.invoices-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

@media (max-width: 768px) {
  .invoices-table :deep(.p-datatable-table-container) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .invoices-table :deep(.p-datatable-table) {
    width: max-content;
    min-width: 100%;
  }
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

.filecell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--color-text-primary);
  min-width: 0;
}

.filecell-name {
  min-width: 0;
  flex: 1 1 auto;
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
  background: color-mix(in srgb, var(--p-surface-200), transparent 70%);
  color: var(--p-text-muted-color);
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

.upload-screenshot-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.upload-invoice-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

.upload-invoice-left,
.upload-invoice-right {
  min-width: 0;
}

.upload-invoice-left .invoice-file-preview {
  margin-top: 0;
}

@media (max-width: 960px) {
  .upload-invoice-layout {
    grid-template-columns: 1fr;
  }
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

.items-table :deep(.p-datatable-table-container) {
  overflow-x: auto;
}

.items-table :deep(.p-datatable-table) {
  width: 100% !important;
  table-layout: auto;
}

.preview .sbm-grid-tight {
  margin: 0;
}

.preview .sbm-grid-tight > [class*='col-'] {
  padding: 0.35rem;
}

.preview :deep(.p-divider.p-divider-horizontal) {
  margin: 10px 0;
}

.invoice-detail-layout {
  display: grid;
  grid-template-columns: minmax(320px, 38%) 1fr;
  gap: 14px;
  align-items: start;
}

.invoice-detail-left,
.invoice-detail-right {
  min-width: 0;
}

.invoice-detail-left .invoice-file-preview {
  margin-top: 0;
}

@media (max-width: 900px) {
  .invoice-detail-layout {
    grid-template-columns: 1fr;
  }
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

.raw-source {
  font-weight: 600;
  color: var(--color-text-secondary);
  margin-left: 8px;
  font-size: 12px;
}

.invoice-file-preview {
  margin-top: 10px;
}

.invoice-file-box {
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: rgba(0, 0, 0, 0.02);
  border-radius: var(--radius-md);
  overflow: hidden;
  padding: 10px;
}

.invoice-attachments {
  margin-top: 10px;
}

.invoice-attachments-box {
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: rgba(0, 0, 0, 0.02);
  border-radius: var(--radius-md);
  overflow: hidden;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.invoice-attachments-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
}

.invoice-attachment-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.6);
}

.invoice-attachment-name {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--color-text-primary);
}

.invoice-attachment-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.invoice-image :deep(img) {
  display: block;
  width: 100%;
  height: auto;
  max-height: 60vh;
  object-fit: contain;
  border-radius: var(--radius-md);
}

.invoice-pdf {
  display: block;
  width: 100%;
  height: 60vh;
  border: 0;
  border-radius: var(--radius-md);
  background: var(--p-content-background, #fff);
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

.ocr-fields {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ocr-fields .field-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 0.95rem;
}

.ocr-hint {
  display: block;
  color: var(--color-text-tertiary);
  margin-top: 2px;
  font-size: 0.8rem;
}

.ocr-hint-low {
  color: #d97706;
  font-weight: 800;
}

.ocr-hint-mid {
  color: var(--color-text-secondary);
}
</style>
