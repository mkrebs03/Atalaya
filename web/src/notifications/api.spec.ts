import { describe, expect, it, vi } from 'vitest'
import { testNotificationChannel } from './api'

describe('notification API', () => {
  it('queues an idempotent test request without exposing channel configuration', async () => {
    const mutate = vi.fn().mockResolvedValue({ delivery_id: 'delivery-1', status: 'queued' })
    await testNotificationChannel(mutate, 'channel / one', 'request-1')
    expect(mutate).toHaveBeenCalledWith('/api/v1/notification-channels/channel%20%2F%20one/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ request_id: 'request-1' }),
    })
  })
})
