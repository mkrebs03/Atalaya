import { getJSON } from '@/api/client'

export type AdministrativeState = 'pending' | 'active' | 'disabled'
export type ObservedState = 'unknown' | 'healthy' | 'degraded' | 'unreachable'

export interface TailscaleState {
  state: 'connected' | 'stopped' | 'needs_login' | 'unknown'
  hostname: string | null
  addresses: string[]
}

export interface ConstrainedFilesystem {
  id: string
  mount_point: string
  used_percent: number
  used_bytes: number
  total_bytes: number
}

export interface HostSummary {
  id: string
  display_name: string
  administrative_state: AdministrativeState
  observed_state: ObservedState
  stale: boolean
  last_attempt_at: string | null
  last_success_received_at: string | null
  freshness_deadline_at: string | null
  consecutive_failures: number
  interval_seconds: number
  timeout_seconds: number
  cpu_used_percent: number | null
  memory_used_bytes: number | null
  memory_total_bytes: number | null
  uptime_seconds: number | null
  tailscale: TailscaleState | null
  warnings: string[]
  agent_version: string | null
  protocol_version: number | null
  most_constrained_filesystem: ConstrainedFilesystem | null
  archived: boolean
  version: number
}

export interface HostSnapshot {
  received_at: string
  observed_at: string
  hostname: string
  boot_id: string | null
  uptime_seconds: number | null
  cpu_used_percent: number | null
  memory_used_bytes: number | null
  memory_total_bytes: number | null
  tailscale: TailscaleState
  collection_warnings: string[]
}

export interface HostDetail extends HostSummary {
  agent_url: string
  snapshot: HostSnapshot | null
}

export interface Filesystem {
  id: string
  identity_kind: 'uuid' | 'fallback'
  mount_point: string
  device: string
  mount_root: string
  major_minor: string
  fs_type: string
  selected: boolean
  present: boolean
  used_bytes: number | null
  total_bytes: number | null
  available_bytes: number | null
  last_observed_at: string | null
  version: number
}

interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export function listHosts(signal?: AbortSignal, cursor?: string, includeArchived = false) {
  const query = new URLSearchParams({ limit: '100' })
  if (cursor) query.set('cursor', cursor)
  if (includeArchived) query.set('include_archived', 'true')
  return getJSON<Page<HostSummary>>(`/api/v1/hosts?${query}`, { signal })
}

export function getHost(id: string, signal?: AbortSignal, etag?: string) {
  return getJSON<HostDetail>(`/api/v1/hosts/${encodeURIComponent(id)}`, { signal, etag })
}

export function listFilesystems(id: string, signal?: AbortSignal, cursor?: string) {
  const query = new URLSearchParams({ limit: '100' })
  if (cursor) query.set('cursor', cursor)
  return getJSON<Page<Filesystem>>(`/api/v1/hosts/${encodeURIComponent(id)}/filesystems?${query}`, {
    signal,
  })
}

async function collectPages<T>(load: (cursor?: string) => Promise<{ data: Page<T> | null }>) {
  const items: T[] = []
  let cursor: string | undefined
  for (let page = 0; page < 100; page += 1) {
    const response = await load(cursor)
    if (!response.data) throw new Error('paginated response missing')
    items.push(...response.data.items)
    if (!response.data.next_cursor) return items
    cursor = response.data.next_cursor
  }
  throw new Error('pagination limit exceeded')
}

export function listAllHosts(signal?: AbortSignal) {
  return collectPages((cursor) => listHosts(signal, cursor))
}

export function listAllHostsIncludingArchived(signal?: AbortSignal) {
  return collectPages((cursor) => listHosts(signal, cursor, true))
}

export function listAllFilesystems(id: string, signal?: AbortSignal) {
  return collectPages((cursor) => listFilesystems(id, signal, cursor))
}
