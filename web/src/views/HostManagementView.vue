<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import { useAuth } from '@/auth/useAuth'
import { listAllHostsIncludingArchived, type HostSummary } from '@/hosts/api'
import { formatDate } from '@/hosts/format'
import {
  changeHostState,
  createEnrollmentToken,
  listEnrollmentTokens,
  patchHost,
  revokeEnrollmentToken,
  type EnrollmentToken,
  type NewEnrollmentToken,
} from '@/hosts/management'

const auth = useAuth()
const hosts = ref<HostSummary[]>([])
const tokens = ref<EnrollmentToken[]>([])
const newToken = ref<NewEnrollmentToken | null>(null)
const label = ref('')
const loading = ref(true)
const busy = ref('')
const error = ref('')
const drafts = reactive<Record<string, { displayName: string; interval: number; timeout: number }>>(
  {},
)

function syncDraft(host: HostSummary) {
  drafts[host.id] = {
    displayName: host.display_name,
    interval: Number(host.interval_seconds ?? 60),
    timeout: Number(host.timeout_seconds ?? 10),
  }
}

async function refresh() {
  loading.value = true
  error.value = ''
  try {
    const [hostItems, tokenResponse] = await Promise.all([
      listAllHostsIncludingArchived(),
      listEnrollmentTokens(),
    ])
    hosts.value = hostItems
    tokens.value = tokenResponse.data?.items ?? []
    for (const host of hostItems) syncDraft(host)
  } catch {
    error.value = 'No se pudo cargar la administración de hosts.'
  } finally {
    loading.value = false
  }
}

async function issueToken() {
  busy.value = 'token-create'
  error.value = ''
  try {
    newToken.value = await createEnrollmentToken(auth.mutate, label.value.trim())
    label.value = ''
    await refresh()
  } catch {
    error.value = 'No se pudo crear el token de enrolamiento.'
  } finally {
    busy.value = ''
  }
}

async function revokeToken(token: EnrollmentToken) {
  busy.value = `token:${token.id}`
  error.value = ''
  try {
    await revokeEnrollmentToken(auth.mutate, token)
    await refresh()
  } catch {
    error.value = 'No se pudo revocar el token.'
  } finally {
    busy.value = ''
  }
}

async function saveHost(host: HostSummary) {
  const draft = drafts[host.id]
  if (!draft) return
  busy.value = `host:${host.id}`
  error.value = ''
  try {
    await patchHost(auth.mutate, host, {
      display_name: draft.displayName.trim(),
      interval_seconds: Number(draft.interval),
      timeout_seconds: Number(draft.timeout),
    })
    await refresh()
  } catch {
    error.value = 'No se pudo guardar la configuración del host; revisa su versión.'
  } finally {
    busy.value = ''
  }
}

async function updateState(
  host: HostSummary,
  action: 'enable' | 'disable' | 'archive' | 'restore',
) {
  busy.value = `host:${host.id}`
  error.value = ''
  try {
    await changeHostState(auth.mutate, host, action)
    await refresh()
  } catch {
    error.value = 'No se pudo cambiar el estado administrativo del host.'
  } finally {
    busy.value = ''
  }
}

onMounted(refresh)
</script>

