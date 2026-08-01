import { getJSON } from '@/api/client'
import type { Mutate } from '@/hosts/management'

export interface Settings {
  raw_retention_seconds: number
  rollup_retention_seconds: number
  check_interval_seconds: number
  check_timeout_seconds: number
  allowed_private_cidrs: string[]
  updated_at: string
  version: number
}

export type RetentionSettings = Settings

export function getRetentionSettings(signal?: AbortSignal) {
  return getJSON<RetentionSettings>('/api/v1/settings', { signal })
}

export function updateRetentionSettings(
  mutate: Mutate,
  settings: Pick<RetentionSettings, 'version'>,
  patch: Partial<
    Pick<
      Settings,
      | 'raw_retention_seconds'
      | 'rollup_retention_seconds'
      | 'check_interval_seconds'
      | 'check_timeout_seconds'
      | 'allowed_private_cidrs'
    >
  >,
) {
  return mutate<Settings>('/api/v1/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'If-Match': `"${settings.version}"` },
    body: JSON.stringify(patch),
  })
}
