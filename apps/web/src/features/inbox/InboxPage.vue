<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ApiError, api, type JobSummary } from '../../data/client'
import {
  batchUploadStateMeta,
  createUploadBatch,
  runUploadBatch,
  summarizeUploadBatch,
  type BatchUploadItem,
} from './batch'
import { canCancel, canRetry, canReview, jobStatusMeta } from './status'
import AppIcon from '../../components/AppIcon.vue'
import ManualReviewStart from './ManualReviewStart.vue'
import { sessionStore } from '../../app/session'

const manualJob = ref<JobSummary | null>(null)
let manualTrigger: HTMLElement | null = null
const canStartManual = computed(() =>
  sessionStore.current.value?.capabilities.includes('claims.review'),
)

function openManual(job: JobSummary, event: Event) {
  manualTrigger = event.currentTarget as HTMLElement
  manualJob.value = job
}

async function closeManual() {
  manualJob.value = null
  await nextTick()
  manualTrigger?.focus()
}

const jobs = ref<JobSummary[]>([])
const loading = ref(true)
const refreshing = ref(false)
const batchRunning = ref(false)
const uploadItems = ref<BatchUploadItem[]>([])
const actionJobId = ref('')
const error = ref('')
const offline = ref(!navigator.onLine)
const filter = ref<'all' | 'attention' | 'active' | 'done'>('all')
const fileInput = ref<HTMLInputElement | null>(null)
let pollTimer: number | undefined

const uploadSummary = computed(() => summarizeUploadBatch(uploadItems.value))
const uploadSummaryText = computed(() => {
  const summary = uploadSummary.value
  const completed = summary.queued + summary.duplicate + summary.rejected
  if (batchRunning.value && completed < summary.total) {
    return `${summary.total} 个文件，已处理 ${completed} 个，正在上传 ${summary.uploading} 个，等待 ${summary.waiting} 个`
  }
  return `${summary.total} 个文件处理完成：已入队 ${summary.queued} 个，已存在 ${summary.duplicate} 个，已拒绝 ${summary.rejected} 个`
})

const filteredJobs = computed(() => {
  if (filter.value === 'attention')
    return jobs.value.filter((job) => canReview(job.status) || job.status === 'failed')
  if (filter.value === 'active')
    return jobs.value.filter((job) =>
      ['queued', 'processing', 'cancel_requested'].includes(job.status),
    )
  if (filter.value === 'done')
    return jobs.value.filter((job) => ['completed', 'cancelled', 'rejected'].includes(job.status))
  return jobs.value
})

