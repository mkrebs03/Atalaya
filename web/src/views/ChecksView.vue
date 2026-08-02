<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { useAuth } from '@/auth/useAuth'
import { useHostPolling } from '@/composables/useHostPolling'
import {
  changeCheckState,
  createCheck,
  listChecks,
  type Check,
  type CheckInput,
  type CheckType,
} from '@/checks/api'
import { checkStateLabel, checkStateTone, formatDateTime } from '@/ui/labels'

const auth = useAuth()
const showCreate = ref(false)
const showArchived = ref(false)
const archiveTarget = ref<Check | null>(null)
const busy = ref('')
const error = ref('')
const form = reactive<CheckInput>({
  name: '',
  type: 'http',
  url: 'https://service.tailnet.ts.net/health',
  interval_seconds: 60,
  timeout_seconds: 10,
  expected_status_min: 200,
  expected_status_max: 299,
  follow_redirects: false,
  max_redirects: 3,
})

const polling = useHostPolling(async (signal) => {
  const result = await listChecks(signal, true)
  if (!result.data) throw new Error('checks missing')
  return result.data.items
})

const checks = computed(() => polling.data.value ?? [])
const visibleChecks = computed(() =>
  checks.value.filter((check) => (showArchived.value ? check.archived : !check.archived)),
)

function setType(type: CheckType) {
  form.type = type
  if (type === 'http') {
    form.url ||= 'https://service.tailnet.ts.net/health'
    delete form.host
    delete form.port
  } else {
    form.host ||= 'service.tailnet.ts.net'
    form.port ||= 5432
    delete form.url
    delete form.expected_status_min
    delete form.expected_status_max
    delete form.follow_redirects
    delete form.max_redirects
  }
}

function resetForm() {
  Object.assign(form, {
    name: '',
    type: 'http',
    url: 'https://service.tailnet.ts.net/health',
    host: undefined,
    port: undefined,
    interval_seconds: 60,
    timeout_seconds: 10,
    expected_status_min: 200,
    expected_status_max: 299,
    follow_redirects: false,
    max_redirects: 3,
  })
  showCreate.value = false
}

async function save() {
  busy.value = 'create'
  error.value = ''
  try {
    await createCheck(auth.mutate, { ...form })
    resetForm()
    polling.restart()
  } catch {
    error.value =
      'No se pudo crear el servicio. Revisa el destino, el tiempo de espera y la política de red.'
  } finally {
    busy.value = ''
  }
}

async function updateState(check: Check, action: 'enable' | 'disable' | 'archive' | 'restore') {
  busy.value = check.id
  error.value = ''
  try {
    await changeCheckState(auth.mutate, check, action)
    polling.restart()
  } catch {
    error.value = 'No se pudo actualizar el servicio; la configuración pudo cambiar en otra sesión.'
  } finally {
    busy.value = ''
  }
}

function requestStateChange(check: Check, action: 'enable' | 'disable' | 'archive' | 'restore') {
  if (action === 'archive') {
    archiveTarget.value = check
    return
  }
  void updateState(check, action)
}

function cancelArchive() {
  archiveTarget.value = null
}

async function confirmArchive() {
  const check = archiveTarget.value
  archiveTarget.value = null
  if (check) await updateState(check, 'archive')
}

onMounted(() => polling.refresh())
</script>

