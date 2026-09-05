<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import {
  ApiError,
  api,
  type InvoiceMaterial,
  type InvoiceMaterialWorkspace,
} from '../../data/client'

const props = defineProps<{
  invoiceId: string
  factVersion: number
  refreshContext: () => Promise<number>
}>()
const emit = defineEmits<{ changed: [] }>()
const workspace = ref<InvoiceMaterialWorkspace | null>(null)
const loading = ref(false),
  loadError = ref(''),
  writeError = ref(''),
  busy = ref(false)
const file = ref<File | null>(null),
  fileInput = ref<HTMLInputElement | null>(null),
  reason = ref('')
const candidates = ref<InvoiceMaterial[]>([]),
  query = ref(''),
  appliedQuery = ref(''),
  cursor = ref('')
const candidateOpen = ref(false),
  candidateBusy = ref(false),
  candidateError = ref(''),
  selected = ref('')
const removeTarget = ref<InvoiceMaterial | null>(null),
  removeConfirmed = ref(false)
const stale = ref(false),
  refreshed = ref(false),
  rechecked = ref(false)
const ready = computed(
  () =>
    !!workspace.value &&
    !loading.value &&
    !loadError.value &&
    !busy.value &&
    (!stale.value || (refreshed.value && rechecked.value)),
)
let lifetime = 0,
  loadEpoch = 0,
  candidateEpoch = 0
let attempt: { fingerprint: string; file: File | null; key: string } | null = null
let removeTrigger: HTMLButtonElement | null = null

async function load() {
  const ticket = ++loadEpoch,
    current = lifetime,
    id = props.invoiceId
  loading.value = true
  loadError.value = ''
  try {
    const value = await api.invoiceMaterials(id)
    if (current !== lifetime || ticket !== loadEpoch) return
    if (value.invoice_id !== id) throw new Error('材料响应与当前发票不一致')
    if (stale.value || value.version !== props.factVersion) {
      const version = await props.refreshContext()
      if (current !== lifetime || ticket !== loadEpoch) return
      if (version !== value.version) throw new Error('刷新期间发票再次变化，请重新刷新并核对')
    }
    workspace.value = value
    if (stale.value) {
      refreshed.value = true
      rechecked.value = false
    }
  } catch (caught) {
    if (current === lifetime && ticket === loadEpoch)
      loadError.value = caught instanceof Error ? caught.message : '材料加载失败'
  } finally {
    if (current === lifetime && ticket === loadEpoch) loading.value = false
  }
}
async function search(append = false) {
  const ticket = ++candidateEpoch,
    current = lifetime
  if (!append) {
    candidates.value = []
    cursor.value = ''
    appliedQuery.value = query.value.trim()
    selected.value = ''
  }
  candidateBusy.value = true
  candidateError.value = ''
  try {
    const page = await api.invoiceMaterialCandidates(
      props.invoiceId,
      appliedQuery.value,
      append ? cursor.value : '',
    )
    if (current !== lifetime || ticket !== candidateEpoch) return
    candidates.value = append ? [...candidates.value, ...page.items] : page.items
    cursor.value = page.next_cursor
  } catch (caught) {
    if (current === lifetime && ticket === candidateEpoch)
      candidateError.value = caught instanceof Error ? caught.message : '候选加载失败'
  } finally {
    if (current === lifetime && ticket === candidateEpoch) candidateBusy.value = false
  }
}
function toggleCandidates() {
  candidateOpen.value = !candidateOpen.value
  if (candidateOpen.value) void search()
}
function chooseFile(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0] ?? null
}
function startRemove(item: InvoiceMaterial, event: Event) {
  removeTarget.value = item
  removeConfirmed.value = false
  removeTrigger = event.currentTarget as HTMLButtonElement
}
async function cancelRemove() {
  removeTarget.value = null
  removeConfirmed.value = false
  await nextTick()
  removeTrigger?.focus()
}
async function save(action: 'upload' | 'add' | 'remove') {
  if (!ready.value || !workspace.value) return
  const why = reason.value.trim(),
    target = action === 'add' ? selected.value : (removeTarget.value?.id ?? '')
  if (
    !why ||
    (action === 'upload' ? !file.value : !target) ||
    (action === 'remove' && !removeConfirmed.value)
  ) {
    writeError.value = '请选择材料、填写操作理由，并确认解除操作'
    return
  }
  const upload = action === 'upload' ? file.value : null
  const fingerprint = JSON.stringify([
    props.invoiceId,
    action,
    target,
    workspace.value.version,
    why,
  ])
  if (attempt?.fingerprint !== fingerprint || attempt.file !== upload)
    attempt = { fingerprint, file: upload, key: crypto.randomUUID() }
  const body = {
    expected_version: workspace.value.version,
    reason: why,
    idempotency_key: attempt.key,
  }
  const current = lifetime
  busy.value = true
  writeError.value = ''
  try {
    if (action === 'upload' && upload)
      await api.uploadInvoiceMaterial(props.invoiceId, upload, body)
    else if (action === 'add') await api.addInvoiceMaterial(props.invoiceId, target, body)
    else await api.removeInvoiceMaterial(props.invoiceId, target, body)
    if (current !== lifetime) return
    attempt = null
    reason.value = ''
    file.value = null
    if (fileInput.value) fileInput.value.value = ''
    removeTarget.value = null
    selected.value = ''
    emit('changed')
  } catch (caught) {
    if (current !== lifetime) return
    writeError.value =
      caught instanceof Error ? caught.message : '保存结果未确认；输入已保留，可重试同一请求'
    if (caught instanceof ApiError && caught.status === 409) {
      stale.value = true
      refreshed.value = false
      rechecked.value = false
    }
  } finally {
    if (current === lifetime) busy.value = false
  }
}
watch(
  () => props.invoiceId,
  () => {
    lifetime++
    candidateEpoch++
    loadEpoch++
    workspace.value = null
    candidates.value = []
    candidateOpen.value = false
    cursor.value = ''
    selected.value = ''
    reason.value = ''
    file.value = null
    removeTarget.value = null
    writeError.value = ''
    candidateError.value = ''
    stale.value = false
    refreshed.value = false
    rechecked.value = false
    busy.value = false
    candidateBusy.value = false
    attempt = null
    if (fileInput.value) fileInput.value.value = ''
    void load()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  lifetime++
  loadEpoch++
  candidateEpoch++
  attempt = null
  file.value = null
})
</script>

