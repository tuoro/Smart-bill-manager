<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { sessionStore } from './session'
import { theme, toggleTheme } from './theme'
import AppIcon from '../components/AppIcon.vue'
import { ApiError } from '../data/client'

const router = useRouter()
const route = useRoute()
const session = sessionStore.current
const mobileQuery = window.matchMedia('(max-width: 767px)')
const isMobile = ref(mobileQuery.matches)
const navigationOpen = ref(false)
const sidebarCollapsed = ref(localStorage.getItem('sbm_sidebar_collapsed') === 'true')
const isCollapsed = computed(() => !isMobile.value && sidebarCollapsed.value)
const navigationElement = ref<HTMLElement | null>(null)
const menuButton = ref<HTMLButtonElement | null>(null)
const mainContent = ref<HTMLElement | null>(null)
const logoutError = ref('')
const loggingOut = ref(false)

const capabilities = computed(() => new Set(session.value?.capabilities ?? []))
const groups = [
  {
    id: 'workbench',
    label: '工作台',
    items: [{ to: '/inbox', label: 'AI 收件箱', icon: 'inbox', capability: 'documents.process' }],
  },
  {
    id: 'finance',
    label: '财务数据',
    items: [
      { to: '/payments', label: '支付管理', icon: 'payment', capability: 'facts.read' },
      { to: '/invoices', label: '发票管理', icon: 'document', capability: 'facts.read' },
      { to: '/trips', label: '行程归属', icon: 'trip', capability: 'facts.read' },
      {
        to: '/reimbursements',
        label: '报销管理',
        icon: 'receipt',
        capability: 'reimbursements.read',
      },
      { to: '/insights', label: '数据洞察', icon: 'chart', capability: 'insights.read' },
    ],
  },
  {
    id: 'sources',
    label: '来源',
    items: [
      { to: '/email-sources', label: '邮箱来源', icon: 'mail', capability: 'email_archive.read' },
    ],
  },
  {
    id: 'system',
    label: '系统',
    items: [
      { to: '/settings/ai', label: 'AI 配置', icon: 'settings', capability: 'providers.manage' },
      { to: '/settings/members', label: '成员管理', icon: 'users', capability: 'members.manage' },
      { to: '/settings/account', label: '账号与密码', icon: 'shield', capability: '' },
    ],
  },
] as const
const navigation = computed(() =>
  groups
    .map((group) => ({
      ...group,
      items: group.items.filter(
        (item) => !item.capability || capabilities.value.has(item.capability),
      ),
    }))
    .filter((group) => group.items.length > 0),
)
const activePath = computed(() => {
  if (route.path.startsWith('/reviews/')) return '/inbox'
  if (route.path.startsWith('/facts/payment/')) return '/payments'
  if (route.path.startsWith('/payments/')) return '/payments'
  if (route.path.startsWith('/invoices/')) return '/invoices'
  if (route.path.startsWith('/facts/invoice/')) return '/invoices'
  if (route.path.startsWith('/facts/trip/')) return '/trips'
  if (route.path.startsWith('/allocations/payment/')) return '/payments'
  if (route.path.startsWith('/allocations/invoice/')) return '/invoices'
  return route.path
})
const roleLabels = { owner: '管理员', finance: '财务', reviewer: '审核员', viewer: '只读成员' }

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
  localStorage.setItem('sbm_sidebar_collapsed', String(sidebarCollapsed.value))
}

function closeNavigation(returnFocus = false) {
  if (!navigationOpen.value) return
  navigationOpen.value = false
  const element = navigationElement.value
  if (element instanceof HTMLDialogElement && element.open) element.close()
  void nextTick(() => (returnFocus ? menuButton.value?.focus() : mainContent.value?.focus()))
}

async function openNavigation() {
  if (!isMobile.value) return
  navigationOpen.value = true
  await nextTick()
  if (!navigationOpen.value || !isMobile.value) return
  const element = navigationElement.value
  if (element instanceof HTMLDialogElement && !element.open) element.showModal()
}

function closeFromBackdrop(event: MouseEvent) {
  const element = navigationElement.value
  if (!navigationOpen.value || event.target !== element || !element) return
  const bounds = element.getBoundingClientRect()
  if (
    event.clientX < bounds.left ||
    event.clientX > bounds.right ||
    event.clientY < bounds.top ||
    event.clientY > bounds.bottom
  ) {
    closeNavigation(true)
  }
}

