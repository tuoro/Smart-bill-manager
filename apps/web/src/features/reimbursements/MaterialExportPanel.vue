<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { sessionStore } from '../../app/session'
import {
  api,
  ApiError,
  type ExportManifest,
  type ExportPrepared,
  type ExportScope,
} from '../../data/client'

const props = defineProps<{ scope: ExportScope; disabled?: boolean }>()
const permitted = computed(() => {
  const capabilities = sessionStore.current.value?.capabilities ?? []
  return (
    capabilities.includes('facts.read') &&
    capabilities.includes('review.source.read') &&
    (props.scope.kind === 'trip' || capabilities.includes('reimbursements.read'))
  )
})
const label = computed(() => (props.scope.kind === 'trip' ? '导出当前行程材料' : '导出此报销快照'))
const opened = ref(false)
const manifest = ref<ExportManifest>()
const prepared = ref<ExportPrepared>()
const acknowledged = ref(false)
const busy = ref<'preview' | 'prepare' | 'cancel'>()
const error = ref('')
const message = ref('')
const trigger = ref<HTMLButtonElement>()
const heading = ref<HTMLElement>()
const downloadLink = ref<HTMLAnchorElement>()
const refreshButton = ref<HTMLButtonElement>()
let controller: AbortController | undefined
let epoch = 0
let mounted = true

function size(bytes: number): string {
  return bytes < 1024 * 1024
    ? `${(bytes / 1024).toFixed(1)} KiB`
    : `${(bytes / 1024 / 1024).toFixed(1)} MiB`
}

async function release(value: ExportPrepared): Promise<boolean> {
  try {
    await api.cancelMaterialExport(value.id)
    return true
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 404) return true
    // 取消结果未知不是成功；页面可重试，离开后的孤立句柄仍由服务端绝对期限回收。
    error.value = '取消结果未确认，请重试；未领取的包最迟在准备完成 5 分钟后自动释放。'
    return false
  }
}

async function preview() {
  if (props.disabled || busy.value || prepared.value) return
  opened.value = true
  error.value = ''
  message.value = ''
  manifest.value = undefined
  acknowledged.value = false
  const current = ++epoch
  controller = new AbortController()
  busy.value = 'preview'
  await nextTick()
  heading.value?.focus()
  try {
    const value = await api.previewMaterialExport(props.scope, controller.signal)
    if (current === epoch && mounted) manifest.value = value
  } catch (cause) {
    if (current === epoch && mounted)
      error.value = cause instanceof ApiError ? cause.message : '材料清单读取失败，请重试。'
  } finally {
    if (current === epoch) {
      busy.value = undefined
      controller = undefined
    }
  }
}

async function prepare() {
  const snapshot = manifest.value
  if (
    !snapshot ||
    props.disabled ||
    busy.value ||
    prepared.value ||
    (snapshot.warnings.length > 0 && !acknowledged.value)
  )
    return
  const current = ++epoch
  controller = new AbortController()
  busy.value = 'prepare'
  error.value = ''
  message.value = ''
  try {
    const value = await api.prepareMaterialExport(
      snapshot.scope,
      snapshot.manifest_hash,
      acknowledged.value,
      controller.signal,
    )
    if (current !== epoch || !mounted) {
      await release(value)
      return
    }
    prepared.value = value
    message.value = '材料已完整校验并准备好，请在 5 分钟内下载一次。'
  } catch (cause) {
    if (current !== epoch || !mounted) return
    error.value =
      cause instanceof ApiError
        ? cause.message
        : '准备结果未确认；没有可下载的成功结果。未领取包最迟 5 分钟后自动释放，请勿连续重复准备。'
    if (cause instanceof ApiError && cause.status === 409) {
      manifest.value = undefined
      acknowledged.value = false
    }
  } finally {
    if (current === epoch) {
      busy.value = undefined
      controller = undefined
    }
    await nextTick()
    if (current === epoch && mounted) {
      if (prepared.value) downloadLink.value?.focus()
      else if (!manifest.value) refreshButton.value?.focus()
    }
  }
}

async function cancelPending() {
  ++epoch
  controller?.abort()
  controller = undefined
  busy.value = undefined
  message.value = '已取消本次等待；若响应在取消前已生成，未领取包仍会在 5 分钟内自动释放。'
  await nextTick()
  heading.value?.focus()
}

async function close() {
  cancelPending()
  if (prepared.value) {
    busy.value = 'cancel'
    const released = await release(prepared.value)
    busy.value = undefined
    if (!released) return
  }
  prepared.value = undefined
  manifest.value = undefined
  opened.value = false
  await nextTick()
  trigger.value?.focus()
}

async function handOffDownload(event: MouseEvent) {
  if (busy.value || props.disabled || !prepared.value) {
    event.preventDefault()
    return
  }
  prepared.value = undefined
  message.value =
    '下载请求已交给浏览器；若新页面显示过期或权限错误，请返回此处重新预览准备。是否保存成功以浏览器下载列表为准。'
  manifest.value = undefined
  await nextTick()
  refreshButton.value?.focus()
}

