<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, api, type JobSummary, type ManualReviewRequest } from '../../data/client'

const props = defineProps<{ job: JobSummary; offline: boolean }>()
const emit = defineEmits<{ close: [] }>()
const router = useRouter()
const documentType = ref<ManualReviewRequest['document_type'] | ''>('')
const reason = ref('')
const busy = ref(false)
const error = ref('')
const currentJob = ref(props.job)
const conflict = ref(false)
const statusNotice = ref('')
const typeInput = ref<HTMLSelectElement | null>(null)
let requestIdentity = ''
let requestKey = ''

async function submit() {
  if (busy.value || props.offline || currentJob.value.status !== 'failed' || conflict.value) return
  if (!documentType.value || !reason.value.trim()) {
    error.value = '请选择单据类型并填写人工接管理由'
    return
  }
  const body: ManualReviewRequest = {
    document_type: documentType.value,
    reason: reason.value.trim(),
    expected_job_version: currentJob.value.version,
  }
  const identity = JSON.stringify(body)
  if (identity !== requestIdentity) {
    requestIdentity = identity
    requestKey = crypto.randomUUID()
  }
  busy.value = true
  error.value = ''
  try {
    await api.startManualReview(props.job.id, body, requestKey)
    await router.push(`/reviews/${encodeURIComponent(props.job.id)}`)
  } catch (caught) {
    conflict.value = caught instanceof ApiError && caught.status === 409
    error.value = caught instanceof ApiError ? caught.message : '转人工失败，可保留当前选择重试'
  } finally {
    busy.value = false
  }
}

async function refreshStatus() {
  if (busy.value || props.offline) return
  busy.value = true
  try {
    currentJob.value = await api.getJob(props.job.id)
    conflict.value = false
    error.value = ''
    statusNotice.value =
      currentJob.value.status === 'failed'
        ? '已核对最新任务状态，类型和理由已保留，请重新确认。'
        : '任务状态已变化，不能再从失败入口接管；当前输入仍保留。'
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '任务状态刷新失败，请重试'
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  await nextTick()
  typeInput.value?.focus()
})
</script>

<template>
  <section class="panel page-stack" aria-labelledby="manual-review-title">
    <h2 id="manual-review-title">转人工录入</h2>
    <p>{{ job.original_name }}：不再次调用模型。请核对原件、填写字段并完成审核后再生成账单。</p>
    <p v-if="job.safe_error_message" class="notice">原识别失败：{{ job.safe_error_message }}</p>
    <form class="page-stack" @submit.prevent="submit">
      <label
        >单据类型<select
          ref="typeInput"
          v-model="documentType"
          class="select"
          :disabled="busy"
          required
        >
          <option value="" disabled>请选择类型</option>
          <option value="payment">支付凭证</option>
          <option value="invoice">发票</option>
          <option value="trip">行程凭证</option>
        </select></label
      >
      <label
        >接管理由<textarea
          v-model="reason"
          class="textarea"
          maxlength="500"
          required
          :disabled="busy"
          rows="2"
        ></textarea>
      </label>
      <p v-if="error" class="notice notice-danger" role="alert">{{ error }}</p>
      <p v-if="statusNotice" class="notice" role="status">{{ statusNotice }}</p>
      <div class="header-actions">
        <button
          type="submit"
          class="button button-primary"
          :disabled="busy || offline || conflict || currentJob.status !== 'failed'"
        >
          {{ busy ? '正在处理…' : '确认转人工' }}
        </button>
        <button
          v-if="conflict"
          type="button"
          class="button"
          :disabled="busy || offline"
          @click="refreshStatus"
        >
          刷新任务状态
        </button>
        <RouterLink
          v-if="currentJob.status === 'blocked' || currentJob.status === 'needs_review'"
          class="button"
          :to="`/reviews/${encodeURIComponent(job.id)}`"
          >进入已有审核</RouterLink
        >
        <button type="button" class="button" :disabled="busy" @click="emit('close')">取消</button>
      </div>
    </form>
  </section>
</template>
