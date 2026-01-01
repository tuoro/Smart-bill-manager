<template>
  <div class="sbm-layout" :class="{ 'sbm-layout-collapsed': sidebarCollapsed }">
    <aside v-if="!isMobile" class="sbm-sidebar sbm-surface">
      <div class="sbm-sidebar-header" @click="router.push('/dashboard')">
        <div class="sbm-logo-icon" aria-hidden="true">
          <i class="pi pi-box" />
        </div>
        <div v-if="!sidebarCollapsed" class="sbm-logo-text">
          <div class="sbm-logo-title">Smart Bill</div>
          <div class="sbm-logo-subtitle">Manager</div>
        </div>
      </div>

      <div class="sbm-sidebar-content">
        <div v-if="sidebarCollapsed" class="sbm-icon-menu">
          <button
            v-for="item in flatNavItems"
            :key="item.path"
            class="sbm-icon-menu-item"
            :class="{ active: currentRoute === item.path }"
            type="button"
            :title="item.label"
            @click="router.push(item.path)"
          >
            <i :class="item.icon" />
          </button>
        </div>

        <PanelMenu v-else v-model:expandedKeys="menuExpandedKeys" :model="menuModel" class="sbm-menu">
          <template #item="{ item }">
            <a
              v-ripple
              href="#"
              class="sbm-menu-item"
              :class="{
                'is-group': !!item.items,
                'is-leaf': !item.items,
                'is-active': item.route && item.route === currentRoute,
              }"
              @click="onMenuItemClick($event, item, false)"
            >
              <span v-if="item.items" class="sbm-menu-icon">
                <span :class="item.icon" />
              </span>
              <span class="sbm-menu-label" :class="{ 'is-group-label': !!item.items }">{{ item.label ?? '' }}</span>
              <span v-if="item.items" class="pi pi-angle-down sbm-menu-chevron" />
            </a>
          </template>
        </PanelMenu>
      </div>

      <div class="sbm-sidebar-footer">
        <Button
          class="sbm-sidebar-toggle"
          severity="secondary"
          :icon="sidebarCollapsed ? 'pi pi-angle-double-right' : 'pi pi-angle-double-left'"
          text
          :aria-label="sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'"
          @click="toggleDesktopSidebar"
        />
      </div>
    </aside>

    <header class="sbm-topbar sbm-surface">
      <div class="sbm-topbar-left">
        <Button
          class="sbm-topbar-menu-btn"
          severity="secondary"
          text
          icon="pi pi-bars"
          aria-label="菜单"
          @click="toggleMobileSidebar"
        />

        <button class="sbm-topbar-brand" type="button" @click="router.push('/dashboard')">
          <span class="sbm-topbar-brand-icon" aria-hidden="true"><i class="pi pi-box" /></span>
          <span class="sbm-topbar-brand-text">Smart Bill</span>
        </button>

        <Breadcrumb class="sbm-breadcrumb" :home="breadcrumbHome" :model="breadcrumbItems" />
      </div>

      <div class="sbm-topbar-right">
        <NotificationCenter />

        <button class="sbm-user-button" type="button" @click="toggleUserMenu">
          <Avatar v-if="userAvatarLabel" :label="userAvatarLabel" shape="circle" class="sbm-user-avatar" />
          <Avatar v-else icon="pi pi-user" shape="circle" class="sbm-user-avatar" />
          <span class="sbm-username">{{ userDisplayName }}</span>
          <i class="pi pi-angle-down sbm-user-chevron" />
        </button>
        <Menu ref="userMenu" :model="userMenuItems" popup />
      </div>
    </header>

    <Drawer
      v-model:visible="mobileSidebarVisible"
      class="sbm-mobile-drawer"
      position="left"
      :dismissable="true"
      :showCloseIcon="true"
      :modal="true"
      :blockScroll="true"
      @show="syncExpandedKeysToRoute"
    >
      <template #header>
        <div class="sbm-drawer-header" @click="go('/dashboard')">
          <div class="sbm-logo-icon" aria-hidden="true">
            <i class="pi pi-box" />
          </div>
          <span class="sbm-drawer-title">Smart Bill</span>
        </div>
      </template>

      <PanelMenu v-model:expandedKeys="menuExpandedKeys" :model="menuModel" class="sbm-menu sbm-menu-mobile">
        <template #item="{ item }">
          <a
            v-ripple
            href="#"
            class="sbm-menu-item"
            :class="{
              'is-group': !!item.items,
              'is-leaf': !item.items,
              'is-active': item.route && item.route === currentRoute,
            }"
            @click="onMenuItemClick($event, item, true)"
          >
            <span v-if="item.items" class="sbm-menu-icon">
              <span :class="item.icon" />
            </span>
            <span class="sbm-menu-label" :class="{ 'is-group-label': !!item.items }">{{ item.label ?? '' }}</span>
            <span v-if="item.items" class="pi pi-angle-down sbm-menu-chevron" />
          </a>
        </template>
      </PanelMenu>
    </Drawer>

    <div class="sbm-main-container">
      <div class="sbm-main">
        <router-view />
      </div>
    </div>

    <ChangePassword v-model="showChangePasswordDialog" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Avatar from 'primevue/avatar'
