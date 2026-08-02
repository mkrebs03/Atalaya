<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import { useAuth } from '@/auth/useAuth'
import { useHostPolling } from '@/composables/useHostPolling'
import {
  changeNotificationChannelState,
  createNotificationChannel,
  listNotificationChannels,
  listNotificationDeliveries,
  patchNotificationChannel,
  testNotificationChannel,
  type NotificationChannel,
} from '@/notifications/api'
import {
  deliveryKindLabel,
  deliveryStateLabel,
  deliveryStateTone,
  formatDateTime,
  transitionLabel,
} from '@/ui/labels'

const auth = useAuth()
const showCreate = ref(false)
const showArchived = ref(false)
const archiveTarget = ref<NotificationChannel | null>(null)
const busy = ref('')
const error = ref('')
const form = reactive({ name: '', webhookURL: '' })

const polling = useHostPolling(async (signal) => {
  const [channelsResponse, deliveriesResponse] = await Promise.all([
    listNotificationChannels(signal, true),
    listNotificationDeliveries(signal),
  ])
  if (!channelsResponse.data || !deliveriesResponse.data)
    throw new Error('notification response missing')
  return { channels: channelsResponse.data.items, deliveries: deliveriesResponse.data.items }
})

const channels = computed(() => {
  const allChannels = polling.data.value?.channels ?? []
  return showArchived.value
    ? allChannels.filter((channel) => channel.archived)
    : allChannels.filter((channel) => !channel.archived)
})
const deliveries = computed(() => polling.data.value?.deliveries ?? [])

function channelName(id: string) {
  return (
    polling.data.value?.channels.find((channel) => channel.id === id)?.name ?? 'Canal no disponible'
  )
}

function resetForm() {
  form.name = ''
  form.webhookURL = ''
  showCreate.value = false
}

async function save() {
  busy.value = 'create'
  error.value = ''
  try {
    await createNotificationChannel(auth.mutate, {
      name: form.name.trim(),
      provider: 'discord',
      webhook_url: form.webhookURL.trim(),
    })
    resetForm()
    polling.restart()
  } catch {
    error.value = 'No se pudo guardar el canal. Verifica que el webhook sea de discord.com.'
  } finally {
    busy.value = ''
  }
}

async function updateChannel(
  channel: NotificationChannel,
  action: 'archive' | 'restore' | 'toggle',
) {
  busy.value = channel.id
  error.value = ''
  try {
    if (action === 'toggle') {
      await patchNotificationChannel(auth.mutate, channel, { enabled: !channel.enabled })
    } else {
      await changeNotificationChannelState(auth.mutate, channel, action)
    }
    polling.restart()
  } catch {
    error.value = 'No se pudo actualizar el canal; la versión pudo cambiar en otra sesión.'
  } finally {
    busy.value = ''
  }
}

function requestArchive(channel: NotificationChannel) {
  archiveTarget.value = channel
}

function confirmArchive() {
  const channel = archiveTarget.value
  archiveTarget.value = null
  if (channel) void updateChannel(channel, 'archive')
}

async function sendTest(channel: NotificationChannel) {
  busy.value = `test:${channel.id}`
  error.value = ''
  try {
    const requestID =
      typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `${Date.now()}-${channel.id}`
    await testNotificationChannel(auth.mutate, channel.id, requestID)
    polling.restart()
  } catch {
    error.value = 'No se pudo encolar la prueba del canal.'
  } finally {
    busy.value = ''
  }
}

onMounted(() => polling.refresh())
</script>