function updateViewport(event: MediaQueryListEvent) {
  closeNavigation()
  isMobile.value = event.matches
}

onMounted(() => mobileQuery.addEventListener('change', updateViewport))
onBeforeUnmount(() => {
  mobileQuery.removeEventListener('change', updateViewport)
  const element = navigationElement.value
  if (element instanceof HTMLDialogElement && element.open) element.close()
})

watch(
  () => route.fullPath,
  () => closeNavigation(),
)

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  logoutError.value = ''
  try {
    await sessionStore.logout()
    await router.replace({ name: 'login' })
  } catch (error) {
    logoutError.value = error instanceof ApiError ? error.message : '退出结果未确认，请重试。'
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <div class="app-frame" :data-sidebar-collapsed="isCollapsed">
    <header class="topbar">
      <RouterLink
        class="brand"
        :to="navigation[0]?.items[0]?.to ?? '/inbox'"
        aria-label="智能账单管理"
      >
        <span class="brand-mark"><AppIcon name="receipt" /></span>
        <span class="brand-name">智能账单管理</span>
      </RouterLink>
      <div class="tenant-context" aria-label="当前工作区">
        <span>{{ session?.tenant.name }}</span>
        <small>{{ session ? roleLabels[session.role] : '' }}</small>
      </div>
      <div class="topbar-actions">
        <button
          ref="menuButton"
          class="icon-button navigation-toggle"
          type="button"
          aria-label="展开导航"
          :aria-expanded="navigationOpen"
          aria-controls="primary-navigation"
          @click="openNavigation"
        >
          <AppIcon name="menu" />
        </button>
        <button
          class="icon-button"
          type="button"
          :aria-label="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
          @click="toggleTheme"
        >
          <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" />
        </button>
        <div class="user-summary">
          <strong>{{ session?.user.display_name }}</strong>
          <span>{{ session?.user.email }}</span>
        </div>
        <button class="button button-quiet" type="button" :disabled="loggingOut" @click="logout">
          退出
        </button>
      </div>
    </header>

    <div class="app-body">
      <component
        :is="isMobile ? 'dialog' : 'aside'"
        id="primary-navigation"
        ref="navigationElement"
        class="sidebar"
        aria-label="主导航"
        @cancel.prevent="closeNavigation(true)"
        @close="closeNavigation(true)"
        @click="closeFromBackdrop"
      >
        <div v-if="isMobile" class="sidebar-heading">
          <strong>导航</strong>
        </div>
        <nav id="sidebar-links">
          <section
            v-for="group in navigation"
            :key="group.id"
            class="nav-group"
            :aria-labelledby="`nav-${group.id}`"
          >
            <h2 :id="`nav-${group.id}`">{{ group.label }}</h2>
            <RouterLink
              v-for="item in group.items"
              :key="item.to"
              class="nav-item"
              :class="{ 'is-current': activePath === item.to }"
              :to="item.to"
              :aria-label="item.label"
              :title="isCollapsed ? item.label : undefined"
              :aria-current="activePath === item.to ? 'page' : undefined"
              @click="closeNavigation()"
            >
              <AppIcon class="nav-icon" :name="item.icon" />
              <span class="nav-label">{{ item.label }}</span>
            </RouterLink>
          </section>
        </nav>
        <div class="sidebar-footer">
          <button
            v-if="isMobile"
            class="sidebar-toggle"
            type="button"
            aria-label="关闭导航"
            autofocus
            @click="closeNavigation(true)"
          >
            <AppIcon name="chevron-left" />
            <span>收起导航</span>
          </button>
          <button
            v-else
            class="sidebar-toggle"
            type="button"
            :aria-label="isCollapsed ? '展开侧栏' : '收起侧栏'"
            :title="isCollapsed ? '展开侧栏' : '收起侧栏'"
            :aria-expanded="!isCollapsed"
            aria-controls="sidebar-links"
            @click="toggleSidebar"
          >
            <AppIcon :name="isCollapsed ? 'chevron-right' : 'chevron-left'" />
            <span class="sidebar-toggle-label">收起侧栏</span>
          </button>
        </div>
      </component>

      <main id="main-content" ref="mainContent" class="main-content" tabindex="-1">
        <p v-if="logoutError" class="notice notice-danger" role="alert">{{ logoutError }}</p>
        <slot />
      </main>
    </div>
  </div>
</template>
