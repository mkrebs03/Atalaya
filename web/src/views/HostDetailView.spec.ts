import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import HostDetailView from './HostDetailView.vue'

afterEach(() => vi.unstubAllGlobals())

function detail(id: string) {
  return {
    id,
    display_name: id.toUpperCase(),
    administrative_state: 'active',
    observed_state: 'healthy',
    stale: false,
    last_attempt_at: null,
    last_success_received_at: null,
    freshness_deadline_at: null,
    consecutive_failures: 0,
    interval_seconds: 60,
    timeout_seconds: 10,
    cpu_used_percent: 0,
    memory_used_bytes: 0,
    memory_total_bytes: 0,
    uptime_seconds: 0,
    tailscale: null,
    warnings: [],
    agent_version: null,
    protocol_version: null,
    most_constrained_filesystem: null,
    archived: false,
    version: 1,
    agent_url: `https://${id}`,
    snapshot: null,
  }
}

describe('HostDetailView resource cache', () => {
  it('resets ETag and refreshes immediately when the route host changes', async () => {
    const requests: Array<{ url: string; etag: string | null }> = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: string, init?: RequestInit) => {
        const headers = init?.headers as Headers
        requests.push({ url: input, etag: headers.get('If-None-Match') })
        if (input.includes('/filesystems'))
          return new Response(JSON.stringify({ items: [], next_cursor: null }), { status: 200 })
        const id = input.includes('/b') ? 'b' : 'a'
        return new Response(JSON.stringify(detail(id)), { status: 200, headers: { ETag: '"1"' } })
      }),
    )
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', name: 'home', component: { template: '<div />' } },
        { path: '/hosts/:id', name: 'host-detail', component: HostDetailView },
      ],
    })
    await router.push('/hosts/a')
    await router.isReady()
    const wrapper = mount(HostDetailView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('A')
    await router.push('/hosts/b')
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('B')
    const bRequest = requests.find((request) => request.url.endsWith('/hosts/b'))
    expect(bRequest?.etag).toBeNull()
    wrapper.unmount()
  })
})
