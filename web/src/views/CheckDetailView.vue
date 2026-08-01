<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageIntro from '@/components/ui/PageIntro.vue'
import HistoricalChart from '@/components/hosts/HistoricalChart.vue'
import { useAuth } from '@/auth/useAuth'
import { useHostPolling } from '@/composables/useHostPolling'
import {
  changeCheckState,
  getCheck,
  getCheckMetrics,
  listCheckRuns,
  patchCheck,
  runCheck,
  type Check,
  type CheckInput,
  type CheckMetrics,
} from '@/checks/api'

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const id = String(route.params.id)
const busy = ref('')
const error = ref('')
const success = ref('')
const rangeHours = ref(24)
const editOpen = ref(false)
const requestId = ref('')
const draft = reactive<CheckInput>({ name: '', type: 'http' })

const detail = useHostPolling(async (signal) => {
  const result = await getCheck(id, signal)
  if (!result.data) throw new Error('check missing')
  return result.data
})

const runs = useHostPolling(async (signal) => {
  const result = await listCheckRuns(id, signal)
  if (!result.data) throw new Error('runs missing')
  return result.data.items
})

const metrics = useHostPolling<CheckMetrics>(async (signal) => {
  const to = new Date()
  const from = new Date(to.getTime() - rangeHours.value * 60 * 60 * 1000)
  const result = await getCheckMetrics(id, from, to, 'auto', signal)
  if (!result.data) throw new Error('metrics missing')
  return result.data
})

const check = computed(() => detail.data.value)
const availability = computed(() =>
  metrics.data.value?.series.find((series) => series.key === 'availability'),
)
const latency = computed(() =>
  metrics.data.value?.series.find((series) => series.key === 'latency_ms'),
)

function syncDraft(value: Check) {
  Object.assign(draft, {
    name: value.name,
    related_host_id: value.related_host_id,
    type: value.type,
    url: value.url,
    host: value.host,
    port: value.port,
    interval_seconds: value.interval_seconds,
    timeout_seconds: value.timeout_seconds,
    expected_status_min: value.expected_status_min,
    expected_status_max: value.expected_status_max,
    follow_redirects: value.follow_redirects,
    max_redirects: value.max_redirects,
  })
}

watch(
  check,
  (value) => {
    if (value && !editOpen.value) syncDraft(value)
  },
  { immediate: true },
)

async function save() {
  if (!check.value) return
  busy.value = 'save'
  error.value = ''
  success.value = ''
  try {
    await patchCheck(auth.mutate, check.value, { ...draft })
    editOpen.value = false
    success.value = 'Configuración guardada. El siguiente slot usará la nueva cadencia.'
    detail.restart()
    metrics.restart()
  } catch {
    error.value = 'No se pudo guardar. Revisa la versión y la política de destino.'
  } finally {
    busy.value = ''
  }
}

async function action(kind: 'enable' | 'disable' | 'archive' | 'restore') {
  if (!check.value) return
  busy.value = kind
  error.value = ''
  try {
    await changeCheckState(auth.mutate, check.value, kind)
    if (kind === 'archive') {
      await router.push({ name: 'checks' })
      return
    }
    detail.restart()
  } catch {
    error.value = 'No se pudo cambiar el estado administrativo del check.'
  } finally {
    busy.value = ''
  }
}

