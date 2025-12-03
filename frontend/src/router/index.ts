import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { setAuthErrorHandler } from '@/api/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('@/views/Setup.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    component: () => import('@/components/Layout/MainLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        redirect: '/dashboard'
      },
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/Dashboard.vue'),
        meta: { title: '仪表盘' }
      },
      {
        path: 'payments',
        name: 'Payments',
        component: () => import('@/views/Payments.vue'),
        meta: { title: '支付记录' }
      },
      {
        path: 'invoices',
        name: 'Invoices',
        component: () => import('@/views/Invoices.vue'),
        meta: { title: '发票管理' }
      },
      {
        path: 'email',
        name: 'EmailMonitor',
        component: () => import('@/views/EmailMonitor.vue'),
        meta: { title: '邮箱监控' }
      },
      {
        path: 'dingtalk',
        name: 'DingTalk',
        component: () => import('@/views/DingTalk.vue'),
        meta: { title: '钉钉机器人' }
      }
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// Set up auth error handler to redirect to login
setAuthErrorHandler(() => {
  router.push('/login')
})

router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()
  
  console.log('[Router Guard] Navigating to:', to.path)
  
  // Check if setup is required
  try {
    const setupResponse = await authStore.checkSetupRequired()
    console.log('[Router Guard] Setup response:', setupResponse)
    if (setupResponse && setupResponse.setupRequired) {
      // Setup is required - redirect to setup page
      console.log('[Router Guard] Setup required, current path:', to.path)
      if (to.path !== '/setup') {
        console.log('[Router Guard] Redirecting to /setup')
        next('/setup')
        return
      }
      // Allow access to setup page
      console.log('[Router Guard] Allowing access to setup page')
      next()
      return
    } else if (setupResponse) {
      // Setup is not required - don't allow access to setup page
      if (to.path === '/setup') {
        console.log('[Router Guard] Setup not required, redirecting to /login')
        next('/login')
        return
      }
    }
  } catch (error) {
    console.error('Failed to check setup status:', error)
  }
  
  if (to.meta.requiresAuth !== false) {
    if (!authStore.isAuthenticated) {
      // Try to verify existing token
      const verified = await authStore.verifyToken()
      if (!verified) {
        console.log('[Router Guard] Not authenticated, redirecting to /login')
        next('/login')
        return
      }
    }
  }
  
  if (to.path === '/login' && authStore.isAuthenticated) {
    next('/dashboard')
    return
  }
  
  next()
})

export default router
