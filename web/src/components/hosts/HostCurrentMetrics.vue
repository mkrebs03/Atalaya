<script setup lang="ts">
import type { HostDetail } from '@/hosts/api'
import type { DeepReadonly } from 'vue'
import { formatBytes, formatDuration, formatPercent } from '@/hosts/format'

defineProps<{ host: DeepReadonly<HostDetail> }>()
</script>

<template>
  <section class="metrics" aria-labelledby="metrics-title">
    <header>
      <p>Lectura inmediata</p>
      <h2 id="metrics-title">Estado actual</h2>
    </header>
    <dl class="metrics__grid">
      <div>
        <dt>CPU usada</dt>
        <dd>{{ formatPercent(host.cpu_used_percent) }}</dd>
      </div>
      <div>
        <dt>Memoria usada</dt>
        <dd>{{ formatBytes(host.memory_used_bytes) }}</dd>
        <small v-if="host.memory_total_bytes !== null"
          >de {{ formatBytes(host.memory_total_bytes) }}</small
        >
      </div>
      <div>
        <dt>Tiempo activo</dt>
        <dd>{{ formatDuration(host.uptime_seconds) }}</dd>
      </div>
      <div>
        <dt>Tailscale</dt>
        <dd>{{ host.tailscale?.state ?? 'Sin dato' }}</dd>
        <small>Hostname · {{ host.tailscale?.hostname ?? 'Sin hostname' }}</small>
        <small class="metrics__addresses">
          <span>Direcciones · </span
          >{{
            host.tailscale?.addresses.length
              ? host.tailscale.addresses.join(', ')
              : 'Sin direcciones'
          }}
        </small>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.metrics {
  border-block: 1px solid var(--line-strong);
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
}
.metrics header p {
  margin: 0 0 0.75rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.metrics header h2 {
  margin: 0 0 2rem;
  font: 500 clamp(2.5rem, 5vw, 5rem)/0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.metrics__grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
}
.metrics__grid div {
  min-width: 0;
  min-height: 10rem;
  border: 1px solid var(--line-strong);
  padding: 1.25rem;
  background: var(--surface);
}
.metrics__grid div + div {
  border-left: 0;
}
.metrics__grid dt {
  color: var(--ink-muted);
  font: 800 0.6rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.metrics__grid dd {
  margin: 2rem 0 0.3rem;
  font-size: clamp(1.35rem, 3vw, 2rem);
  font-weight: 800;
  overflow-wrap: anywhere;
}
.metrics__grid small {
  color: var(--ink-muted);
}
.metrics__addresses {
  display: block;
  margin-top: 0.35rem;
  overflow-wrap: anywhere;
}
@media (max-width: 60rem) {
  .metrics__grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .metrics__grid div + div {
    border-left: 1px solid var(--line-strong);
  }
  .metrics__grid div:nth-child(even) {
    border-left: 0;
  }
  .metrics__grid div:nth-child(n + 3) {
    border-top: 0;
  }
}
@media (max-width: 30rem) {
  .metrics__grid {
    grid-template-columns: 1fr;
  }
  .metrics__grid div:nth-child(n) {
    border: 1px solid var(--line-strong);
  }
  .metrics__grid div + div {
    border-top: 0;
  }
}
</style>
