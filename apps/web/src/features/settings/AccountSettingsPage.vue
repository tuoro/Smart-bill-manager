<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { sessionStore } from '../../app/session'
import { ApiError, api, type ChatBinding } from '../../data/client'

const currentPassword = ref(''),
  nextPassword = ref(''),
  confirmation = ref(''),
  error = ref(''),
  pending = ref(false)
let live = true
onBeforeUnmount(() => {
  live = false
  currentPassword.value = ''
  nextPassword.value = ''
  confirmation.value = ''
})
// 明文绑定码只在生成那一次返回，服务端只留哈希，所以不提供「再看一次」。
// 忘了就重新生成一张，旧的到期自然作废。
const bindings = ref<ChatBinding[]>([])
const bindingError = ref('')
const bindingPending = ref(false)
const issuedCode = ref('')
const issuedExpiresAt = ref('')
const copied = ref(false)

async function loadBindings() {
  try {
    const page = await api.chatBindings()
    if (live) bindings.value = page.items
  } catch (caught) {
    if (live) bindingError.value = caught instanceof ApiError ? caught.message : '绑定状态加载失败'
  }
}

onMounted(loadBindings)

async function issueCode() {
  if (bindingPending.value) return
  bindingPending.value = true
  bindingError.value = ''
  copied.value = false
  try {
    const issued = await api.createChatBindingCode('dingtalk')
    if (!live) return
    issuedCode.value = issued.code
    issuedExpiresAt.value = issued.expires_at
  } catch (caught) {
    if (live) bindingError.value = caught instanceof ApiError ? caught.message : '生成绑定码失败'
  } finally {
    if (live) bindingPending.value = false
  }
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(issuedCode.value)
    copied.value = true
  } catch {
    // 剪贴板不可用时不报错：码就显示在旁边，手动选中复制即可。
    copied.value = false
  }
}

async function unbind(platform: ChatBinding['platform']) {
  if (bindingPending.value) return
  bindingPending.value = true
  bindingError.value = ''
  try {
    await api.deleteChatBinding(platform)
    if (live) await loadBindings()
  } catch (caught) {
    if (live) bindingError.value = caught instanceof ApiError ? caught.message : '解除绑定失败'
  } finally {
    if (live) bindingPending.value = false
  }
}

function formatMoment(value: string) {
  const instant = new Date(value)
  if (Number.isNaN(instant.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    instant,
  )
}

async function change() {
  if (pending.value) return
  error.value = ''
  const size = new TextEncoder().encode(nextPassword.value).length
  if (size < 12 || size > 1024 || nextPassword.value !== confirmation.value) {
    error.value = '新密码需要 12–1024 字节，且两次输入一致'
    return
  }
  pending.value = true
  try {
    await sessionStore.changePassword(currentPassword.value, nextPassword.value)
  } catch (caught) {
    if (live)
      error.value =
        caught instanceof ApiError ? caught.message : '修改结果未确认，请尝试用新密码登录后核对'
  } finally {
    if (live) pending.value = false
  }
}
</script>

<template>
  <section class="account-settings">
    <header class="page-heading">
      <div>
        <p class="eyebrow">设置</p>
        <h1>账号与密码</h1>
        <p>{{ sessionStore.current.value?.user.email }}</p>
      </div>
    </header>
    <p class="notice">
      账号由所有已加入的工作区共用。修改密码后，所有工作区的会话都会退出；工作区管理员不能替你修改全局密码。
    </p>
    <form class="password-form" @submit.prevent="change">
      <h2>修改密码</h2>
      <div v-if="error" class="notice notice-danger" role="alert">{{ error }}</div>
      <label class="field-stack"
        ><span>当前密码</span
        ><input
          v-model="currentPassword"
          class="input"
          type="password"
          autocomplete="current-password"
          required
          maxlength="1024"
          :disabled="pending"
      /></label>
      <label class="field-stack"
        ><span>新密码</span
        ><input
          v-model="nextPassword"
          class="input"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          :disabled="pending"
      /></label>
      <label class="field-stack"
        ><span>确认新密码</span
        ><input
          v-model="confirmation"
          class="input"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          :disabled="pending"
      /></label>
      <button type="submit" class="button button-primary" :disabled="pending">
        {{ pending ? '正在修改…' : '修改密码并退出所有会话' }}
      </button>
      <p class="muted">
        忘记密码时，请联系本地部署维护者使用受控账号恢复命令；不要重新初始化或清空数据库。
      </p>
    </form>

    <section class="panel chat-binding" aria-labelledby="chat-binding-title">
      <div class="panel-heading">
        <div>
          <h2 id="chat-binding-title">聊天投递</h2>
          <p>绑定后可直接把单据发给机器人，不必先打开网页。绑定的是你本人的账号。</p>
        </div>
      </div>
      <div class="chat-binding-body">
        <div v-if="bindingError" class="notice notice-danger" role="alert">{{ bindingError }}</div>

        <dl v-if="bindings.length" class="chat-binding-list">
          <div v-for="binding in bindings" :key="binding.platform">
            <dt>钉钉</dt>
            <dd>
              <strong>{{ binding.external_user_id }}</strong>
              <small>{{ formatMoment(binding.created_at) }} 绑定</small>
            </dd>
            <dd>
              <button
                class="button button-small"
                type="button"
                :disabled="bindingPending"
                @click="unbind(binding.platform)"
              >
                解除绑定
              </button>
            </dd>
          </div>
        </dl>
        <p v-else class="muted">当前没有绑定任何聊天账号。</p>

        <button
          class="button button-primary"
          type="button"
          :disabled="bindingPending"
          @click="issueCode"
        >
          {{ bindings.length ? '重新生成绑定码' : '生成钉钉绑定码' }}
        </button>

        <div v-if="issuedCode" class="chat-binding-code" role="status">
          <p>把下面这串发给钉钉机器人即可完成绑定。</p>
          <code>{{ issuedCode }}</code>
          <div class="chat-binding-code-actions">
            <button class="button button-small" type="button" @click="copyCode">
              {{ copied ? '已复制' : '复制' }}
            </button>
            <small>{{ formatMoment(issuedExpiresAt) }} 前有效，只能使用一次。</small>
          </div>
          <small class="muted">
            这串只显示这一次，服务端只保存校验值。关掉页面后需要重新生成。重新绑定会替换你当前的账号。
          </small>
        </div>
      </div>
    </section>
  </section>
</template>

<style scoped>
.account-settings {
  display: grid;
  gap: 1.25rem;
}
.password-form {
  display: grid;
  gap: 1rem;
  width: min(100%, 32rem);
}
</style>
