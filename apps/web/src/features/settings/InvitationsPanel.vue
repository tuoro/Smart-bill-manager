<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ApiError, api, type Invitation, type InvitationRequest } from '../../data/client'

const props = defineProps<{ disabled: boolean }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const items = ref<Invitation[]>([]),
  cursor = ref(''),
  nextCursor = ref(''),
  history = ref<string[]>([])
const loading = ref(false),
  pending = ref(false),
  error = ref(''),
  code = ref(''),
  codeInvitationID = ref(''),
  copyMessage = ref(''),
  notice = ref('')
const uncertain = ref(false),
  revokeTarget = ref<Invitation | null>(null),
  revokeReason = ref('')
const draft = reactive({ email: '', role: 'viewer' as Invitation['role'], reason: '' })
const busy = computed(() => props.disabled || pending.value)
const roles = { owner: '管理员', finance: '财务', reviewer: '审核员', viewer: '只读成员' }
const statuses = { pending: '待使用', consumed: '已使用', revoked: '已撤销', expired: '已过期' }
let epoch = 0,
  live = true
let request: { fingerprint: string; body: InvitationRequest } | null = null

function setPending(value: boolean) {
  pending.value = value
  emit('busy', value)
}
async function load(selectedCursor = cursor.value) {
  const current = ++epoch
  loading.value = true
  error.value = ''
  try {
    const result = await api.invitations(selectedCursor)
    const latest = revokeTarget.value ? await api.invitation(revokeTarget.value.id) : null
    if (current !== epoch || !live) return false
    items.value = result.items
    cursor.value = selectedCursor
    nextCursor.value = result.next_cursor
    if (latest) revokeTarget.value = latest
    return true
  } catch (caught) {
    if (current === epoch && live) {
      items.value = []
      nextCursor.value = ''
      error.value = caught instanceof ApiError ? caught.message : '邀请列表加载失败，请重试'
    }
    return false
  } finally {
    if (current === epoch && live) loading.value = false
  }
}
async function create() {
  if (busy.value || code.value) return
  const fingerprint = JSON.stringify(draft)
  if (!request || request.fingerprint !== fingerprint)
    request = { fingerprint, body: { ...draft, idempotency_key: crypto.randomUUID() } }
  setPending(true)
  error.value = ''
  notice.value = ''
  code.value = ''
  copyMessage.value = ''
  try {
    const result = await api.createInvitation(request.body)
    if (!live) return
    code.value = result.code
    codeInvitationID.value = result.invitation.id
    if (!result.code)
      notice.value = '邀请已创建，但代码只在首次响应中返回。请在列表撤销该邀请后重新创建。'
    request = null
    uncertain.value = false
    draft.email = ''
    draft.reason = ''
    history.value = []
    await load('')
  } catch (caught) {
    if (!live) return
    uncertain.value = !(caught instanceof ApiError)
    error.value =
      caught instanceof ApiError
        ? caught.message
        : '创建结果未确认。重试将核对同一请求，不会连续创建新的邀请。'
  } finally {
    if (live) setPending(false)
  }
}
async function copyCode() {
  if (!code.value) return
  try {
    await navigator.clipboard.writeText(code.value)
    if (live && code.value) copyMessage.value = '邀请代码已复制'
  } catch {
    if (live) copyMessage.value = '无法自动复制，请手动选择上方代码复制'
  }
}
function closeCode() {
  code.value = ''
  codeInvitationID.value = ''
  copyMessage.value = ''
}
function selectRevoke(item: Invitation) {
  revokeTarget.value = item
  revokeReason.value = ''
  error.value = ''
}
async function revoke() {
  if (
    busy.value ||
    !revokeTarget.value ||
    revokeTarget.value.version !== 1 ||
    !revokeReason.value.trim()
  )
    return
  setPending(true)
  error.value = ''
  const selectedID = revokeTarget.value.id
  try {
    await api.revokeInvitation(
      revokeTarget.value.id,
      revokeTarget.value.version,
      revokeReason.value,
    )
    if (!live) return
    revokeTarget.value = null
    revokeReason.value = ''
    if (codeInvitationID.value === selectedID) closeCode()
    await load()
  } catch (caught) {
    if (!live) return
    if (caught instanceof ApiError && caught.status === 409) await load()
    if (live)
      error.value =
        caught instanceof ApiError ? caught.message : '撤销结果未确认，请刷新邀请状态后核对'
  } finally {
    if (live) setPending(false)
  }
}
async function nextPage() {
  if (busy.value || loading.value || !nextCursor.value) return
  const previous = cursor.value
  if (await load(nextCursor.value)) history.value.push(previous)
}
async function previousPage() {
  if (busy.value || loading.value || !history.value.length) return
  if (await load(history.value.at(-1) ?? '')) history.value.pop()
}
onMounted(() => {
  void load()
})
onBeforeUnmount(() => {
  live = false
  epoch++
  code.value = ''
  request = null
  emit('busy', false)
})
</script>

