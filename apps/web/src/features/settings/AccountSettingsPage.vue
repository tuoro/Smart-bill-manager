<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { sessionStore } from '../../app/session'
import { ApiError } from '../../data/client'

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
