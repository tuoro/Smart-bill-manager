<template>
  <div ref="rootEl" class="sbm-pdf-preview" :style="{ height }">
    <div v-if="effectiveShowControls" class="sbm-pdf-toolbar">
      <div class="sbm-pdf-toolbar-left">
        <Button
          class="p-button-text"
          icon="pi pi-angle-left"
          aria-label="Previous page"
          :disabled="loading || !pdfReady || pageNumber <= 1"
          @click="pageNumber = Math.max(1, pageNumber - 1)"
        />
        <span class="sbm-pdf-page">{{ pageNumber }} / {{ numPages || 1 }}</span>
        <Button
          class="p-button-text"
          icon="pi pi-angle-right"
          aria-label="Next page"
          :disabled="loading || !pdfReady || pageNumber >= numPages"
          @click="pageNumber = Math.min(numPages || 1, pageNumber + 1)"
        />
      </div>
      <div class="sbm-pdf-toolbar-right">
        <Button class="p-button-text" icon="pi pi-search-minus" aria-label="Zoom out" :disabled="loading || !pdfReady || zoom <= 0.6" @click="zoom = Math.max(0.6, +(zoom - 0.1).toFixed(2))" />
        <span class="sbm-pdf-zoom">{{ Math.round(zoom * 100) }}%</span>
        <Button class="p-button-text" icon="pi pi-search-plus" aria-label="Zoom in" :disabled="loading || !pdfReady || zoom >= 2.2" @click="zoom = Math.min(2.2, +(zoom + 0.1).toFixed(2))" />
        <Button class="p-button-text" icon="pi pi-external-link" aria-label="Open" :disabled="!src" @click="openInNewTab" />
      </div>
    </div>

    <div ref="canvasWrapEl" class="sbm-pdf-canvas-wrap">
      <Message v-if="error" severity="warn" :closable="false">
        PDF 预览加载失败：{{ error }}
        <Button class="p-button-text" icon="pi pi-external-link" :label="'打开原文件'" @click="openInNewTab" />
      </Message>
      <div v-else class="sbm-pdf-canvas-stage">
        <div v-if="loading" class="sbm-pdf-loading">正在加载 PDF…</div>
        <canvas ref="canvasEl" class="sbm-pdf-canvas" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { GlobalWorkerOptions, getDocument, type PDFDocumentProxy } from 'pdfjs-dist'

GlobalWorkerOptions.workerSrc = new URL('pdfjs-dist/build/pdf.worker.min.mjs', import.meta.url).toString()

const props = withDefaults(
  defineProps<{
    src: string
    height?: string
    showControls?: boolean
  }>(),
  {
    height: '60vh',
    showControls: false,
  }
)

const rootEl = ref<HTMLElement | null>(null)
const canvasWrapEl = ref<HTMLElement | null>(null)
const canvasEl = ref<HTMLCanvasElement | null>(null)

const loading = ref(false)
const error = ref<string | null>(null)
const pdfDoc = ref<PDFDocumentProxy | null>(null)
const numPages = ref(0)
const pageNumber = ref(1)
const zoom = ref(1)

const src = computed(() => String(props.src || '').trim())
const height = computed(() => String(props.height || '60vh'))
const pdfReady = computed(() => !!pdfDoc.value && numPages.value > 0)
const effectiveShowControls = computed(() => !!props.showControls || numPages.value > 1)

let loadingTask: { destroy?: () => void } | null = null
let renderTask: { cancel?: () => void } | null = null
let resizeObserver: ResizeObserver | null = null
let renderSeq = 0

const cleanupPdf = async () => {
  try {
    renderTask?.cancel?.()
  } catch {}
  renderTask = null

  try {
    await pdfDoc.value?.destroy?.()
  } catch {}
  pdfDoc.value = null
  numPages.value = 0

  try {
    loadingTask?.destroy?.()
  } catch {}
  loadingTask = null
}

const openInNewTab = () => {
  const u = src.value
  if (!u) return
  window.open(u, '_blank')
}

const resolveRequestUrl = (u: string) => {
  // Ensure axios treats it as an absolute URL (so it won't prefix /api baseURL from our auth client).
  try {
    return new URL(u, window.location.origin).toString()
  } catch {
    return u
  }
}