<template>
  <section class="invitations-panel" aria-labelledby="invitations-title">
    <header class="invitation-actions">
      <h2 id="invitations-title">邀请成员</h2>
      <button class="button button-secondary" :disabled="busy || loading" @click="load()">
        刷新邀请
      </button>
    </header>
    <p>
      邀请有效期为 48 小时。把一次性代码单独交给受邀人，由其在“加入工作区”页面填写；不会发送邮件。
    </p>
    <p v-if="error" class="notice notice-danger" role="alert">{{ error }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <form class="invitation-form" @submit.prevent="create">
      <label class="field-stack"
        ><span>受邀邮箱</span
        ><input
          v-model.trim="draft.email"
          class="input"
          type="email"
          maxlength="254"
          required
          :disabled="busy || uncertain"
      /></label>
      <label class="field-stack"
        ><span>邀请角色</span
        ><select v-model="draft.role" class="input" :disabled="busy || uncertain">
          <option v-for="(label, role) in roles" :key="role" :value="role">{{ label }}</option>
        </select></label
      >
      <label class="field-stack"
        ><span>邀请理由</span
        ><textarea
          v-model="draft.reason"
          class="input"
          maxlength="500"
          required
          :disabled="busy || uncertain"
        ></textarea>
      </label>
      <button class="button button-primary" type="submit" :disabled="busy || !!code">
        {{ uncertain ? '核对上次邀请请求' : pending ? '正在处理…' : '创建邀请' }}
      </button>
    </form>
    <div v-if="code" class="notice notice-stack invitation-code">
      <p>请先保存并关闭当前代码，再创建下一份邀请。</p>
      <strong>此代码只显示一次</strong
      ><label class="field-stack"
        ><span>一次性邀请代码</span
        ><input :value="code" class="input" readonly autocomplete="off" spellcheck="false"
      /></label>
      <div class="invitation-actions">
        <button class="button button-secondary" @click="copyCode">复制邀请代码</button
        ><button class="button button-secondary" @click="closeCode">已保存，关闭代码</button>
      </div>
      <p v-if="copyMessage" role="status">{{ copyMessage }}</p>
    </div>
    <p v-if="loading" role="status">正在加载邀请…</p>
    <ul class="invitation-list" aria-label="邀请记录">
      <li v-for="item in items" :key="item.id" class="invitation-row">
        <div>
          <strong>{{ item.email }}</strong>
          <p>{{ roles[item.role] }} · {{ statuses[item.status] }}</p>
          <span>有效至 {{ new Date(item.expires_at).toLocaleString('zh-CN') }}</span>
        </div>
        <button
          v-if="item.version === 1"
          class="button button-secondary"
          :disabled="busy || loading"
          :aria-label="`撤销 ${item.email} 的邀请`"
          @click="selectRevoke(item)"
        >
          撤销
        </button>
      </li>
    </ul>
    <div class="invitation-actions">
      <button
        class="button button-secondary"
        :disabled="busy || loading || !!revokeTarget || !history.length"
        @click="previousPage"
      >
        上一页邀请</button
      ><button
        class="button button-secondary"
        :disabled="busy || loading || !!revokeTarget || !nextCursor"
        @click="nextPage"
      >
        下一页邀请
      </button>
    </div>
    <form v-if="revokeTarget" class="invitation-form" @submit.prevent="revoke">
      <h3>撤销 {{ revokeTarget.email }} 的邀请</h3>
      <p>当前状态：{{ statuses[revokeTarget.status] }}。撤销后，原代码不可再使用。</p>
      <label class="field-stack"
        ><span>撤销理由</span
        ><textarea
          v-model="revokeReason"
          class="input"
          maxlength="500"
          required
          :disabled="busy"
        ></textarea>
      </label>
      <div class="invitation-actions">
        <button
          class="button button-primary"
          type="submit"
          :disabled="busy || loading || revokeTarget.version !== 1"
        >
          确认撤销邀请</button
        ><button
          class="button button-secondary"
          type="button"
          :disabled="busy"
          @click="revokeTarget = null"
        >
          取消撤销
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.invitations-panel,
.invitation-form,
.invitation-code {
  display: grid;
  gap: 1rem;
}
.invitation-form {
  max-width: 40rem;
}
.invitation-actions,
.invitation-row {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  flex-wrap: wrap;
}
.invitation-row {
  justify-content: space-between;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
}
.invitation-row > div {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.invitation-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 0.75rem;
}
.invitation-row p {
  margin: 0.25rem 0;
}
</style>
