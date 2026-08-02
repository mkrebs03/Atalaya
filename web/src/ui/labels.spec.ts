import { describe, expect, it } from 'vitest'
import {
  alertRuleTypeLabel,
  alertStateLabel,
  checkStateLabel,
  deliveryStateLabel,
  hostStateLabel,
  severityLabel,
} from './labels'

describe('user-facing status labels', () => {
  it('translates operational states without hiding stale data', () => {
    expect(hostStateLabel('healthy')).toBe('Saludable')
    expect(hostStateLabel('healthy', true)).toBe('Datos desactualizados')
    expect(checkStateLabel('down')).toBe('No disponible')
    expect(checkStateLabel('up', true)).toBe('Datos desactualizados')
  })

  it('translates alert and delivery vocabulary', () => {
    expect(alertRuleTypeLabel('host_unreachable')).toBe('Host sin respuesta')
    expect(severityLabel('critical')).toBe('Crítica')
    expect(
      alertStateLabel({
        archived: false,
        enabled: true,
        evaluation: {
          state: 'firing',
          first_true_at: null,
          last_evaluated_at: null,
          last_value: null,
          last_condition_status: 'true',
          incident_id: null,
          updated_at: null,
        },
      }),
    ).toBe('Activa')
    expect(deliveryStateLabel('failed')).toBe('Fallida')
  })
})
