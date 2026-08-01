import { createRouter, createWebHistory, type RouterHistory, type RouteRecordRaw } from 'vue-router'
import { auth } from '@/auth/useAuth'
import AuthenticatedLayout from '@/layouts/AuthenticatedLayout.vue'
import AccountView from '@/views/AccountView.vue'
import HomeView from '@/views/HomeView.vue'
import HostDetailView from '@/views/HostDetailView.vue'
import HostManagementView from '@/views/HostManagementView.vue'
import LoginView from '@/views/LoginView.vue'
import NotFoundView from '@/views/NotFoundView.vue'
import SettingsView from '@/views/SettingsView.vue'

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
  {
    path: '/',
    component: AuthenticatedLayout,
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'home', component: HomeView },
      { path: 'hosts/:id', name: 'host-detail', component: HostDetailView },
      { path: 'manage', name: 'host-management', component: HostManagementView },
      { path: 'account', name: 'account', component: AccountView },
      { path: 'settings', name: 'settings', component: SettingsView },
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
