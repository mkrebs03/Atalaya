export interface Administrator {
  id: string
  username: string
}

export interface SessionResponse {
  administrator: Administrator
  session: { idle_expires_at: string; absolute_expires_at: string }
}

interface ErrorResponse {
  error?: { code?: string; message?: string; request_id?: string }
}

export class APIError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
  }
}

type AuthFailureHandler = (error: APIError) => void

export function createAuthAPI() {
  let csrf: string | null = null
  let authFailureHandler: AuthFailureHandler = () => undefined

  async function request<T>(path: string, init: RequestInit = {}, handleAuth = true): Promise<T> {
    const response = await fetch(path, {
      ...init,
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...init.headers },
    })
    if (!response.ok) {
      const body = (await response.json().catch(() => ({}))) as ErrorResponse
      const error = new APIError(
        response.status,
        body.error?.code ?? 'unexpected_error',
        body.error?.message ?? 'La solicitud falló.',
      )
      if (error.status === 403 && error.code === 'csrf_failed') csrf = null
      if (handleAuth && (error.status === 401 || error.status === 403)) authFailureHandler(error)
      throw error
    }
    if (response.status === 204) return undefined as T
    return (await response.json()) as T
  }

  async function csrfToken() {
    if (csrf) return csrf
    const result = await request<{ csrf_token: string }>('/api/v1/auth/csrf')
    csrf = result.csrf_token
    return csrf
  }

  return {
    setAuthFailureHandler(handler: AuthFailureHandler) {
      authFailureHandler = handler
    },
    clearCSRFToken() {
      csrf = null
    },
    fetchSession() {
      // Initial restoration is handled by the router guard; redirecting here would
      // race the navigation and lose its intended destination.
      return request<SessionResponse>('/api/v1/auth/me', {}, false)
    },
    login(username: string, password: string) {
      return request<SessionResponse>(
        '/api/v1/auth/login',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, password }),
        },
        false,
      )
    },
    async logout() {
      const token = await csrfToken()
      return request<void>('/api/v1/auth/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
      })
    },
    async changePassword(currentPassword: string, newPassword: string) {
      const token = await csrfToken()
      const session = await request<SessionResponse>('/api/v1/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      })
      csrf = null
      return session
    },
  }
}

export type AuthAPI = ReturnType<typeof createAuthAPI>
