<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { sessionStore } from '../../app/session'
import { ApiError, api, type Member } from '../../data/client'
import InvitationsPanel from './InvitationsPanel.vue'

const canManage = computed(
  () => sessionStore.current.value?.capabilities.includes('members.manage') === true,
)
const items = ref<Member[]>([]),
  loading = ref(false),
  error = ref(''),
  nextCursor = ref(''),
  cursor = ref('')
const history = ref<string[]>([]),
  target = ref<Member | null>(null),
  confirmation = ref(false),
  pending = ref(false),
  inviteBusy = ref(false)
const stale = ref(false),
  recheckReady = ref(false)
const draft = reactive({
  role: 'viewer' as Member['role'],
  status: 'active' as Member['status'],
  reason: '',
})
const roles: Record<Member['role'], string> = {
  owner: '管理员',
  finance: '财务',
  reviewer: '审核员',
  viewer: '只读成员',
}
const busy = computed(() => pending.value || inviteBusy.value)
let epoch = 0,
  live = true
let returnFocus: HTMLElement | null = null

async function load(selectedCursor = cursor.value) {
  if (!canManage.value) return false
  const current = ++epoch
  loading.value = true
  error.value = ''
  recheckReady.value = false
  try {
    const page = await api.members(selectedCursor)
    const latest = target.value ? await api.member(target.value.user_id) : null
    if (current !== epoch || !live) return false
    items.value = page.items
    nextCursor.value = page.next_cursor
    cursor.value = selectedCursor
    if (target.value && latest) {
      if (latest.version !== target.value.version) {
        stale.value = true
        confirmation.value = false
      }
      target.value = latest
      recheckReady.value = true
    }
    return true
  } catch (caught) {
    if (current === epoch && live) {
      items.value = []
      nextCursor.value = ''
      if (target.value) {
        stale.value = true
        confirmation.value = false
      }
      error.value = caught instanceof ApiError ? caught.message : '成员列表加载失败，请重试'
    }
    return false
  } finally {
    if (current === epoch && live) loading.value = false
  }
}

function edit(member: Member, event: MouseEvent) {
  target.value = member
  draft.role = member.role
  draft.status = member.status
  draft.reason = ''
  confirmation.value = false
  stale.value = false
  recheckReady.value = false
  error.value = ''
  returnFocus = event.currentTarget as HTMLElement
}
async function cancel() {
  if (busy.value) return
  target.value = null
  confirmation.value = false
  stale.value = false
  await nextTick()
  returnFocus?.focus()
}
function prepare() {
  if (!target.value || busy.value || stale.value || !draft.reason.trim()) return
  confirmation.value = true
}
async function save() {
  if (!target.value || !confirmation.value || busy.value || stale.value) return
  const selected = target.value
  pending.value = true
  error.value = ''
  try {
    await api.changeMember(selected.user_id, { ...draft, expected_version: selected.version })
    if (!live) return
    if (selected.user_id === sessionStore.current.value?.user.id) {
      sessionStore.invalidate()
      return
    }
    target.value = null
    confirmation.value = false
    await load()
  } catch (caught) {
    if (!live) return
    confirmation.value = false
    if (caught instanceof ApiError && caught.status === 409) {
      stale.value = true
      await load()
      if (live)
        error.value = '未保存：' + caught.message + '。选择和理由已保留，请核对最新成员状态。'
    } else {
      if (!(caught instanceof ApiError)) stale.value = true
      error.value =
        caught instanceof ApiError ? caught.message : '保存结果未确认，请刷新核对成员状态后再决定'
    }
  } finally {
    if (live) pending.value = false
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
  if (canManage.value) void load()
})
onBeforeUnmount(() => {
  live = false
  epoch++
})
</script>

