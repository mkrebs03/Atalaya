import { getJSON } from '@/api/client'
import type { Mutate } from '@/hosts/management'

export interface RetentionSettings {
  raw_retention_seconds: number
  rollup_retention_seconds: number
  updated_at: string
  version: number
}

export function getRetentionSettings(signal?: AbortSignal) {
  return getJSON<RetentionSettings>('/api/v1/settings', { signal })
}

export function updateRetentionSettings(
  mutate: Mutate,
  settings: Pick<RetentionSettings, 'version'>,
  patch: Partial<Pick<RetentionSettings, 'raw_retention_seconds' | 'rollup_retention_seconds'>>,
) {
  return mutate<RetentionSettings>('/api/v1/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'If-Match': `"${settings.version}"` },
    body: JSON.stringify(patch),
  })
}
