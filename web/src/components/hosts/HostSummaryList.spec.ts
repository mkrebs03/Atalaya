import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import HostSummaryList from './HostSummaryList.vue'

describe('HostSummaryList states', () => {
  it('announces loading, empty and error states', async () => {
    const wrapper = mount(HostSummaryList, {
      props: { hosts: [], loading: true, error: false, refreshing: false },
    })
    expect(wrapper.get('[role="status"]').text()).toContain('Consultando')
    await wrapper.setProps({ loading: false })
    expect(wrapper.text()).toContain('Todavía no hay hosts')
    await wrapper.setProps({ error: true })
    expect(wrapper.get('[role="alert"]').text()).toContain('No pudimos')
  })
})
