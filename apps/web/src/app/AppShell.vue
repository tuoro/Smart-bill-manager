<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { sessionStore } from './session'

const router = useRouter()
const session = sessionStore.current
const theme = ref<'light' | 'dark'>('light')

const capabilities = computed(() => new Set(session.value?.capabilities ?? []))

onMounted(() => {
  const stored = localStorage.getItem('sbm_theme')
  const preferredDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  theme.value = stored === 'dark' || (!stored && preferredDark) ? 'dark' : 'light'
  applyTheme()
})

function applyTheme() {
  document.documentElement.dataset.theme = theme.value
}

function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
  localStorage.setItem('sbm_theme', theme.value)
  applyTheme()
}

async function logout() {
  await sessionStore.logout()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <div class="app-frame">
    <header class="topbar">
      <RouterLink class="brand" to="/inbox" aria-label="账 智能账单管理">
        <span class="brand-mark" aria-hidden="true">账</span>
        <span class="brand-name">智能账单管理</span>
      </RouterLink>
      <div class="tenant-context" aria-label="当前工作区">
        <span>{{ session?.tenant.name }}</span>
        <small>{{ session?.role }}</small>
      </div>
      <div class="topbar-actions">
        <button
          class="icon-button"
          type="button"
          :aria-label="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
          @click="toggleTheme"
        >
          <span aria-hidden="true">{{ theme === 'light' ? '◐' : '☼' }}</span>
        </button>
        <div class="user-summary">
          <strong>{{ session?.user.display_name }}</strong>
          <span>{{ session?.user.email }}</span>
        </div>
        <button class="button button-quiet" type="button" @click="logout">退出</button>
      </div>
    </header>

    <div class="app-body">
      <aside class="sidebar" aria-label="主导航">
        <nav>
          <section
            v-if="capabilities.has('documents.process')"
            class="nav-group"
            aria-labelledby="nav-workbench"
          >
            <h2 id="nav-workbench">工作台</h2>
            <RouterLink class="nav-item" to="/inbox">
              <span class="nav-icon" aria-hidden="true">收</span>
              <span class="nav-label">AI 收件箱</span>
            </RouterLink>
          </section>
          <section
            v-if="capabilities.has('reimbursements.read')"
            class="nav-group"
            aria-labelledby="nav-reimbursements"
          >
            <h2 id="nav-reimbursements">报销</h2>
            <RouterLink class="nav-item" to="/reimbursements">
              <span class="nav-icon" aria-hidden="true">报</span>
              <span class="nav-label">报销管理</span>
            </RouterLink>
          </section>
          <section
            v-if="capabilities.has('email_archive.read')"
            class="nav-group"
            aria-labelledby="nav-sources"
          >
            <h2 id="nav-sources">来源</h2>
            <RouterLink class="nav-item" to="/email-sources">
              <span class="nav-icon" aria-hidden="true">邮</span>
              <span class="nav-label">邮箱来源</span>
            </RouterLink>
          </section>
          <section
            v-if="capabilities.has('facts.read')"
            class="nav-group"
            aria-labelledby="nav-finance"
          >
            <h2 id="nav-finance">财务数据</h2>
            <RouterLink class="nav-item" to="/payments">
              <span class="nav-icon" aria-hidden="true">支</span>
              <span class="nav-label">支付管理</span>
            </RouterLink>
            <RouterLink class="nav-item" to="/invoices">
              <span class="nav-icon" aria-hidden="true">票</span>
              <span class="nav-label">发票管理</span>
            </RouterLink>
            <RouterLink class="nav-item" to="/trips">
              <span class="nav-icon" aria-hidden="true">行</span>
              <span class="nav-label">行程归属</span>
            </RouterLink>
            <RouterLink v-if="capabilities.has('insights.read')" class="nav-item" to="/insights">
              <span class="nav-icon" aria-hidden="true">析</span>
              <span class="nav-label">数据洞察</span>
            </RouterLink>
          </section>
          <section
            v-if="capabilities.has('providers.manage')"
            class="nav-group"
            aria-labelledby="nav-system"
          >
            <h2 id="nav-system">系统</h2>
            <RouterLink class="nav-item" to="/settings/ai">
              <span class="nav-icon" aria-hidden="true">配</span>
              <span class="nav-label">AI 配置</span>
            </RouterLink>
          </section>
        </nav>
      </aside>

      <main id="main-content" class="main-content" tabindex="-1">
        <slot />
      </main>
    </div>
  </div>
</template>
