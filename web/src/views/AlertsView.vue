<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import { useAuth } from '@/auth/useAuth'
import { useHostPolling } from '@/composables/useHostPolling'
import { listAllChecks } from '@/checks/api'
import { listAllFilesystems, listAllHosts } from '@/hosts/api'
import { listAllNotificationChannels } from '@/notifications/api'
import {
  changeAlertRuleState,
  createAlertRule,
  listAlertRules,
  listIncidents,
  patchAlertRule,
  type AlertRule,
  type AlertRuleInput,
  type AlertRuleType,
} from '@/alerts/api'

const auth = useAuth()
type ReadonlyAlertRule = {
  readonly [Key in keyof AlertRule]: Key extends 'channel_ids' ? readonly string[] : AlertRule[Key]
}
const showForm = ref(false)
const editingRule = ref<ReadonlyAlertRule | null>(null)
const busy = ref('')
const error = ref('')
const form = reactive({
  name: '',
  type: 'host_unreachable' as AlertRuleType,
  severity: 'warning' as 'warning' | 'critical',
  targetID: '',
  channelIDs: [] as string[],
  mode: 'duration' as 'duration' | 'attempts',
  forSeconds: 180,
  consecutiveFailures: 3,
  metric: 'cpu_used_percent' as 'cpu_used_percent' | 'memory_used_percent',
  operator: 'gte' as 'gt' | 'gte',
  threshold: 90,
})

const resourcesPolling = useHostPolling(async (signal) => {
  const [hosts, checks, channels] = await Promise.all([
    listAllHosts(signal),
    listAllChecks(signal),
    listAllNotificationChannels(signal),
  ])
  const filesystemGroups = await Promise.all(
    hosts.map(async (host) => ({
      host,
      filesystems: await listAllFilesystems(host.id, signal),
    })),
  )
  return {
    hosts,
    checks,
    channels,
    filesystems: filesystemGroups.flatMap(({ host, filesystems }) =>
      filesystems.map((filesystem) => ({ filesystem, hostName: host.display_name })),
    ),
  }
})

const polling = useHostPolling(async (signal) => {
  const [rulesResponse, incidentsResponse] = await Promise.all([
    listAlertRules(signal, true),
    listIncidents(signal, 'open'),
  ])
  if (!rulesResponse.data || !incidentsResponse.data) throw new Error('alert response missing')
  return { rules: rulesResponse.data.items ?? [], incidents: incidentsResponse.data.items ?? [] }
})

const rules = computed(() => polling.data.value?.rules ?? [])
const incidents = computed(() => polling.data.value?.incidents ?? [])
const hosts = computed(() => resourcesPolling.data.value?.hosts ?? [])
const checks = computed(() => resourcesPolling.data.value?.checks ?? [])
const channels = computed(() => resourcesPolling.data.value?.channels ?? [])
const filesystems = computed(() => resourcesPolling.data.value?.filesystems ?? [])

const targetOptions = computed(() => {
  if (form.type === 'check_failed')
    return checks.value.map((check) => ({ id: check.id, label: check.name }))
  if (form.type === 'filesystem_threshold')
    return filesystems.value.map(({ filesystem, hostName }) => ({
      id: filesystem.id,
      label: `${hostName} · ${filesystem.mount_point}`,
    }))
  return hosts.value.map((host) => ({ id: host.id, label: host.display_name }))
})

function inputFromForm(): AlertRuleInput {
  const input: AlertRuleInput = {
    name: form.name.trim(),
    type: form.type,
    severity: form.severity,
    channel_ids: [...form.channelIDs],
  }
  if (form.type === 'host_unreachable') {
    input.host_id = form.targetID.trim()
    input.for_seconds = Number(form.forSeconds)
  } else if (form.type === 'check_failed') {
    input.check_id = form.targetID.trim()
    input.mode = form.mode
    if (form.mode === 'duration') input.for_seconds = Number(form.forSeconds)
    else input.consecutive_failures = Number(form.consecutiveFailures)
  } else if (form.type === 'host_metric_threshold') {
    input.host_id = form.targetID.trim()
    input.metric = form.metric
    input.operator = form.operator
    input.threshold = Number(form.threshold)
    input.for_seconds = Number(form.forSeconds)
  } else {
    input.filesystem_id = form.targetID.trim()
    input.operator = form.operator
    input.threshold = Number(form.threshold)
    input.for_seconds = Number(form.forSeconds)
  }
  return input
}

