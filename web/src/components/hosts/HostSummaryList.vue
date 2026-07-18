<script setup lang="ts">
import type { HostSummary } from '@/hosts/api'
import type { DeepReadonly } from 'vue'
import HostSummaryCard from './HostSummaryCard.vue'

defineProps<{
  hosts: readonly DeepReadonly<HostSummary>[]
  loading: boolean
  error: boolean
  refreshing: boolean
}>()
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <section class="host-list" aria-labelledby="hosts-title" :aria-busy="loading || refreshing">
    <header class="host-list__heading">
      <div>
        <p>Inventario / estado actual</p>
        <h2 id="hosts-title">Señales bajo vigilancia.</h2>
      </div>
      <span v-if="refreshing" role="status">Actualizando</span>
    </header>
    <div v-if="loading" class="host-list__status" role="status">Consultando hosts…</div>
    <div v-else-if="error && hosts.length === 0" class="host-list__status" role="alert">
      <p>No pudimos leer el estado actual.</p>
      <button type="button" @click="emit('retry')">Reintentar</button>
    </div>
    <div v-else-if="hosts.length === 0" class="host-list__status">
      <p>Todavía no hay hosts listos para mostrar.</p>
      <span>Completa el bootstrap del agente local para recibir la primera señal.</span>
    </div>
    <template v-else>
      <p v-if="error" class="host-list__notice" role="alert">
        La actualización falló. Conservamos la última lectura.
      </p>
      <div class="host-list__grid">
        <HostSummaryCard v-for="host in hosts" :key="host.id" :host="host" />
      </div>
    </template>
  </section>
</template>

<style scoped>
.host-list {
  padding: clamp(3.5rem, 8vw, 7rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.host-list__heading {
  display: flex;
  justify-content: space-between;
  gap: 2rem;
  align-items: end;
  margin-bottom: 2.5rem;
}
.host-list__heading p,
.host-list__heading span {
  margin: 0 0 0.75rem;
  color: var(--accent-text);
  font: 800 0.68rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.host-list__heading h2 {
  max-width: 12ch;
  margin: 0;
  font: 500 clamp(2.6rem, 6vw, 6rem)/0.9 var(--font-display);
  letter-spacing: -0.05em;
}
.host-list__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.host-list__status {
  min-height: 16rem;
  display: grid;
  place-content: center;
  gap: 1rem;
  border: 1px solid var(--line-strong);
  padding: 2rem;
  text-align: center;
  background: var(--surface);
}
.host-list__status p {
  margin: 0;
  font: 500 clamp(1.8rem, 4vw, 3rem) var(--font-display);
}
.host-list__status span {
  color: var(--ink-muted);
}
.host-list__status button {
  justify-self: center;
  min-height: 2.75rem;
  border: 1px solid var(--ink);
  padding: 0.6rem 1rem;
  background: var(--ink);
  color: var(--paper);
  font-weight: 800;
  cursor: pointer;
}
.host-list__notice {
  border-left: 0.35rem solid var(--accent-surface);
  margin: 0 0 1rem;
  padding: 0.8rem 1rem;
  background: var(--surface);
}
@media (max-width: 50rem) {
  .host-list__grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 32rem) {
  .host-list__heading {
    align-items: start;
    flex-direction: column;
  }
}
</style>