<template>
  <main id="main-content" class="management route-view" tabindex="-1">
    <section class="management__hero">
      <PageIntro
        index="03"
        eyebrow="Administración / control plane"
        title="Decide qué sigue en vigilancia."
        copy="Crea enrolamientos de un solo uso, ajusta la cadencia de cada host y conserva archivados los recursos que ya no deben aparecer en el resumen operativo."
      />
    </section>

    <p v-if="error" class="management__error" role="alert">{{ error }}</p>

    <section class="management__section token-panel" aria-labelledby="tokens-title">
      <header class="management__heading">
        <div>
          <p class="management__eyebrow">Credenciales efímeras</p>
          <h2 id="tokens-title">Tokens de enrolamiento</h2>
        </div>
        <span>TTL 15 min · uso único</span>
      </header>
      <form class="token-panel__form" @submit.prevent="issueToken">
        <label>
          Etiqueta opcional
          <input v-model="label" maxlength="128" placeholder="Servidor remoto" />
        </label>
        <button class="primary-button" type="submit" :disabled="busy === 'token-create'">
          {{ busy === 'token-create' ? 'Creando…' : 'Crear token' }}
        </button>
      </form>
      <div v-if="newToken" class="token-panel__secret" role="status">
        <strong>Token visible una sola vez</strong>
        <code>{{ newToken.token }}</code>
        <small>Guárdalo en un archivo 0600 y ejecuta <code>atalaya agent enroll</code>.</small>
      </div>
      <p v-if="!loading && tokens.length === 0" class="management__empty">
        No hay tokens emitidos.
      </p>
      <ul v-else class="token-list">
        <li v-for="token in tokens" :key="token.id">
          <div>
            <strong>{{ token.label || 'Sin etiqueta' }}</strong>
            <small>Expira {{ formatDate(token.expires_at) }}</small>
          </div>
          <span v-if="token.revoked_at" class="token-list__state">Revocado</span>
          <span v-else-if="token.consumed_at" class="token-list__state">Consumido</span>
          <button
            v-else
            type="button"
            :disabled="busy === `token:${token.id}`"
            @click="revokeToken(token)"
          >
            Revocar
          </button>
        </li>
      </ul>
    </section>

    <section class="management__section" aria-labelledby="hosts-management-title">
      <header class="management__heading">
        <div>
          <p class="management__eyebrow">Inventario editable</p>
          <h2 id="hosts-management-title">Hosts y cadencias</h2>
        </div>
        <span>{{ hosts.length }} recursos</span>
      </header>
      <p v-if="loading" class="management__empty" role="status">Cargando administración…</p>
      <p v-else-if="hosts.length === 0" class="management__empty">No hay hosts enrolados.</p>
      <div v-else class="host-management-list">
        <article v-for="host in hosts" :key="host.id" class="host-management-card">
          <header>
            <div>
              <p class="management__eyebrow">
                {{ host.archived ? 'Archivado' : host.administrative_state }}
              </p>
              <h3>{{ host.display_name }}</h3>
              <code>{{ host.id }}</code>
            </div>
            <span>v{{ host.version }}</span>
          </header>
          <form
            v-if="drafts[host.id]"
            class="host-management-card__form"
            @submit.prevent="saveHost(host)"
          >
            <label>
              Nombre
              <input
                v-model="drafts[host.id]!.displayName"
                maxlength="128"
                :disabled="host.archived"
              />
            </label>
            <label>
              Intervalo (s)
              <input
                v-model.number="drafts[host.id]!.interval"
                type="number"
                min="15"
                max="3600"
                :disabled="host.archived"
              />
            </label>
            <label>
              Timeout (s)
              <input
                v-model.number="drafts[host.id]!.timeout"
                type="number"
                min="1"
                max="60"
                :disabled="host.archived"
              />
            </label>
            <button
              class="primary-button"
              type="submit"
              :disabled="host.archived || busy === `host:${host.id}`"
            >
              Guardar
            </button>
          </form>
          <div class="host-management-card__actions">
            <RouterLink v-if="!host.archived" :to="{ name: 'host-detail', params: { id: host.id } }"
              >Ver estado</RouterLink
            >
            <button
              v-if="host.archived"
              type="button"
              :disabled="busy === `host:${host.id}`"
              @click="updateState(host, 'restore')"
            >
              Restaurar
            </button>
            <button
              v-else-if="host.administrative_state === 'disabled'"
              type="button"
              :disabled="busy === `host:${host.id}`"
              @click="updateState(host, 'enable')"
            >
              Activar
            </button>
            <button
              v-else
              type="button"
              :disabled="busy === `host:${host.id}`"
              @click="updateState(host, 'disable')"
            >
              Desactivar
            </button>
            <button
              v-if="!host.archived"
              type="button"
              :disabled="busy === `host:${host.id}`"
              @click="updateState(host, 'archive')"
            >
              Archivar
            </button>
          </div>
        </article>
      </div>
    </section>
  </main>
