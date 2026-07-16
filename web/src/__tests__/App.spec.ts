import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'

import { mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'

async function mountAt(path: string, attachToDocument = false) {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()

  const wrapper = mount(App, {
    attachTo: attachToDocument ? document.body : undefined,
    global: {
      plugins: [router],
    },
  })

  return { router, wrapper }
}

describe('application routes', () => {
  it('renders the operational Phase 0 bootstrap at the root', async () => {
    const { wrapper } = await mountAt('/')

    expect(wrapper.get('h1').text()).toContain('La torre de vigilancia')
    expect(wrapper.text()).toContain('Bootstrap operativo')
    expect(wrapper.findAll('.foundation-card')).toHaveLength(3)
    expect(wrapper.get('.skip-link').attributes('href')).toBe('#main-content')
    expect(wrapper.get('#main-content').attributes('tabindex')).toBe('-1')
    expect(wrapper.get('.foundation-grid').attributes('role')).toBe('list')
    expect(wrapper.get('.wordmark').attributes('href')).toBe('/')
  })

  it('renders a useful not-found view for an unknown route', async () => {
    const { wrapper } = await mountAt('/fuera-de-mapa')

    expect(wrapper.get('h1').text()).toBe('Esta ruta no está en el mapa.')
    expect(wrapper.get('a[href="/"]').text()).toContain('Volver')
  })

  it('moves focus to the route main content after SPA navigation', async () => {
    const { router, wrapper } = await mountAt('/fuera-de-mapa', true)

    await router.push('/')
    await nextTick()

    const main = wrapper.get<HTMLElement>('#main-content')
    expect(document.activeElement).toBe(main.element)

    wrapper.unmount()
  })

  it('preserves focus when only the query or hash changes', async () => {
    const { router, wrapper } = await mountAt('/', true)
    const wordmark = wrapper.get<HTMLElement>('.wordmark')
    wordmark.element.focus()

    await router.push({ path: '/', query: { view: 'summary' } })
    await nextTick()
    expect(document.activeElement).toBe(wordmark.element)

    await router.push({ path: '/', hash: '#main-content' })
    await nextTick()
    expect(document.activeElement).toBe(wordmark.element)

    wrapper.unmount()
  })
})
