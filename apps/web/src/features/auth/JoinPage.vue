<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { ApiError, api, type InvitationView } from '../../data/client'

const code = ref(''),
  name = ref(''),
  password = ref(''),
  confirmation = ref('')
const view = ref<InvitationView | null>(null),
  pending = ref(false),
  error = ref(''),
  joined = ref(false)
const roles = { owner: '管理员', finance: '财务', reviewer: '审核员', viewer: '只读成员' }
let epoch = 0
watch(code, () => {
  if (joined.value) return
  epoch++
  view.value = null
  error.value = ''
  password.value = ''
  confirmation.value = ''
  joined.value = false
})
onBeforeUnmount(() => {
  epoch++
  code.value = ''
  password.value = ''
  confirmation.value = ''
})

async function check() {
  if (pending.value) return
  const current = ++epoch,
    selected = code.value
  pending.value = true
  error.value = ''
  view.value = null
  try {
    const result = await api.checkInvitation(selected)
    if (current === epoch) view.value = result
  } catch (caught) {
    if (current === epoch)
      error.value = caught instanceof ApiError ? caught.message : '邀请检查失败，请重试'
  } finally {
    if (current === epoch) pending.value = false
  }
}

async function join() {
  if (pending.value || !view.value) return
  error.value = ''
  const existing = view.value.existing_account
  if (
    !existing &&
    (password.value !== confirmation.value || new TextEncoder().encode(password.value).length < 12)
  ) {
    error.value = '请使用至少 12 字节的新密码，并确认两次输入一致'
    return
  }
  const current = epoch
  pending.value = true
  try {
    await api.acceptInvitation(code.value, existing ? '' : name.value, password.value)
    if (current !== epoch) return
    joined.value = true
    code.value = ''
    view.value = null
    password.value = ''
    confirmation.value = ''
  } catch (caught) {
    if (current === epoch)
      error.value =
        caught instanceof ApiError ? caught.message : '加入结果未确认，请重新检查邀请；不要连续提交'
  } finally {
    if (current === epoch) pending.value = false
  }
}
</script>

<template>
  <main id="main-content" class="join-page" tabindex="-1">
    <header>
      <p class="eyebrow">智能账单管理</p>
      <h1>加入工作区</h1>
      <p>向邀请你的管理员获取一次性代码，在这里核对后加入。</p>
    </header>
    <div v-if="error" class="notice notice-danger" role="alert">{{ error }}</div>
    <section v-if="joined" class="notice notice-stack" role="status">
      <h2>已加入工作区</h2>
      <p>请使用你的账号登录。此操作不会自动切换正在使用的工作区。</p>
    </section>
    <template v-else>
      <form class="join-form" @submit.prevent="check">
        <label class="field-stack"
          ><span>邀请代码</span
          ><input
            v-model.trim="code"
            class="input"
            autocomplete="off"
            spellcheck="false"
            maxlength="43"
            required
            :disabled="pending"
        /></label>
        <button
          class="button button-secondary"
          type="submit"
          :disabled="pending || code.length !== 43"
        >
          {{ view ? '重新检查邀请' : '检查邀请' }}
        </button>
      </form>
      <form v-if="view" class="join-form" @submit.prevent="join">
        <div class="notice notice-stack">
          <strong>{{ view.tenant_name }}</strong>
          <p>{{ view.email }} · {{ roles[view.role] }}</p>
          <p>
            {{
              view.existing_account
                ? '已有账号：使用现有密码验证身份，不会改写其他工作区的账号或密码。'
                : '新账号：设置姓名与密码。只有完成这次邀请才能加入。'
            }}
          </p>
        </div>
        <label v-if="!view.existing_account" class="field-stack"
          ><span>姓名</span
          ><input
            v-model.trim="name"
            class="input"
            maxlength="100"
            autocomplete="name"
            required
            :disabled="pending"
        /></label>
        <label class="field-stack"
          ><span>{{ view.existing_account ? '现有账号密码' : '设置密码' }}</span
          ><input
            v-model="password"
            class="input"
            type="password"
            :autocomplete="view.existing_account ? 'current-password' : 'new-password'"
            maxlength="1024"
            required
            :disabled="pending"
        /></label>
        <label v-if="!view.existing_account" class="field-stack"
          ><span>确认密码</span
          ><input
            v-model="confirmation"
            class="input"
            type="password"
            autocomplete="new-password"
            maxlength="1024"
            required
            :disabled="pending"
        /></label>
        <button class="button button-primary" type="submit" :disabled="pending">
          {{ pending ? '正在处理…' : '确认加入工作区' }}
        </button>
      </form>
    </template>
    <RouterLink to="/login">返回登录</RouterLink>
  </main>
</template>

<style scoped>
.join-page {
  width: min(100% - 2rem, 36rem);
  margin: 4rem auto;
  display: grid;
  gap: 1.5rem;
}
.join-form {
  display: grid;
  gap: 1rem;
}
.join-page p {
  overflow-wrap: anywhere;
}
</style>
