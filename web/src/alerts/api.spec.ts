import { afterEach, describe, expect, it, vi } from 'vitest'
import { changeAlertRuleState, listIncidents } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('alert API', () => {
  it('encodes incident filters and keeps pagination bounded', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ items: [], next_cursor: null }), { status: 200 }),
      )
    vi.stubGlobal('fetch', fetch)
    await listIncidents(undefined, 'open', 'critical')
    expect(fetch.mock.calls[0]?.[0]).toBe(
      '/api/v1/incidents?limit=100&state=open&severity=critical',
    )
  })

  it('uses ETag semantics for rule state mutations', async () => {
    const mutate = vi.fn().mockResolvedValue(undefined)
    await changeAlertRuleState(mutate, { id: 'rule / one', version: 7 }, 'disable')
    expect(mutate).toHaveBeenCalledWith('/api/v1/alert-rules/rule%20%2F%20one/disable', {
      method: 'POST',
      headers: { 'If-Match': '"7"' },
    })
  })
})
