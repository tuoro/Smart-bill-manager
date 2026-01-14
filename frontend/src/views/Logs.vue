<template>
  <div class="logs-page">
    <Card class="panel sbm-surface">
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
            <MultiSelect
              v-model="selectedCategories"
              :options="categoryOptions"
              optionLabel="label"
              optionValue="name"
              display="chip"
              :placeholder="'\u9009\u62E9\u65E5\u5FD7\u7C7B\u522B'"
              :maxSelectedLabels="2"
              class="sources"
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
            <Button :label="'\u91CD\u8FDE'" icon="pi pi-refresh" :loading="connecting" :disabled="connecting" @click="reconnect" />
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
type LogEvent = { type: 'log' | 'ping'; timestamp: string; source?: string; message?: string; category?: string }

const toast = useToast()
const API_BASE_URL = import.meta.env.VITE_API_URL || '/api'

const sources = ref<LogSource[]>([])
const selectedSources = ref<string[]>(['backend_out', 'backend_err'])
const selectedCategories = ref<string[]>([])

const logs = ref<LogEvent[]>([])
const connected = ref(false)
const connecting = ref(false)
const filter = ref('')
const autoScroll = ref(true)
const logContainer = ref<HTMLDivElement | null>(null)

let abortController: AbortController | null = null
let streamAttempt = 0

const sourceOptions = computed(() =>
  sources.value.map((s) => ({
    name: s.name,
    label: s.available ? s.name : `${s.name} (\u4E0D\u53EF\u7528)`,
    disabled: !s.available,
  })),
)

const normalizeCategory = (v: string): string =>
  v.trim().toLowerCase().replace(/\s+/g, '_')

const isTimingTag = (v: string): boolean =>
  /^\d+(\.\d+)?(ns|us|µs|ms|s)$/.test(v)

const isRowsTag = (v: string): boolean =>
  /^rows:\d+$/i.test(v)

const looksLikeSQL = (msg: string): boolean => {
  const s = msg.toUpperCase()
  if (!/(SELECT|INSERT|UPDATE|DELETE)\s/.test(s)) return false
  // Common gorm log fragments: "[rows:0]" and statement verbs.
  if (/\bROWS:\d+\b/i.test(msg)) return true
  if (/INSERT\s+INTO\s/.test(s)) return true
  if (/UPDATE\s+\S+\s+SET\s/.test(s)) return true
  if (/DELETE\s+FROM\s/.test(s)) return true
  if (/SELECT\s+.+\s+FROM\s/.test(s)) return true
  return false
}

const inferCategory = (evt: LogEvent): string => {
  const msg = String(evt.message || '')

  // Prefer known subsystem tags regardless of other bracket fragments.
  if (msg.includes('[Email Monitor]')) return 'email_monitor'
  if (msg.includes('[DB]')) return 'db'
  if (msg.includes('[Regression]')) return 'regression'

  // Parse bracket tags, skipping gorm timing/rows markers like "[0.013ms]" or "[rows:0]".
  const re = /\[([^\]]+)\]/g
  let m: RegExpExecArray | null
  while ((m = re.exec(msg))) {
    const raw = String(m?.[1] || '')
    const norm = normalizeCategory(raw)
    if (!norm) continue
    if (isTimingTag(norm) || isRowsTag(norm)) continue
    return norm
  }

  if (looksLikeSQL(msg)) return 'db_sql'

  // Fall back to broad source-based categories.
  const src = String(evt.source || '').toLowerCase()
  if (src.includes('nginx')) return 'nginx'
  if (src.includes('backend')) return 'backend'
  return 'other'
}

const categoryOptions = computed(() => {
  const pinned = ['email_monitor', 'db', 'db_sql', 'nginx', 'backend', 'other']
  const set = new Set<string>(pinned)
  for (const l of logs.value) {
    if (l.type !== 'log') continue
    const cat = String(l.category || '').trim()
    if (cat) set.add(cat)
  }
  return Array.from(set)
    .sort((a, b) => {
      const ai = pinned.indexOf(a)
      const bi = pinned.indexOf(b)
      if (ai >= 0 && bi >= 0) return ai - bi
      if (ai >= 0) return -1
      if (bi >= 0) return 1
      return a.localeCompare(b)
    })
    .map((c) => ({ name: c, label: c }))
})

const filteredLogs = computed(() => {
  const q = filter.value.trim()
  const cats = selectedCategories.value
  return logs.value.filter((l) => {
    if (l.type !== 'log') return false
    if (cats.length > 0 && !cats.includes(String(l.category || ''))) return false
    if (!q) return true
    return (l.message || '').includes(q) || (l.source || '').includes(q)
  })
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
  evt.category = inferCategory(evt)
  logs.value.push(evt)
  if (logs.value.length > 2000) {
    logs.value.splice(0, logs.value.length - 2000)
  }
  scrollToBottom()
}

const stopStream = () => {
  streamAttempt++
  abortController?.abort()
  abortController = null
  connected.value = false
  connecting.value = false
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
  const attempt = ++streamAttempt

  if (selectedSources.value.length === 0) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u9009\u62E9\u81F3\u5C11\u4E00\u4E2A\u65E5\u5FD7\u6E90', life: 2500 })
    return
  }

  const token = localStorage.getItem('token')
  if (!token) {
    toast.add({ severity: 'error', summary: '\u672A\u767B\u5F55\uFF0C\u65E0\u6CD5\u67E5\u770B\u65E5\u5FD7', life: 3000 })
    return
  }

  const isCurrent = (ctrl?: AbortController | null) => attempt === streamAttempt && (!ctrl || abortController === ctrl)

  connecting.value = true
  const controller = new AbortController()
  abortController = controller

  try {
    const url = `${API_BASE_URL}/logs/stream?sources=${encodeURIComponent(selectedSources.value.join(','))}`
    // Avoid getting stuck in a perpetual "connecting" state when the network silently hangs.
    const timeoutMs = 12000
    const timeout = window.setTimeout(() => controller.abort(), timeoutMs)
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    }).finally(() => window.clearTimeout(timeout))

    if (!res.ok) {
      const text = await res.text()
      throw new Error(text || `HTTP ${res.status}`)
    }
    if (!res.body) {
      throw new Error('\u6D4F\u89C8\u5668\u4E0D\u652F\u6301\u6D41\u5F0F\u8BFB\u53D6')
    }

    if (isCurrent(controller)) {
      connected.value = true
      connecting.value = false
    }
    const reader = res.body.getReader()
    const decoder = new TextDecoder('utf-8')
    let buffer = ''

    while (true) {
      if (!isCurrent(controller)) break
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
    if (isCurrent(controller)) {
      connecting.value = false
      connected.value = false
      abortController = null
    }
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
  border: 1px solid color-mix(in srgb, var(--p-content-border-color), transparent 20%);
  background: color-mix(in srgb, var(--p-content-background), transparent 12%);
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
  border: 1px solid color-mix(in srgb, var(--p-content-border-color), transparent 20%);
  background: color-mix(in srgb, var(--p-content-background), transparent 10%);
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
  color: color-mix(in srgb, var(--p-text-muted-color), transparent 20%);
}

.src {
  flex: 0 0 auto;
  color: var(--p-text-muted-color);
}

.msg {
  flex: 1 1 auto;
}
</style>
