<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, api, type Trip, type TripManagementRequest } from '../../data/client'

const props = defineProps<{ trip?: Trip; canDelete: boolean; offline: boolean }>()
const emit = defineEmits<{ saved: [id: string]; cancel: [] }>()
const draft = ref<TripManagementRequest>({
  name: props.trip?.name ?? '',
  start_date: props.trip?.start_date ?? '',
  end_date: props.trip?.end_date ?? '',
  timezone: props.trip ? (props.trip.timezone ?? '') : 'Asia/Shanghai',
  notes: props.trip?.notes ?? '',
  expected_version: props.trip?.version ?? 0,
  reason: '',
})
const busy = ref(false)
const error = ref('')
const deleting = ref(false)
const stale = ref(false)
const latest = ref<Trip>()
const badDebtLocked = ref(Boolean(props.trip?.bad_debt_locked))
const editor = ref<HTMLElement>()
onMounted(() => editor.value?.querySelector<HTMLInputElement>('input')?.focus())
let attempt = { fingerprint: '', key: '' }

async function save() {
  if (busy.value || props.offline || stale.value || (deleting.value && badDebtLocked.value)) return
  const body = {
    ...draft.value,
    name: draft.value.name.trim(),
    notes: draft.value.notes.trim(),
    reason: draft.value.reason.trim(),
  }
  if (!body.reason) {
    error.value = '请填写操作理由'
    return
  }
  if (!deleting.value && body.end_date < body.start_date) {
    error.value = '结束日期不能早于开始日期'
    return
  }
  const fingerprint = JSON.stringify({ body, deleting: deleting.value })
  if (attempt.fingerprint !== fingerprint) attempt = { fingerprint, key: crypto.randomUUID() }
  busy.value = true
  error.value = ''
  try {
    const result =
      deleting.value && props.trip
        ? await api.deleteTrip(props.trip.id, body.expected_version, body.reason, attempt.key)
        : props.trip
          ? await api.editTrip(props.trip.id, body, attempt.key)
          : await api.createTrip(body, attempt.key)
    emit('saved', deleting.value ? '' : result.trip_id)
  } catch (caught) {
    stale.value = caught instanceof ApiError && caught.status === 409
    error.value = caught instanceof ApiError ? caught.message : '保存失败，草稿已保留，请重试'
  } finally {
    busy.value = false
  }
}

async function compareLatest() {
  if (!props.trip || busy.value || props.offline) return
  busy.value = true
  try {
    latest.value = (await api.trips()).items.find((item) => item.id === props.trip?.id)
    if (!latest.value) error.value = '该行程已被删除，请保留所需草稿后关闭编辑器。'
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '最新行程读取失败'
  } finally {
    busy.value = false
  }
}

function confirmLatest() {
  if (!latest.value) return
  draft.value.expected_version = latest.value.version
  badDebtLocked.value = Boolean(latest.value.bad_debt_locked)
  stale.value = false
  latest.value = undefined
  error.value = ''
}
</script>

<template>
  <section ref="editor" class="panel trip-editor" aria-labelledby="trip-editor-title">
    <div class="panel-heading">
      <div>
        <h2 id="trip-editor-title">{{ deleting ? '删除行程' : trip ? '编辑行程' : '新建行程' }}</h2>
        <p>一趟行程可包含多张机票、酒店凭证和相关费用，无需先上传票据。</p>
      </div>
    </div>
    <form class="trip-editor-form" @submit.prevent="save">
      <fieldset :disabled="busy || offline">
        <template v-if="!deleting">
          <label
            >行程名称<input
              v-model="draft.name"
              class="input"
              required
              maxlength="500"
              placeholder="例如：上海出差 · 客户拜访"
          /></label>
          <div class="trip-editor-dates">
            <label
              >开始日期<input v-model="draft.start_date" class="input" type="date" required
            /></label>
            <label
              >结束日期<input
                v-model="draft.end_date"
                class="input"
                type="date"
                :min="draft.start_date"
                required
            /></label>
          </div>
          <label
            >行程时区<input v-model="draft.timezone" class="input" list="trip-timezones" required
          /></label>
          <p v-if="trip && !trip.timezone" class="notice notice-warning">
            原行程未记录时区，请明确选择后保存，不会默认补齐。
          </p>
          <datalist id="trip-timezones">
            <option value="Asia/Shanghai" />
            <option value="Asia/Singapore" />
            <option value="Asia/Tokyo" />
            <option value="Europe/London" />
            <option value="America/New_York" />
            <option value="UTC" />
          </datalist>
          <p class="muted">
            按所选时区计算首尾整天；只有唯一时间匹配的支付会自动归入，人工选择保持不变。
          </p>
          <label
            >备注（可选）<textarea
              v-model="draft.notes"
              class="textarea"
              rows="2"
              maxlength="2000"
            ></textarea>
          </label>
        </template>
        <p v-else class="notice notice-warning">
          确认删除「{{ trip?.name }}」？只解除行程关联，保留支付、发票、凭证和报销历史。
        </p>
        <label
          >操作理由<textarea
            v-model="draft.reason"
            class="textarea"
            rows="2"
            required
            maxlength="500"
          ></textarea>
        </label>
        <p v-if="error" class="danger-text" role="alert">{{ error }}</p>
        <div v-if="stale" class="quiet-block">
          <p>草稿仍保留。请核对最新行程后，再决定是否提交当前草稿。</p>
          <button type="button" class="button" @click="compareLatest">读取最新版本</button>
          <template v-if="latest">
            <p>
              最新行程：{{ latest.name }} · {{ latest.start_date }} 至 {{ latest.end_date }} ·
              {{ latest.timezone || '未确认时区' }}
            </p>
            <p>最新备注：{{ latest.notes || '无' }}</p>
            <button type="button" class="button" @click="confirmLatest">
              已核对，使用当前草稿继续
            </button>
          </template>
        </div>
        <p v-if="badDebtLocked" class="notice notice-warning" role="status">
          关联坏账单据，暂不可删除行程；请先处理坏账或调整关联。
        </p>
        <div class="trip-editor-actions">
          <button
            v-if="trip && canDelete && !deleting"
            class="text-button danger-text"
            type="button"
            @click="deleting = true"
            :disabled="badDebtLocked"
          >
            删除行程…
          </button>
          <button class="button" type="button" @click="emit('cancel')">取消</button>
          <button
            class="button button-primary"
            type="submit"
            :disabled="stale || (deleting && badDebtLocked)"
          >
            {{ busy ? '正在保存…' : deleting ? '确认删除行程' : '保存行程' }}
          </button>
        </div>
      </fieldset>
    </form>
  </section>
</template>

<style scoped>
.trip-editor-form {
  padding: 0 1.5rem 1.5rem;
}
fieldset {
  border: 0;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 1rem;
  min-width: 0;
}
label {
  display: grid;
  gap: 0.5rem;
}
.trip-editor-dates {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}
.trip-editor-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.75rem;
}
@media (max-width: 600px) {
  .trip-editor-dates {
    grid-template-columns: 1fr;
  }
}
</style>
