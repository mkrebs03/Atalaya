<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import 'uplot/dist/uPlot.min.css'
import type { MetricPoint } from '@/hosts/api'
import type uPlot from 'uplot'
import { hasIncompleteHourlyRollups, hasMetricGaps, metricGapThresholdSeconds } from './historical'

const props = withDefaults(
  defineProps<{
    title: string
    unit: 'percent' | 'bytes' | 'ratio' | 'milliseconds'
    points: readonly MetricPoint[]
    gapThresholdSeconds?: number
  }>(),
  { gapThresholdSeconds: 300 },
)

const chartElement = ref<HTMLElement | null>(null)
let chart: uPlot | undefined
let ChartConstructor: typeof uPlot | undefined
let observer: ResizeObserver | undefined

const hasGaps = computed(() => {
  return hasMetricGaps(props.points, props.gapThresholdSeconds)
})
const hasIncomplete = computed(() => hasIncompleteHourlyRollups(props.points))

function formatValue(value: number): string {
  if (props.unit === 'percent')
    return `${new Intl.NumberFormat('es-CL', { maximumFractionDigits: 1 }).format(value)} %`
  if (props.unit === 'ratio')
    return `${new Intl.NumberFormat('es-CL', { style: 'percent', maximumFractionDigits: 1 }).format(value)}`
  if (props.unit === 'milliseconds')
    return `${new Intl.NumberFormat('es-CL', { maximumFractionDigits: 0 }).format(value)} ms`
  if (value === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${new Intl.NumberFormat('es-CL', { maximumFractionDigits: 1 }).format(value / 1024 ** index)} ${units[index]}`
}

function buildData(): uPlot.AlignedData {
  const timestamps: number[] = []
  const values: (number | null)[] = []
  props.points.forEach((point, index) => {
    const timestamp = Date.parse(point.timestamp) / 1000
    if (index > 0) {
      const previousPoint = props.points[index - 1]!
      const previous = Date.parse(previousPoint.timestamp) / 1000
      const threshold = metricGapThresholdSeconds(previousPoint, point, props.gapThresholdSeconds)
      if (timestamp - previous > threshold) {
        timestamps.push(previous + Math.min(threshold, 1))
        values.push(null)
      }
    }
    timestamps.push(timestamp)
    values.push(point.value)
  })
  return [timestamps, values]
}

function renderChart() {
  chart?.destroy()
  chart = undefined
  if (!ChartConstructor || !chartElement.value || props.points.length === 0) return
  const width = Math.max(chartElement.value.clientWidth, 280)
  chart = new ChartConstructor(
    {
      width,
      height: 250,
      cursor: { drag: { x: false, y: false } },
      scales: { x: { time: true } },
      axes: [
        { stroke: '#6d756e', grid: { stroke: '#d8d4c7', width: 1 } },
        {
          stroke: '#6d756e',
          grid: { stroke: '#d8d4c7', width: 1 },
          values: (_u, values) => values.map((value) => formatValue(value)),
        },
      ],
      series: [
        {},
        {
          label: props.title,
          stroke: '#d04a24',
          fill: 'rgba(208, 74, 36, 0.12)',
          width: 2,
          spanGaps: false,
          points: { show: true, size: 5 },
        },
      ],
    },
    buildData(),
    chartElement.value,
  )
}

function resizeChart() {
  if (chart && chartElement.value)
    chart.setSize({ width: Math.max(chartElement.value.clientWidth, 280), height: 250 })
}

watch(
  () => props.points,
  () => void nextTick(renderChart),
  { deep: true },
)
onMounted(() => {
  void import('uplot').then((module) => {
    ChartConstructor = module.default
    return nextTick(renderChart)
  })
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(resizeChart)
    if (chartElement.value) observer.observe(chartElement.value)
  }
})
onUnmounted(() => {
  observer?.disconnect()
  chart?.destroy()
})
</script>

<template>
  <article class="historical-chart">
    <header class="historical-chart__header">
      <div>
        <p>Histórico</p>
        <h3>{{ title }}</h3>
      </div>
      <div class="historical-chart__labels">
        <span v-if="hasGaps" class="historical-chart__gap-label">Periodos sin datos</span>
        <span v-if="hasIncomplete" class="historical-chart__partial-label">Cobertura parcial</span>
      </div>
    </header>
    <p v-if="points.length === 0" class="historical-chart__empty">Sin muestras en este rango.</p>
    <div
      ref="chartElement"
      class="historical-chart__plot"
      :class="{ 'historical-chart__plot--empty': points.length === 0 }"
      :aria-label="title"
      role="img"
      v-show="points.length > 0"
    />
    <p v-if="hasGaps" class="historical-chart__hint">
      Los periodos sin recepción se mantienen como huecos.
    </p>
    <p v-if="hasIncomplete" class="historical-chart__hint">
      Algunas mediciones cubren sólo parte del periodo y se muestran sin ocultarlas.
    </p>
  </article>
</template>

<style scoped>
.historical-chart {
  min-width: 0;
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--surface);
}
.historical-chart__header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: start;
  margin-bottom: 1rem;
}
.historical-chart__header p,
.historical-chart__gap-label,
.historical-chart__partial-label,
.historical-chart__hint {
  margin: 0;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.09em;
  text-transform: uppercase;
}
.historical-chart__header h3 {
  margin: 0.5rem 0 0;
  font-size: 1.15rem;
}
.historical-chart__gap-label {
  color: var(--accent-text);
  white-space: nowrap;
}
.historical-chart__partial-label {
  color: var(--warning-text, var(--accent-text));
  white-space: nowrap;
}
.historical-chart__labels {
  display: flex;
  flex-wrap: wrap;
  justify-content: end;
  gap: 0.5rem;
}
.historical-chart__plot {
  min-height: 250px;
  overflow: hidden;
}
.historical-chart__plot--empty {
  display: none;
}
.historical-chart__empty {
  min-height: 250px;
  display: grid;
  place-items: center;
  margin: 0;
  color: var(--ink-muted);
}
.historical-chart__hint {
  margin-top: 0.75rem;
  font-weight: 400;
  text-transform: none;
}
</style>
