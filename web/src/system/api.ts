import { getJSON } from '@/api/client'

export interface WorkerStatus {
  state: 'healthy' | 'degraded' | 'unknown' | 'idle'
  error_code: string
  last_heartbeat_at: string | null
}

export interface SystemStatus {
  version: string
  schema_version: number
  database_size_bytes: number
  last_heartbeat_at: string | null
  collection: WorkerStatus
  retention: WorkerStatus
  checks: WorkerStatus
  notifications: WorkerStatus
}

export function getSystemStatus(signal?: AbortSignal) {
  return getJSON<SystemStatus>('/api/v1/system/status', { signal })
}
