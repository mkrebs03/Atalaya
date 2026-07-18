import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import HostCurrentMetrics from './HostCurrentMetrics.vue'
import type { HostDetail } from '@/hosts/api'

function host(cpu: number | null, memory: number | null): HostDetail {
  return {
    id: '1',
    display_name: 'Oracle',
    administrative_state: 'active',
    observed_state: 'healthy',
    stale: false,
    last_attempt_at: null,
    last_success_received_at: null,
    freshness_deadline_at: null,
    consecutive_failures: 0,
    cpu_used_percent: cpu,
    memory_used_bytes: memory,
    memory_total_bytes: memory,
    uptime_seconds: 0,
    tailscale: null,
    warnings: [],
    agent_version: null,
    protocol_version: null,
    most_constrained_filesystem: null,
    version: 1,
    agent_url: 'https://oracle',
    snapshot: null,
  }
}

describe('HostCurrentMetrics', () => {
  it('distinguishes zero readings from missing data', () => {
    const zero = mount(HostCurrentMetrics, { props: { host: host(0, 0) } })
    expect(zero.text()).toContain('0 %')
    expect(zero.text()).toContain('0 B')
    expect(zero.text()).toContain('0 s')
    const missing = mount(HostCurrentMetrics, { props: { host: host(null, null) } })
    expect(missing.text()).toContain('Sin dato')
  })

  it('renders Tailscale hostname and addresses as distinct fields', () => {
    const connected = host(12.5, 1024)
    connected.tailscale = {
      state: 'connected',
      hostname: 'oracle.tailnet.ts.net',
      addresses: ['100.64.0.10'],
    }
    const wrapper = mount(HostCurrentMetrics, { props: { host: connected } })
    expect(wrapper.text()).toContain('Hostname · oracle.tailnet.ts.net')
    expect(wrapper.text()).toContain('Direcciones · 100.64.0.10')
  })
})
