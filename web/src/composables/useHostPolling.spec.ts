import { defineComponent, nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useHostPolling } from './useHostPolling'

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('useHostPolling', () => {
  it('does not overlap requests and aborts the active request on unmount', async () => {
    vi.useFakeTimers()
    let signal: AbortSignal | undefined
    const loader = vi.fn((nextSignal: AbortSignal) => {
      signal = nextSignal
      return new Promise<string>(() => undefined)
    })
    const component = defineComponent({ setup: () => useHostPolling(loader), template: '<div />' })
    const wrapper = mount(component)
    await nextTick()
    expect(loader).toHaveBeenCalledTimes(1)
    await (wrapper.vm as unknown as { refresh: () => Promise<void> }).refresh()
    expect(loader).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    expect(signal?.aborted).toBe(true)
    await vi.runAllTimersAsync()
    expect(loader).toHaveBeenCalledTimes(1)
  })

  it('polls after thirty seconds while visible', async () => {
    vi.useFakeTimers()
    const loader = vi.fn().mockResolvedValue('ready')
    const component = defineComponent({ setup: () => useHostPolling(loader), template: '<div />' })
    const wrapper = mount(component)
    await flushPromises()
    expect(loader).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(loader).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('refreshes immediately after a hide-abort-show race', async () => {
    let visibility: DocumentVisibilityState = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility)
    const loader = vi.fn(
      (signal: AbortSignal) =>
        new Promise<string>((resolve, reject) => {
          signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
          if (loader.mock.calls.length > 1) resolve('fresh')
        }),
    )
    const component = defineComponent({ setup: () => useHostPolling(loader), template: '<div />' })
    const wrapper = mount(component)
    await nextTick()
    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(loader).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