</template>

<style scoped>
.management {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.management__hero {
  min-height: min(35rem, calc(100svh - 8rem));
  display: grid;
  align-items: center;
  border-bottom: 1px solid var(--line-strong);
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
}
.management__hero :deep(.page-intro) {
  width: 100%;
}
.management__section {
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.management__heading {
  display: flex;
  justify-content: space-between;
  gap: 2rem;
  align-items: end;
  margin-bottom: 2rem;
}
.management__heading h2 {
  margin: 0;
  font: 500 clamp(2.5rem, 5vw, 5rem)/0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.management__heading > span {
  color: var(--ink-muted);
  font: 800 0.65rem var(--font-mono);
  text-transform: uppercase;
}
.management__eyebrow {
  margin: 0 0 0.65rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.management__error {
  margin: 1rem var(--page-gutter);
  border-left: 0.3rem solid var(--accent-text);
  padding: 0.85rem 1rem;
  background: var(--surface);
}
.management__empty {
  color: var(--ink-muted);
}
.token-panel__form,
.host-management-card__form {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  align-items: end;
}
label {
  display: grid;
  gap: 0.4rem;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
input {
  min-height: 2.9rem;
  border: 1px solid var(--line-strong);
  padding: 0.65rem 0.75rem;
  background: var(--paper);
  color: var(--ink);
  font: 1rem var(--font-body);
}
.token-panel__form label {
  flex: 1 1 18rem;
}
.token-panel__secret {
  display: grid;
  gap: 0.7rem;
  margin: 2rem 0;
  border: 2px solid var(--accent-text);
  padding: 1rem;
  background: var(--surface);
}
.token-panel__secret code {
  overflow-wrap: anywhere;
  color: var(--accent-text);
  font: 800 0.95rem var(--font-mono);
}
.token-panel__secret small {
  color: var(--ink-muted);
}
.token-panel__secret small code {
  font-size: 0.75rem;
}
.token-list,
.host-management-list {
  display: grid;
  gap: 0.75rem;
  padding: 0;
  list-style: none;
}
.token-list li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: center;
  border-block: 1px solid var(--line-soft);
  padding: 1rem 0;
}
.token-list li > div {
  display: grid;
  gap: 0.3rem;
}
.token-list small,
.token-list__state {
  color: var(--ink-muted);
  font: 0.65rem var(--font-mono);
}
.token-list button,
.host-management-card__actions button,
.host-management-card__actions a {
  border: 1px solid var(--line-strong);
  padding: 0.55rem 0.75rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  text-decoration: none;
  text-transform: uppercase;
  cursor: pointer;
}
.host-management-card {
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--surface);
}
.host-management-card > header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.5rem;
}
.host-management-card h3 {
  margin: 0 0 0.4rem;
  font: 500 clamp(2rem, 4vw, 3.5rem)/0.9 var(--font-display);
}
.host-management-card header code {
  color: var(--ink-muted);
  font-size: 0.65rem;
  overflow-wrap: anywhere;
}
.host-management-card > header > span {
  color: var(--ink-muted);
  font: 0.7rem var(--font-mono);
}
.host-management-card__form label {
  flex: 1 1 10rem;
}
.host-management-card__form .primary-button {
  flex: 0 0 auto;
}
.host-management-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.6rem;
  margin-top: 1.5rem;
}
button:disabled {
  cursor: wait;
  opacity: 0.55;
}
@media (max-width: 42rem) {
  .management__heading {
    display: block;
  }
  .management__heading > span {
    display: block;
    margin-top: 1rem;
  }
  .token-list li {
    align-items: start;
    flex-direction: column;
  }
}
</style>
