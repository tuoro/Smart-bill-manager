<template>
  <div class="logs-page">
    <Card class="panel">
      <template #title>
        <div class="header">
          <div class="title">
            <span>&#23454;&#26102;&#26085;&#24535;</span>
            <Tag
              :severity="connected ? 'success' : 'secondary'"
              :value="connected ? '\u5DF2\u8FDE\u63A5' : '\u672A\u8FDE\u63A5'"
            />
          </div>
          <div class="controls">
            <MultiSelect
              v-model="selectedSources"
              :options="sourceOptions"
              optionLabel="label"
              optionValue="name"
              optionDisabled="disabled"
              display="chip"
              :placeholder="'\u9009\u62E9\u65E5\u5FD7\u6E90'"
              :maxSelectedLabels="2"
              class="sources"
              @change="handleSourcesChange"
            />
            <span class="p-input-icon-left">
              <i class="pi pi-filter" />
              <InputText v-model="filter" :placeholder="'\u8FC7\u6EE4\u5173\u952E\u8BCD'" />
            </span>
            <div class="switch">
              <span class="switch-label">&#33258;&#21160;&#28378;&#21160;</span>
              <InputSwitch v-model="autoScroll" />
            </div>
            <Button :label="'\u6E05\u7A7A'" class="p-button-outlined" severity="secondary" @click="clearLogs" />
            <Button :label="'\u91CD\u8FDE'" icon="pi pi-refresh" :loading="connecting" @click="reconnect" />
          </div>
        </div>
      </template>

      <template #content>
        <div ref="logContainer" class="log-container">
          <div v-for="(l, idx) in filteredLogs" :key="idx" class="log-line">
            <span class="ts">{{ formatTs(l.timestamp) }}</span>
            <span class="src">[{{ l.source }}]</span>
            <span class="msg">{{ l.message }}</span>
          </div>
        </div>
      </template>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import MultiSelect from 'primevue/multiselect'
import InputText from 'primevue/inputtext'
import InputSwitch from 'primevue/inputswitch'
import Button from 'primevue/button'
import { useToast } from 'primevue/usetoast'
import api from '@/api/auth'

type LogSource = { name: string; available: boolean }
type LogEvent = { type: 'log' | 'ping'; timestamp: string; source?: string; message?: string }

const toast = useToast()
const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'

const sources = ref<LogSource[]>([])
const selectedSources = ref<string[]>(['backend_out', 'backend_err'])

const logs = ref<LogEvent[]>([])
const connected = ref(false)
const connecting = ref(false)
const filter = ref('')
const autoScroll = ref(true)
const logContainer = ref<HTMLDivElement | null>(null)

let abortController: AbortController | null = null

const sourceOptions = computed(() =>
  sources.value.map((s) => ({
    name: s.name,
    label: s.available ? s.name : `${s.name} (\u4E0D\u53EF\u7528)`,
    disabled: !s.available,
  })),
)

const filteredLogs = computed(() => {
  const q = filter.value.trim()
  if (!q) return logs.value
  return logs.value.filter((l) => (l.message || '').includes(q) || (l.source || '').includes(q))
})

const formatTs = (ts: string) => {
  if (!ts) return ''
  try {
    return new Date(ts).toLocaleString()
  } catch {
    return ts
  }
}

const clearLogs = () => {
  logs.value = []
}

const scrollToBottom = async () => {
  if (!autoScroll.value) return
  await nextTick()
  const el = logContainer.value
  if (!el) return
  el.scrollTop = el.scrollHeight
}

const appendLog = (evt: LogEvent) => {
  if (evt.type !== 'log') return
  logs.value.push(evt)
  if (logs.value.length > 2000) {
    logs.value.splice(0, logs.value.length - 2000)
  }
  scrollToBottom()
}

const stopStream = () => {
  abortController?.abort()
  abortController = null
  connected.value = false
}

