import { requestJson } from './client'

export type NotificationEvent =
  | 'library.new'
  | 'library.deleted'
  | 'item.rate'
  | 'item.markplayed'
  | 'item.markunplayed'
  | 'playback.start'
  | 'playback.stop'
  | 'system.notificationtest'
  | string

export interface WebhookSubscription {
  id: string
  name: string
  url: string
  events: NotificationEvent[]
  enabled: boolean
  group_items: boolean
  last_status?: number
  last_error?: string
  last_sent_at?: string
  created_at: string
  updated_at: string
}

export interface WebhookSubscriptionInput {
  name: string
  url: string
  events: NotificationEvent[]
  enabled?: boolean
  group_items?: boolean
}

export async function listWebhookSubscriptions() {
  return requestJson<WebhookSubscription[]>('/Admin/Notifications/Subscriptions')
}

export async function createWebhookSubscription(input: WebhookSubscriptionInput) {
  return requestJson<WebhookSubscription>('/Admin/Notifications/Subscriptions', {
    method: 'POST',
    body: JSON.stringify({ ...input, group_items: input.group_items ?? false }),
  })
}

export async function updateWebhookSubscription(id: string, input: WebhookSubscriptionInput) {
  return requestJson<WebhookSubscription>(`/Admin/Notifications/Subscriptions/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify({ ...input, group_items: input.group_items ?? false }),
  })
}

export async function deleteWebhookSubscription(id: string) {
  return requestJson<void>(`/Admin/Notifications/Subscriptions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function testWebhookSubscription(id: string) {
  return requestJson<void>(`/Admin/Notifications/Subscriptions/${encodeURIComponent(id)}/Test`, {
    method: 'POST',
    timeoutMs: 60_000,
  })
}

export async function getNotificationSamplePayload(event: NotificationEvent) {
  const qs = new URLSearchParams({ Event: event })
  return requestJson<Record<string, unknown>>(`/Admin/Notifications/SamplePayload?${qs.toString()}`)
}

export async function getSupportedNotificationEvents() {
  return requestJson<{ events: NotificationEvent[] }>('/Admin/Notifications/SupportedEvents')
}
