import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { setAuthErrorHandler } from '@/api/auth'

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
        path: 'dingtalk',
        name: 'DingTalk',
        component: () => import('@/views/DingTalk.vue'),
        meta: { title: '\u9489\u9489\u673A\u5668\u4EBA' },
      },
      {
        path: 'logs',
        name: 'Logs',
        component: () => import('@/views/Logs.vue'),
        meta: { title: '\u65E5\u5FD7' },
      },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// Set up auth error handler to redirect to login
setAuthErrorHandler(() => {
  router.push('/login')
})

router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  // Check if setup is required
  let setupCheckFailed = false
  try {
    const setupResponse = await authStore.checkSetupRequired()

    if (setupResponse === null) {
      setupCheckFailed = true
      console.warn('Setup check returned null - setup status unknown')
    } else if (setupResponse.setupRequired) {
      if (to.path !== '/setup') {
        next('/setup')
        return
      }
      next()
      return
    } else {
      if (to.path === '/setup') {
        next('/login')
        return
      }
    }
  } catch (error) {
    console.error('Failed to check setup status:', error)
    setupCheckFailed = true
  }

  // If setup check failed, allow access to setup page to prevent lockout
  if (setupCheckFailed && to.path === '/setup') {
    next()
    return
  }

  if (to.meta.requiresAuth !== false) {
    if (!authStore.isAuthenticated) {
      const verified = await authStore.verifyToken()
      if (!verified) {
        if (setupCheckFailed) {
          next('/setup')
          return
        }
        next('/login')
        return
      }
    }
  }

  next()
})

export default router