function newRequestId() {
  requestId.value =
    typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`
}

async function manualRun() {
  if (!requestId.value) newRequestId()
  busy.value = 'run'
  error.value = ''
  success.value = ''
  try {
    const result = await runCheck(auth.mutate, id, requestId.value)
    success.value = `Ejecución ${result.status}. Puedes seguirla en el historial manual.`
    runs.restart()
    newRequestId()
  } catch {
    error.value = 'No se pudo encolar la ejecución manual.'
  } finally {
    busy.value = ''
  }
}

function refreshMetrics() {
  metrics.restart(true)
}

function toggleEdit() {
  editOpen.value = !editOpen.value
  if (check.value) syncDraft(check.value)
}

onMounted(() => {
  newRequestId()
  detail.refresh()
  runs.refresh()
  metrics.refresh()
})
</script>

<template>
  <main id="main-content" class="check-detail route-view" tabindex="-1">
    <p v-if="detail.loading.value && !check" class="check-detail__loading" role="status">
      Consultando señal…
    </p>
    <p v-else-if="detail.error.value && !check" class="check-detail__loading" role="alert">
      No pudimos cargar este check.
    </p>
    <template v-else-if="check">
      <section class="check-detail__hero">
        <PageIntro
          index="04 / 01"
          :eyebrow="`${check.type.toUpperCase()} / ${check.observed_state}`"
          :title="check.name"
          :copy="check.url || `${check.host}:${check.port}`"
          compact
        />
        <aside class="check-detail__hero-actions">
          <span class="check-state" :class="`check-state--${check.observed_state}`"
            ><i aria-hidden="true"></i>{{ check.observed_state }}</span
          >
          <span>{{ check.stale ? 'Señal stale' : 'Señal fresca' }}</span>
          <button v-if="check.enabled" type="button" :disabled="!!busy" @click="action('disable')">
            Desactivar
          </button>
          <button
            v-else-if="!check.archived"
            type="button"
            :disabled="!!busy"
            @click="action('enable')"
          >
            Activar
          </button>
          <button v-if="check.archived" type="button" :disabled="!!busy" @click="action('restore')">
            Restaurar
          </button>
          <button v-else type="button" :disabled="!!busy" @click="action('archive')">
            Archivar
          </button>
        </aside>
      </section>

      <p v-if="error" class="check-detail__message check-detail__message--error" role="alert">
        {{ error }}
      </p>
      <p v-if="success" class="check-detail__message check-detail__message--success" role="status">
        {{ success }}
      </p>

      <section class="check-detail__workspace">
        <div class="check-detail__topline">
          <div>
            <p class="check-detail__eyebrow">01 / Estado derivado</p>
            <h2>La última pregunta</h2>
          </div>
          <button type="button" @click="toggleEdit">
            {{ editOpen ? 'Cerrar edición' : 'Editar configuración' }}
          </button>
        </div>
        <dl class="check-detail__facts">
          <div>
            <dt>Último intento</dt>
            <dd>
              {{
                check.last_attempt_at
                  ? new Date(check.last_attempt_at).toLocaleString('es-CL')
                  : '—'
              }}
            </dd>
          </div>
          <div>
            <dt>Último resultado</dt>
            <dd>
              {{
                check.last_result_at ? new Date(check.last_result_at).toLocaleString('es-CL') : '—'
              }}
            </dd>
          </div>
          <div>
            <dt>Latencia</dt>
            <dd>{{ check.last_latency_ms === null ? '—' : `${check.last_latency_ms} ms` }}</dd>
          </div>
          <div>
            <dt>Error</dt>
            <dd>{{ check.last_error_code || 'ninguno' }}</dd>
          </div>
          <div>
            <dt>Status HTTP</dt>
            <dd>{{ check.last_http_status || '—' }}</dd>
          </div>
          <div>
            <dt>Fallos consecutivos</dt>
            <dd>{{ check.consecutive_failures }}</dd>
          </div>
        </dl>

        <form v-if="editOpen" class="check-detail__edit" @submit.prevent="save">
          <label>Nombre<input v-model.trim="draft.name" maxlength="128" required /></label>
          <label
            >Intervalo (s)<input
              v-model.number="draft.interval_seconds"
              type="number"
              min="15"
              max="3600"
              required
          /></label>
          <label
            >Timeout (s)<input
              v-model.number="draft.timeout_seconds"
              type="number"
              min="1"
              max="60"
              required
          /></label>
          <label v-if="draft.type === 'http'" class="check-detail__edit-wide"
            >URL<input v-model.trim="draft.url" required
          /></label>
          <label v-else>Host<input v-model.trim="draft.host" required /></label>
          <label v-if="draft.type === 'tcp'"
            >Puerto<input v-model.number="draft.port" type="number" min="1" max="65535" required
          /></label>
          <template v-if="draft.type === 'http'">
            <label
              >Status mínimo<input
                v-model.number="draft.expected_status_min"
                type="number"
                min="100"
                max="599"
                required
            /></label>
            <label
              >Status máximo<input
                v-model.number="draft.expected_status_max"
                type="number"
                min="100"
                max="599"
                required
            /></label>
            <label class="check-detail__edit-check"
              ><input v-model="draft.follow_redirects" type="checkbox" /> Seguir redirects</label
            >
            <label
              >Máximo redirects<input
                v-model.number="draft.max_redirects"
                type="number"
                min="1"
                max="3"
                required
            /></label>
          </template>
          <button class="primary-button" type="submit" :disabled="busy === 'save'">
            {{ busy === 'save' ? 'Guardando…' : 'Guardar configuración' }}
          </button>
        </form>

        <section class="check-detail__history" aria-labelledby="history-title">
          <header class="check-detail__section-heading">
            <div>
              <p class="check-detail__eyebrow">02 / Histórico scheduled</p>
              <h2 id="history-title">La señal en el tiempo</h2>
            </div>
            <label
              >Rango<select v-model.number="rangeHours" @change="refreshMetrics">
                <option :value="6">6 horas</option>
                <option :value="24">24 horas</option>
                <option :value="168">7 días</option>
              </select></label
            >
          </header>
          <p v-if="metrics.error.value" class="check-detail__hint" role="alert">
            No hay histórico disponible para este rango.
          </p>
          <div v-else class="check-detail__charts">
            <HistoricalChart
              v-if="availability"
              title="Disponibilidad"
              unit="ratio"
              :points="availability.points"
            />
            <HistoricalChart
              v-if="latency"
              title="Latencia exitosa"
              unit="milliseconds"
              :points="latency.points"
            />
          </div>
        </section>

        <section class="check-detail__runs" aria-labelledby="runs-title">
          <header class="check-detail__section-heading">
            <div>
              <p class="check-detail__eyebrow">03 / Diagnóstico</p>
              <h2 id="runs-title">Ejecuciones manuales</h2>
            </div>
            <button
              class="primary-button"
              type="button"
              :disabled="!!busy || check.archived"
              @click="manualRun"
            >
              {{ busy === 'run' ? 'Encolando…' : 'Ejecutar ahora' }}
            </button>
          </header>
          <p class="check-detail__hint">
            Las ejecuciones manuales no cambian el estado derivado ni alimentan las series
            programadas.
          </p>
          <p v-if="runs.error.value" class="check-detail__hint" role="alert">
            No pudimos consultar las ejecuciones.
          </p>
          <p v-else-if="runs.data.value?.length === 0" class="check-detail__empty">
            Todavía no hay ejecuciones manuales.
          </p>
          <ul v-else class="run-list">
            <li v-for="run in runs.data.value" :key="run.run_id">
              <div>
                <strong>{{ run.state }}</strong
                ><span>{{ new Date(run.requested_at).toLocaleString('es-CL') }}</span>
              </div>
              <div>
                <span v-if="run.result"
                  >{{ run.result.status }} ·
                  {{ run.result.latency_ms === null ? '—' : `${run.result.latency_ms} ms` }}</span
                ><span v-else>{{ run.failure_code || 'En espera' }}</span>
              </div>
            </li>
          </ul>
        </section>
      </section>
    </template>
  </main>
</template>

<style scoped>
.check-detail {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.check-detail__loading {
  min-height: 30rem;
  display: grid;
  place-items: center;
  color: var(--ink-muted);
}
.check-detail__hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(12rem, 0.25fr);
  border-bottom: 1px solid var(--line-strong);
}
.check-detail__hero :deep(.page-intro) {
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
}
.check-detail__hero-actions {
  display: flex;
  flex-direction: column;
  justify-content: end;
  align-items: start;
  gap: 0.7rem;
  border-left: 1px solid var(--line-soft);
  padding: 2rem var(--page-gutter);
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.check-detail__hero-actions button {
  font: inherit;
}
.check-state {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
}
.check-state i {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
  background: var(--ink-muted);
}
.check-state--up i {
  background: var(--signal);
}
.check-state--down i {
  background: var(--accent-text);
}
.check-detail__message {
  margin: 1rem var(--page-gutter) 0;
  padding: 0.8rem 1rem;
  border: 1px solid;
}
.check-detail__message--error {
  color: var(--accent-text);
}
.check-detail__message--success {
  color: var(--signal);
}
.check-detail__workspace {
  padding: clamp(2rem, 6vw, 5rem) var(--page-gutter) clamp(4rem, 9vw, 8rem);
}
.check-detail__topline,
.check-detail__section-heading {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: start;
}
.check-detail__topline h2,
.check-detail__section-heading h2 {
  margin: 0 0 1.5rem;
  font: 500 clamp(2rem, 4vw, 3.7rem)/0.95 var(--font-display);
  letter-spacing: -0.05em;
}
.check-detail__eyebrow {
  margin: 0 0 0.65rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.check-detail__facts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1rem;
  margin: 0 0 3rem;
  border-block: 1px solid var(--line-soft);
  padding-block: 1.25rem;
}
.check-detail__facts dt {
  color: var(--ink-muted);
  font: 800 0.6rem var(--font-mono);
  text-transform: uppercase;
}
.check-detail__facts dd {
  margin: 0.4rem 0 0;
  overflow-wrap: anywhere;
}
.check-detail__edit {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 1rem;
  margin: 0 0 3rem;
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--accent-surface);
}
.check-detail__edit label {
  display: grid;
  gap: 0.4rem;
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.check-detail__edit input {
  min-height: 2.7rem;
  border: 1px solid var(--line-strong);
  background: var(--surface);
  padding: 0.55rem;
  font: 400 0.95rem var(--font-body);
}
.check-detail__edit-wide {
  grid-column: span 2;
}
.check-detail__edit-check {
  display: flex !important;
  align-items: center;
  padding-top: 1.65rem;
}
.check-detail__edit-check input {
  min-height: auto;
}
.check-detail__edit button {
  align-self: end;
}
.check-detail__history,
.check-detail__runs {
  margin-top: 3rem;
  border-top: 1px solid var(--line-strong);
  padding-top: 2rem;
}
.check-detail__section-heading > label {
  display: grid;
  gap: 0.35rem;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
.check-detail__section-heading select {
  min-height: 2.5rem;
  border: 1px solid var(--line-strong);
  background: var(--surface);
  padding: 0.45rem;
  font: inherit;
  color: var(--ink);
}
.check-detail__charts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.check-detail__hint,
.check-detail__empty {
  color: var(--ink-muted);
  line-height: 1.6;
}
.run-list {
  list-style: none;
  margin: 1rem 0 0;
  padding: 0;
  border-top: 1px solid var(--line-soft);
}
.run-list li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  border-bottom: 1px solid var(--line-soft);
  padding: 1rem 0;
}
.run-list li > div {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.run-list span {
  color: var(--ink-muted);
}
.run-list strong,
.run-list li > div:last-child {
  font: 800 0.68rem var(--font-mono);
  text-transform: uppercase;
}
@media (max-width: 60rem) {
  .check-detail__hero {
    grid-template-columns: 1fr;
  }
  .check-detail__hero-actions {
    border-top: 1px solid var(--line-soft);
    border-left: 0;
  }
  .check-detail__charts {
    grid-template-columns: 1fr;
  }
  .check-detail__edit {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 38rem) {
  .check-detail__facts {
    grid-template-columns: 1fr 1fr;
  }
  .check-detail__topline,
  .check-detail__section-heading {
    flex-direction: column;
  }
  .check-detail__edit {
    grid-template-columns: 1fr;
  }
  .check-detail__edit-wide {
    grid-column: auto;
  }
  .run-list li {
    flex-direction: column;
  }
}
</style>
