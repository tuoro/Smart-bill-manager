<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { sessionStore } from '../../app/session'
import { RouterLink } from 'vue-router'
import {
  ApiError,
  api,
  type Trip,
  type TripEvidence,
  type TripMaterialRequest,
} from '../../data/client'

const props = defineProps<{ trip?: Trip; canManage: boolean; offline: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const items = ref<TripEvidence[]>([])
const cursor = ref('')
const onlyAssigned = ref(false)
const loading = ref(false)
const busyID = ref('')
const error = ref('')
const canCorrect = computed(() =>
  ['facts.read', 'claims.review'].every((capability) =>
    sessionStore.current.value?.capabilities.includes(capability),
  ),
)
const reasons = ref<Record<string, string>>({})
const attempts = new Map<string, { fingerprint: string; key: string }>()
let loadRevision = 0

async function load(append = false) {
  const revision = ++loadRevision
  loading.value = true
  error.value = ''
  try {
    const page = await api.tripEvidence(
      onlyAssigned.value ? props.trip?.id : '',
      append ? cursor.value : '',
    )
    if (revision !== loadRevision) return
    items.value = append ? [...items.value, ...page.items] : page.items
    cursor.value = page.next_cursor ?? ''
  } catch (caught) {
    if (revision === loadRevision)
      error.value = caught instanceof ApiError ? caught.message : '凭证加载失败，请重试'
  } finally {
    if (revision === loadRevision) loading.value = false
  }
}

async function assign(item: TripEvidence) {
  if (!props.trip || !props.canManage || props.offline || busyID.value) return
  const reason = (reasons.value[item.id] ?? '').trim()
  if (!reason) {
    error.value = '请填写材料归属理由'
    return
  }
  const body: TripMaterialRequest = {
    evidence_id: item.id,
    desired_trip_id: item.current_trip_id === props.trip.id ? null : props.trip.id,
    expected_link_id: item.current_link_id ?? null,
    expected_version: item.version,
    reason,
  }
  const fingerprint = JSON.stringify(body)
  let attempt = attempts.get(item.id)
  if (attempt?.fingerprint !== fingerprint) {
    attempt = { fingerprint, key: crypto.randomUUID() }
    attempts.set(item.id, attempt)
  }
  busyID.value = item.id
  error.value = ''
  try {
    await api.assignTripMaterial(body, attempt.key)
    attempts.delete(item.id)
    reasons.value[item.id] = ''
    await load()
    emit('changed')
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '凭证归属失败，理由已保留'
  } finally {
    busyID.value = ''
  }
}

watch(
  () => [props.trip?.id, onlyAssigned.value, props.offline, props.trip?.version],
  (current, previous) => {
    if (current[0] !== previous?.[0] || current[1] !== previous?.[1]) {
      loadRevision++
      items.value = []
      cursor.value = ''
    }
    if (!props.trip && onlyAssigned.value) {
      onlyAssigned.value = false
      return
    }
    if (!props.offline) void load()
  },
  { immediate: true },
)
</script>

<template>
  <section id="trip-materials" class="panel trip-materials" aria-labelledby="trip-materials-title">
    <div class="panel-heading">
      <div>
        <h2 id="trip-materials-title">行程凭证</h2>
        <p>机票、行程单等审核凭证单独保存；多张凭证可归入同一趟行程，不计入费用金额。</p>
      </div>
      <RouterLink v-if="canManage" class="button" to="/inbox">上传凭证</RouterLink>
    </div>
    <div class="materials-toolbar">
      <label
        ><input v-model="onlyAssigned" type="checkbox" :disabled="!trip || loading" />
        只看当前行程凭证</label
      >
      <button class="button button-small" :disabled="offline || loading" @click="load()">
        刷新凭证
      </button>
    </div>
    <p v-if="error" class="notice notice-danger" role="alert">{{ error }}</p>
    <p v-if="loading && !items.length" class="quiet-block" role="status">正在加载凭证…</p>
    <p v-else-if="!items.length" class="quiet-block">
      {{
        onlyAssigned
          ? '这趟行程还没有关联凭证。取消筛选可从全部凭证中选择。'
          : '还没有审核后的行程凭证。可先创建行程，稍后上传并审核材料。'
      }}
    </p>
    <ul class="trip-candidate-list">
      <li v-for="item in items" :key="item.id">
        <article class="trip-candidate">
          <header>
            <div>
              <strong>{{ item.origin ? `${item.origin} → ` : '' }}{{ item.destination }}</strong>
              <small
                >{{ item.start_date }} 至 {{ item.end_date }} ·
                {{ item.transport_type || '行程凭证' }}</small
              >
            </div>
            <a
              v-if="canManage"
              class="button button-small"
              :href="api.documentContentURL(item.document_id)"
              target="_blank"
              rel="noopener"
              >查看原件</a
            >
          </header>
          <p>当前行程：{{ item.current_trip_name || '未归属' }}</p>
          <RouterLink
            v-if="canCorrect"
            class="text-button"
            :to="`/facts/trip/${encodeURIComponent(item.id)}/correction`"
            >纠正凭证字段</RouterLink
          >
          <div v-if="canManage && trip" class="trip-assignment-form">
            <label :for="`material-reason-${item.id}`">材料归属理由</label>
            <textarea
              :id="`material-reason-${item.id}`"
              v-model="reasons[item.id]"
              class="textarea"
              rows="2"
              maxlength="500"
            ></textarea>
            <div class="trip-assignment-actions">
              <button
                class="button"
                :disabled="Boolean(busyID) || offline || loading"
                @click="assign(item)"
              >
                {{
                  item.current_trip_id === trip.id
                    ? '移出当前行程'
                    : item.current_trip_id
                      ? '移到当前行程'
                      : '加入当前行程'
                }}
              </button>
            </div>
          </div>
          <p v-else-if="canManage" class="quiet-block">先创建或选择行程，即可关联这张凭证。</p>
        </article>
      </li>
    </ul>
    <div v-if="cursor" class="trip-load-more">
      <button class="button" :disabled="loading || offline" @click="load(true)">
        加载更多凭证
      </button>
    </div>
  </section>
</template>

<style scoped>
.materials-toolbar {
  padding: 0 1.5rem 1rem;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.materials-toolbar label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