async function load(silent = false) {
  if (offline.value) return
  if (silent) refreshing.value = true
  else loading.value = true
  try {
    jobs.value = (await api.listJobs()).items
    error.value = ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '收件箱连接失败，请稍后重试'
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  if (files.length === 0) return
  error.value = ''
  batchRunning.value = true
  uploadItems.value = createUploadBatch(files)
  try {
    uploadItems.value = await runUploadBatch(uploadItems.value, api.upload, (items) => {
      uploadItems.value = [...items]
    })
    await load(true)
  } finally {
    batchRunning.value = false
    input.value = ''
  }
}

async function cancel(job: JobSummary) {
  actionJobId.value = job.id
  try {
    const updated = await api.cancelJob(job.id)
    replaceJob(updated)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '取消请求失败'
  } finally {
    actionJobId.value = ''
  }
}

async function retry(job: JobSummary) {
  actionJobId.value = job.id
  try {
    const updated = await api.retryJob(job.id)
    replaceJob(updated)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '重试请求失败'
  } finally {
    actionJobId.value = ''
  }
}

function replaceJob(updated: JobSummary) {
  const index = jobs.value.findIndex((job) => job.id === updated.id)
  if (index >= 0) jobs.value[index] = updated
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value) void load(true)
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024)
    return `${(value / (1024 * 1024)).toFixed(value >= 10 * 1024 * 1024 ? 0 : 1)} MiB`
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${value} B`
}

onMounted(() => {
  void load()
  pollTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible' && !batchRunning.value) void load(true)
  }, 2500)
  window.addEventListener('online', setOnlineState)
  window.addEventListener('offline', setOnlineState)
})

onUnmounted(() => {
  if (pollTimer) window.clearInterval(pollTimer)
  window.removeEventListener('online', setOnlineState)
  window.removeEventListener('offline', setOnlineState)
})
</script>

<template>
  <div class="page-stack inbox-page">
    <ManualReviewStart
      v-if="manualJob"
      :key="manualJob.id"
      :job="manualJob"
      :offline="offline"
      @close="closeManual"
    />
    <nav class="breadcrumb" aria-label="面包屑">
      <span>工作台</span><span aria-hidden="true">/</span><strong>AI 收件箱</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>AI 收件箱</h1>
        <p>从一张单据开始，让 AI 整理，由你确认。</p>
      </div>
      <label
        class="button upload-button"
        :class="{ 'button-primary': jobs.length > 0 || loading }"
        :aria-disabled="batchRunning || offline"
      >
        <input
          ref="fileInput"
          class="visually-hidden"
          type="file"
          accept="image/jpeg,image/png,image/webp,application/pdf"
          multiple
          :disabled="batchRunning || offline"
          @change="upload"
        />
        <AppIcon name="upload" />
        {{ batchRunning ? '正在逐项上传…' : '上传单据' }}
      </label>
    </header>

    <div v-if="offline" class="notice notice-warning" role="status">
      <span aria-hidden="true">!</span>
      <span>当前离线。已有任务仍保留在服务端，网络恢复后会自动重新连接。</span>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span><span>{{ error }}</span>
      <button class="text-button" type="button" @click="load()">重试</button>
    </div>

    <section
      v-if="uploadItems.length > 0"
      class="panel batch-panel"
      aria-labelledby="batch-title"
      :aria-busy="batchRunning"
    >
      <div class="panel-heading batch-heading">
        <div>
          <h2 id="batch-title">本次上传</h2>
          <p aria-live="polite" aria-atomic="true">{{ uploadSummaryText }}</p>
        </div>
        <span class="quiet">按选择顺序逐项处理</span>
      </div>
      <ol class="batch-list">
        <li v-for="item in uploadItems" :key="item.key" class="batch-item">
          <span class="batch-index numeric">{{ item.index + 1 }}</span>
          <span class="batch-file">
            <strong>{{ item.file.name }}</strong>
            <small>{{ formatBytes(item.file.size) }}</small>
          </span>
          <span class="status batch-status" :data-tone="batchUploadStateMeta[item.state].tone">
            <span aria-hidden="true">●</span>{{ batchUploadStateMeta[item.state].label }}
          </span>
          <span class="batch-message" :class="{ 'danger-text': item.state === 'rejected' }">
            {{ item.message }}
          </span>
        </li>
      </ol>
    </section>

    <section
      class="panel queue-panel"
      aria-labelledby="queue-title"
      :aria-busy="loading || refreshing"
    >
      <div class="panel-heading queue-heading">
        <h2 id="queue-title" class="visually-hidden">处理队列</h2>
        <div class="segmented" aria-label="队列筛选">
          <button
            v-for="item in [
              ['all', '全部'],
              ['attention', '需处理'],
              ['active', '处理中'],
              ['done', '已结束'],
            ] as const"
            :key="item[0]"
            type="button"
            :aria-pressed="filter === item[0]"
            @click="filter = item[0]"
          >
            {{ item[1] }}
          </button>
        </div>
        <span class="queue-count"
          >{{ jobs.length }} 个任务<span v-if="refreshing"> · 正在同步</span></span
        >
      </div>

      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span>
        <strong>正在读取收件箱</strong>
        <span>任务状态会在加载后自动刷新。</span>
      </div>

      <div v-else-if="filteredJobs.length === 0" class="state-layout inbox-empty">
        <span class="empty-icon"><AppIcon :name="jobs.length ? 'inbox' : 'document'" /></span>
        <strong>{{ jobs.length ? '当前筛选没有任务' : '收件箱还是空的' }}</strong>
        <span>{{
          jobs.length
            ? '切换筛选查看其他状态。'
            : '支付截图、发票或行程单，上传后会自动整理为待审核记录。'
        }}</span>
        <button
          v-if="jobs.length === 0"
          class="button button-primary empty-upload"
          type="button"
          :disabled="batchRunning || offline"
          @click="fileInput?.click()"
        >
          <AppIcon name="upload" />上传第一张单据
        </button>
        <button v-else class="text-button" type="button" @click="filter = 'all'">
          查看全部任务
        </button>
        <small v-if="jobs.length === 0" class="upload-formats"
          >支持 JPG、PNG、WebP、PDF · 单文件最大 20 MiB · 每批最多 20 个</small
        >
      </div>

      <div v-else class="table-scroll">
        <table class="data-table queue-table">
          <thead>
            <tr>
              <th scope="col">单据</th>
              <th scope="col">状态</th>
              <th scope="col">进入时间</th>
              <th scope="col">需要处理</th>
              <th scope="col" class="actions-column">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in filteredJobs" :key="job.id">
              <td>
                <div class="document-cell">
                  <span class="document-mark" aria-hidden="true">{{
                    job.detected_mime === 'application/pdf' ? 'PDF' : 'IMG'
                  }}</span
                  ><span
                    ><strong>{{ job.original_name }}</strong
                    ><small>{{ job.id }}</small></span
                  >
                </div>
              </td>
              <td>
                <span class="status" :data-tone="jobStatusMeta[job.status].tone"
                  ><span aria-hidden="true">●</span>{{ jobStatusMeta[job.status].label }}</span
                >
              </td>
              <td class="numeric">
                <time :datetime="job.created_at">{{ formatDate(job.created_at) }}</time
                ><small>尝试 {{ job.attempt_count }} 次</small>
              </td>
              <td>
                <span
                  :class="{ 'danger-text': job.status === 'blocked' || job.status === 'failed' }"
                  >{{ job.safe_error_message || jobStatusMeta[job.status].description }}</span
                >
              </td>
              <td class="row-actions">
                <RouterLink
                  v-if="canReview(job.status)"
                  class="button button-small button-primary"
                  :to="`/reviews/${job.id}`"
                  >审核</RouterLink
                >
                <button
                  v-if="canRetry(job.status)"
                  class="button button-small"
                  type="button"
                  :disabled="Boolean(actionJobId) || Boolean(manualJob) || offline"
                  @click="retry(job)"
                >
                  重试
                </button>
                <button
                  v-if="job.status === 'failed' && canStartManual"
                  class="button button-small"
                  type="button"
                  :disabled="Boolean(actionJobId) || Boolean(manualJob) || offline"
                  @click="openManual(job, $event)"
                >
                  转人工录入
                </button>
                <button
                  v-if="canCancel(job.status)"
                  class="text-button danger-text"
                  type="button"
                  :disabled="actionJobId === job.id"
                  @click="cancel(job)"
                >
                  取消
                </button>
                <span
                  v-if="!canReview(job.status) && !canRetry(job.status) && !canCancel(job.status)"
                  class="quiet"
                  >—</span
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
    <p class="inbox-footnote">
      <AppIcon name="shield" />AI 只生成待审核结果，确认后才计入正式账单。
    </p>
  </div>
</template>
