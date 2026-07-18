<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import HostCurrentMetrics from '@/components/hosts/HostCurrentMetrics.vue'
import FilesystemList from '@/components/hosts/FilesystemList.vue'
import { useHostPolling } from '@/composables/useHostPolling'
import { getHost, listAllFilesystems, type Filesystem, type HostDetail } from '@/hosts/api'
import { formatDate } from '@/hosts/format'

interface DetailState {
  host: HostDetail
  filesystems: Filesystem[]
}
const route = useRoute()
const hostId = computed(() => String(route.params.id))
let etag: string | undefined
let currentHost: HostDetail | null = null

const polling = useHostPolling<DetailState>(async (signal) => {
  const detail = await getHost(hostId.value, signal, etag)
  if (detail.etag) etag = detail.etag
  if (detail.data) currentHost = detail.data
  if (!currentHost) throw new Error('host detail missing')
  const filesystems = await listAllFilesystems(hostId.value, signal)
  return { host: currentHost, filesystems }
})

watch(hostId, () => {
  etag = undefined
  currentHost = null
  polling.restart(true)
})
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
      </header>
      <HostCurrentMetrics :host="polling.data.value.host" />
      <FilesystemList
        :filesystems="polling.data.value.filesystems"
        :loading="false"
        :error="false"
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
</style>