import Breadcrumb from 'primevue/breadcrumb'
import Drawer from 'primevue/drawer'
import Menu from 'primevue/menu'
import PanelMenu from 'primevue/panelmenu'
import { useToast } from 'primevue/usetoast'
import ChangePassword from '@/components/ChangePassword.vue'
import NotificationCenter from '@/components/NotificationCenter.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const toast = useToast()

const showChangePasswordDialog = ref(false)
const userMenu = ref<InstanceType<typeof Menu> | null>(null)

const mobileSidebarVisible = ref(false)
const isMobile = ref(false)

const sidebarCollapsed = ref(false)

const currentRoute = computed(() => route.path)

const updateIsMobile = () => {
  if (typeof window === 'undefined') return
  isMobile.value = window.matchMedia('(max-width: 768px)').matches
  if (!isMobile.value) mobileSidebarVisible.value = false
}

onMounted(() => {
  updateIsMobile()
  sidebarCollapsed.value = window.localStorage.getItem('sbm_sidebar_collapsed') === '1'
  syncExpandedKeysToRoute()

  if (typeof window === 'undefined') return
  window.addEventListener('resize', updateIsMobile, { passive: true })
})

onBeforeUnmount(() => {
  if (typeof window === 'undefined') return
  window.removeEventListener('resize', updateIsMobile as any)
})

watch(
  () => route.path,
  () => {
    syncExpandedKeysToRoute()
    if (isMobile.value) mobileSidebarVisible.value = false
  },
)

const breadcrumbHome = computed(() => ({ icon: 'pi pi-home', route: '/dashboard' }))
const breadcrumbItems = computed(() => {
  const title = (route.meta?.title as string | undefined) || ''
  if (!title) return []
  if (route.path === '/dashboard') return [{ label: title }]
  return [{ label: title, route: route.path }]
})

const flatNavItems = [
  { path: '/dashboard', label: '仪表盘', icon: 'pi pi-chart-bar' },
  { path: '/payments', label: '支付记录', icon: 'pi pi-wallet' },
  { path: '/invoices', label: '发票管理', icon: 'pi pi-file' },
  { path: '/trips', label: '行程日历', icon: 'pi pi-calendar' },
  { path: '/email', label: '邮箱监控', icon: 'pi pi-inbox' },
  { path: '/logs', label: '日志', icon: 'pi pi-book' },
] as const

const menuModel = computed<any[]>(() => [
  {
    key: 'overview',
    label: '概览',
    icon: 'pi pi-home',
    items: [{ label: '仪表盘', route: '/dashboard' }],
  },
  {
    key: 'bills',
    label: '账单',
    icon: 'pi pi-wallet',
    items: [
      { label: '支付记录', route: '/payments' },
      { label: '发票管理', route: '/invoices' },
    ],
  },
  {
    key: 'tools',
    label: '工具',
    icon: 'pi pi-wrench',
    items: [{ label: '行程日历', route: '/trips' }],
  },
  {
    key: 'system',
    label: '系统',
    icon: 'pi pi-cog',
    items: [
      { label: '邮箱监控', route: '/email' },
      { label: '日志', route: '/logs' },
    ],
  },
])

const menuExpandedKeys = ref<Record<string, boolean>>({})

const syncExpandedKeysToRoute = () => {
  const current = currentRoute.value
  const groups = menuModel.value
  const found = groups.find((g: any) => g.key && g.items?.some((c: any) => c.route === current))
  if (found?.key) {
    menuExpandedKeys.value = { [found.key]: true }
    return
  }
  const fallback = groups.find((g: any) => g.key)?.key
  if (fallback) menuExpandedKeys.value = { [fallback]: true }
}

const toggleDesktopSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
  window.localStorage.setItem('sbm_sidebar_collapsed', sidebarCollapsed.value ? '1' : '0')
}

const toggleMobileSidebar = () => {
  if (!isMobile.value) return
  mobileSidebarVisible.value = true
}

const go = (path: string) => {
  mobileSidebarVisible.value = false
  router.push(path)
}

const onMenuItemClick = (event: MouseEvent, item: any, isMobileMenu: boolean) => {
  event.preventDefault()

  if (item?.items && item?.key) {
    const next: Record<string, boolean> = {}
    const isOpen = !!menuExpandedKeys.value[item.key]
    if (!isOpen) next[item.key] = true
    menuExpandedKeys.value = next
    return
  }

  if (typeof item?.route === 'string' && item.route) {
    if (isMobileMenu) {
      go(item.route)
    } else {
      router.push(item.route)
    }
  }
}

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
</script>

<style scoped>
.sbm-layout {
  --sbm-topbar-height: 68px;
  --sbm-sidebar-width: 280px;
  --sbm-sidebar-collapsed-width: 84px;
  --sbm-main-padding: 18px;

  min-height: 100vh;
}

.sbm-topbar {
  position: fixed;
  inset-block-start: 12px;
  inset-inline-end: 12px;
  inset-inline-start: calc(var(--sbm-sidebar-width) + 12px);
  height: var(--sbm-topbar-height);
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px 0 10px;
  border-radius: 22px;
}

.sbm-layout.sbm-layout-collapsed .sbm-topbar {
  inset-inline-start: calc(var(--sbm-sidebar-collapsed-width) + 12px);
}

.sbm-topbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.sbm-topbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
}

.sbm-topbar-menu-btn {
  width: 42px;
  height: 42px;
  border-radius: 14px !important;
  display: none;
}

.sbm-topbar-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  border: 0;
  background: transparent;
  padding: 8px 10px;
  border-radius: 999px;
  cursor: pointer;
  user-select: none;
  transition: background var(--transition-base);
}

.sbm-topbar-brand:hover {
  background: rgba(2, 6, 23, 0.04);
}

.sbm-topbar-brand-icon {
  width: 34px;
  height: 34px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: rgba(2, 6, 23, 0.04);
  color: var(--p-primary-color);
}

.sbm-topbar-brand-text {
  font-weight: 900;
  color: var(--p-text-color);
  letter-spacing: -0.2px;
  white-space: nowrap;
}

.sbm-breadcrumb {
  min-width: 0;
}