const loadSources = async () => {
  const res = await api.get('/logs/sources')
  if (res.data?.success && res.data.data) {
    sources.value = res.data.data
    const available = new Set(sources.value.filter((s) => s.available).map((s) => s.name))
    selectedSources.value = selectedSources.value.filter((s) => available.has(s))
    if (selectedSources.value.length === 0) {
      selectedSources.value = sources.value.filter((s) => s.available).slice(0, 2).map((s) => s.name)
    }
  }
}

const loadTail = async () => {
  if (selectedSources.value.length === 0) return
  const res = await api.get('/logs/tail', {
    params: { sources: selectedSources.value.join(','), lines: 200 },
  })
  if (res.data?.success && Array.isArray(res.data.data)) {
    for (const evt of res.data.data as LogEvent[]) {
      appendLog(evt)
    }
  }
}

const startStream = async () => {
  stopStream()

  if (selectedSources.value.length === 0) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u9009\u62E9\u81F3\u5C11\u4E00\u4E2A\u65E5\u5FD7\u6E90', life: 2500 })
    return
  }

  const token = localStorage.getItem('token')
  if (!token) {
    toast.add({ severity: 'error', summary: '\u672A\u767B\u5F55\uFF0C\u65E0\u6CD5\u67E5\u770B\u65E5\u5FD7', life: 3000 })
    return
  }

  connecting.value = true
  abortController = new AbortController()

  try {
    const url = `${API_BASE_URL}/logs/stream?sources=${encodeURIComponent(selectedSources.value.join(','))}`
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}` },
      signal: abortController.signal,
    })

    if (!res.ok) {
      const text = await res.text()
      throw new Error(text || `HTTP ${res.status}`)
    }
    if (!res.body) {
      throw new Error('\u6D4F\u89C8\u5668\u4E0D\u652F\u6301\u6D41\u5F0F\u8BFB\u53D6')
    }

    connected.value = true
    const reader = res.body.getReader()
    const decoder = new TextDecoder('utf-8')
    let buffer = ''

    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      let idx = buffer.indexOf('\n')
      while (idx >= 0) {
        const line = buffer.slice(0, idx).trim()
        buffer = buffer.slice(idx + 1)
        idx = buffer.indexOf('\n')
        if (!line) continue
        try {
          const evt = JSON.parse(line) as LogEvent
          appendLog(evt)
        } catch {
          appendLog({ type: 'log', timestamp: new Date().toISOString(), source: 'parse', message: line })
        }
      }
    }
  } catch (e: unknown) {
    if ((e as any)?.name !== 'AbortError') {
      const msg = e instanceof Error ? e.message : String(e)
      toast.add({ severity: 'error', summary: `\u65E5\u5FD7\u8FDE\u63A5\u5931\u8D25\uFF1A${msg}`, life: 3500 })
    }
  } finally {
    connecting.value = false
    connected.value = false
  }
}

const reconnect = async () => {
  clearLogs()
  await loadTail()
  await startStream()
}

const handleSourcesChange = async () => {
  await reconnect()
}

onMounted(async () => {
  try {
    await loadSources()
    await loadTail()
    await startStream()
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    toast.add({ severity: 'error', summary: `\u52A0\u8F7D\u65E5\u5FD7\u5931\u8D25\uFF1A${msg}`, life: 3500 })
  }
})

onBeforeUnmount(() => {
  stopStream()
})
</script>

<style scoped>
.logs-page {
  width: 100%;
}

.panel {
  border-radius: var(--radius-lg);
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.title {
  font-size: 16px;
  font-weight: 800;
  display: flex;
  align-items: center;
  gap: 10px;
}

.controls {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.sources {
  width: 280px;
  max-width: 92vw;
}

.switch {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: var(--radius-md);
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(255, 255, 255, 0.6);
}

.switch-label {
  font-weight: 600;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.log-container {
  height: 70vh;
  overflow: auto;
  border-radius: var(--radius-md);
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(0, 0, 0, 0.02);
  padding: 10px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.65;
}

.log-line {
  display: flex;
  gap: 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

.ts {
  flex: 0 0 auto;
  color: rgba(0, 0, 0, 0.45);
}

.src {
  flex: 0 0 auto;
  color: rgba(0, 0, 0, 0.65);
}

.msg {
  flex: 1 1 auto;
}
</style>

