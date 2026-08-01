import { getJSON } from '@/api/client'
import type { MetricsResolution } from '@/hosts/api'

export type CheckType = 'http' | 'tcp'
export type CheckState = 'unknown' | 'up' | 'down'

export interface Check {
  id: string
  name: string
  related_host_id: string | null
  type: CheckType
  url?: string
  host?: string
  port?: number
  interval_seconds: number
  timeout_seconds: number
  expected_status_min?: number
  expected_status_max?: number
  follow_redirects: boolean
  max_redirects?: number
  enabled: boolean
  archived: boolean
  archived_at: string | null
  observed_state: CheckState
  last_attempt_at: string | null
  last_result_at: string | null
  freshness_deadline_at: string | null
  stale: boolean
  consecutive_failures: number
  last_latency_ms: number | null
  last_error_code: string | null
  last_http_status: number | null
  created_at: string
  updated_at: string
  configuration_generation: number
  version: number
}

export interface CheckInput {
  name: string
  related_host_id?: string | null
  type: CheckType
  url?: string
  host?: string
  port?: number
  interval_seconds?: number
  timeout_seconds?: number
  expected_status_min?: number
  expected_status_max?: number
  follow_redirects?: boolean
  max_redirects?: number
}

export interface CheckResult {
  status: CheckState
  latency_ms: number | null
  error_code: string | null
  http_status: number | null
}

export interface CheckRun {
  run_id: string
  check_id: string
  request_id: string
  state: 'queued' | 'running' | 'completed' | 'failed'
  requested_at: string
  started_at: string | null
  completed_at: string | null
  lease_until?: string | null
  process_attempts: number
  type: CheckType
  url?: string
  host?: string
  port?: number
  interval_seconds: number
  timeout_seconds: number
  result?: CheckResult
  failure_code?: string
}

export interface CheckMetrics {
  from: string
  to: string
  resolution: Exclude<MetricsResolution, 'mixed'>
  series: Array<{
    key: 'availability' | 'latency_ms' | string
    unit: 'ratio' | 'milliseconds' | string
    points: Array<{
      timestamp: string
      resolution: Exclude<MetricsResolution, 'auto'>
      value: number
      min: number
      max: number
      sample_count: number
      successful_count?: number
      expected_sample_count?: number
      covered_slot_count?: number
      complete?: boolean
    }>
  }>
}

export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export type Mutate = <T>(path: string, init?: RequestInit) => Promise<T>

export function listChecks(signal?: AbortSignal, includeArchived = false) {
  const query = new URLSearchParams({ limit: '100' })
  if (includeArchived) query.set('include_archived', 'true')
  return getJSON<Page<Check>>(`/api/v1/checks?${query}`, { signal })
}

export function getCheck(id: string, signal?: AbortSignal, etag?: string) {
  return getJSON<Check>(`/api/v1/checks/${encodeURIComponent(id)}`, { signal, etag })
}

export function createCheck(mutate: Mutate, input: CheckInput) {
  return mutate<Check>('/api/v1/checks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
}

export function patchCheck(
  mutate: Mutate,
  check: Pick<Check, 'id' | 'version'>,
  patch: CheckInput,
) {
  return mutate<Check>(`/api/v1/checks/${encodeURIComponent(check.id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'If-Match': `"${check.version}"` },
    body: JSON.stringify(patch),
  })
}

export function changeCheckState(
  mutate: Mutate,
  check: Pick<Check, 'id' | 'version'>,
  action: 'enable' | 'disable' | 'restore' | 'archive',
) {
  return mutate<Check | void>(`/api/v1/checks/${encodeURIComponent(check.id)}/${action}`, {
    method: action === 'archive' ? 'DELETE' : 'POST',
    headers: { 'If-Match': `"${check.version}"` },
  })
}

export function runCheck(mutate: Mutate, id: string, requestId: string) {
  return mutate<{ run_id: string; status: string }>(
    `/api/v1/checks/${encodeURIComponent(id)}/run`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ request_id: requestId }),
    },
  )
}

export function getCheckRun(id: string, runId: string, signal?: AbortSignal) {
  return getJSON<CheckRun>(
    `/api/v1/checks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runId)}`,
    { signal },
  )
}

export function listCheckRuns(id: string, signal?: AbortSignal) {
  return getJSON<Page<CheckRun>>(`/api/v1/checks/${encodeURIComponent(id)}/runs?limit=100`, {
    signal,
  })
}

export function getCheckMetrics(
  id: string,
  from: Date,
  to: Date,
  resolution: Exclude<MetricsResolution, 'mixed'> = 'auto',
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), resolution })
  return getJSON<CheckMetrics>(
    `/api/v1/checks/${encodeURIComponent(id)}/results?${query.toString()}`,
    { signal },
  )
}
