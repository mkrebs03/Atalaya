import type { ObservedState, TailscaleState } from '@/hosts/api'
import type { CheckState } from '@/checks/api'
import type { AlertRule, AlertRuleType, AlertSeverity } from '@/alerts/api'
import type { DeliveryState, NotificationDelivery } from '@/notifications/api'

export type StatusTone = 'success' | 'warning' | 'danger' | 'neutral'

export function hostStateLabel(state: ObservedState, stale = false): string {
  if (state === 'unreachable') return 'Sin conexión'
  if (stale) return 'Datos desactualizados'
  return {
    healthy: 'Saludable',
    degraded: 'Degradado',
    unknown: 'Sin datos',
  }[state]
}

export function hostStateTone(state: ObservedState, stale = false): StatusTone {
  if (state === 'unreachable') return 'danger'
  if (stale || state === 'degraded') return 'warning'
  if (state === 'healthy') return 'success'
  return 'neutral'
}

export function checkStateLabel(state: CheckState, stale = false): string {
  if (stale) return 'Datos desactualizados'
  return { up: 'Disponible', down: 'No disponible', unknown: 'Sin datos' }[state]
}

export function checkStateTone(state: CheckState, stale = false): StatusTone {
  if (stale) return 'warning'
  if (state === 'up') return 'success'
  if (state === 'down') return 'danger'
  return 'neutral'
}

export function alertRuleTypeLabel(type: AlertRuleType): string {
  return {
    host_unreachable: 'Host sin respuesta',
    check_failed: 'Servicio con errores',
    host_metric_threshold: 'Uso de CPU o memoria',
    filesystem_threshold: 'Uso de almacenamiento',
  }[type]
}

export function severityLabel(severity: AlertSeverity): string {
  return severity === 'critical' ? 'Crítica' : 'Advertencia'
}

export function alertStateLabel(
  rule: Pick<AlertRule, 'archived' | 'enabled' | 'evaluation'>,
): string {
  if (rule.archived) return 'Archivada'
  if (!rule.enabled) return 'Pausada'
  return {
    inactive: 'Sin actividad',
    pending: 'En evaluación',
    firing: 'Activa',
  }[rule.evaluation?.state ?? 'inactive']
}

export function alertStateTone(
  rule: Pick<AlertRule, 'archived' | 'enabled' | 'evaluation'>,
): StatusTone {
  if (rule.archived || !rule.enabled) return 'neutral'
  if (rule.evaluation?.state === 'firing') return 'danger'
  if (rule.evaluation?.state === 'pending') return 'warning'
  return 'success'
}

export function deliveryStateLabel(state: DeliveryState): string {
  return {
    pending: 'Pendiente',
    sending: 'Enviando',
    delivered: 'Entregada',
    failed: 'Fallida',
    skipped: 'Omitida',
  }[state]
}

export function deliveryStateTone(state: DeliveryState): StatusTone {
  if (state === 'delivered') return 'success'
  if (state === 'failed') return 'danger'
  if (state === 'pending' || state === 'sending') return 'warning'
  return 'neutral'
}

export function deliveryKindLabel(kind: NotificationDelivery['kind']): string {
  return kind === 'test' ? 'Prueba' : 'Incidente'
}

export function transitionLabel(transition: NotificationDelivery['transition']): string {
  if (transition === 'firing') return 'Apertura de alerta'
  if (transition === 'resolved') return 'Resolución de alerta'
  return 'Prueba de canal'
}

export function tailscaleStateLabel(
  state: Pick<TailscaleState, 'state'> | null | undefined,
): string {
  if (!state) return 'Sin datos'
  return {
    connected: 'Conectado',
    stopped: 'Detenido',
    needs_login: 'Requiere autenticación',
    unknown: 'Sin datos',
  }[state.state]
}

export function workerStateLabel(state: 'healthy' | 'degraded' | 'unknown' | 'idle'): string {
  return {
    healthy: 'Operativo',
    degraded: 'Degradado',
    unknown: 'No disponible',
    idle: 'Sin tareas',
  }[state]
}

export function workerStateTone(state: 'healthy' | 'degraded' | 'unknown' | 'idle'): StatusTone {
  if (state === 'healthy') return 'success'
  if (state === 'degraded') return 'warning'
  if (state === 'unknown') return 'danger'
  return 'neutral'
}

export function metricLabel(metric: 'cpu_used_percent' | 'memory_used_percent'): string {
  return metric === 'cpu_used_percent' ? 'CPU usada' : 'Memoria usada'
}

export function operatorLabel(operator: 'gt' | 'gte'): string {
  return operator === 'gt' ? '>' : '≥'
}

export function resolutionLabel(resolution: string): string {
  return (
    {
      raw: 'Datos recientes',
      hourly: 'Datos agregados',
      mixed: 'Datos recientes y agregados',
      auto: 'Resolución automática',
    }[resolution] ?? 'Datos históricos'
  )
}

export function formatDateTime(value: string | null, fallback = 'Sin datos'): string {
  if (!value) return fallback
  return new Intl.DateTimeFormat('es-CL', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

export function errorCodeLabel(code: string | null | undefined): string {
  if (!code) return 'Sin errores'
  return (
    {
      authentication_failed: 'No se pudo autenticar',
      connection_failed: 'No se pudo conectar',
      timeout: 'Tiempo de espera agotado',
      dns_failed: 'No se pudo resolver el destino',
      tls_failed: 'Falló la conexión segura',
      policy_blocked: 'Destino bloqueado por la política de red',
      invalid_response: 'Respuesta no válida',
    }[code] ?? 'Error registrado'
  )
}