function resetForm() {
  form.name = ''
  form.type = 'host_unreachable'
  form.severity = 'warning'
  form.targetID = ''
  form.channelIDs = []
  form.mode = 'duration'
  form.forSeconds = 180
  form.consecutiveFailures = 3
  form.metric = 'cpu_used_percent'
  form.operator = 'gte'
  form.threshold = 90
  editingRule.value = null
  showForm.value = false
}

function startCreate() {
  resetForm()
  showForm.value = true
}

function startEdit(rule: ReadonlyAlertRule) {
  editingRule.value = rule
  form.name = rule.name
  form.type = rule.type
  form.severity = rule.severity
  form.targetID = rule.host_id ?? rule.check_id ?? rule.filesystem_id ?? ''
  form.channelIDs = [...rule.channel_ids]
  form.mode = rule.mode ?? 'duration'
  form.forSeconds = rule.for_seconds ?? 180
  form.consecutiveFailures = rule.consecutive_failures ?? 3
  form.metric = rule.metric ?? 'cpu_used_percent'
  form.operator = rule.operator ?? 'gte'
  form.threshold = rule.threshold ?? 90
  showForm.value = true
}

async function save() {
  busy.value = 'save'
  error.value = ''
  try {
    const input = inputFromForm()
    if (editingRule.value) await patchAlertRule(auth.mutate, editingRule.value, input)
    else await createAlertRule(auth.mutate, input)
    resetForm()
    polling.restart()
  } catch {
    error.value = editingRule.value
      ? 'No se pudo editar la regla; revisa la versión y los recursos seleccionados.'
      : 'No se pudo crear la regla. Revisa la condición y los canales seleccionados.'
  } finally {
    busy.value = ''
  }
}

async function updateState(
  rule: ReadonlyAlertRule,
  action: 'enable' | 'disable' | 'archive' | 'restore',
) {
  busy.value = rule.id
  error.value = ''
  try {
    await changeAlertRuleState(auth.mutate, rule, action)
    polling.restart()
  } catch {
    error.value = 'No se pudo actualizar la regla; la versión pudo cambiar en otra sesión.'
  } finally {
    busy.value = ''
  }
}

function formatDate(value: string) {
  return new Date(value).toLocaleString('es-CL')
}

function condition(rule: ReadonlyAlertRule) {
  const targetID = rule.host_id ?? rule.check_id ?? rule.filesystem_id ?? 'sin target'
  if (rule.type === 'host_unreachable') return `host ${targetID} · ${rule.for_seconds}s`
  if (rule.type === 'check_failed')
    return rule.mode === 'attempts'
      ? `check ${targetID} · ${rule.consecutive_failures} fallos`
      : `check ${targetID} · ${rule.for_seconds}s`
  if (rule.type === 'filesystem_threshold')
    return `filesystem ${targetID} · ${rule.operator} ${rule.threshold}%`
  return `host ${targetID} · ${rule.metric} ${rule.operator} ${rule.threshold}%`
}

function evaluationTone(rule: ReadonlyAlertRule) {
  if (rule.archived || !rule.enabled) return 'paused'
  return rule.evaluation?.state ?? 'inactive'
}

function evaluationLabel(rule: ReadonlyAlertRule) {
  if (rule.archived) return 'Archivada'
  if (!rule.enabled) return 'Pausada'
  if (rule.evaluation?.state === 'pending') return 'Pendiente'
  if (rule.evaluation?.state === 'firing') return 'En firing'
  return 'Activa'
}

