<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { JSONAPIError } from '@/api/client'
import HostCurrentMetrics from '@/components/hosts/HostCurrentMetrics.vue'
import FilesystemList from '@/components/hosts/FilesystemList.vue'
import HistoricalChart from '@/components/hosts/HistoricalChart.vue'
import { useHostPolling } from '@/composables/useHostPolling'
import {
  getHost,
  getHostMetrics,
  listAllFilesystems,
  type Filesystem,
  type HostDetail,
  type MetricSeries,
} from '@/hosts/api'
import { formatDate } from '@/hosts/format'
import { authKey } from '@/auth/useAuth'
import { selectFilesystem } from '@/hosts/management'

interface DetailState {
  host: HostDetail
  filesystems: Filesystem[]
}
const route = useRoute()
const auth = inject(authKey, null)
const hostId = computed(() => String(route.params.id))
let etag: string | undefined
let currentHost: HostDetail | null = null
const filesystemBusy = ref('')
const managementError = ref('')

const polling = useHostPolling<DetailState>(async (signal) => {
  const detail = await getHost(hostId.value, signal, etag)
  if (detail.etag) etag = detail.etag
  if (detail.data) currentHost = detail.data
  if (!currentHost) throw new Error('host detail missing')
  const filesystems = await listAllFilesystems(hostId.value, signal)
  return { host: currentHost, filesystems }
})

const ranges = [
  { key: '24h', label: '24 h', duration: 24 * 60 * 60 * 1000 },
  { key: '7d', label: '7 días', duration: 7 * 24 * 60 * 60 * 1000 },
  { key: '30d', label: '30 días', duration: 30 * 24 * 60 * 60 * 1000 },
  { key: '90d', label: '90 días', duration: 90 * 24 * 60 * 60 * 1000 },
] as const
type RangeKey = (typeof ranges)[number]['key']
const selectedRange = ref<RangeKey>('24h')
const history = useHostPolling(async (signal) => {
  const range = ranges.find((item) => item.key === selectedRange.value) ?? ranges[0]
  const to = new Date()
  const from = new Date(to.getTime() - range.duration)
  try {
    const result = await getHostMetrics(hostId.value, from, to, 'auto', signal)
    if (!result.data) throw new Error('host metrics missing')
    return result.data
  } catch (error: unknown) {
    if (!(error instanceof JSONAPIError) || error.code !== 'point_limit_exceeded') throw error
    const hourly = await getHostMetrics(hostId.value, from, to, 'hourly', signal)
    if (!hourly.data) throw new Error('host metrics missing')
    return hourly.data
  }
})

const historySeries = computed(() => history.data.value?.series ?? [])
const historyGapThreshold = computed(() => {
  const interval = polling.data.value?.host.interval_seconds ?? 60
  return interval * 2 + 10
})

function seriesTitle(series: Pick<MetricSeries, 'key' | 'mount_point'>): string {
  const prefix = series.mount_point ? `${series.mount_point} · ` : ''
  const labels: Record<string, string> = {
    cpu_used_percent: 'CPU usada',
    memory_used_bytes: 'Memoria usada',
    memory_total_bytes: 'Memoria total',
    filesystem_used_bytes: 'Espacio usado',
    filesystem_total_bytes: 'Espacio total',
  }
  return prefix + (labels[series.key] ?? series.key)
}

watch(hostId, () => {
  etag = undefined
  currentHost = null
  polling.restart(true)
  history.restart(true)
})

watch(selectedRange, () => history.restart(true))

async function updateFilesystem(filesystem: Filesystem, selected: boolean) {
  const state = polling.data.value
  if (!auth || !state) return
  filesystemBusy.value = filesystem.id
  managementError.value = ''
  try {
    await selectFilesystem(auth.mutate, state.host, filesystem, selected)
    polling.restart()
  } catch {
    managementError.value = 'No se pudo actualizar la selección de almacenamiento.'
  } finally {
    filesystemBusy.value = ''
  }
}
</script>

