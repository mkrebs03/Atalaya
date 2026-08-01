import { describe, expect, it } from 'vitest'
import { hasIncompleteHourlyRollups, hasMetricGaps } from './historical'

function point(timestamp: string, resolution: 'raw' | 'hourly') {
  return { timestamp, resolution } as const
}

describe('historical gap detection', () => {
  it('does not mark consecutive hourly buckets as gaps', () => {
    expect(
      hasMetricGaps(
        [point('2026-07-31T10:00:00Z', 'hourly'), point('2026-07-31T11:00:00Z', 'hourly')],
        130,
      ),
    ).toBe(false)
  })

  it('marks a missing hourly bucket as a gap', () => {
    expect(
      hasMetricGaps(
        [point('2026-07-31T10:00:00Z', 'hourly'), point('2026-07-31T12:00:00Z', 'hourly')],
        130,
      ),
    ).toBe(true)
  })

  it('allows the hourly-to-raw transition at the source boundary', () => {
    expect(
      hasMetricGaps(
        [point('2026-07-31T10:00:00Z', 'hourly'), point('2026-07-31T11:01:00Z', 'raw')],
        130,
      ),
    ).toBe(false)
  })

  it('marks a missing hourly bucket even with a large raw tolerance', () => {
    expect(
      hasMetricGaps(
        [point('2026-07-31T10:00:00Z', 'hourly'), point('2026-07-31T12:00:00Z', 'hourly')],
        7210,
      ),
    ).toBe(true)
  })

  it('distinguishes incomplete hourly rollups from raw points', () => {
    expect(
      hasIncompleteHourlyRollups([
        { resolution: 'raw', complete: false },
        { resolution: 'hourly', complete: true },
        { resolution: 'hourly', complete: false },
      ]),
    ).toBe(true)
  })
})
