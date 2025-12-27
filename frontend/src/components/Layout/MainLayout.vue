<template>
  <div class="layout">
    <aside class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="brand" @click="router.push('/dashboard')">
        <div class="brand-logo" aria-hidden="true">
          <i class="pi pi-box" />
        </div>
        <span v-if="!isCollapsed" class="brand-text">Smart Bill</span>
      </div>

      <nav class="nav">
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/dashboard' }"
          title="仪表盘"
          @click="router.push('/dashboard')"
        >
          <i class="pi pi-chart-bar" />
          <span v-if="!isCollapsed">&#20202;&#34920;&#30424;</span>
        </button>
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/payments' }"
          title="支付记录"
          @click="router.push('/payments')"
        >
          <i class="pi pi-wallet" />
          <span v-if="!isCollapsed">&#25903;&#20184;&#35760;&#24405;</span>
        </button>
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/invoices' }"
          title="发票管理"
          @click="router.push('/invoices')"
        >
          <i class="pi pi-file" />
          <span v-if="!isCollapsed">&#21457;&#31080;&#31649;&#29702;</span>
        </button>
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/email' }"
          title="邮箱监控"
          @click="router.push('/email')"
        >
          <i class="pi pi-inbox" />
          <span v-if="!isCollapsed">&#37038;&#31665;&#30417;&#25511;</span>
        </button>
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/dingtalk' }"
          title="钉钉机器人"
          @click="router.push('/dingtalk')"
        >
          <i class="pi pi-comments" />
          <span v-if="!isCollapsed">&#38025;&#38025;&#26426;&#22120;&#20154;</span>
        </button>
        <button
          class="nav-item"
          :class="{ active: currentRoute === '/logs' }"
          title="日志"
          @click="router.push('/logs')"
        >
          <i class="pi pi-book" />
          <span v-if="!isCollapsed">&#26085;&#24535;</span>
        </button>
      </nav>

      <div class="sidebar-footer">
        <Button
          class="collapse-btn"
          severity="secondary"
          :icon="isCollapsed ? 'pi pi-angle-double-right' : 'pi pi-angle-double-left'"
          @click="isCollapsed = !isCollapsed"
        />
      </div>
    </aside>

    <div class="content">
      <header class="topbar">
        <div class="topbar-left">
          <div class="page-kicker">Overview</div>
          <h2 class="page-title">{{ pageTitle }}</h2>
        </div>
        <div class="topbar-right">
          <NotificationCenter />
          <button class="user-button" type="button" @click="toggleUserMenu">
            <Avatar :label="userInitial" shape="circle" class="user-avatar" />
            <span class="username">{{ authStore.user?.username || '\u7528\u6237' }}</span>
            <i class="pi pi-angle-down" />
          </button>
          <Menu ref="userMenu" :model="userMenuItems" popup />
        </div>
      </header>

      <main class="main">
        <router-view />
      </main>
    </div>

    <ChangePassword v-model="showChangePasswordDialog" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Avatar from 'primevue/avatar'
import Menu from 'primevue/menu'
import { useToast } from 'primevue/usetoast'
import ChangePassword from '@/components/ChangePassword.vue'
import NotificationCenter from '@/components/NotificationCenter.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const toast = useToast()

const isCollapsed = ref(true)
const showChangePasswordDialog = ref(false)
const userMenu = ref<InstanceType<typeof Menu> | null>(null)

const currentRoute = computed(() => route.path)

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/dashboard': '\u4EEA\u8868\u76D8',
    '/payments': '\u652F\u4ED8\u8BB0\u5F55',
    '/invoices': '\u53D1\u7968\u7BA1\u7406',
    '/email': '\u90AE\u7BB1\u76D1\u63A7',
    '/dingtalk': '\u9489\u9489\u673A\u5668\u4EBA',
    '/logs': '\u65E5\u5FD7',
  }
  return titles[route.path] || titles['/dashboard']
})

const userInitial = computed(() => {
  const name = authStore.user?.username || ''
  const trimmed = name.trim()
  if (!trimmed) return '?'
  return trimmed[0].toUpperCase()
})

