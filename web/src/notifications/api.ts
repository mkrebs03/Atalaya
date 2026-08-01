import { getJSON } from '@/api/client'
import type { Mutate, Page } from '@/checks/api'

export type NotificationProvider = 'discord'
export type DeliveryState = 'pending' | 'sending' | 'delivered' | 'failed' | 'skipped'

export interface NotificationChannel {
  id: string
  name: string
  provider: NotificationProvider
  secret_configured: boolean
  enabled: boolean
  archived: boolean
  archived_at: string | null
  last_test_at: string | null
  created_at: string
  updated_at: string
  version: number
}

export interface NotificationChannelInput {
  name: string
  provider: NotificationProvider
  webhook_url?: string
  enabled?: boolean
}

export interface NotificationDelivery {
  id: string
  kind: 'incident' | 'test'
  channel_id: string
  incident_id: string | null
  transition: 'firing' | 'resolved' | null
  test_request_id: string | null
  state: DeliveryState
  attempts: number
  next_attempt_at: string
  lease_until: string | null
  last_error_code: string
  skipped_reason: string
  created_at: string
  updated_at: string
  delivered_at: string | null
}

export function listNotificationChannels(
  signal?: AbortSignal,
  includeArchived = false,
  cursor?: string,
) {
  const query = new URLSearchParams({ limit: '100' })
  if (includeArchived) query.set('include_archived', 'true')
  if (cursor) query.set('cursor', cursor)
  return getJSON<Page<NotificationChannel>>(`/api/v1/notification-channels?${query}`, { signal })
}

export async function listAllNotificationChannels(signal?: AbortSignal) {
  const items: NotificationChannel[] = []
  let cursor: string | undefined
  for (let page = 0; page < 100; page += 1) {
    const response = await listNotificationChannels(signal, false, cursor)
    if (!response.data) throw new Error('paginated response missing')
    items.push(...response.data.items)
    if (!response.data.next_cursor) return items
    cursor = response.data.next_cursor
  }
  throw new Error('pagination limit exceeded')
}

export function listNotificationDeliveries(
  signal?: AbortSignal,
  channelID?: string,
  state?: DeliveryState,
) {
  const query = new URLSearchParams({ limit: '100' })
  if (channelID) query.set('channel_id', channelID)
  if (state) query.set('state', state)
  return getJSON<Page<NotificationDelivery>>(`/api/v1/notification-deliveries?${query}`, { signal })
}

export function createNotificationChannel(mutate: Mutate, input: NotificationChannelInput) {
  return mutate<NotificationChannel>('/api/v1/notification-channels', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
}

export function patchNotificationChannel(
  mutate: Mutate,
  channel: Pick<NotificationChannel, 'id' | 'version'>,
  patch: Partial<NotificationChannelInput>,
) {
  return mutate<NotificationChannel>(
    `/api/v1/notification-channels/${encodeURIComponent(channel.id)}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', 'If-Match': `"${channel.version}"` },
      body: JSON.stringify(patch),
    },
  )
}

export function changeNotificationChannelState(
  mutate: Mutate,
  channel: Pick<NotificationChannel, 'id' | 'version'>,
  action: 'archive' | 'restore',
) {
  return mutate<NotificationChannel | void>(
    `/api/v1/notification-channels/${encodeURIComponent(channel.id)}/${action}`,
    {
      method: action === 'archive' ? 'DELETE' : 'POST',
      headers: { 'If-Match': `"${channel.version}"` },
    },
  )
}

export function testNotificationChannel(mutate: Mutate, id: string, requestID: string) {
  return mutate<{ delivery_id: string; status: string }>(
    `/api/v1/notification-channels/${encodeURIComponent(id)}/test`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ request_id: requestID }),
    },
  )
}

export function getNotificationDelivery(id: string, signal?: AbortSignal) {
  return getJSON<NotificationDelivery>(
    `/api/v1/notification-deliveries/${encodeURIComponent(id)}`,
    { signal },
  )
}
