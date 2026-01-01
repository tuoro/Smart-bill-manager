<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>系统设置</span>
          <div class="toolbar">
            <Button class="p-button-outlined" icon="pi pi-refresh" label="刷新" :loading="loading" @click="load" />
            <Button icon="pi pi-save" label="保存" :loading="saving" :disabled="!dirty" @click="save" />
          </div>
        </div>
      </template>

      <template #content>
        <Message v-if="!isAdmin" severity="warn" :closable="false">仅管理员可访问</Message>

        <div v-else class="content">
          <div class="section">
            <div class="section-title">去重</div>
            <div class="grid">
              <div class="field">
                <label>Hash 强去重</label>
                <div class="field-row">
                  <InputSwitch v-model="form.dedupe.strict_hash_reject" />
                </div>
                <small class="muted">开启后：文件 hash 完全一致会直接判定重复（更安全）。</small>
              </div>

              <div class="field">
                <label>软判重（提示）</label>
                <div class="field-row">
                  <InputSwitch v-model="form.dedupe.soft_enabled" />
                </div>
                <small class="muted">用于金额+时间、发票号等“疑似重复”提示。</small>
              </div>

              <div class="field">
                <label>支付：金额+时间窗口（分钟）</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.dedupe.payment_amount_time_window_minutes"
                    :min="0"
                    :max="60"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.dedupe.soft_enabled"
                  />
                </div>
                <small class="muted">0 表示不做金额+时间软判重。</small>
              </div>

              <div class="field">
                <label>支付：最多候选数</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.dedupe.payment_amount_time_max_candidates"
                    :min="1"
                    :max="20"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.dedupe.soft_enabled"
                  />
                </div>
              </div>

              <div class="field">
                <label>发票：发票号最多候选数</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.dedupe.invoice_number_max_candidates"
                    :min="1"
                    :max="20"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.dedupe.soft_enabled"
                  />
                </div>
              </div>
            </div>
          </div>

          <Divider />

          <div class="section">
            <div class="section-title">清理</div>
            <div class="grid">
              <div class="field">
                <label>启用草稿清理</label>
                <div class="field-row">
                  <InputSwitch v-model="form.cleanup.enabled" />
                </div>
                <small class="muted">用于清理刷新/取消/异常退出遗留的草稿记录与文件。</small>
              </div>

              <div class="field">
                <label>草稿保留（小时）</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.cleanup.draft_ttl_hours"
                    :min="0"
                    :max="2160"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.cleanup.enabled"
                  />
                </div>
              </div>

              <div class="field">
                <label>清理间隔（分钟）</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.cleanup.interval_minutes"
                    :min="1"
                    :max="1440"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.cleanup.enabled"
                  />
                </div>
              </div>

              <div class="field">
                <label>孤儿文件清理（小时）</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.cleanup.orphan_file_ttl_hours"
                    :min="0"
                    :max="2160"
                    showButtons
                    buttonLayout="horizontal"
                    :step="1"
                    :disabled="!form.cleanup.enabled"
                  />
                </div>
                <small class="muted">0 表示不清理孤儿文件（uploads 内不被任何记录引用的旧文件）。</small>
              </div>

              <div class="field">
                <label>单次最多删除</label>
                <div class="field-row">
                  <InputNumber
                    v-model="form.cleanup.max_delete_per_run"
                    :min="10"
                    :max="5000"
                    showButtons
                    buttonLayout="horizontal"
                    :step="10"
                    :disabled="!form.cleanup.enabled"
                  />
                </div>
              </div>
            </div>
          </div>

          <Divider />

          <div class="section">
            <div class="section-title">OCR</div>
            <div class="grid">
              <div class="field">
                <label>引擎</label>
                <div class="field-row">
                  <Dropdown v-model="form.ocr.engine" :options="ocrEngines" class="dropdown" />
                </div>
                <small class="muted">当前版本仅支持 RapidOCR。</small>
              </div>

              <div class="field">
                <label>模式</label>
                <div class="field-row">
                  <Dropdown v-model="form.ocr.worker_mode" :options="ocrWorkerModes" class="dropdown" />
                </div>
                <small class="muted">worker 模式后续用于常驻 OCR 进程（需要额外实现）。</small>
              </div>

              <div class="field">
                <label>并发数</label>
                <div class="field-row">
                  <InputNumber v-model="form.ocr.max_concurrency" :min="1" :max="16" showButtons buttonLayout="horizontal" :step="1" />
                </div>
              </div>

              <div class="field">
                <label>OCR 超时（毫秒）</label>
                <div class="field-row">
                  <InputNumber v-model="form.ocr.timeout_ms" :min="1000" :max="300000" showButtons buttonLayout="horizontal" :step="1000" />
                </div>
              </div>
            </div>
          </div>

          <small class="muted bottom-note">
            提示：去重与清理会即时生效；OCR 引擎/模式等配置可能需要重启服务或后续功能接入。
          </small>
        </div>
      </template>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Divider from 'primevue/divider'
