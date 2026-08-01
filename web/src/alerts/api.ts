import { getJSON } from '@/api/client'
import type { Mutate, Page } from '@/checks/api'

export type AlertRuleType =
  | 'host_unreachable'
  | 'check_failed'
  | 'host_metric_threshold'
  | 'filesystem_threshold'
export type AlertSeverity = 'warning' | 'critical'
export type AlertState = 'inactive' | 'pending' | 'firing'
export type ConditionStatus = 'true' | 'false' | 'unknown'

export interface AlertEvaluation {
  state: AlertState
  first_true_at: string | null
  last_evaluated_at: string | null
  last_value: number | null
  last_condition_status: ConditionStatus
  incident_id: string | null
  updated_at: string | null
}

export interface AlertRule {
  id: string
  name: string
  type: AlertRuleType
  severity: AlertSeverity
  host_id?: string | null
  check_id?: string | null
  filesystem_id?: string | null
  mode?: 'duration' | 'attempts'
  for_seconds?: number
  consecutive_failures?: number
  metric?: 'cpu_used_percent' | 'memory_used_percent'
  operator?: 'gt' | 'gte'
  threshold?: number
  channel_ids: string[]
  enabled: boolean
  archived: boolean
  archived_at: string | null
  created_at: string
  updated_at: string
  version: number
  evaluation: AlertEvaluation
}

export type AlertRuleInput = Omit<
  AlertRule,
  | 'id'
  | 'enabled'
  | 'archived'
  | 'archived_at'
  | 'created_at'
  | 'updated_at'
  | 'version'
  | 'evaluation'
> & { severity: AlertSeverity; channel_ids: string[] }

export interface Incident {
  id: string
  rule_id: string
  rule_name: string
  rule_type: AlertRuleType
  target_id: string
  target_name: string
  severity: AlertSeverity
  state: 'open' | 'resolved'
  condition_status: ConditionStatus
  started_at: string
  fired_at: string
  resolved_at: string | null
  initial_value: number | null
  last_value: number | null
  resolution_reason: string
}

export function listAlertRules(signal?: AbortSignal, includeArchived = false) {
  const query = new URLSearchParams({ limit: '100' })
  if (includeArchived) query.set('include_archived', 'true')
  return getJSON<Page<AlertRule>>(`/api/v1/alert-rules?${query}`, { signal })
}

export function listIncidents(
  signal?: AbortSignal,
  state?: 'open' | 'resolved',
  severity?: AlertSeverity,
) {
  const query = new URLSearchParams({ limit: '100' })
  if (state) query.set('state', state)
  if (severity) query.set('severity', severity)
  return getJSON<Page<Incident>>(`/api/v1/incidents?${query}`, { signal })
}

export function getIncident(id: string, signal?: AbortSignal) {
  return getJSON<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}`, { signal })
}

export function createAlertRule(mutate: Mutate, input: AlertRuleInput) {
  return mutate<AlertRule>('/api/v1/alert-rules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
}

export function patchAlertRule(
  mutate: Mutate,
  rule: Pick<AlertRule, 'id' | 'version'>,
  patch: Partial<AlertRuleInput>,
) {
  return mutate<AlertRule>(`/api/v1/alert-rules/${encodeURIComponent(rule.id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'If-Match': `"${rule.version}"` },
    body: JSON.stringify(patch),
  })
}

export function changeAlertRuleState(
  mutate: Mutate,
  rule: Pick<AlertRule, 'id' | 'version'>,
  action: 'enable' | 'disable' | 'restore' | 'archive',
) {
  return mutate<AlertRule | void>(`/api/v1/alert-rules/${encodeURIComponent(rule.id)}/${action}`, {
    method: action === 'archive' ? 'DELETE' : 'POST',
    headers: { 'If-Match': `"${rule.version}"` },
  })
}