<template>
  <section class="members-settings">
    <header class="members-heading">
      <div>
        <p class="eyebrow">工作区设置</p>
        <h1>成员管理</h1>
        <p>管理当前工作区成员，不会改写其在其他工作区的身份。</p>
      </div>
      <button
        v-if="canManage"
        class="button button-secondary"
        :disabled="busy || loading"
        @click="load()"
      >
        刷新成员
      </button>
    </header>
    <p v-if="!canManage" class="notice">仅管理员可以查看和管理工作区成员。</p>
    <template v-else>
      <div v-if="error" class="notice notice-danger" role="alert">{{ error }}</div>
      <p v-if="loading" role="status">正在加载成员…</p>
      <ul class="member-list" aria-label="工作区成员">
        <li v-for="member in items" :key="member.user_id" class="member-row">
          <div>
            <strong>{{ member.display_name }}</strong>
            <p>{{ member.email }}</p>
            <span
              >{{ roles[member.role] }} ·
              {{ member.status === 'active' ? '已启用' : '已停用' }}</span
            >
          </div>
          <button
            class="button button-secondary"
            :aria-label="`管理 ${member.email}`"
            :disabled="busy || loading"
            @click="edit(member, $event)"
          >
            管理
          </button>
        </li>
      </ul>
      <nav class="member-actions" aria-label="成员分页">
        <button
          class="button button-secondary"
          :disabled="busy || loading || !!target || !history.length"
          @click="previousPage"
        >
          上一页</button
        ><button
          class="button button-secondary"
          :disabled="busy || loading || !!target || !nextCursor"
          @click="nextPage"
        >
          下一页
        </button>
      </nav>
      <form v-if="target" class="member-editor" @submit.prevent="prepare">
        <h2>管理 {{ target.display_name }}</h2>
        <p>
          {{ target.email }} · 当前：{{ roles[target.role] }} /
          {{ target.status === 'active' ? '已启用' : '已停用' }} · 版本 {{ target.version }}
        </p>
        <p class="notice">
          角色或状态改变后，该成员需要重新登录当前工作区；最后一名启用管理员不能被降级或停用。
        </p>
        <label class="field-stack"
          ><span>角色</span
          ><select v-model="draft.role" class="input" :disabled="busy || confirmation">
            <option v-for="(label, role) in roles" :key="role" :value="role">{{ label }}</option>
          </select></label
        >
        <label class="field-stack"
          ><span>成员状态</span
          ><select v-model="draft.status" class="input" :disabled="busy || confirmation">
            <option value="active">启用</option>
            <option value="suspended">停用</option>
          </select></label
        >
        <label class="field-stack"
          ><span>变更理由</span
          ><textarea
            v-model="draft.reason"
            class="input"
            maxlength="500"
            required
            :disabled="busy || confirmation"
          ></textarea>
        </label>
        <div v-if="stale" class="notice notice-stack">
          <p>请比较上方最新状态与保留的选择，再次确认。</p>
          <button
            class="button button-secondary"
            type="button"
            :disabled="busy || loading || !recheckReady"
            @click="stale = false"
          >
            我已核对最新成员状态
          </button>
        </div>
        <div v-if="confirmation" class="notice notice-stack" role="status">
          <p>
            确认将 {{ target.email }} 设为 {{ roles[draft.role] }} /
            {{ draft.status === 'active' ? '启用' : '停用' }}？该工作区旧会话将失效。
          </p>
          <button class="button button-primary" type="button" :disabled="busy" @click="save">
            确认保存成员变更
          </button>
        </div>
        <div class="member-actions">
          <button
            v-if="!confirmation"
            class="button button-primary"
            type="submit"
            :disabled="busy || loading || stale || !draft.reason.trim()"
          >
            核对变更</button
          ><button class="button button-secondary" type="button" :disabled="busy" @click="cancel">
            取消编辑
          </button>
        </div>
      </form>
      <InvitationsPanel :disabled="pending" @busy="inviteBusy = $event" />
    </template>
  </section>
</template>

<style scoped>
.members-settings,
.member-editor {
  display: grid;
  gap: 1.25rem;
}
.members-heading,
.member-row,
.member-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
}
.member-actions {
  justify-content: flex-start;
}
.member-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 0.75rem;
}
.member-row {
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
}
.member-row > div {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.member-row p {
  margin: 0.25rem 0;
}
.member-editor {
  max-width: 40rem;
}
</style>
