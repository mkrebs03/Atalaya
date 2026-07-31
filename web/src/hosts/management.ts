import { getJSON } from '@/api/client'
import type { HostSummary } from './api'

export interface EnrollmentToken {
  id: string
  label: string | null
  created_at: string
  expires_at: string
  revoked_at: string | null
  consumed_at: string | null
  replay_until: string | null
  host_id: string | null
  version: number
}

export interface NewEnrollmentToken extends EnrollmentToken {
  token: string
}

interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export type Mutate = <T>(path: string, init?: RequestInit) => Promise<T>

export function listEnrollmentTokens(signal?: AbortSignal) {
  return getJSON<Page<EnrollmentToken>>('/api/v1/enrollment-tokens?limit=100', { signal })
}

export function createEnrollmentToken(mutate: Mutate, label: string) {
  return mutate<NewEnrollmentToken>('/api/v1/enrollment-tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label }),
  })
}

export function revokeEnrollmentToken(
  mutate: Mutate,
  token: Pick<EnrollmentToken, 'id' | 'version'>,
) {
  return mutate<void>(`/api/v1/enrollment-tokens/${encodeURIComponent(token.id)}`, {
    method: 'DELETE',
    headers: { 'If-Match': `"${token.version}"` },
  })
}

type VersionedResource = { id: string; version: number }

export function patchHost(mutate: Mutate, host: VersionedResource, patch: Record<string, unknown>) {
  return mutate<HostSummary>(`/api/v1/hosts/${encodeURIComponent(host.id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'If-Match': `"${host.version}"` },
    body: JSON.stringify(patch),
  })
}

export function changeHostState(
  mutate: Mutate,
  host: VersionedResource,
  action: 'enable' | 'disable' | 'archive' | 'restore',
) {
  return mutate<HostSummary | void>(`/api/v1/hosts/${encodeURIComponent(host.id)}/${action}`, {
    method: 'POST',
    headers: { 'If-Match': `"${host.version}"` },
  })
}

export function selectFilesystem(
  mutate: Mutate,
  host: VersionedResource,
  filesystem: VersionedResource,
  selected: boolean,
) {
  return mutate<void>(
    `/api/v1/hosts/${encodeURIComponent(host.id)}/filesystems/${encodeURIComponent(filesystem.id)}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', 'If-Match': `"${filesystem.version}"` },
      body: JSON.stringify({ selected }),
    },
  )
}