import InputSwitch from 'primevue/inputswitch'
import InputNumber from 'primevue/inputnumber'
import Dropdown from 'primevue/dropdown'
import { useToast } from 'primevue/usetoast'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/api'

type SystemSettings = {
  ocr: { engine: string; worker_mode: string; max_concurrency: number; timeout_ms: number }
  dedupe: {
    strict_hash_reject: boolean
    soft_enabled: boolean
    payment_amount_time_window_minutes: number
    payment_amount_time_max_candidates: number
    invoice_number_max_candidates: number
  }
  cleanup: {
    enabled: boolean
    draft_ttl_hours: number
    interval_minutes: number
    orphan_file_ttl_hours: number
    max_delete_per_run: number
  }
}

const authStore = useAuthStore()
const toast = useToast()

const isAdmin = computed(() => authStore.user?.role === 'admin')

const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)

const ocrEngines = [{ label: 'RapidOCR', value: 'rapidocr' }]
const ocrWorkerModes = [
  { label: '当前模式（process）', value: 'process' },
  { label: 'Worker（规划中）', value: 'worker' },
]

const form = reactive<SystemSettings>({
  ocr: { engine: 'rapidocr', worker_mode: 'process', max_concurrency: 2, timeout_ms: 60000 },
  dedupe: {
    strict_hash_reject: true,
    soft_enabled: true,
    payment_amount_time_window_minutes: 5,
    payment_amount_time_max_candidates: 5,
    invoice_number_max_candidates: 5,
  },
  cleanup: { enabled: true, draft_ttl_hours: 6, interval_minutes: 15, orphan_file_ttl_hours: 0, max_delete_per_run: 200 },
})

let lastLoadedSnapshot = ''

const snapshot = (v: SystemSettings) => JSON.stringify(v)

const load = async () => {
  if (!isAdmin.value) return
  loading.value = true
  try {
    const res = await authApi.adminGetSettings()
    if (res.data.success && res.data.data) {
      Object.assign(form, res.data.data as any)
      lastLoadedSnapshot = snapshot(form as any)
      dirty.value = false
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '获取系统设置失败', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '获取系统设置失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

const save = async () => {
  if (!isAdmin.value) return
  saving.value = true
  try {
    const patch = {
      ocr: { ...form.ocr },
      dedupe: { ...form.dedupe },
      cleanup: { ...form.cleanup },
    }
    const res = await authApi.adminUpdateSettings(patch)
    if (res.data.success && res.data.data) {
      Object.assign(form, res.data.data as any)
      lastLoadedSnapshot = snapshot(form as any)
      dirty.value = false
      toast.add({ severity: 'success', summary: '系统设置已保存', life: 2000 })
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '保存系统设置失败', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '保存系统设置失败', life: 3000 })
  } finally {
    saving.value = false
  }
}

watch(
  () => snapshot(form as any),
  (s) => {
    if (!lastLoadedSnapshot) return
    dirty.value = s !== lastLoadedSnapshot
  }
)

onMounted(() => {
  load()
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

.content {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.section-title {
  font-weight: 900;
  margin-bottom: 10px;
}

.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--p-surface-0), transparent 0%);
  border: 1px solid color-mix(in srgb, var(--p-surface-200), transparent 20%);
}

.field-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

label {
  font-weight: 700;
  color: var(--p-text-color);
}

.muted {
  color: var(--p-text-muted-color);
}

.dropdown {
  min-width: 220px;
}

.bottom-note {
  margin-top: 6px;
}

@media (max-width: 900px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>