<template>
  <section class="panel invoice-materials" aria-labelledby="invoice-materials-title">
    <header class="panel-heading">
      <div>
        <h2 id="invoice-materials-title">辅助材料</h2>
        <p>补充图片或 PDF，不改变发票字段，不进行 AI 识别。</p>
      </div>
      <button class="button" :disabled="loading || busy" @click="load">刷新材料</button>
    </header>
    <div class="materials-body">
      <p v-if="loading" role="status">正在加载材料…</p>
      <p v-if="loadError" class="notice notice-danger" role="alert">{{ loadError }}</p>
      <p v-if="writeError" class="notice notice-danger" role="alert">{{ writeError }}</p>
      <div v-if="stale" class="notice notice-warning">
        <p>发票或材料已变化。文件、选择和理由已保留；请刷新材料，核对后再提交。</p>
        <label
          ><input v-model="rechecked" type="checkbox" :disabled="!refreshed || loading" />
          我已核对刷新后的材料和发票版本</label
        >
      </div>
      <template v-if="workspace">
        <p class="quiet">
          {{ workspace.items.length }} / 100 份 · 发票版本 {{ workspace.version }}
        </p>
        <p v-if="!workspace.items.length && !loading">还没有辅助材料。票面原件仍在下方的来源区。</p>
        <ul class="material-list">
          <li v-for="item in workspace.items" :key="item.id">
            <div class="material-name">
              <strong>{{ item.original_name }}</strong
              ><small
                >{{ item.mime }} · {{ (item.size_bytes / 1024).toFixed(1) }} KiB ·
                {{ item.page_count }} 页</small
              >
            </div>
            <div class="material-actions">
              <a
                class="button button-small"
                :href="api.documentContentURL(item.document_id)"
                target="_blank"
                rel="noopener"
                :aria-label="`打开 ${item.original_name}`"
                >打开</a
              >
              <a
                class="button button-small"
                :href="api.documentContentURL(item.document_id)"
                :download="item.original_name"
                :aria-label="`下载 ${item.original_name}`"
                >下载</a
              >
              <button
                class="button button-small"
                :disabled="!ready"
                :aria-label="`解除关联 ${item.original_name}`"
                @click="startRemove(item, $event)"
              >
                解除关联
              </button>
            </div>
          </li>
        </ul>
        <fieldset :disabled="!ready" class="material-form">
          <legend>添加辅助材料</legend>
          <label class="field"
            ><span>选择图片或 PDF</span
            ><input
              ref="fileInput"
              type="file"
              accept="image/jpeg,image/png,image/webp,application/pdf"
              @change="chooseFile"
          /></label>
          <small>每次 1 个文件，最多 20 MiB、20 页；相同内容复用已有原件。</small>
          <label class="field"
            ><span>操作理由</span
            ><textarea
              v-model="reason"
              rows="2"
              maxlength="500"
              placeholder="说明添加或解除材料的原因"
            ></textarea>
          </label>
          <div class="material-actions">
            <button
              class="button button-primary"
              :disabled="!file || !reason.trim() || workspace.items.length >= 100"
              @click="save('upload')"
            >
              上传并关联
            </button>
            <button class="button" @click="toggleCandidates">
              {{ candidateOpen ? '收起已有材料' : '关联已有材料' }}
            </button>
          </div>
        </fieldset>
        <div v-if="candidateOpen" class="material-form">
          <form class="material-actions" @submit.prevent="search()">
            <label class="field"
              ><span>查找已有材料</span
              ><input v-model="query" maxlength="200" placeholder="按文件名搜索" :disabled="busy"
            /></label>
            <button class="button" :disabled="busy" type="submit">查找材料</button>
          </form>
          <p v-if="candidateBusy" role="status">正在查找材料…</p>
          <p v-if="candidateError" role="alert">{{ candidateError }}</p>
          <p v-else-if="!candidateBusy && !candidates.length">没有匹配的未关联材料</p>
          <ul class="candidate-list">
            <li v-for="item in candidates" :key="item.document_id">
              <label
                ><input
                  v-model="selected"
                  type="radio"
                  name="invoice-material-candidate"
                  :value="item.document_id"
                  :disabled="!ready || candidateBusy"
                />{{ item.original_name }}</label
              >
            </li>
          </ul>
          <div class="material-actions">
            <button
              v-if="cursor"
              class="button"
              :disabled="candidateBusy || busy"
              @click="search(true)"
            >
              加载更多材料
            </button>
            <button
              class="button button-primary"
              :disabled="
                !ready ||
                !selected ||
                !reason.trim() ||
                candidateBusy ||
                !!candidateError ||
                workspace.items.length >= 100
              "
              @click="save('add')"
            >
              确认关联
            </button>
          </div>
        </div>
        <div
          v-if="removeTarget"
          class="notice notice-warning"
          role="group"
          aria-label="解除材料确认"
        >
          <p>解除 {{ removeTarget.original_name }}？仅结束关联，保留原件和历史报销材料。</p>
          <label
            ><input v-model="removeConfirmed" type="checkbox" :disabled="!ready" />
            确认解除此辅助材料关联</label
          >
          <div class="material-actions">
            <button class="button" :disabled="busy" @click="cancelRemove">取消解除</button
            ><button
              class="button button-danger"
              :disabled="!ready || !removeConfirmed || !reason.trim()"
              @click="save('remove')"
            >
              确认解除
            </button>
          </div>
        </div>
        <p v-if="busy" role="status">正在保存材料，请勿重复提交…</p>
      </template>
    </div>
  </section>