.sbm-user-button {
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

.sbm-user-button:hover {
  background: rgba(2, 6, 23, 0.06);
}

.sbm-user-avatar {
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  box-shadow: none;
}

.sbm-username {
  color: var(--p-text-color);
  font-weight: 800;
  line-height: 1;
  white-space: nowrap;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sbm-user-chevron {
  color: var(--p-text-muted-color);
}

.sbm-sidebar {
  position: fixed;
  inset-block: 12px;
  inset-inline-start: 12px;
  width: var(--sbm-sidebar-width);
  z-index: 60;
  display: flex;
  flex-direction: column;
  padding: 12px 10px 10px;
  border-radius: 24px;
}

.sbm-layout.sbm-layout-collapsed .sbm-sidebar {
  width: var(--sbm-sidebar-collapsed-width);
}

.sbm-sidebar-header {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  border-radius: 18px;
  padding: 10px 10px;
  user-select: none;
  transition: background var(--transition-base);
}

.sbm-sidebar-header:hover {
  background: rgba(2, 6, 23, 0.04);
}

.sbm-logo-icon {
  width: 44px;
  height: 44px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  background: rgba(2, 6, 23, 0.04);
  color: var(--p-primary-color);
  flex: 0 0 auto;
}

.sbm-logo-text {
  min-width: 0;
}

.sbm-logo-title {
  font-weight: 900;
  letter-spacing: -0.2px;
  color: var(--p-text-color);
  line-height: 1.1;
}

.sbm-logo-subtitle {
  font-weight: 700;
  color: var(--p-text-muted-color);
  font-size: 12px;
  line-height: 1.2;
}

.sbm-sidebar-content {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 10px 4px 8px;
}

.sbm-icon-menu {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  padding-top: 8px;
}

.sbm-icon-menu-item {
  width: 52px;
  height: 52px;
  border-radius: 16px;
  border: 0;
  cursor: pointer;
  background: transparent;
  color: var(--p-text-muted-color);
  display: grid;
  place-items: center;
  transition: background var(--transition-base), color var(--transition-base);
}

.sbm-icon-menu-item:hover {
  background: rgba(2, 6, 23, 0.05);
  color: var(--p-text-color);
}

.sbm-icon-menu-item.active {
  background: var(--p-primary-color);
  color: var(--p-primary-contrast-color);
}

.sbm-icon-menu-item i {
  font-size: 18px;
}

.sbm-sidebar-footer {
  display: flex;
  justify-content: center;
  padding: 4px 0 2px;
}

.sbm-sidebar-toggle {
  width: 44px;
  height: 44px;
  border-radius: 999px !important;
}

.sbm-main-container {
  min-height: 100vh;
  padding-top: calc(var(--sbm-topbar-height) + 24px);
  padding-left: calc(var(--sbm-sidebar-width) + 24px);
  padding-right: 24px;
  padding-bottom: 24px;
}

.sbm-layout.sbm-layout-collapsed .sbm-main-container {
  padding-left: calc(var(--sbm-sidebar-collapsed-width) + 24px);
}

.sbm-main {
  max-width: 1280px;
  margin: 0 auto;
  padding: 0;
}

@media (max-width: 768px) {
  .sbm-topbar {
    inset-inline-start: 12px;
  }

  .sbm-topbar-menu-btn {
    display: inline-flex;
  }

  .sbm-topbar-brand-text {
    display: none;
  }

  .sbm-breadcrumb {
    display: none;
  }

  .sbm-main-container {
    padding-left: 12px;
    padding-right: 12px;
    padding-bottom: 16px;
  }

  .sbm-username {
    display: none;
  }
}
</style>

<style>
/* Drawer is teleported to <body>, so styles must be global. */
.p-drawer.sbm-mobile-drawer {
  width: min(88vw, 380px);
  border-radius: 0 20px 20px 0;
}

.p-drawer.sbm-mobile-drawer .p-drawer-header {
  padding: 16px 16px 8px;
}

.p-drawer.sbm-mobile-drawer .p-drawer-content {
  padding: 10px 10px 16px;
}

.p-drawer.sbm-mobile-drawer .sbm-drawer-header {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  user-select: none;
}

.p-drawer.sbm-mobile-drawer .sbm-drawer-title {
  font-weight: 900;
  letter-spacing: -0.2px;
}

.sbm-menu {
  border: 0;
}

.sbm-menu .p-panelmenu-panel,
.sbm-menu .p-panelmenu-header,
.sbm-menu .p-panelmenu-content {
  border: 0;
  background: transparent;
}

.sbm-menu .p-panelmenu-header {
  padding: 0;
}

.sbm-menu .p-panelmenu-content {
  padding: 0 0 6px;
}

.sbm-menu-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-decoration: none;
  color: var(--p-text-color);
  border-radius: 14px;
  transition: background var(--transition-base);
  user-select: none;
}

.sbm-menu-item.is-group {
  padding: 10px 10px;
  margin: 6px 0;
}

.sbm-menu-item.is-group:hover {
  background: rgba(2, 6, 23, 0.05);
}

.sbm-menu-icon {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--p-surface-0);
  border: 1px solid rgba(2, 6, 23, 0.1);
  color: var(--p-text-color);
  flex: 0 0 auto;
}

.sbm-menu-label.is-group-label {
  font-weight: 800;
}

.sbm-menu-chevron {
  margin-left: auto;
  color: var(--p-text-muted-color);
}

.sbm-menu-item.is-leaf {
  position: relative;
  padding: 8px 10px 8px 58px;
  margin: 2px 0;
  color: var(--p-text-muted-color);
  border-radius: 12px;
}

.sbm-menu-item.is-leaf::before {
  content: '';
  position: absolute;
  left: 32px;
  top: 8px;
  bottom: 8px;
  width: 2px;
  border-radius: 999px;
  background: rgba(2, 6, 23, 0.12);
}

.sbm-menu-item.is-leaf.is-active {
  color: var(--p-text-color);
  font-weight: 700;
  background: rgba(2, 6, 23, 0.03);
}

.sbm-menu-item.is-leaf.is-active::before {
  background: var(--p-primary-color);
}
</style>

