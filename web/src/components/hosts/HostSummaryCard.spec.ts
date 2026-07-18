import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import HostSummaryCard from './HostSummaryCard.vue'
import type { HostSummary, ObservedState } from '@/hosts/api'

function host(state: ObservedState, stale = false): HostSummary {
  return {
    id: state,
    display_name: 'Oracle',
    administrative_state: 'active',
    observed_state: state,
    stale,
    last_attempt_at: null,
    last_success_received_at: null,
    freshness_deadline_at: null,
    consecutive_failures: 0,
    cpu_used_percent: null,
    memory_used_bytes: null,
    memory_total_bytes: null,
    uptime_seconds: null,
    tailscale: null,
    warnings: [],
    agent_version: null,
    protocol_version: null,
    most_constrained_filesystem: null,
    version: 1,
  }
}

describe('HostSummaryCard states', () => {
  for (const [state, stale, tone, label] of [
    ['healthy', false, 'success', 'Saludable'],
    ['degraded', false, 'warning', 'Degradado'],
    ['unreachable', false, 'danger', 'Sin alcance'],
    ['unknown', false, 'neutral', 'Esperando señal'],
    ['healthy', true, 'warning', 'Señal vencida'],
  ] as const) {
    it(`renders ${state}${stale ? ' stale' : ''} with its normative tone`, async () => {
      const router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: '/hosts/:id', name: 'host-detail', component: { template: '<div />' } }],
      })
      const wrapper = mount(HostSummaryCard, {
        props: { host: host(state, stale) },
        global: { plugins: [router] },
      })
      expect(wrapper.get('.host-card__state').attributes('data-tone')).toBe(tone)
      expect(wrapper.get('.host-card__state').text()).toBe(label)
      expect(wrapper.get('a').attributes('aria-label')).toContain(`Estado: ${label}`)
    })
  }
})
