import { computed, inject, readonly, shallowRef, type InjectionKey } from 'vue'
import { APIError, createAuthAPI, type Administrator, type AuthAPI } from './api'
import { setJSONAuthFailureHandler } from '@/api/client'

export function createAuthContext(client: AuthAPI = createAuthAPI()) {
  const administrator = shallowRef<Administrator | null>(null)
  const restored = shallowRef(false)
  const alert = shallowRef('')
  let restoring: Promise<boolean> | null = null
  let redirectToLogin: () => void = () => undefined

  function clear(message = '') {
    client.clearCSRFToken()
    administrator.value = null
    restored.value = true
    alert.value = message
  }

  client.setAuthFailureHandler((error) => {
    if (error.status === 401) {
      clear('Tu sesión terminó. Vuelve a ingresar.')
      redirectToLogin()
    } else if (error.code === 'csrf_failed') {
      clear('La sesión de seguridad cambió. Vuelve a ingresar.')
      redirectToLogin()
    } else if (error.code !== 'current_password_invalid') {
      alert.value = 'La operación fue rechazada. Intenta nuevamente.'
    }
  })

  setJSONAuthFailureHandler(() => {
    if (administrator.value === null) return
    clear('Tu sesión terminó. Vuelve a ingresar.')
    redirectToLogin()
  })

  async function restore(): Promise<boolean> {
    if (restored.value) return administrator.value !== null
    if (restoring) return restoring
    restoring = client
      .fetchSession()
      .then((session) => {
        administrator.value = session.administrator
        return true
      })
      .catch((error: unknown) => {
        administrator.value = null
        if (!(error instanceof APIError && error.status === 401))
          alert.value = 'No se pudo verificar la sesión. Intenta nuevamente.'
        return false
      })
      .finally(() => {
        restored.value = true
        restoring = null
      })
    return restoring
  }

  async function signIn(username: string, password: string) {
    const session = await client.login(username, password)
    client.clearCSRFToken()
    administrator.value = session.administrator
    restored.value = true
    alert.value = ''
  }

  async function signOut() {
    try {
      await client.logout()
    } finally {
      clear()
    }
  }

  async function updatePassword(currentPassword: string, newPassword: string) {
    const session = await client.changePassword(currentPassword, newPassword)
    administrator.value = session.administrator
    restored.value = true
  }

  return {
    administrator: readonly(administrator),
    isAuthenticated: computed(() => administrator.value !== null),
    alert: readonly(alert),
    restore,
    signIn,
    signOut,
    updatePassword,
    clear,
    setLoginRedirect(handler: () => void) {
      redirectToLogin = handler
    },
  }
}

export type AuthContext = ReturnType<typeof createAuthContext>
export const authKey: InjectionKey<AuthContext> = Symbol('atalaya-auth')
export const auth = createAuthContext()

export function useAuth(): AuthContext {
  const context = inject(authKey)
  if (!context) throw new Error('authentication context is required')
  return context
}
