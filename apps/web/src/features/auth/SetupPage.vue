<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, api } from '../../data/client'
import { theme, toggleTheme } from '../../app/theme'
import AppIcon from '../../components/AppIcon.vue'

const router = useRouter()
const identifier = ref('')
const name = ref('管理员')
const tenant = ref('我的工作区')
const currency = ref('CNY')
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai')
const password = ref('')
const confirmation = ref('')
const showPassword = ref(false)
const pending = ref(false)
const error = ref('')
const ready = ref(false)
const advanced = ref(false)
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
      email: identifier.value,
      password: password.value,
      display_name: name.value,
      tenant_name: tenant.value,
      default_currency: currency.value,
      timezone: timezone.value,
    })
    password.value = ''
    confirmation.value = ''
    void router.replace({ name: 'login', query: { reason: 'setup_complete' } })
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '初始化失败，请重试'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <!-- 复用登录页的认证外壳（.login-* 为全局样式），保证两页视觉一致。 -->
  <main id="main-content" class="login-page" tabindex="-1">
    <header class="login-topbar">
      <span class="brand" aria-label="智能账单管理">
        <span class="brand-mark"><AppIcon name="receipt" /></span>
        <span class="brand-name">智能账单管理</span>
      </span>
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
      <section class="login-story" aria-labelledby="setup-story-title">
        <p class="eyebrow">一次性初始化</p>
        <h1 id="setup-story-title">创建你的工作区</h1>
        <p class="login-intro">
          这台部署还没有账号。现在创建的是管理员账号，完成后本页面将永久关闭。
        </p>
        <ol class="trace-flow" aria-label="初始化说明">
          <li><strong>管理员</strong><span>拥有全部权限，可邀请成员并分配角色</span></li>
          <li><strong>工作区</strong><span>你的账单、行程和报销都归属于它</span></li>
          <li><strong>只有一次</strong><span>创建后无法再通过本页添加账号</span></li>
        </ol>
      </section>

      <section class="login-form-area" aria-labelledby="setup-title">
        <p v-if="!ready && !error" class="login-form">正在检查初始化状态…</p>
        <form v-else-if="ready" class="login-form" @submit.prevent="submit">
          <div>
            <h2 id="setup-title">创建管理员账号</h2>
            <p>这是本机自托管的部署，下面的信息只保存在你自己的数据库里。</p>
          </div>

          <div v-if="error" id="setup-error" class="notice notice-danger" role="alert">
            <AppIcon name="alert" />
            <span>{{ error }}</span>
          </div>

          <label class="field-stack">
            <span>管理员用户名</span>
            <small class="field-hint"
              >登录时用的账号名。本系统不发送任何邮件，可以直接用 admin；填邮箱也可以。</small
            >
            <input
              v-model.trim="identifier"
              class="input"
              type="text"
              autocomplete="username"
              maxlength="254"
              placeholder="admin"
              required
              :disabled="pending"
              :aria-invalid="Boolean(error)"
              :aria-describedby="error ? 'setup-error' : undefined"
            />
          </label>

          <label class="field-stack">
            <span>设置密码</span>
            <small class="field-hint">至少 12 个字符。忘记后只能通过服务器上的命令行恢复。</small>
            <span class="password-control">
              <input
                v-model="password"
                class="input"
                aria-label="设置密码"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="new-password"
                maxlength="1024"
                required
                :disabled="pending"
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

          <label class="field-stack">
            <span>确认密码</span>
            <input
              v-model="confirmation"
              class="input"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password"
              maxlength="1024"
              required
              :disabled="pending"
            />
          </label>

          <button
            class="setup-more"
            type="button"
            :aria-expanded="advanced"
            @click="advanced = !advanced"
          >
            <span class="setup-more-mark" :class="{ 'is-open': advanced }">
              <AppIcon name="chevron-right" />
            </span>
            <span>更多设置（姓名、工作区名称、币种、时区）</span>
          </button>

          <template v-if="advanced">
            <label class="field-stack">
              <span>姓名</span>
              <small class="field-hint">显示在界面右上角和成员列表里，用来区分是谁。</small>
              <input
                v-model.trim="name"
                class="input"
                maxlength="100"
                autocomplete="name"
                required
                :disabled="pending"
              />
            </label>
            <label class="field-stack">
              <span>工作区名称</span>
              <small class="field-hint">显示在界面左上角，你的账单、行程和报销都归属于它。</small>
              <input v-model.trim="tenant" class="input" maxlength="120" :disabled="pending" />
            </label>
            <label class="field-stack">
              <span>默认币种</span>
              <small class="field-hint">新建单据时的默认币种，每张单据仍可单独修改。</small>
              <select v-model="currency" class="input" :disabled="pending">
                <option v-for="item in currencies" :key="item" :value="item">{{ item }}</option>
              </select>
            </label>
            <label class="field-stack">
              <span>时区</span>
              <small class="field-hint">用于判断单据日期和跨期分配，已按浏览器时区预填。</small>
              <input
                v-model.trim="timezone"
                class="input"
                maxlength="64"
                required
                :disabled="pending"
              />
            </label>
          </template>

          <button class="button button-primary login-submit" type="submit" :disabled="pending">
            <span v-if="pending" class="spinner" aria-hidden="true"></span>
            {{ pending ? '正在创建…' : '创建并进入' }}
          </button>
          <p class="login-security">
            <AppIcon name="shield" /> 密码只提交到本机服务，不写入配置文件或日志。
          </p>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.field-hint {
  margin-top: -2px;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 400;
  line-height: 1.6;
}

.setup-more {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: -4px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--brand);
  cursor: pointer;
  font: inherit;
}

.setup-more-mark {
  display: inline-flex;
  transition: transform 0.15s ease;
}

.setup-more-mark.is-open {
  transform: rotate(90deg);
}

@media (prefers-reduced-motion: reduce) {
  .setup-more-mark {
    transition: none;
  }
}
</style>
