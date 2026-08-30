<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ApiError, api, type JobSummary } from '../../data/client'
import { canCancel, canRetry, canReview, jobStatusMeta } from './status'

const jobs = ref<JobSummary[]>([])
const loading = ref(true)
const refreshing = ref(false)
const uploading = ref(false)
const actionJobId = ref('')
const error = ref('')
const offline = ref(!navigator.onLine)
const filter = ref<'all' | 'attention' | 'active' | 'done'>('all')
let pollTimer: number | undefined

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
  const file = input.files?.[0]
  if (!file) return
  error.value = ''
  uploading.value = true
  try {
    await api.upload(file)
    await load(true)
  } catch (caught) {
    if (caught instanceof ApiError && caught.code === 'duplicate_document' && caught.resourceId) {
      error.value = `该文件已上传（Document ${caught.resourceId}），未创建重复任务。`
    } else {
      error.value = caught instanceof ApiError ? caught.message : '文件上传失败，请检查网络后重试'
    }
  } finally {
    uploading.value = false
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

onMounted(() => {
  void load()
  pollTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible') void load(true)
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
  <div class="page-stack">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>工作台</span><span aria-hidden="true">/</span><strong>AI 收件箱</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>AI 收件箱</h1>
        <p>上传单据、跟踪提取状态，并处理需要人工判断的结果。</p>
      </div>
      <label class="button button-primary upload-button" :aria-disabled="uploading">
        <input
          class="visually-hidden"
          type="file"
          accept="image/jpeg,image/png,image/webp,application/pdf"
          :disabled="uploading"
          @change="upload"
        />
        <span aria-hidden="true">＋</span>
        {{ uploading ? '正在上传…' : '上传单据' }}
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
      class="panel queue-panel"
      aria-labelledby="queue-title"
      :aria-busy="loading || refreshing"
    >
      <div class="panel-heading queue-heading">
        <div>
          <h2 id="queue-title">处理队列</h2>
          <p>{{ jobs.length }} 个任务<span v-if="refreshing"> · 正在同步</span></p>
        </div>
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
      </div>

      <div v-if="loading" class="state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span>
        <strong>正在读取收件箱</strong>
        <span>任务状态会在加载后自动刷新。</span>
      </div>

      <div v-else-if="filteredJobs.length === 0" class="state-layout">
        <span class="state-glyph" aria-hidden="true">收</span>
        <strong>{{ jobs.length ? '当前筛选没有任务' : '收件箱还是空的' }}</strong>
        <span>{{
          jobs.length ? '切换筛选查看其他状态。' : '上传一张支付截图或发票，开始第一条可信链路。'
        }}</span>
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
                  :disabled="actionJobId === job.id"
                  @click="retry(job)"
                >
                  重试
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
  </div>
</template>
