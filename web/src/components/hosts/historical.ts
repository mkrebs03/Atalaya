import type { MetricPoint } from '@/hosts/api'

const hourlyPointDurationSeconds = 60 * 60
const hourlyGapToleranceSeconds = 60

type HistoricalPoint = Pick<MetricPoint, 'timestamp' | 'resolution'>

export function metricGapThresholdSeconds(
  previous: HistoricalPoint,
  current: HistoricalPoint,
  rawThresholdSeconds: number,
): number {
  if (previous.resolution === 'hourly' && current.resolution === 'hourly') {
    return hourlyPointDurationSeconds + Math.min(rawThresholdSeconds, hourlyGapToleranceSeconds)
  }
  if (previous.resolution === 'hourly' || current.resolution === 'hourly') {
    return hourlyPointDurationSeconds + rawThresholdSeconds
  }
  return rawThresholdSeconds
}

export function hasIncompleteHourlyRollups(
  points: readonly Pick<MetricPoint, 'resolution' | 'complete'>[],
): boolean {
  return points.some((point) => point.resolution === 'hourly' && point.complete === false)
}

export function hasMetricGaps(
  points: readonly HistoricalPoint[],
  rawThresholdSeconds: number,
): boolean {
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1]!
    const current = points[index]!
    const elapsedSeconds = (Date.parse(current.timestamp) - Date.parse(previous.timestamp)) / 1000
    if (elapsedSeconds > metricGapThresholdSeconds(previous, current, rawThresholdSeconds))
      return true
  }
  return false
}
