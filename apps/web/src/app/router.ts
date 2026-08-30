import { createRouter, createWebHistory } from 'vue-router'
import { sessionStore } from './session'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/inbox' },
    {
      path: '/login',
      name: 'login',
      component: () => import('../features/auth/LoginPage.vue'),
      meta: { public: true },
    },
    { path: '/inbox', name: 'inbox', component: () => import('../features/inbox/InboxPage.vue') },
    {
      path: '/reviews/:jobId',
      name: 'review',
      component: () => import('../features/review/ReviewPage.vue'),
    },
    {
      path: '/payments',
      name: 'payments',
      component: () => import('../features/facts/PaymentsPage.vue'),
    },
    {
      path: '/invoices',
      name: 'invoices',
      component: () => import('../features/facts/InvoicesPage.vue'),
    },
    {
      path: '/allocations/:factType/:factId',
      name: 'allocation',
      component: () => import('../features/allocations/AllocationPage.vue'),
    },
    {
      path: '/settings/ai',
      name: 'settings-ai',
      component: () => import('../features/settings/ProviderSettingsPage.vue'),
    },
    { path: '/:pathMatch(.*)*', redirect: '/inbox' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  const session = await sessionStore.resolve()
  if (to.meta.public) {
    if (to.name === 'login' && session) return { name: 'inbox' }
    return true
  }
  if (!session) return { name: 'login', query: { redirect: to.fullPath } }
  return true
})

export default router
