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
        path: 'trips',
        name: 'Trips',
        component: () => import('@/views/Trips.vue'),
        meta: { title: '\u884C\u7A0B\u65E5\u5386' },
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

  // Only check setup status when needed:
  // - when user tries to visit /setup
  // - when user is not authenticated (first install / logged out)
  let setupResponse: { setupRequired: boolean } | null = null
  let setupCheckFailed = false
  if (to.path === '/setup' || !authStore.isAuthenticated) {
    try {
      setupResponse = await authStore.checkSetupRequired()
      if (setupResponse === null) {
        setupCheckFailed = true
        console.warn('Setup check returned null - setup status unknown')
      }
    } catch (error) {
      console.error('Failed to check setup status:', error)
      setupCheckFailed = true
    }
  }

  if (to.path === '/setup') {
    // If setup is not required (admin already exists) or status is unknown, always redirect to login.
    if (setupCheckFailed || (setupResponse && !setupResponse.setupRequired)) {
      next('/login')
      return
    }
    next()
    return
  }

  if (!authStore.isAuthenticated && setupResponse && setupResponse.setupRequired) {
    next('/setup')
    return
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

  next()
})

export default router