</template>

<style scoped>
.invoice-materials .panel-heading {
  flex-wrap: wrap;
  align-items: start;
}
.invoice-materials .panel-heading > div {
  flex: 1 1 12rem;
}
.invoice-materials .panel-heading > button {
  flex-shrink: 0;
  white-space: nowrap;
}
.materials-body {
  padding: 0 1.5rem 1.5rem;
  display: grid;
  gap: 1rem;
}
.material-list,
.candidate-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.material-list li {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  justify-content: space-between;
  padding: 1rem 0;
  border-bottom: 1px solid var(--border);
}
.material-name {
  display: grid;
  gap: 0.35rem;
  min-width: 0;
  overflow-wrap: anywhere;
  flex: 1 1 12rem;
}
.material-name small {
  color: var(--text-muted);
}
.material-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 0.65rem;
}
.material-form {
  display: grid;
  gap: 0.8rem;
  margin: 0;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  min-width: 0;
}
.material-form .field {
  display: grid;
  gap: 0.4rem;
  min-width: 0;
}
.material-form input:not([type='radio']),
.material-form textarea {
  max-width: 100%;
  box-sizing: border-box;
}
.candidate-list {
  max-height: 20rem;
  overflow-y: auto;
}
.candidate-list label,
.notice label {
  display: flex;
  align-items: start;
  gap: 0.5rem;
  overflow-wrap: anywhere;
}
.candidate-list li {
  padding: 0.6rem 0;
}
.notice .material-actions {
  margin-top: 0.8rem;
}
@media (max-width: 600px) {
  .materials-body {
    padding: 0 1rem 1rem;
  }
  .material-actions {
    align-items: stretch;
  }
}
</style>
