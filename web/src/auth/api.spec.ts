import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAuthAPI } from './api'

let api = createAuthAPI()

afterEach(() => {
  api.clearCSRFToken()
  api = createAuthAPI()
  vi.unstubAllGlobals()
})

function json(status: number, body: object = {}) {
  return Promise.resolve(
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

describe('authentication API client', () => {
  it('keeps CSRF only in process memory and reuses it until the session changes', async () => {
    const localSetItem = vi.fn()
    const sessionSetItem = vi.fn()
    vi.stubGlobal('localStorage', { setItem: localSetItem })
    vi.stubGlobal('sessionStorage', { setItem: sessionSetItem })
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => json(200, { csrf_token: 'memory-token' }))
      .mockImplementationOnce(() => json(204))
      .mockImplementationOnce(() => json(204))
    vi.stubGlobal('fetch', fetchMock)

    await api.logout()
    await api.logout()

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock.mock.calls[1]?.[1]?.headers).toMatchObject({
      'X-CSRF-Token': 'memory-token',
    })
    expect(fetchMock.mock.calls[2]?.[1]?.headers).toMatchObject({
      'X-CSRF-Token': 'memory-token',
    })
    expect(localSetItem).not.toHaveBeenCalled()
    expect(sessionSetItem).not.toHaveBeenCalled()
  })

  it('drops the old CSRF token after password replacement', async () => {
    const session = {
      administrator: { id: '1', username: 'admin' },
      session: { idle_expires_at: '', absolute_expires_at: '' },
    }
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => json(200, { csrf_token: 'old-token' }))
      .mockImplementationOnce(() => json(200, session))
      .mockImplementationOnce(() => json(200, { csrf_token: 'new-token' }))
      .mockImplementationOnce(() => json(204))
    vi.stubGlobal('fetch', fetchMock)

    await api.changePassword('old password', 'new password value')
    await api.logout()

    expect(fetchMock.mock.calls[1]?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'old-token' })
    expect(fetchMock.mock.calls[3]?.[1]?.headers).toMatchObject({
      'X-CSRF-Token': 'new-token',
    })
  })

  it('notifies globally and discards a rejected CSRF token', async () => {
    const handler = vi.fn()
    api.setAuthFailureHandler(handler)
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => json(200, { csrf_token: 'stale' }))
      .mockImplementationOnce(() => json(403, { error: { code: 'csrf_failed' } }))
      .mockImplementationOnce(() => json(200, { csrf_token: 'fresh' }))
      .mockImplementationOnce(() => json(204))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.logout()).rejects.toMatchObject({ status: 403, code: 'csrf_failed' })
    await api.logout()

    expect(handler).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls[3]?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'fresh' })
  })
})
