import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ChangePasswordForm from '@/components/auth/ChangePasswordForm.vue'
import { createAuthContext } from './useAuth'

const session = {
  administrator: { id: '1', username: 'admin' },
  session: { idle_expires_at: '', absolute_expires_at: '' },
}

function json(status: number, body: object = {}) {
  return Promise.resolve(
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('authentication context', () => {
  it('deduplicates concurrent restore and keeps contexts isolated', async () => {
    let resolve!: (value: Response) => void
    const pending = new Promise<Response>((done) => (resolve = done))
    const fetchMock = vi.fn<typeof fetch>().mockReturnValue(pending)
    vi.stubGlobal('fetch', fetchMock)
    const first = createAuthContext()
    const second = createAuthContext()
    const restores = [first.restore(), first.restore()]
    resolve(new Response(JSON.stringify(session), { status: 200 }))
    await expect(Promise.all(restores)).resolves.toEqual([true, true])
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(first.isAuthenticated.value).toBe(true)
    expect(second.isAuthenticated.value).toBe(false)
  })

  it('reports a network failure without authenticating', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const context = createAuthContext()
    await expect(context.restore()).resolves.toBe(false)
    expect(context.alert.value).toContain('verificar la sesión')
  })

  it('globally clears and redirects on 401', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => json(200, session))
      .mockImplementationOnce(() => json(200, { csrf_token: 'token' }))
      .mockImplementationOnce(() => json(401, { error: { code: 'authentication_required' } }))
    vi.stubGlobal('fetch', fetchMock)
    const context = createAuthContext()
    const redirect = vi.fn()
    context.setLoginRedirect(redirect)
    await context.signIn('admin', 'password')
    await expect(context.signOut()).rejects.toMatchObject({ status: 401 })
    expect(context.isAuthenticated.value).toBe(false)
    expect(redirect).toHaveBeenCalledOnce()
  })

  it('clears a failed CSRF token and keeps invalid current password inline', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(() => json(200, session))
      .mockImplementationOnce(() => json(200, { csrf_token: 'stale' }))
      .mockImplementationOnce(() => json(403, { error: { code: 'csrf_failed' } }))
      .mockImplementationOnce(() => json(200, session))
      .mockImplementationOnce(() => json(200, { csrf_token: 'fresh' }))
      .mockImplementationOnce(() => json(403, { error: { code: 'current_password_invalid' } }))
    vi.stubGlobal('fetch', fetchMock)
    const first = createAuthContext()
    const redirect = vi.fn()
    first.setLoginRedirect(redirect)
    await first.signIn('admin', 'password')
    await expect(first.updatePassword('old', 'new password')).rejects.toMatchObject({
      code: 'csrf_failed',
    })
    expect(first.isAuthenticated.value).toBe(false)
    expect(redirect).toHaveBeenCalledOnce()

    const second = createAuthContext()
    await second.signIn('admin', 'password')
    await expect(second.updatePassword('bad', 'new password')).rejects.toMatchObject({
      code: 'current_password_invalid',
    })
    expect(second.isAuthenticated.value).toBe(true)
    expect(second.alert.value).toBe('')
  })
})

describe('password form', () => {
  it('clears all password fields immediately after success', async () => {
    const wrapper = mount(ChangePasswordForm, {
      props: { busy: false, error: '', success: '' },
    })
    const inputs = wrapper.findAll('input')
    await inputs[0]!.setValue('current secret')
    await inputs[1]!.setValue('new password value')
    await inputs[2]!.setValue('new password value')
    await wrapper.find('form').trigger('submit')
    expect(wrapper.emitted('submit')).toHaveLength(1)
    await wrapper.setProps({ success: 'Contraseña actualizada.' })
    await flushPromises()
    expect(inputs.map((input) => input.element.value)).toEqual(['', '', ''])
  })
})
