import { createRouter, createWebHistory } from 'vue-router'
import { sessionStore } from './session'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/inbox' },
    {
      path: '/join',
      name: 'join',
      component: () => import('../features/auth/JoinPage.vue'),
      meta: { public: true },
    },
    {
      path: '/settings/account',
      name: 'settings-account',
      component: () => import('../features/settings/AccountSettingsPage.vue'),
    },
    {
      path: '/settings/members',
      name: 'settings-members',
      component: () => import('../features/settings/MembersSettingsPage.vue'),
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('../features/auth/LoginPage.vue'),
      meta: { public: true },
    },
    { path: '/inbox', name: 'inbox', component: () => import('../features/inbox/InboxPage.vue') },
    {
      path: '/email-sources',
      name: 'email-sources',
      component: () => import('../features/email-sources/EmailSourcesPage.vue'),
    },
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
      path: '/payments/:factId',
      name: 'payment-detail',
      component: () => import('../features/facts/FactDetailPage.vue'),
      props: { kind: 'payment' },
    },
    {
      path: '/invoices/:factId',
      name: 'invoice-detail',
      component: () => import('../features/facts/FactDetailPage.vue'),
      props: { kind: 'invoice' },
    },
    {
      path: '/facts/:factType/:factId/correction',
      name: 'fact-correction',
      component: () => import('../features/facts/FactCorrectionPage.vue'),
    },
    {
      path: '/invoices',
      name: 'invoices',
      component: () => import('../features/facts/InvoicesPage.vue'),
    },
    {
      path: '/trips',
      name: 'trips',
      component: () => import('../features/trips/TripsPage.vue'),
    },
    {
      path: '/reimbursements',
      name: 'reimbursements',
      component: () => import('../features/reimbursements/ReimbursementsPage.vue'),
    },
    {
      path: '/insights',
      name: 'insights',
      component: () => import('../features/insights/InsightsPage.vue'),
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
  if (to.name === 'join') return true
  const session = await sessionStore.resolve()
  if (to.meta.public) {
    if (to.name === 'login' && session) return { name: 'inbox' }
    return true
  }
  if (!session) return { name: 'login', query: { redirect: to.fullPath } }
  return true
})

sessionStore.onInvalidated((reason) => {
  const route = router.currentRoute.value
  // 初次导航尚未匹配页面时，由 beforeEach 保留用户原本的目标地址。
  if (route.name && !route.meta.public)
    void router.replace({ name: 'login', query: { redirect: route.fullPath, reason } })
})

export default router
