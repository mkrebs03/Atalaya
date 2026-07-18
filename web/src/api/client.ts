export interface APIErrorBody {
  error?: { code?: string; message?: string; request_id?: string }
}

export class JSONAPIError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly requestId?: string,
  ) {
    super(code)
  }
}

let authFailureHandler: (error: JSONAPIError) => void = () => undefined

export function setJSONAuthFailureHandler(handler: (error: JSONAPIError) => void) {
  authFailureHandler = handler
}

export async function getJSON<T>(
  path: string,
  options: { signal?: AbortSignal; etag?: string } = {},
): Promise<{ data: T | null; etag: string | null; notModified: boolean }> {
  const headers = new Headers({ Accept: 'application/json' })
  if (options.etag) headers.set('If-None-Match', options.etag)
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers,
    signal: options.signal,
  })
  if (response.status === 304) return { data: null, etag: options.etag ?? null, notModified: true }
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as APIErrorBody
    const error = new JSONAPIError(
      response.status,
      body.error?.code ?? 'unexpected_error',
      body.error?.request_id,
    )
    if (error.status === 401 || error.status === 403) authFailureHandler(error)
    throw error
  }
  return {
    data: (await response.json()) as T,
    etag: response.headers.get('ETag'),
    notModified: false,
  }
}
