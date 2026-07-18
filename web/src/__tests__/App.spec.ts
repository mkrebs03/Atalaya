import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../App.vue'
import { auth } from '../auth/useAuth'
import { createAppRouter } from '../router'
import { getJSON } from '../api/client'

afterEach(() => {
  vi.unstubAllGlobals()
  auth.clear()
})

function response(status: number, body: object = {}) {
  return Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  )
}

async function mountAt(path: string, authenticated = false) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      authenticated
        ? response(200, {
            administrator: { id: '1', username: 'admin' },
            session: { idle_expires_at: '', absolute_expires_at: '' },
          })
        : response(401, { error: { code: 'authentication_required' } }),
    ),
  )
  auth.clear()
  if (authenticated) await auth.signIn('admin', 'correct horse battery staple')
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, { global: { plugins: [router] } })
  await flushPromises()
  return { router, wrapper }
}

describe('authenticated application routes', () => {
  it('redirects a private direct visit to login', async () => {
    const { router, wrapper } = await mountAt('/account')
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.redirect).toBe('/account')
    expect(wrapper.get('h1').text()).toBe('La torre está cerrada por dentro.')
  })

  it('renders the protected shell for an authenticated administrator', async () => {
    const { wrapper } = await mountAt('/', true)
    expect(wrapper.get('h1').text()).toBe('La torre recibe señal.')
    expect(wrapper.text()).toContain('admin')
    expect(wrapper.get('.skip-link').attributes('href')).toBe('#main-content')
    expect(wrapper.get('[aria-label="Principal"]').text()).toContain('Resumen')
    expect(wrapper.get('[aria-label="Principal"]').text()).toContain('Cuenta')
  })

  it('rejects an external-looking login redirect', async () => {
    const { router } = await mountAt('/login?redirect=//example.test', true)
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('clears and redirects when host polling reports an expired session', async () => {
    const { router } = await mountAt('/', true)
    vi.stubGlobal(
      'fetch',
      vi.fn(() => response(401, { error: { code: 'authentication_required' } })),
    )
    await expect(getJSON('/api/v1/hosts')).rejects.toMatchObject({ status: 401 })
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('login')
    expect(auth.isAuthenticated.value).toBe(false)
    expect(auth.alert.value).toContain('Tu sesión terminó')
  })
})