<template>
  <main id="main-content" class="notifications route-view" tabindex="-1">
    <section class="notifications__hero">
      <PageIntro
        eyebrow="Notificaciones"
        title="Lleva las alertas a tu equipo."
        copy="Configura los canales que recibirán los incidentes. Los secretos se guardan de forma segura y en la interfaz solo verás su estado y el historial de entregas."
      />
    </section>

    <p v-if="error" class="notifications__error" role="alert">{{ error }}</p>

    <section class="notifications__section" aria-labelledby="channels-title">
      <header class="notifications__heading">
        <div>
          <p class="notifications__eyebrow">Canales configurados</p>
          <h2 id="channels-title">Destinos de alerta.</h2>
        </div>
        <div class="notifications__heading-actions">
          <div class="notifications__filters" role="group" aria-label="Filtro de canales">
            <button
              type="button"
              :class="{ 'is-active': !showArchived }"
              @click="showArchived = false"
            >
              Activos
            </button>
            <button
              type="button"
              :class="{ 'is-active': showArchived }"
              @click="showArchived = true"
            >
              Archivados
            </button>
          </div>
          <button class="primary-button" type="button" @click="showCreate = !showCreate">
            {{ showCreate ? 'Cerrar formulario' : 'Agregar canal' }}
          </button>
        </div>
      </header>
      <form v-if="showCreate" class="channel-form" @submit.prevent="save">
        <label>Nombre<input v-model.trim="form.name" maxlength="128" required /></label>
        <label class="channel-form__wide"
          >Webhook URL<input
            v-model.trim="form.webhookURL"
            type="url"
            required
            placeholder="https://discord.com/api/webhooks/..."
        /></label>
        <button class="primary-button" type="submit" :disabled="busy === 'create'">
          {{ busy === 'create' ? 'Guardando…' : 'Guardar canal' }}
        </button>
      </form>
      <p v-if="polling.loading.value" class="notifications__empty" role="status">
        Cargando canales…
      </p>
      <p
        v-else-if="polling.error.value && !polling.data.value"
        class="notifications__empty"
        role="alert"
      >
        No pudimos consultar las notificaciones.
        <button type="button" @click="polling.refresh">Reintentar</button>
      </p>
      <p v-else-if="channels.length === 0" class="notifications__empty">
        {{ showArchived ? 'No hay canales archivados.' : 'No hay canales activos.' }}
      </p>
      <div v-else class="channel-list">
        <article
          v-for="channel in channels"
          :key="channel.id"
          class="channel-card"
          :class="{ 'channel-card--archived': channel.archived }"
        >
          <header>
            <div>
              <p class="notifications__eyebrow">
                Discord ·
                {{ channel.archived ? 'Archivado' : channel.enabled ? 'Activo' : 'Pausado' }}
              </p>
              <h3>{{ channel.name }}</h3>
              <code>{{
                channel.secret_configured
                  ? 'Webhook configurado · secreto oculto'
                  : 'Webhook ausente'
              }}</code>
            </div>
          </header>
          <footer class="channel-card__actions">
            <button
              v-if="channel.archived"
              type="button"
              :disabled="busy === channel.id"
              @click="updateChannel(channel, 'restore')"
            >
              Restaurar
            </button>
            <template v-else>
              <button
                type="button"
                :disabled="busy === channel.id"
                @click="updateChannel(channel, 'toggle')"
              >
                {{ channel.enabled ? 'Desactivar' : 'Activar' }}
              </button>
              <button
                type="button"
                :disabled="busy === `test:${channel.id}` || !channel.enabled"
                @click="sendTest(channel)"
              >
                Probar
              </button>
              <button
                type="button"
                :disabled="busy === channel.id"
                @click="requestArchive(channel)"
              >
                Archivar
              </button>
            </template>
          </footer>
        </article>
      </div>
    </section>

    <section class="notifications__section" aria-labelledby="deliveries-title">
      <header class="notifications__heading">
        <div>
          <p class="notifications__eyebrow">Historial de entregas</p>
          <h2 id="deliveries-title">Cada intento deja rastro.</h2>
        </div>
        <span>{{ deliveries.length }} recientes</span>
      </header>
      <p v-if="deliveries.length === 0" class="notifications__empty">No hay entregas todavía.</p>
      <div v-else class="delivery-list">
        <article v-for="delivery in deliveries" :key="delivery.id" class="delivery-card">
          <div>
            <p class="notifications__eyebrow">
              {{ deliveryKindLabel(delivery.kind) }} · {{ channelName(delivery.channel_id) }}
            </p>
            <h3>{{ transitionLabel(delivery.transition) }}</h3>
            <p>
              {{ deliveryStateLabel(delivery.state) }} · {{ delivery.attempts }} intentos ·
              {{ formatDateTime(delivery.created_at) }}
            </p>
          </div>
          <span class="delivery-state" :data-tone="deliveryStateTone(delivery.state)">{{
            deliveryStateLabel(delivery.state)
          }}</span>
        </article>
      </div>
    </section>

    <ConfirmDialog
      :open="archiveTarget !== null"
      title="¿Archivar este canal?"
      copy="Dejará de recibir nuevas alertas, pero podrás restaurarlo después."
      confirm-label="Archivar canal"
      danger
      @confirm="confirmArchive"
      @cancel="archiveTarget = null"
    />
  </main>