onUnmounted(() => {
  mounted = false
  ++epoch
  controller?.abort()
  const pending = prepared.value
  if (pending) void release(pending)
})
</script>

<template>
  <div v-if="permitted" class="material-export">
    <button
      v-if="!opened"
      ref="trigger"
      class="button button-small"
      type="button"
      :disabled="disabled"
      @click="preview"
    >
      {{ label }}
    </button>
    <section v-else class="export-workspace" aria-label="材料包导出">
      <div class="panel-heading">
        <h3 ref="heading" tabindex="-1">{{ label }}</h3>
        <button
          class="button button-small"
          type="button"
          :disabled="busy === 'cancel'"
          @click="close"
        >
          关闭导出
        </button>
      </div>
      <p class="quiet">
        {{
          scope.kind === 'trip'
            ? '包含本行程全部活动支付、发票、行程凭证及发票辅助材料，不受当前列表分页或筛选影响。'
            : '只包含此报销快照的已知原件与固定辅助材料，不补入当前行程凭证或后来添加的附件。'
        }}
      </p>
      <p v-if="error" class="notice notice-danger" role="alert">{{ error }}</p>
      <p v-if="message" class="quiet" role="status">{{ message }}</p>
      <div v-if="busy === 'preview' || busy === 'prepare'" class="inline-actions" role="status">
        <span>{{ busy === 'preview' ? '正在核对完整材料清单…' : '正在逐份校验并准备 ZIP…' }}</span>
        <button class="button button-small" type="button" @click="cancelPending">
          取消{{ busy === 'prepare' ? '准备' : '读取' }}
        </button>
      </div>
      <template v-if="manifest">
        <p>
          <strong>{{ manifest.name }}</strong> · {{ manifest.references.length }} 个业务引用 ·
          {{ manifest.files.length }} 份去重文件 · {{ size(manifest.source_bytes) }}
        </p>
        <p class="quiet">
          {{ scope.kind === 'trip' ? '当前行程' : '固定报销' }} v{{
            manifest.version
          }}；包内清单保留正式字段版本和每个业务引用。
        </p>
        <div v-if="manifest.warnings.length" class="notice notice-warning notice-stack">
          <p v-for="warning in manifest.warnings" :key="warning">{{ warning }}</p>
          <label
            ><input v-model="acknowledged" type="checkbox" :disabled="!!busy || !!prepared" />
            我已理解并确认以上材料范围限制</label
          >
        </div>
        <details>
          <summary>查看全部 {{ manifest.files.length }} 份文件</summary>
          <ol class="export-files">
            <li v-for="file in manifest.files" :key="file.document_id">
              <strong>{{ file.original_name }}</strong>
              <span>{{ size(file.size_bytes) }} · {{ file.path }}</span>
              <small>单据 {{ file.document_id }}</small>
            </li>
          </ol>
        </details>
        <button
          v-if="!prepared"
          class="button button-primary"
          type="button"
          :disabled="disabled || !!busy || (manifest.warnings.length > 0 && !acknowledged)"
          @click="prepare"
        >
          确认清单并准备 ZIP
        </button>
      </template>
      <div v-if="prepared" class="inline-actions">
        <button v-if="busy || disabled" class="button button-primary" type="button" disabled>
          下载 ZIP（{{ size(prepared.size_bytes) }}）
        </button>
        <a
          v-else
          ref="downloadLink"
          class="button button-primary"
          :href="api.materialExportURL(prepared.id)"
          target="_blank"
          rel="noopener"
          @click="handOffDownload"
          >下载 ZIP（{{ size(prepared.size_bytes) }}）</a
        >
        <span class="quiet"
          >有效至
          {{ new Date(prepared.expires_at).toLocaleTimeString('zh-CN') }}，过期需重新准备。</span
        >
      </div>
      <button
        v-if="!busy && !manifest && !prepared"
        ref="refreshButton"
        class="button"
        type="button"
        :disabled="disabled"
        @click="preview"
      >
        重新预览材料
      </button>
      <p class="quiet">
        每包最多 1,000 份文件、512 MiB
        原件；不是数据库备份。原生下载不支持续传，请只向有权接收者交付。
      </p>
    </section>
  </div>
</template>

<style scoped>
.material-export {
  min-width: 0;
  width: 100%;
}
.export-workspace {
  display: grid;
  gap: 14px;
  padding: 18px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  overflow-wrap: anywhere;
}
.export-workspace .panel-heading {
  margin: 0;
}
.export-workspace p {
  margin: 0;
}
.export-workspace label {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.export-files {
  max-height: 300px;
  overflow-y: auto;
  padding-left: 26px;
}
.export-files li {
  padding: 8px 4px;
}
.export-files span,
.export-files small {
  display: block;
  color: var(--text-muted);
}
.export-workspace > .button {
  justify-self: start;
}
</style>