const loadPdf = async () => {
  const u = src.value
  await cleanupPdf()

  if (!u) {
    error.value = null
    return
  }

  loading.value = true
  error.value = null
  const seq = ++renderSeq
  try {
    const token = localStorage.getItem('token')
    const res = await axios.get<ArrayBuffer>(resolveRequestUrl(u), {
      responseType: 'arraybuffer',
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    })
    if (seq !== renderSeq) return
    const bytes = new Uint8Array(res.data)
    const task = getDocument({ data: bytes })
    loadingTask = task as any
    const doc = await task.promise
    if (seq !== renderSeq) {
      await doc.destroy()
      return
    }

    pdfDoc.value = doc
    numPages.value = doc.numPages || 0
    pageNumber.value = 1
    await renderPage()
  } catch (e: any) {
    if (seq !== renderSeq) return
    error.value = String(e?.message || e || '未知错误')
  } finally {
    if (seq === renderSeq) loading.value = false
  }
}

const getStageWidth = () => {
  const el = canvasWrapEl.value || rootEl.value
  if (!el) return 0
  const style = getComputedStyle(el)
  const padX = (parseFloat(style.paddingLeft || '0') || 0) + (parseFloat(style.paddingRight || '0') || 0)
  return Math.max(0, el.clientWidth - padX)
}

const renderPage = async () => {
  const doc = pdfDoc.value
  const canvas = canvasEl.value
  if (!doc || !canvas) return
  if (pageNumber.value < 1 || pageNumber.value > (doc.numPages || 1)) return

  const seq = ++renderSeq
  try {
    renderTask?.cancel?.()
  } catch {}

  const page = await doc.getPage(pageNumber.value)
  if (seq !== renderSeq) return

  const stageWidth = getStageWidth()
  const baseViewport = page.getViewport({ scale: 1 })
  const fitScale = stageWidth > 0 ? stageWidth / baseViewport.width : 1
  const dpr = Math.max(1, Math.min(2, window.devicePixelRatio || 1))
  const viewport = page.getViewport({ scale: fitScale * zoom.value * dpr })

  canvas.width = Math.floor(viewport.width)
  canvas.height = Math.floor(viewport.height)
  canvas.style.width = `${Math.floor(viewport.width / dpr)}px`
  canvas.style.height = `${Math.floor(viewport.height / dpr)}px`

  const ctx = canvas.getContext('2d', { alpha: false })
  if (!ctx) return
  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.clearRect(0, 0, canvas.width, canvas.height)

  const task = page.render({ canvasContext: ctx, viewport })
  renderTask = task as any
  await task.promise
}

watch(src, () => {
  void loadPdf()
})

watch([pageNumber, zoom], () => {
  if (!pdfReady.value) return
  void renderPage()
})

onMounted(() => {
  if (!rootEl.value) return
  resizeObserver = new ResizeObserver(() => {
    if (!pdfReady.value) return
    void renderPage()
  })
  resizeObserver.observe(rootEl.value)
  void loadPdf()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
  void cleanupPdf()
})
</script>

<style scoped>
.sbm-pdf-preview {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
}

.sbm-pdf-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 6px 2px;
  color: var(--color-text-secondary);
}

.sbm-pdf-toolbar-left,
.sbm-pdf-toolbar-right {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.sbm-pdf-page,
.sbm-pdf-zoom {
  font-size: 12px;
  font-weight: 800;
  color: var(--color-text-secondary);
  min-width: 64px;
  text-align: center;
}

.sbm-pdf-canvas-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
  border-radius: var(--radius-md);
  background: var(--p-content-background, #fff);
}

.sbm-pdf-canvas-stage {
  position: relative;
  width: 100%;
  min-height: 100%;
  padding: 0;
  display: flex;
  justify-content: center;
}

.sbm-pdf-loading {
  position: absolute;
  top: 10px;
  left: 10px;
  font-size: 12px;
  color: var(--color-text-tertiary);
  font-weight: 800;
}

.sbm-pdf-canvas {
  display: block;
  max-width: 100%;
  border-radius: var(--radius-md);
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.06);
  background: #fff;
}
</style>