const userMenuItems = computed(() => [
  {
    label: authStore.user?.username || '\u7528\u6237',
    icon: 'pi pi-user',
    disabled: true,
  },
  {
    label: '\u4FEE\u6539\u5BC6\u7801',
    icon: 'pi pi-key',
    command: () => {
      showChangePasswordDialog.value = true
    },
  },
  { separator: true },
  {
    label: '\u9000\u51FA\u767B\u5F55',
    icon: 'pi pi-sign-out',
    command: () => {
      authStore.logout()
      toast.add({ severity: 'success', summary: '\u5DF2\u9000\u51FA\u767B\u5F55', life: 2000 })
      router.push('/login')
    },
  },
])

const toggleUserMenu = (event: MouseEvent) => {
  userMenu.value?.toggle(event)
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
  height: 100vh;
  display: flex;
  gap: 20px;
  padding: 20px;
  width: 100%;
  margin: 0;
}

.sidebar {
  width: 260px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: width var(--transition-base);
  position: relative;
  padding: 20px 14px 14px;
  border-radius: 24px;
  background: var(--p-surface-50);
}

.sidebar.collapsed {
  width: 88px;
}

.sidebar.collapsed .brand {
  justify-content: center;
  padding: 0;
}

.sidebar.collapsed .nav {
  align-items: center;
  padding-left: 0;
  padding-right: 0;
}

.brand {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  color: var(--p-text-color);
  font-size: 16px;
  font-weight: 800;
  white-space: nowrap;
  overflow: hidden;
  transition: all var(--transition-base);
  position: relative;
  cursor: pointer;
  gap: 10px;
  user-select: none;
  padding: 0 6px;
}

.brand-logo {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--p-surface-0);
  border: 1px solid var(--p-primary-color);
  color: var(--p-primary-color);
}

.brand-text {
  letter-spacing: -0.2px;
  font-weight: 900;
}

.nav {
  flex: 1;
  padding: 18px 6px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: stretch;
}

.nav-item {
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  padding: 0 12px;
  border: 0;
  border-radius: 12px;
  background: transparent;
  color: var(--p-text-muted-color);
  cursor: pointer;
  transition: all var(--transition-base);
  text-align: left;
  position: relative;
}

.sidebar.collapsed .nav-item {
  width: 44px;
  justify-content: center;
  padding: 0;
  margin: 0 auto;
}

.nav-item i {
  font-size: 18px;
}

.nav-item:hover {
  background: rgba(2, 6, 23, 0.06);
}

.nav-item.active {
  background: var(--p-primary-color);
  color: var(--p-primary-contrast-color);
}

.nav-item.active::before {
  content: none;
}

.sidebar-footer {
  padding: 8px 0 2px;
  display: flex;
  justify-content: center;
}

.collapse-btn {
  width: 44px;
  height: 44px;
  border-radius: 999px;
  background: transparent !important;
  border: 0 !important;
}

.collapse-btn:hover {
  background: rgba(2, 6, 23, 0.06) !important;
}

.content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
  overflow: hidden;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 10px 0;
  position: relative;
  z-index: 20;
}

.topbar-left {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 180px;
}

.page-kicker {
  font-size: 13px;
  font-weight: 700;
  color: var(--p-text-muted-color);
}

.page-title {
  margin: 0;
  color: var(--p-text-color);
  font-size: 32px;
  font-weight: 650;
  letter-spacing: -0.3px;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.user-button {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  padding: 8px 10px;
  border-radius: 999px;
  transition: all var(--transition-base);
  border: 1px solid rgba(2, 6, 23, 0.10);
  background: rgba(2, 6, 23, 0.03);
}

.user-button:hover {
  background: rgba(2, 6, 23, 0.06);
}

.user-avatar {
  box-shadow: none;
}

.username {
  color: var(--p-text-color);
  font-weight: 800;
}

.user-button i {
  color: var(--p-text-muted-color);
}

.main {
  padding: 18px 10px 10px;
  flex: 1;
  overflow: auto;
}

@media (max-width: 768px) {
  .layout {
    padding: 12px;
    gap: 12px;
  }

  .page-title {
    font-size: 22px;
  }

  .username {
    display: none;
  }
}
</style>
