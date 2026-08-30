<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../../data/client'
import { sessionStore } from '../../app/session'

const route = useRoute()
const router = useRouter()
const email = ref('')
const password = ref('')
const showPassword = ref(false)
const pending = ref(false)
const error = ref('')

const redirectPath = computed(() => {
  const value = typeof route.query.redirect === 'string' ? route.query.redirect : '/inbox'
  return value.startsWith('/') && !value.startsWith('//') ? value : '/inbox'
})

async function submit() {
  error.value = ''
  pending.value = true
  try {
    await sessionStore.login(email.value, password.value)
    await router.replace(redirectPath.value)
  } catch (caught) {
    if (caught instanceof ApiError) {
      error.value = caught.message
    } else {
      error.value = '网络连接失败，请检查服务状态后重试'
    }
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main id="main-content" class="login-page" tabindex="-1">
    <header class="login-topbar">
      <a class="brand" href="/login" aria-label="账 智能账单管理">
        <span class="brand-mark" aria-hidden="true">账</span>
        <span class="brand-name">智能账单管理</span>
      </a>
      <span class="login-edition">Clean Slate · M1</span>
    </header>

    <div class="login-layout">
      <section class="login-story" aria-labelledby="login-story-title">
        <p class="eyebrow">可信的 AI 财务工作台</p>
        <h1 id="login-story-title">让每一笔账，都能回到它的原始依据</h1>
        <p class="login-intro">
          原始单据保持不变，AI 只生成候选 Claim；只有经过人的确认，才形成正式财务事实。
        </p>
        <ol class="trace-flow" aria-label="数据可信链">
          <li><strong>Source</strong><span>保留原始文件与页级证据</span></li>
          <li><strong>Claim</strong><span>结构化提取并运行确定性校验</span></li>
          <li><strong>Fact</strong><span>人工审核后才写入正式账单</span></li>
        </ol>
      </section>

      <section class="login-form-area" aria-labelledby="login-title">
        <form class="login-form" @submit.prevent="submit">
          <div>
            <h2 id="login-title">登录工作区</h2>
            <p>使用管理员创建的账号继续。</p>
          </div>

          <div v-if="error" id="login-error" class="notice notice-danger" role="alert">
            <span aria-hidden="true">!</span>
            <span>{{ error }}</span>
          </div>

          <label class="field-stack">
            <span>邮箱</span>
            <input
              v-model.trim="email"
              class="input"
              type="email"
              name="email"
              autocomplete="username"
              maxlength="254"
              required
              :aria-invalid="Boolean(error)"
              :aria-describedby="error ? 'login-error' : undefined"
            />
          </label>

          <label class="field-stack">
            <span>密码</span>
            <span class="password-control">
              <input
                v-model="password"
                class="input"
                aria-label="密码"
                :type="showPassword ? 'text' : 'password'"
                name="password"
                autocomplete="current-password"
                maxlength="1024"
                required
                :aria-invalid="Boolean(error)"
                :aria-describedby="error ? 'login-error' : undefined"
              />
              <button
                class="password-toggle"
                type="button"
                :aria-label="showPassword ? '隐藏密码' : '显示密码'"
                @click="showPassword = !showPassword"
              >
                {{ showPassword ? '隐藏' : '显示' }}
              </button>
            </span>
          </label>

          <button class="button button-primary login-submit" type="submit" :disabled="pending">
            <span v-if="pending" class="spinner" aria-hidden="true"></span>
            {{ pending ? '正在登录…' : '登录' }}
          </button>
          <p class="login-security">会话仅保存在安全 Cookie 中；浏览器不会保存 Provider 密钥。</p>
        </form>
      </section>
    </div>
  </main>
</template>