onMounted(() => {
  void polling.refresh()
  void resourcesPolling.refresh()
})
</script>

<template>
  <main id="main-content" class="alerts route-view" tabindex="-1">
    <section class="alerts__hero">
      <PageIntro
        index="05"
        eyebrow="Alertas / señal derivada"
        title="Actúa cuando la señal cruza la línea."
        copy="Las reglas nacen de resultados programados, conservan continuidad explícita y abren incidentes que se entregan a los canales configurados. Los datos ausentes nunca se convierten silenciosamente en ceros."
      />
      <aside class="alerts__hero-rail">
        <span>Abiertos</span><strong>{{ incidents.length }} incidentes</strong
        ><span>Evaluación</span><strong>Después de cada muestra</strong>
      </aside>
    </section>

    <p v-if="error" class="alerts__error" role="alert">{{ error }}</p>

    <section class="alerts__section" aria-labelledby="rules-title">
      <header class="alerts__heading">
        <div>
          <p class="alerts__eyebrow">01 / Reglas</p>
          <h2 id="rules-title">Condiciones sostenidas</h2>
        </div>
        <button
          class="primary-button"
          type="button"
          @click="showForm ? resetForm() : startCreate()"
        >
          {{ showForm ? 'Cerrar formulario' : 'Nueva regla' }}
        </button>
      </header>

      <form v-if="showForm" class="alert-form" @submit.prevent="save">
        <p class="alert-form__title">
          {{ editingRule ? `Editando: ${editingRule.name}` : 'Nueva regla' }}
        </p>
        <label>Nombre<input v-model.trim="form.name" maxlength="128" required /></label>
        <label
          >Tipo<select v-model="form.type" @change="form.targetID = ''">
            <option value="host_unreachable">Host inalcanzable</option>
            <option value="check_failed">Check fallido</option>
            <option value="host_metric_threshold">Umbral de host</option>
            <option value="filesystem_threshold">Umbral de filesystem</option>
          </select></label
        >
        <label
          >Severidad<select v-model="form.severity">
            <option value="warning">Warning</option>
            <option value="critical">Critical</option>
          </select></label
        >
        <label class="alert-form__wide"
          >Target
          <select v-model="form.targetID" required>
            <option value="" disabled>Selecciona un recurso</option>
            <option v-for="target in targetOptions" :key="target.id" :value="target.id">
              {{ target.label }}
            </option>
          </select>
          <small v-if="resourcesPolling.loading.value">Cargando recursos…</small>
          <small v-else-if="targetOptions.length === 0"
            >No hay recursos compatibles disponibles.</small
          >
        </label>
        <label class="alert-form__wide"
          >Canales de notificación
          <select v-model="form.channelIDs" multiple required size="4">
            <option v-for="channel in channels" :key="channel.id" :value="channel.id">
              {{ channel.name }} · {{ channel.provider }}
            </option>
          </select>
          <small v-if="channels.length === 0">Configura primero un canal de notificación.</small>
        </label>
        <label v-if="form.type === 'check_failed'"
          >Modo<select v-model="form.mode">
            <option value="duration">Duración</option>
            <option value="attempts">Intentos</option>
          </select></label
        >
        <label v-if="form.type === 'host_metric_threshold'"
          >Métrica<select v-model="form.metric">
            <option value="cpu_used_percent">CPU usada</option>
            <option value="memory_used_percent">Memoria usada</option>
          </select></label
        >
        <label v-if="form.type === 'host_metric_threshold' || form.type === 'filesystem_threshold'"
          >Operador<select v-model="form.operator">
            <option value="gte">Mayor o igual</option>
            <option value="gt">Mayor que</option>
          </select></label
        >
        <label v-if="form.type === 'host_metric_threshold' || form.type === 'filesystem_threshold'"
          >Umbral (%)<input
            v-model.number="form.threshold"
            type="number"
            min="0"
            max="100"
            required
        /></label>
        <label v-if="form.type !== 'check_failed' || form.mode === 'duration'"
          >Duración (s)<input
            v-model.number="form.forSeconds"
            type="number"
            min="0"
            max="86400"
            required
        /></label>
        <label v-if="form.type === 'check_failed' && form.mode === 'attempts'"
          >Fallos seguidos<input
            v-model.number="form.consecutiveFailures"
            type="number"
            min="1"
            max="1000"
            required
        /></label>
        <button
          class="primary-button alert-form__submit"
          type="submit"
          :disabled="busy === 'save' || resourcesPolling.loading.value"
        >
          {{ busy === 'save' ? 'Guardando…' : editingRule ? 'Guardar cambios' : 'Crear regla' }}
        </button>
      </form>

      <p v-if="polling.loading.value" class="alerts__empty" role="status">Cargando reglas…</p>
      <p v-else-if="polling.error.value && !polling.data.value" class="alerts__empty" role="alert">
        No pudimos consultar las alertas.
        <button type="button" @click="polling.refresh">Reintentar</button>
      </p>
      <p v-else-if="rules.length === 0" class="alerts__empty">
        Todavía no hay reglas configuradas.
      </p>
      <div v-else class="alert-list">
        <article
          v-for="rule in rules"
          :key="rule.id"
          class="alert-card"
          :class="{ 'alert-card--archived': rule.archived }"
        >
          <header class="alert-card__header">
            <div>
              <p class="alerts__eyebrow">
                {{ rule.type }} / {{ rule.severity }} / v{{ rule.version }}
              </p>
              <h3>{{ rule.name }}</h3>
              <code>{{ condition(rule) }}</code>
            </div>
            <span class="alert-state" :class="`alert-state--${evaluationTone(rule)}`">
              {{ evaluationLabel(rule) }}
            </span>
          </header>
          <footer class="alert-card__actions">
            <button
              v-if="!rule.archived"
              type="button"
              :disabled="busy === rule.id"
              @click="startEdit(rule)"
            >
              Editar
            </button>
            <button
              v-if="rule.archived"
              type="button"
              :disabled="busy === rule.id"
              @click="updateState(rule, 'restore')"
            >
              Restaurar
            </button>
            <button
              v-else-if="rule.enabled"
              type="button"
              :disabled="busy === rule.id"
              @click="updateState(rule, 'disable')"
            >
              Desactivar
            </button>
            <button
              v-else
              type="button"
              :disabled="busy === rule.id"
              @click="updateState(rule, 'enable')"
            >
              Activar
            </button>
            <button
              v-if="!rule.archived"
              type="button"
              :disabled="busy === rule.id"
              @click="updateState(rule, 'archive')"
            >
              Archivar
            </button>
          </footer>
        </article>
      </div>
    </section>

    <section class="alerts__section alerts__section--incidents" aria-labelledby="incidents-title">
      <header class="alerts__heading">
        <div>
          <p class="alerts__eyebrow">02 / Incidentes abiertos</p>
          <h2 id="incidents-title">Lo que pide atención ahora.</h2>
        </div>
        <RouterLink class="alerts__link" to="/notifications">Ver entregas</RouterLink>
      </header>
      <p v-if="incidents.length === 0" class="alerts__empty">No hay incidentes abiertos.</p>
      <div v-else class="incident-list">
        <article v-for="incident in incidents" :key="incident.id" class="incident-card">
          <div>
            <p class="alerts__eyebrow">{{ incident.severity }} / {{ incident.condition_status }}</p>
            <h3>{{ incident.rule_name }}</h3>
            <p>{{ incident.target_name }} · iniciado {{ formatDate(incident.fired_at) }}</p>
          </div>
          <span>{{ incident.rule_type }}</span>
        </article>
      </div>
    </section>
  </main>