<template>
  <main id="main-content" class="host-detail route-view" tabindex="-1">
    <div v-if="polling.loading.value" class="host-detail__status" role="status">
      Consultando host…
    </div>
    <div
      v-else-if="polling.error.value && !polling.data.value"
      class="host-detail__status"
      role="alert"
    >
      <p>No pudimos abrir este host.</p>
      <button type="button" @click="polling.refresh">Reintentar</button>
    </div>
    <template v-else-if="polling.data.value">
      <header class="host-detail__hero">
        <RouterLink class="host-detail__back" :to="{ name: 'home' }"
          >← Volver al resumen</RouterLink
        >
        <p class="host-detail__eyebrow">Host / estado actual</p>
        <h1>{{ polling.data.value.host.display_name }}</h1>
        <div class="host-detail__state">
          <strong>{{ polling.data.value.host.observed_state }}</strong>
          <span v-if="polling.data.value.host.stale">Señal vencida</span>
          <span v-if="polling.data.value.host.observed_state === 'unreachable'"
            >Agente sin alcance</span
          >
          <span class="host-detail__received"
            >Última recepción ·
            {{ formatDate(polling.data.value.host.last_success_received_at) }}</span
          >
        </div>
        <ul
          v-if="polling.data.value.host.warnings.length"
          class="host-detail__warnings"
          aria-label="Advertencias activas"
        >
          <li v-for="warning in polling.data.value.host.warnings" :key="warning">{{ warning }}</li>
        </ul>
        <p v-if="polling.error.value" class="host-detail__notice" role="alert">
          La actualización falló; se conserva la lectura anterior.
        </p>
        <p v-if="managementError" class="host-detail__notice" role="alert">{{ managementError }}</p>
      </header>
      <HostCurrentMetrics :host="polling.data.value.host" />
      <section class="host-history" aria-labelledby="history-title">
        <header class="host-history__header">
          <div>
            <p>Lectura histórica</p>
            <h2 id="history-title">La señal a través del tiempo.</h2>
          </div>
          <div class="host-history__ranges" role="group" aria-label="Rango histórico">
            <button
              v-for="range in ranges"
              :key="range.key"
              type="button"
              :aria-pressed="selectedRange === range.key"
              @click="selectedRange = range.key"
            >
              {{ range.label }}
            </button>
          </div>
        </header>
        <p v-if="history.loading.value" class="host-history__status" role="status">
          Consultando históricos…
        </p>
        <p
          v-else-if="history.error.value && !history.data.value"
          class="host-history__status"
          role="alert"
        >
          No pudimos leer las series históricas.
          <button type="button" @click="history.refresh">Reintentar</button>
        </p>
        <template v-else-if="history.data.value">
          <p v-if="history.error.value" class="host-history__notice" role="alert">
            La actualización histórica falló; se conserva la consulta anterior.
          </p>
          <p class="host-history__resolution">
            Resolución recibida: <strong>{{ history.data.value.resolution }}</strong
            >. Los datos ausentes no se rellenan.
          </p>
          <div class="host-history__grid">
            <HistoricalChart
              v-for="series in historySeries"
              :key="`${series.key}-${series.filesystem_id ?? 'host'}`"
              :title="seriesTitle(series)"
              :unit="series.unit"
              :points="series.points"
              :gap-threshold-seconds="historyGapThreshold"
            />
          </div>
        </template>
      </section>
      <FilesystemList
        :filesystems="polling.data.value.filesystems"
        :loading="false"
        :error="false"
        :editable="auth !== null"
        :busy-id="filesystemBusy"
        @select="updateFilesystem"
      />
    </template>
  </main>
</template>

<style scoped>
.host-detail {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.host-detail__hero {
  min-height: 32rem;
  padding: clamp(3rem, 8vw, 7rem) var(--page-gutter);
}
.host-detail__back {
  color: var(--ink);
  font: 800 0.68rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.host-detail__eyebrow {
  margin: 5rem 0 1rem;
  color: var(--accent-text);
  font: 800 0.7rem var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.host-detail__hero h1 {
  max-width: 14ch;
  margin: 0;
  font: 500 clamp(3.5rem, 9vw, 9rem)/0.82 var(--font-display);
  letter-spacing: -0.06em;
  overflow-wrap: anywhere;
}
.host-detail__state {
  display: flex;
  flex-wrap: wrap;
  gap: 0.6rem 1.5rem;
  margin-top: 3rem;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.host-detail__state strong {
  color: var(--accent-text);
}
.host-detail__received {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-detail__warnings {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0;
  margin: 2rem 0 0;
  list-style: none;
}
.host-detail__warnings li {
  border: 1px solid var(--accent-text);
  padding: 0.4rem 0.55rem;
  color: var(--accent-text);
  font: 800 0.6rem var(--font-mono);
}
.host-detail__notice {
  margin-top: 1.5rem;
  border-left: 0.3rem solid var(--accent-surface);
  padding: 0.8rem;
  background: var(--surface);
}
.host-detail__status {
  min-height: 55svh;
  display: grid;
  place-content: center;
  gap: 1rem;
  padding: 2rem;
  text-align: center;
}
.host-detail__status p {
  font: 500 2.5rem var(--font-display);
}
.host-detail__status button {
  justify-self: center;
  border: 1px solid var(--ink);
  padding: 0.7rem 1rem;
  background: var(--ink);
  color: var(--paper);
  cursor: pointer;
}
.host-history {
  border-block: 1px solid var(--line-strong);
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
}
.host-history__header {
  display: flex;
  justify-content: space-between;
  gap: 2rem;
  align-items: end;
  margin-bottom: 2rem;
}
.host-history__header p,
.host-history__resolution,
.host-history__notice {
  margin: 0;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.host-history__header h2 {
  margin: 0.75rem 0 0;
  font: 500 clamp(2.5rem, 5vw, 5rem) / 0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.host-history__ranges {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}
.host-history__ranges button {
  min-height: 2.6rem;
  border: 1px solid var(--line-strong);
  padding: 0.5rem 0.75rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  cursor: pointer;
}
.host-history__ranges button[aria-pressed='true'] {
  border-color: var(--ink);
  background: var(--ink);
  color: var(--paper);
}
.host-history__status {
  min-height: 12rem;
  display: grid;
  place-items: center;
  border: 1px solid var(--line-strong);
  padding: 1rem;
  text-align: center;
}
.host-history__status button {
  border: 0;
  border-bottom: 1px solid currentColor;
  background: none;
  color: inherit;
  font: inherit;
  cursor: pointer;
}
.host-history__notice {
  margin-bottom: 1rem;
  border-left: 0.3rem solid var(--accent-surface);
  padding: 0.8rem 1rem;
  color: var(--ink);
  background: var(--surface);
}
.host-history__resolution {
  margin-bottom: 1rem;
  color: var(--ink-muted);
  font-weight: 400;
  text-transform: none;
}
.host-history__resolution strong {
  color: var(--accent-text);
}
.host-history__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
@media (max-width: 55rem) {
  .host-history__header {
    align-items: start;
    flex-direction: column;
  }
  .host-history__grid {
    grid-template-columns: 1fr;
  }
}
</style>
