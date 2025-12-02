import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { setAuthErrorHandler } from '@/api/auth'

const routes: RouteRecordRaw[] = [
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
  
  if (to.meta.requiresAuth !== false) {
    if (!authStore.isAuthenticated) {
      // Try to verify existing token
      const verified = await authStore.verifyToken()
      if (!verified) {
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