</template>

<style scoped>
.notifications {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.notifications__hero {
  min-height: min(28rem, calc(100svh - 8rem));
  display: grid;
  align-items: center;
  border-bottom: 1px solid var(--line-strong);
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
}
.notifications__section {
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.notifications__heading {
  display: flex;
  justify-content: space-between;
  align-items: end;
  gap: 2rem;
  margin-bottom: 2rem;
}
.notifications__heading h2 {
  max-width: 12ch;
  margin: 0;
  font: 500 clamp(2.5rem, 5vw, 5rem)/0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.notifications__heading > span {
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
.notifications__heading-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: end;
  align-items: center;
  gap: 0.75rem;
}
.notifications__filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}
.notifications__filters button {
  border: 1px solid var(--line-strong);
  padding: 0.5rem 0.7rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
  cursor: pointer;
}
.notifications__filters button.is-active {
  background: var(--ink);
  color: var(--paper);
}
.notifications__eyebrow {
  margin: 0 0 0.65rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.notifications__error {
  margin: 1rem var(--page-gutter);
  border-left: 0.3rem solid var(--accent-text);
  padding: 0.85rem 1rem;
  background: var(--surface);
}
.notifications__empty {
  color: var(--ink-muted);
}
.channel-form {
  display: grid;
  grid-template-columns: minmax(12rem, 0.5fr) minmax(0, 1.5fr) auto;
  gap: 1rem;
  align-items: end;
  margin-bottom: 2.5rem;
  padding: 1.25rem;
  border: 1px solid var(--line-strong);
  background: var(--surface);
}
.channel-form label {
  display: grid;
  gap: 0.4rem;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.channel-form input {
  min-height: 2.9rem;
  border: 1px solid var(--line-strong);
  padding: 0.65rem 0.75rem;
  background: var(--paper);
  color: var(--ink);
  font: 1rem var(--font-body);
}
.channel-list,
.delivery-list {
  display: grid;
  gap: 0.75rem;
}
.channel-card,
.delivery-card {
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--surface);
}
.channel-card--archived {
  opacity: 0.65;
}
.channel-card header,
.delivery-card {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: start;
}
.channel-card h3,
.delivery-card h3 {
  margin: 0 0 0.4rem;
  font: 500 clamp(2rem, 4vw, 3.5rem)/0.9 var(--font-display);
}
.channel-card code {
  color: var(--ink-muted);
  overflow-wrap: anywhere;
}
.channel-card header > span,
.delivery-card > span {
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
}
.channel-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.6rem;
  margin-top: 1.5rem;
}
.channel-card__actions button {
  border: 1px solid var(--line-strong);
  padding: 0.55rem 0.75rem;
  background: transparent;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  text-transform: uppercase;
  cursor: pointer;
}
.delivery-card p:last-child {
  margin: 0;
  color: var(--ink-muted);
}
.delivery-state[data-tone='success'] {
  color: var(--signal) !important;
}
.delivery-state[data-tone='danger'] {
  color: var(--accent-text) !important;
}
.delivery-state[data-tone='warning'] {
  color: #8a5a00 !important;
}
@media (max-width: 52rem) {
  .channel-form {
    grid-template-columns: 1fr;
  }
  .channel-form__wide {
    grid-column: auto;
  }
  .notifications__heading,
  .channel-card header,
  .delivery-card {
    display: block;
  }
  .notifications__heading-actions {
    justify-content: start;
    margin-top: 1rem;
  }
  .channel-card header > span,
  .delivery-card > span {
    display: inline-block;
    margin-top: 1rem;
  }
}
</style>
