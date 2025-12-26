<template>
  <div class="layout">
    <aside class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="brand" @click="router.push('/dashboard')">
        <span class="brand-icon">\uD83D\uDCB0</span>
        <span v-if="!isCollapsed" class="brand-text">&#26234;&#33021;&#36134;&#21333;&#31649;&#29702;</span>
      </div>

      <nav class="nav">
        <button class="nav-item" :class="{ active: currentRoute === '/dashboard' }" @click="router.push('/dashboard')">
          <i class="pi pi-chart-bar" />
          <span v-if="!isCollapsed">&#20202;&#34920;&#30424;</span>
        </button>
        <button class="nav-item" :class="{ active: currentRoute === '/payments' }" @click="router.push('/payments')">
          <i class="pi pi-wallet" />
          <span v-if="!isCollapsed">&#25903;&#20184;&#35760;&#24405;</span>
        </button>
        <button class="nav-item" :class="{ active: currentRoute === '/invoices' }" @click="router.push('/invoices')">
          <i class="pi pi-file" />
          <span v-if="!isCollapsed">&#21457;&#31080;&#31649;&#29702;</span>
        </button>
        <button class="nav-item" :class="{ active: currentRoute === '/email' }" @click="router.push('/email')">
          <i class="pi pi-inbox" />
          <span v-if="!isCollapsed">&#37038;&#31665;&#30417;&#25511;</span>
        </button>
        <button class="nav-item" :class="{ active: currentRoute === '/dingtalk' }" @click="router.push('/dingtalk')">
          <i class="pi pi-comments" />
          <span v-if="!isCollapsed">&#38025;&#38025;&#26426;&#22120;&#20154;</span>
        </button>
        <button class="nav-item" :class="{ active: currentRoute === '/logs' }" @click="router.push('/logs')">
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
          <h2 class="page-title">{{ pageTitle }}</h2>
        </div>
        <div class="topbar-right">
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
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const toast = useToast()

const isCollapsed = ref(false)
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
  display: flex;
}

.sidebar {
  width: 260px;
  background: linear-gradient(180deg, #001529 0%, #003a70 100%);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: width var(--transition-base);
  position: relative;
}

.sidebar.collapsed {
  width: 76px;
}

/* Glassmorphism overlay for sidebar */
.sidebar::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.05) 0%, transparent 100%);
  pointer-events: none;
}

.brand {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 16px;
  font-weight: 700;
  border-bottom: 1px solid rgba(255,255,255,0.1);
  white-space: nowrap;
  overflow: hidden;
  transition: all var(--transition-base);
  position: relative;
  z-index: 1;
  cursor: pointer;
  gap: 10px;
  user-select: none;
}

.brand-icon {
  font-size: 22px;
}

.brand-text {
  letter-spacing: 0.2px;
}

.nav {
  flex: 1;
  padding: 12px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  position: relative;
  z-index: 1;
}

.nav-item {
  height: 44px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 14px;
  width: 100%;
  border: 0;
  border-radius: var(--radius-md);
  background: transparent;
  color: rgba(255, 255, 255, 0.85);
  cursor: pointer;
  transition: all var(--transition-base);
  text-align: left;
}

.sidebar.collapsed .nav-item {
  justify-content: center;
  padding: 0;
}

.nav-item i {
  font-size: 18px;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.08);
  transform: translateX(2px);
}

.nav-item.active {
  background: rgba(24, 144, 255, 0.15);
  box-shadow: 0 2px 10px rgba(24, 144, 255, 0.25);
}

.nav-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  width: 3px;
  height: 28px;
  border-radius: 999px;
  background: #1890ff;
}

.sidebar-footer {
  padding: 12px 10px 14px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  position: relative;
  z-index: 1;
  display: flex;
  justify-content: center;
}

.collapse-btn {
  width: 44px;
  height: 44px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.08) !important;
  border: 1px solid rgba(255, 255, 255, 0.12) !important;
}

.collapse-btn:hover {
  background: rgba(255, 255, 255, 0.12) !important;
}

.content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.topbar {
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  height: 64px;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.06);
  position: relative;
  z-index: 20;
}

.page-title {
  margin: 0;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 20px;
  font-weight: 600;
  animation: slideInLeft 0.3s ease;
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
  border: 1px solid rgba(0, 0, 0, 0.06);
  background: rgba(255, 255, 255, 0.7);
}

.user-button:hover {
  background: rgba(102, 126, 234, 0.06);
  border-color: rgba(102, 126, 234, 0.25);
}

.user-avatar {
  box-shadow: 0 6px 16px rgba(102, 126, 234, 0.25);
}

.username {
  color: var(--color-text-primary);
  font-weight: 600;
}

.user-button i {
  color: var(--color-text-tertiary);
}

.main {
  background: var(--color-bg-primary);
  padding: 20px;
  flex: 1;
  overflow: auto;
}

@media (max-width: 768px) {
  .page-title {
    font-size: 18px;
  }

  .username {
    display: none;
  }
}
</style>
