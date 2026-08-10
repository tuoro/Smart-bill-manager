import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { setAuthErrorHandler } from '@/api/auth'
import { getStoredUser, getToken } from '@/api/storage'

const routes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('@/views/Setup.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/views/Register.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/',
    component: () => import('@/components/Layout/MainLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', redirect: '/dashboard' },
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/Dashboard.vue'),
        meta: { title: '\u4EEA\u8868\u76D8' },
      },
      {
        path: 'payments',
        name: 'Payments',
        component: () => import('@/views/Payments.vue'),
        meta: { title: '\u652F\u4ED8\u8BB0\u5F55' },
      },
      {
        path: 'invoices',
        name: 'Invoices',
        component: () => import('@/views/Invoices.vue'),
        meta: { title: '\u53D1\u7968\u7BA1\u7406' },
      },
      {
        path: 'email',
        name: 'EmailMonitor',
        component: () => import('@/views/EmailMonitor.vue'),
        meta: { title: '\u90AE\u7BB1\u76D1\u63A7' },
      },
      {
        path: 'trips',
        name: 'Trips',
        component: () => import('@/views/Trips.vue'),
        meta: { title: '\u884C\u7A0B\u65E5\u5386' },
      },
      {
        path: 'logs',
        name: 'Logs',
        component: () => import('@/views/Logs.vue'),
        meta: { title: '\u65E5\u5FD7', requiresAdmin: true },
      },
      {
        path: 'admin/invites',
        name: 'AdminInvites',
        component: () => import('@/views/AdminInvites.vue'),
        meta: { title: '邀请码管理', requiresAdmin: true },
      },
      {
        path: 'admin/users',
        name: 'AdminUsers',
        component: () => import('@/views/AdminUsers.vue'),
        meta: { title: '用户', requiresAdmin: true },
      },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

setAuthErrorHandler(() => {
  const authStore = useAuthStore()
  authStore.clearSession()
  void router.push('/login')
})

router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  let setupResponse: { setupRequired: boolean } | null = null
  if (to.path === '/setup' || !authStore.isAuthenticated) {
    try {
      setupResponse = await authStore.checkSetupRequired()
    } catch (error) {
      console.error('Failed to check setup status:', error)
    }
  }

  if (to.path === '/setup') {
    if (setupResponse && !setupResponse.setupRequired) {
      next('/login')
      return
    }
    next()
    return
  }

  if (!authStore.isAuthenticated) {
    if (setupResponse?.setupRequired) {
      next('/setup')
      return
    }

    if (setupResponse === null) {
      const hasLocalToken = Boolean(getToken())
      const hasLocalUser = Boolean(getStoredUser())
      if (!hasLocalToken && !hasLocalUser) {
        next('/setup')
        return
      }
    }
  }

  if (to.meta.requiresAuth !== false) {
    if (!authStore.isAuthenticated) {
      const verified = await authStore.verifyToken()
      if (!verified) {
        next('/login')
        return
      }
    }
  }

  if (to.meta.requiresAdmin) {
    if (authStore.user?.role !== 'admin') {
      next('/dashboard')
      return
    }
  }

  next()
})

export default router
