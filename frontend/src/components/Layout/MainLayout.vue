<template>
  <div class="layout">
    <aside v-if="!isMobile" class="sidebar" :class="{ collapsed: isCollapsed }">
      <div class="brand" @click="router.push('/dashboard')">
        <div class="brand-logo" aria-hidden="true">
          <i class="pi pi-box" />
        </div>
        <span v-if="!isCollapsed" class="brand-text">Smart Bill</span>
      </div>

      <nav class="nav">
        <button
          v-for="item in navItems"
          :key="item.path"
          class="nav-item"
          :class="{ active: currentRoute === item.path }"
          type="button"
          :title="item.label"
          @click="router.push(item.path)"
        >
          <i :class="item.icon" />
          <span v-if="!isCollapsed">{{ item.label }}</span>
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

    <Drawer
      v-model:visible="mobileNavVisible"
      class="mobile-drawer"
      position="left"
      :dismissable="true"
      :showCloseIcon="true"
      :modal="true"
      :blockScroll="true"
    >
      <template #header>
        <div class="drawer-header">
          <div class="brand-logo" aria-hidden="true">
            <i class="pi pi-box" />
          </div>
          <span class="drawer-title">Smart Bill</span>
        </div>
      </template>

      <nav class="drawer-nav">
        <button
          v-for="item in navItems"
          :key="item.path"
          class="nav-item"
          :class="{ active: currentRoute === item.path }"
          type="button"
          @click="go(item.path)"
        >
          <i :class="item.icon" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
    </Drawer>

    <div class="content">
      <header class="topbar">
        <div class="topbar-left">
          <div class="page-kicker">Overview</div>
          <h2 class="page-title">{{ pageTitle }}</h2>
        </div>

        <div class="topbar-right">
          <NotificationCenter />

          <Button
            v-if="isMobile"
            class="mobile-menu-btn"
            severity="secondary"
            outlined
            icon="pi pi-bars"
            aria-label="菜单"
            @click="mobileNavVisible = true"
          />

          <button class="user-button" type="button" @click="toggleUserMenu">
            <Avatar v-if="userAvatarLabel" :label="userAvatarLabel" shape="circle" class="user-avatar" />
            <Avatar v-else icon="pi pi-user" shape="circle" class="user-avatar" />
            <span class="username">{{ userDisplayName }}</span>
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
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Avatar from 'primevue/avatar'
import Drawer from 'primevue/drawer'
import Menu from 'primevue/menu'
import { useToast } from 'primevue/usetoast'
import ChangePassword from '@/components/ChangePassword.vue'
import NotificationCenter from '@/components/NotificationCenter.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const toast = useToast()

const navItems = [
  { path: '/dashboard', label: '仪表盘', icon: 'pi pi-chart-bar' },
  { path: '/payments', label: '支付记录', icon: 'pi pi-wallet' },
  { path: '/invoices', label: '发票管理', icon: 'pi pi-file' },
  { path: '/trips', label: '行程日历', icon: 'pi pi-calendar' },
  { path: '/email', label: '邮箱监控', icon: 'pi pi-inbox' },
  { path: '/logs', label: '日志', icon: 'pi pi-book' },
] as const

const isCollapsed = ref(true)
const showChangePasswordDialog = ref(false)
const userMenu = ref<InstanceType<typeof Menu> | null>(null)
const mobileNavVisible = ref(false)
const isMobile = ref(false)

const currentRoute = computed(() => route.path)

const updateIsMobile = () => {
  if (typeof window === 'undefined') return
  isMobile.value = window.matchMedia('(max-width: 768px)').matches
  if (!isMobile.value) mobileNavVisible.value = false
}

onMounted(() => {
  updateIsMobile()
  if (typeof window === 'undefined') return
  window.addEventListener('resize', updateIsMobile, { passive: true })
})

onBeforeUnmount(() => {
  if (typeof window === 'undefined') return
  window.removeEventListener('resize', updateIsMobile as any)
})

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/dashboard': '仪表盘',
    '/payments': '支付记录',
    '/invoices': '发票管理',
    '/trips': '行程日历',
    '/email': '邮箱监控',
    '/logs': '日志',
  }
  return titles[route.path] || titles['/dashboard']
})

const userDisplayName = computed(() => authStore.user?.username?.trim() || '用户')

const userAvatarLabel = computed(() => {
  const trimmed = userDisplayName.value.trim()
  if (!trimmed || trimmed === '用户') return ''
  const first = trimmed[0]
  if (/^\\d$/.test(first)) return ''
  return /[a-z]/i.test(first) ? first.toUpperCase() : first
})

const userMenuItems = computed(() => [
  {
    label: userDisplayName.value,
    icon: 'pi pi-user',
    disabled: true,
  },
  {
    label: '修改密码',
    icon: 'pi pi-key',
    command: () => {
      showChangePasswordDialog.value = true
    },
  },
  { separator: true },
  {
    label: '退出登录',
    icon: 'pi pi-sign-out',
    command: () => {
      authStore.logout()
      toast.add({ severity: 'success', summary: '已退出登录', life: 2000 })
      router.push('/login')
    },
  },
])

const toggleUserMenu = (event: MouseEvent) => {
  userMenu.value?.toggle(event)
}

const go = (path: string) => {
  mobileNavVisible.value = false
  router.push(path)
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
  display: flex;
  gap: 20px;
  padding: 20px;
  width: 100%;
  margin: 0;
  align-items: flex-start;
}

.sidebar {
  width: 260px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: width var(--transition-base);
  position: sticky;
  top: 20px;
  height: calc(100vh - 40px);
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
  min-width: 0;
  overflow: visible;
}

.topbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 10px 10px 0;
  position: relative;
  z-index: 20;
  gap: 12px;
}

.topbar-left {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 180px;
  flex: 1;
  min-width: 0;
}

.mobile-menu-btn {
  width: 42px;
  height: 42px;
  border-radius: 12px !important;
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
  gap: 12px;
  flex: 0 0 auto;
}

.user-button {
  height: 44px;
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  padding: 8px 10px;
  border-radius: 999px;
  transition: all var(--transition-base);
  border: 1px solid rgba(2, 6, 23, 0.1);
  background: rgba(2, 6, 23, 0.03);
}

.user-button:hover {
  background: rgba(2, 6, 23, 0.06);
}

.user-avatar {
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  box-shadow: none;
}

.username {
  color: var(--p-text-color);
  font-weight: 800;
  line-height: 1;
  white-space: nowrap;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.user-button i {
  color: var(--p-text-muted-color);
}

.main {
  padding: 18px 10px 10px;
  overflow: visible;
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

<style>
/* Drawer is teleported to <body>, so styles must be global. */
.p-drawer.mobile-drawer {
  width: min(84vw, 360px);
  border-radius: 0 18px 18px 0;
}

.p-drawer.mobile-drawer .p-drawer-content {
  padding: 12px 12px 16px;
}
</style>