<template>
  <main id="main-content" class="checks route-view" tabindex="-1">
    <section class="checks__hero">
      <PageIntro
        eyebrow="Servicios"
        title="Servicios monitorizados."
        copy="Comprueba disponibilidad HTTP, HTTPS y TCP desde este servidor. Los destinos sólo pueden pertenecer a tu tailnet o a redes privadas autorizadas."
      />
      <aside class="checks__hero-rail">
        <span>Servicios</span
        ><strong>{{ checks.filter((check) => !check.archived).length }} activos</strong>
        <span>Actualización</span><strong>Automática</strong>
      </aside>
    </section>

    <p v-if="error" class="checks__error" role="alert">{{ error }}</p>

    <section class="checks__workspace" aria-labelledby="checks-title">
      <header class="checks__heading">
        <div>
          <p class="checks__eyebrow">Servicios configurados</p>
          <h2 id="checks-title">Disponibilidad de tus servicios.</h2>
        </div>
        <div class="checks__heading-actions">
          <div class="checks__filters" role="group" aria-label="Filtro de servicios">
            <button type="button" :aria-pressed="!showArchived" @click="showArchived = false">
              Activos
            </button>
            <button type="button" :aria-pressed="showArchived" @click="showArchived = true">
              Archivados
            </button>
          </div>
          <button class="primary-button" type="button" @click="showCreate = !showCreate">
            {{ showCreate ? 'Cerrar formulario' : 'Agregar servicio' }}
          </button>
        </div>
      </header>

      <form v-if="showCreate" class="check-form" @submit.prevent="save">
        <label
          >Nombre del servicio<input v-model.trim="form.name" maxlength="128" required
        /></label>
        <label>
          Tipo de comprobación
          <select v-model="form.type" @change="setType(form.type)">
            <option value="http">HTTP / HTTPS</option>
            <option value="tcp">TCP</option>
          </select>
        </label>
        <label v-if="form.type === 'http'" class="check-form__wide">
          URL del servicio<input v-model.trim="form.url" type="url" required />
        </label>
        <label v-else>Host del servicio<input v-model.trim="form.host" required /></label>
        <label v-if="form.type === 'tcp'">
          Puerto TCP<input v-model.number="form.port" type="number" min="1" max="65535" required />
        </label>
        <label>
          Intervalo de consulta (segundos)
          <input
            v-model.number="form.interval_seconds"
            type="number"
            min="15"
            max="3600"
            required
          />
        </label>
        <label>
          Tiempo de espera (segundos)
          <input v-model.number="form.timeout_seconds" type="number" min="1" max="60" required />
        </label>
        <template v-if="form.type === 'http'">
          <label>
            Código HTTP mínimo
            <input
              v-model.number="form.expected_status_min"
              type="number"
              min="100"
              max="599"
              required
            />
          </label>
          <label>
            Código HTTP máximo
            <input
              v-model.number="form.expected_status_max"
              type="number"
              min="100"
              max="599"
              required
            />
          </label>
          <label class="check-form__check">
            <input v-model="form.follow_redirects" type="checkbox" /> Seguir redirecciones
          </label>
          <label>
            Máximo de redirecciones
            <input v-model.number="form.max_redirects" type="number" min="1" max="3" required />
          </label>
        </template>
        <button
          class="primary-button check-form__submit"
          type="submit"
          :disabled="busy === 'create'"
        >
          {{ busy === 'create' ? 'Creando…' : 'Agregar servicio' }}
        </button>
      </form>

      <p v-if="polling.loading.value" class="checks__empty" role="status">Cargando servicios…</p>
      <p v-else-if="polling.error.value" class="checks__empty" role="alert">
        No pudimos consultar los servicios.
        <button type="button" @click="polling.refresh">Reintentar</button>
      </p>
      <p v-else-if="visibleChecks.length === 0" class="checks__empty">
        {{ showArchived ? 'No hay servicios archivados.' : 'Todavía no hay servicios activos.' }}
      </p>
      <div v-else class="check-list">
        <article
          v-for="check in visibleChecks"
          :key="check.id"
          class="check-card"
          :class="{ 'check-card--archived': check.archived }"
        >
          <header class="check-card__header">
            <div>
              <p class="checks__eyebrow">{{ check.type === 'http' ? 'HTTP / HTTPS' : 'TCP' }}</p>
              <h3>
                <RouterLink :to="{ name: 'check-detail', params: { id: check.id } }">
                  {{ check.name }}
                </RouterLink>
              </h3>
              <code>{{ check.url || check.host + ':' + check.port }}</code>
            </div>
            <span
              class="check-state"
              :data-tone="checkStateTone(check.observed_state, check.stale)"
            >
              <i aria-hidden="true"></i>{{ checkStateLabel(check.observed_state, check.stale) }}
            </span>
          </header>
          <dl class="check-card__facts">
            <div>
              <dt>Cadencia</dt>
              <dd>Cada {{ check.interval_seconds }} s · espera {{ check.timeout_seconds }} s</dd>
            </div>
            <div>
              <dt>Último resultado</dt>
              <dd>
                {{ check.last_result_at ? formatDateTime(check.last_result_at) : 'Sin resultados' }}
              </dd>
            </div>
            <div>
              <dt>Latencia</dt>
              <dd>
                {{ check.last_latency_ms === null ? 'Sin dato' : check.last_latency_ms + ' ms' }}
              </dd>
            </div>
            <div>
              <dt>Fallos consecutivos</dt>
              <dd>{{ check.consecutive_failures }}</dd>
            </div>
          </dl>
          <footer class="check-card__actions">
            <RouterLink :to="{ name: 'check-detail', params: { id: check.id } }"
              >Ver detalle</RouterLink
            >
            <button
              v-if="check.enabled"
              type="button"
              :disabled="busy === check.id"
              @click="requestStateChange(check, 'disable')"
            >
              Pausar
            </button>
            <button
              v-else-if="!check.archived"
              type="button"
              :disabled="busy === check.id"
              @click="requestStateChange(check, 'enable')"
            >
              Reanudar
            </button>
            <button
              v-if="check.archived"
              type="button"
              :disabled="busy === check.id"
              @click="requestStateChange(check, 'restore')"
            >
              Restaurar
            </button>
            <button
              v-else
              type="button"
              :disabled="busy === check.id"
              @click="requestStateChange(check, 'archive')"
            >
              Archivar
            </button>
          </footer>
        </article>
      </div>
    </section>

    <ConfirmDialog
      :open="archiveTarget !== null"
      title="Archivar servicio"
      :copy="
        archiveTarget
          ? 'El histórico se conservará, pero el servicio dejará de ejecutarse y de generar alertas.'
          : ''
      "
      confirm-label="Archivar servicio"
      danger
      @cancel="cancelArchive"
      @confirm="confirmArchive"
    />
  </main>
