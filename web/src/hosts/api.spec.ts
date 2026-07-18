import { afterEach, describe, expect, it, vi } from 'vitest'
import { getHost, listAllHosts, listHosts } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('host API', () => {
  it('uses same-origin JSON and forwards ETags', async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: 'host-1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json', ETag: '"7"' },
      }),
    )
    vi.stubGlobal('fetch', fetch)
    const result = await getHost('host / one', undefined, '"6"')
    expect(result.etag).toBe('"7"')
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/hosts/host%20%2F%20one',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
    const headers = fetch.mock.calls[0]?.[1]?.headers as Headers
    expect(headers.get('If-None-Match')).toBe('"6"')
  })

  it('represents 304 without attempting to decode JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 304 })))
    await expect(getHost('host-1', undefined, '"7"')).resolves.toEqual({
      data: null,
      etag: '"7"',
      notModified: true,
    })
  })

  it('requests the bounded first host page', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ items: [], next_cursor: null }), { status: 200 }),
      )
    vi.stubGlobal('fetch', fetch)
    await listHosts()
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/v1/hosts?limit=100')
  })

  it('consumes every page without truncation', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [{ id: 'a' }], next_cursor: 'next' }), {
          status: 200,
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [{ id: 'b' }], next_cursor: null }), { status: 200 }),
      )
    vi.stubGlobal('fetch', fetch)
    const items = await listAllHosts()
    expect(items.map((item) => item.id)).toEqual(['a', 'b'])
    expect(fetch.mock.calls[1]?.[0]).toContain('cursor=next')
  })
})
