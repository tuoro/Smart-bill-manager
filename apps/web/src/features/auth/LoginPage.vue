<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../../data/client'
import { sessionStore } from '../../app/session'
import { theme, toggleTheme } from '../../app/theme'
import AppIcon from '../../components/AppIcon.vue'

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
      <a class="brand" href="/login" aria-label="智能账单管理">
        <span class="brand-mark"><AppIcon name="receipt" /></span>
        <span class="brand-name">智能账单管理</span>
      </a>
      <button
        class="icon-button"
        type="button"
        :aria-label="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
        @click="toggleTheme"
      >
        <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" />
      </button>
    </header>

    <div class="login-layout">
      <section class="login-story" aria-labelledby="login-story-title">
        <p class="eyebrow">智能账单管理</p>
        <h1 id="login-story-title">整理单据，从这里开始</h1>
        <p class="login-intro">
          上传支付凭证、发票或行程单，让 AI 整理信息。核对原件并确认后，再存入正式记录。
        </p>
        <ol class="trace-flow" aria-label="单据处理流程">
          <li><strong>上传</strong><span>保留原始单据，随时查看依据</span></li>
          <li><strong>整理</strong><span>AI 提取信息，提示待核对项目</span></li>
          <li><strong>确认</strong><span>由你审核，确认后保存正式记录</span></li>
        </ol>
      </section>

      <section class="login-form-area" aria-labelledby="login-title">
        <form class="login-form" @submit.prevent="submit">
          <div>
            <h2 id="login-title">登录工作区</h2>
            <p>使用管理员创建的账号继续。</p>
          </div>

          <div v-if="error" id="login-error" class="notice notice-danger" role="alert">
            <AppIcon name="alert" />
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
          <p class="login-security">
            <AppIcon name="shield" /> AI 识别结果由你核对，不会自动入账。
          </p>
        </form>
      </section>
    </div>
  </main>
</template>