</template>

<style scoped>
.alerts {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.alerts__hero {
  min-height: min(35rem, calc(100svh - 8rem));
  display: grid;
  align-items: center;
  gap: 2rem;
  border-bottom: 1px solid var(--line-strong);
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
}
.alerts__hero-rail {
  display: grid;
  grid-template-columns: repeat(4, auto);
  gap: 0.65rem 1rem;
  justify-content: end;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
.alerts__hero-rail strong {
  color: var(--ink);
}
.alerts__section {
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.alerts__heading {
  display: flex;
  justify-content: space-between;
  align-items: end;
  gap: 2rem;
  margin-bottom: 2rem;
}
.alerts__heading h2 {
  max-width: 12ch;
  margin: 0;
  font: 500 clamp(2.5rem, 5vw, 5rem)/0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.alerts__eyebrow {
  margin: 0 0 0.65rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.alerts__error {
  margin: 1rem var(--page-gutter);
  border-left: 0.3rem solid var(--accent-text);
  padding: 0.85rem 1rem;
  background: var(--surface);
}
.alerts__empty {
  color: var(--ink-muted);
}
.alerts__empty button,
.alerts__link {
  color: var(--accent-text);
  font-weight: 800;
}
.alert-form {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1rem;
  margin-bottom: 2.5rem;
  padding: 1.25rem;
  border: 1px solid var(--line-strong);
  background: var(--surface);
}
.alert-form__title {
  grid-column: 1 / -1;
  margin: 0;
  color: var(--ink);
  font: 500 1.6rem/1 var(--font-display);
}
.alert-form label {
  display: grid;
  gap: 0.4rem;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.alert-form input,
.alert-form select {
  min-height: 2.9rem;
  border: 1px solid var(--line-strong);
  padding: 0.65rem 0.75rem;
  background: var(--paper);
  color: var(--ink);
  font: 1rem var(--font-body);
}
.alert-form select[multiple] {
  min-height: 7rem;
}
.alert-form small {
  color: var(--ink-muted);
  font: 0.72rem/1.35 var(--font-body);
  letter-spacing: normal;
  text-transform: none;
}
.alert-form__wide,
.alert-form__submit {
  grid-column: span 3;
}
.alert-list,
.incident-list {
  display: grid;
  gap: 0.75rem;
}
.alert-card,
.incident-card {
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--surface);
}
.alert-card--archived {
  opacity: 0.65;
}
.alert-card__header,
.incident-card {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: start;
}
.alert-card h3,
.incident-card h3 {
  margin: 0 0 0.4rem;
  font: 500 clamp(2rem, 4vw, 3.5rem)/0.9 var(--font-display);
}
.alert-card code {
  color: var(--ink-muted);
  overflow-wrap: anywhere;
}
.alert-state {
  border: 1px solid var(--line-strong);
  padding: 0.5rem 0.7rem;
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
.alert-state--active {
  color: var(--signal);
}
.alert-state--paused {
  color: var(--accent-text);
}
.alert-state--pending {
  border-color: #8a5a00;
  color: #8a5a00;
}
.alert-state--firing {
  border-color: var(--signal);
  background: var(--ink);
  color: var(--paper);
}
.alert-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.6rem;
  margin-top: 1.5rem;
}
.alert-card__actions button {
  border: 1px solid var(--line-strong);
  padding: 0.55rem 0.75rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
  cursor: pointer;
}
.incident-card p:last-child {
  margin: 0;
  color: var(--ink-muted);
}
.incident-card > span {
  color: var(--accent-text);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
@media (max-width: 48rem) {
  .alert-form {
    grid-template-columns: 1fr;
  }
  .alert-form__wide,
  .alert-form__submit {
    grid-column: auto;
  }
  .alerts__heading,
  .alert-card__header,
  .incident-card {
    display: block;
  }
  .alert-state {
    display: inline-block;
    margin-top: 1rem;
  }
  .alerts__hero-rail {
    justify-content: start;
  }
}
</style>
