<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, api } from '../../data/client'

const router = useRouter()
const email = ref(''),
  name = ref('Owner'),
  tenant = ref(''),
  currency = ref('CNY'),
  timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'),
  password = ref(''),
  confirmation = ref('')
const pending = ref(false),
  error = ref(''),
  ready = ref(false),
  done = ref(false)
const currencies = ['CNY', 'USD', 'EUR', 'JPY']

onMounted(async () => {
  try {
    const state = await api.setupRequired()
    if (!state.required) {
      void router.replace({ name: 'login' })
      return
    }
    ready.value = true
  } catch {
    error.value = '无法读取初始化状态，请确认服务已连接数据库后刷新页面'
  }
})

async function submit() {
  if (pending.value) return
  error.value = ''
  if (password.value !== confirmation.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  if (new TextEncoder().encode(password.value).length < 12) {
    error.value = '密码至少需要 12 字节'
    return
  }
  pending.value = true
  try {
    await api.createOwner({
      email: email.value,
      password: password.value,
      display_name: name.value,
      tenant_name: tenant.value,
      default_currency: currency.value,
      timezone: timezone.value,
    })
    done.value = true
    password.value = ''
    confirmation.value = ''
    void router.replace({ name: 'login' })
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '初始化失败，请重试'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main class="setup-page">
    <h1>初始化工作区</h1>
    <p v-if="!ready && !error">正在检查初始化状态…</p>
    <p v-if="error" class="notice" role="alert">{{ error }}</p>
    <template v-if="ready && !done">
      <div class="notice notice-stack">
        <strong>这是这台部署的一次性初始化</strong>
        <p>创建后本页面将永久关闭。该账号是工作区的 Owner，之后可以邀请成员并分配角色。</p>
      </div>
      <form class="setup-form" @submit.prevent="submit">
        <label class="field-stack"
          ><span>登录邮箱</span
          ><input
            v-model.trim="email"
            class="input"
            type="email"
            autocomplete="username"
            maxlength="320"
            required
            :disabled="pending"
        /></label>
        <label class="field-stack"
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
          ><span>工作区名称</span
          ><input
            v-model.trim="tenant"
            class="input"
            maxlength="120"
            required
            :disabled="pending"
        /></label>
        <label class="field-stack"
          ><span>默认币种</span
          ><select v-model="currency" class="input" :disabled="pending">
            <option v-for="item in currencies" :key="item" :value="item">{{ item }}</option>
          </select></label
        >
        <label class="field-stack"
          ><span>时区</span
          ><input
            v-model.trim="timezone"
            class="input"
            maxlength="64"
            required
            :disabled="pending"
        /></label>
        <label class="field-stack"
          ><span>设置密码</span
          ><input
            v-model="password"
            class="input"
            type="password"
            autocomplete="new-password"
            maxlength="1024"
            required
            :disabled="pending"
        /></label>
        <label class="field-stack"
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
          {{ pending ? '正在创建…' : '创建 Owner 并完成初始化' }}
        </button>
      </form>
    </template>
  </main>
</template>

<style scoped>
.setup-page {
  width: min(100% - 2rem, 36rem);
  margin: 4rem auto;
  display: grid;
  gap: 1.5rem;
}
.setup-form {
  display: grid;
  gap: 1rem;
}
</style>
