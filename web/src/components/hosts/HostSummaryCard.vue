<script setup lang="ts">
import { computed, type DeepReadonly } from 'vue'
import type { HostSummary } from '@/hosts/api'
import { formatBytes, formatDate, formatPercent } from '@/hosts/format'

const props = defineProps<{ host: DeepReadonly<HostSummary> }>()
const stateLabel = computed(() => {
  if (props.host.observed_state === 'unreachable') return 'Sin alcance'
  if (props.host.stale) return 'Señal vencida'
  return { healthy: 'Saludable', degraded: 'Degradado', unknown: 'Esperando señal' }[
    props.host.observed_state
  ]
})
const tone = computed(() => {
  if (props.host.observed_state === 'unreachable') return 'danger'
  if (props.host.stale || props.host.observed_state === 'degraded') return 'warning'
  if (props.host.observed_state === 'healthy') return 'success'
  return 'neutral'
})
const accessibleLabel = computed(
  () => `Abrir ${props.host.display_name}. Estado: ${stateLabel.value}`,
)
</script>

<template>
  <RouterLink
    class="host-card"
    :to="{ name: 'host-detail', params: { id: host.id } }"
    :aria-label="accessibleLabel"
  >
    <article>
      <header class="host-card__header">
        <p class="host-card__index">HOST / {{ host.id.slice(0, 8) }}</p>
        <span class="host-card__state" :data-tone="tone">{{ stateLabel }}</span>
      </header>
      <h3 class="host-card__title">{{ host.display_name }}</h3>
      <p class="host-card__seen">Última señal · {{ formatDate(host.last_success_received_at) }}</p>
      <dl class="host-card__metrics">
        <div>
          <dt>CPU</dt>
          <dd>{{ formatPercent(host.cpu_used_percent) }}</dd>
        </div>
        <div>
          <dt>Memoria</dt>
          <dd>{{ formatBytes(host.memory_used_bytes) }}</dd>
        </div>
        <div>
          <dt>Disco crítico</dt>
          <dd>{{ formatPercent(host.most_constrained_filesystem?.used_percent ?? null) }}</dd>
        </div>
      </dl>
      <p v-if="host.warnings.length" class="host-card__warning">
        {{ host.warnings.length }} advertencia{{ host.warnings.length === 1 ? '' : 's' }} activa{{
          host.warnings.length === 1 ? '' : 's'
        }}
      </p>
      <span class="host-card__link">Abrir estado actual →</span>
    </article>
  </RouterLink>
</template>

<style scoped>
.host-card {
  display: block;
  min-width: 0;
  border: 1px solid var(--line-strong);
  color: inherit;
  background: var(--surface);
  text-decoration: none;
  transition:
    transform 140ms ease,
    background 140ms ease;
}
.host-card:hover {
  transform: translateY(-0.25rem);
  background: #fffdf5;
}
.host-card article {
  min-height: 25rem;
  display: flex;
  flex-direction: column;
  padding: clamp(1.25rem, 3vw, 2rem);
}
.host-card__header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: center;
}
.host-card__index,
.host-card__state,
.host-card__seen,
.host-card__metrics dt,
.host-card__warning,
.host-card__link {
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.09em;
}
.host-card__index,
.host-card__seen {
  margin: 0;
  color: var(--ink-muted);
  font-size: 0.62rem;
}
.host-card__seen {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-card__state {
  padding: 0.35rem 0.5rem;
  border: 1px solid currentColor;
  font-size: 0.6rem;
  font-weight: 800;
}
.host-card__state[data-tone='success'] {
  color: #476800;
}
.host-card__state[data-tone='warning'] {
  color: #765600;
}
.host-card__state[data-tone='danger'] {
  color: var(--accent-text);
}
.host-card__state[data-tone='neutral'] {
  color: var(--ink-muted);
}
.host-card__title {
  margin: 3rem 0 0.75rem;
  font: 500 clamp(2.25rem, 4vw, 4rem)/0.92 var(--font-display);
  letter-spacing: -0.045em;
  overflow-wrap: anywhere;
}
.host-card__metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin: auto 0 1.5rem;
  border-block: 1px solid var(--line-soft);
}
.host-card__metrics div {
  min-width: 0;
  padding: 1rem 0.5rem;
}
.host-card__metrics div + div {
  border-left: 1px solid var(--line-soft);
}
.host-card__metrics dt {
  color: var(--ink-muted);
  font-size: 0.55rem;
}
.host-card__metrics dd {
  margin: 0.45rem 0 0;
  font-weight: 800;
  overflow-wrap: anywhere;
}
.host-card__warning {
  margin: 0 0 1rem;
  color: var(--accent-text);
  font-size: 0.62rem;
  font-weight: 800;
}
.host-card__link {
  font-size: 0.65rem;
  font-weight: 800;
}
@media (max-width: 25rem) {
  .host-card__metrics {
    grid-template-columns: 1fr;
  }
  .host-card__metrics div + div {
    border-left: 0;
    border-top: 1px solid var(--line-soft);
  }
}
</style>
