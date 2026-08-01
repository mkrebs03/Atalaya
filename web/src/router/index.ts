import { createRouter, createWebHistory, type RouterHistory, type RouteRecordRaw } from 'vue-router'
import { auth } from '@/auth/useAuth'
import AuthenticatedLayout from '@/layouts/AuthenticatedLayout.vue'
import AccountView from '@/views/AccountView.vue'
import AlertsView from '@/views/AlertsView.vue'
import CheckDetailView from '@/views/CheckDetailView.vue'
import ChecksView from '@/views/ChecksView.vue'
import HomeView from '@/views/HomeView.vue'
import HostDetailView from '@/views/HostDetailView.vue'
import HostManagementView from '@/views/HostManagementView.vue'
import LoginView from '@/views/LoginView.vue'
import NotFoundView from '@/views/NotFoundView.vue'
import NotificationsView from '@/views/NotificationsView.vue'
import SettingsView from '@/views/SettingsView.vue'
import SystemView from '@/views/SystemView.vue'

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
  {
    path: '/',
    component: AuthenticatedLayout,
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'home', component: HomeView },
      { path: 'hosts/:id', name: 'host-detail', component: HostDetailView },
      { path: 'checks', name: 'checks', component: ChecksView },
      { path: 'checks/:id', name: 'check-detail', component: CheckDetailView },
      { path: 'manage', name: 'host-management', component: HostManagementView },
      { path: 'account', name: 'account', component: AccountView },
      { path: 'settings', name: 'settings', component: SettingsView },
      { path: 'alerts', name: 'alerts', component: AlertsView },
      { path: 'notifications', name: 'notifications', component: NotificationsView },
      { path: 'system', name: 'system', component: SystemView },
      { path: ':pathMatch(.*)*', name: 'not-found', component: NotFoundView },
    ],
  },
]

function safeRedirect(value: unknown): string {
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '/'
}

export function createAppRouter(
  history: RouterHistory = createWebHistory(import.meta.env.BASE_URL),
) {
  const router = createRouter({ history, routes, scrollBehavior: () => ({ top: 0 }) })
  auth.setLoginRedirect(() => {
    if (router.currentRoute.value.name !== 'login')
      void router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
  })
  router.beforeEach(async (to) => {
    const authenticated = await auth.restore()
    if (to.meta.requiresAuth && !authenticated)
      return { name: 'login', query: { redirect: to.fullPath } }
    if (to.name === 'login' && authenticated) return safeRedirect(to.query.redirect)
    return true
  })
  return router
}

export default createAppRouter()