</template>

<style scoped>
.checks {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.checks__hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(8rem, 0.16fr);
  border-bottom: 1px solid var(--line-strong);
}
.checks__hero :deep(.page-intro) {
  padding: clamp(3.5rem, 7vw, 6rem) var(--page-gutter);
}
.checks__hero-rail {
  display: flex;
  flex-direction: column;
  justify-content: end;
  gap: 0.3rem;
  border-left: 1px solid var(--line-soft);
  padding: 2rem 1.2rem;
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  writing-mode: vertical-rl;
  transform: rotate(180deg);
}
.checks__hero-rail span {
  color: var(--ink-muted);
}
.checks__error {
  margin: 1rem var(--page-gutter) 0;
  padding: 0.8rem 1rem;
  border: 1px solid var(--accent-text);
  color: var(--accent-text);
}
.checks__workspace {
  padding: clamp(2rem, 5vw, 4rem) var(--page-gutter) clamp(4rem, 8vw, 7rem);
}
.checks__heading,
.check-card__header,
.check-card__actions {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: start;
}
.checks__heading-actions {
  display: flex;
  align-items: end;
  flex-wrap: wrap;
  justify-content: end;
  gap: 0.75rem;
}
.checks__filters {
  display: flex;
  gap: 0.35rem;
}
.checks__filters button {
  min-height: 2.75rem;
  border: 1px solid var(--line-strong);
  padding: 0.6rem 0.75rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.64rem var(--font-mono);
  text-transform: uppercase;
  cursor: pointer;
}
.checks__filters button[aria-pressed='true'] {
  background: var(--ink);
  color: var(--paper);
}
.checks__eyebrow {
  margin: 0 0 0.65rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.checks__heading h2 {
  margin: 0 0 2rem;
  font: 500 clamp(2.5rem, 5vw, 4.5rem)/0.95 var(--font-display);
  letter-spacing: -0.05em;
}
.check-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr));
  gap: 1rem;
}
.check-card {
  border: 1px solid var(--line-strong);
  background: var(--surface);
  padding: 1.25rem;
}
.check-card--archived {
  opacity: 0.72;
}
.check-card h3 {
  margin: 0 0 0.45rem;
  font-size: 1.4rem;
}
.check-card h3 a {
  color: inherit;
  text-decoration-thickness: 0.08em;
  text-underline-offset: 0.15em;
}
.check-card code {
  color: var(--ink-muted);
  overflow-wrap: anywhere;
}
.check-state {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.check-state i {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
  background: var(--ink-muted);
}
.check-state[data-tone='success'] i {
  background: var(--signal);
}
.check-state[data-tone='danger'] i {
  background: var(--accent-text);
}
.check-state[data-tone='warning'] i {
  background: #8a5a00;
}
.check-card__facts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.9rem 1rem;
  margin: 1.5rem 0;
  border-block: 1px solid var(--line-soft);
  padding-block: 1rem;
}
.check-card__facts div {
  min-width: 0;
}
.check-card__facts dt {
  color: var(--ink-muted);
  font: 800 0.6rem var(--font-mono);
  text-transform: uppercase;
}
.check-card__facts dd {
  margin: 0.35rem 0 0;
  overflow-wrap: anywhere;
}
.check-card__actions {
  align-items: center;
}
.check-card__actions a,
.check-card__actions button {
  color: var(--ink);
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.check-card__actions a {
  color: var(--accent-text);
}
.check-form {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 1rem;
  margin-bottom: 2rem;
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--accent-surface);
}
.check-form label {
  display: grid;
  gap: 0.4rem;
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.check-form input,
.check-form select {
  min-width: 0;
  min-height: 2.75rem;
  border: 1px solid var(--line-strong);
  background: var(--surface);
  padding: 0.6rem;
  font: 400 0.95rem var(--font-body);
}
.check-form__wide {
  grid-column: span 2;
}
.check-form__check {
  display: flex !important;
  align-items: center;
  gap: 0.55rem !important;
  padding-top: 1.7rem;
}
.check-form__check input {
  min-height: auto;
}
.check-form__submit {
  align-self: end;
}
.checks__empty {
  border: 1px dashed var(--line-strong);
  padding: 3rem 1rem;
  text-align: center;
  color: var(--ink-muted);
}
@media (max-width: 60rem) {
  .checks__hero {
    grid-template-columns: 1fr;
  }
  .checks__hero-rail {
    display: none;
  }
  .check-form {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 38rem) {
  .check-form {
    grid-template-columns: 1fr;
  }
  .check-form__wide {
    grid-column: auto;
  }
  .check-card__header,
  .check-card__actions {
    flex-direction: column;
  }
  .check-card__facts {
    grid-template-columns: 1fr;
  }
  .checks__heading-actions {
    width: 100%;
    justify-content: start;
    align-items: stretch;
    flex-direction: column;
  }
  .checks__filters {
    width: 100%;
  }
  .checks__filters button,
  .checks__heading-actions .primary-button {
    flex: 1;
  }
}
</style>
