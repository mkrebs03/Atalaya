<script setup lang="ts">
import { computed } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import { useHostPolling } from '@/composables/useHostPolling'
import { getSystemStatus, type WorkerStatus } from '@/system/api'

const polling = useHostPolling(async (signal) => {
  const result = await getSystemStatus(signal)
  if (!result.data) throw new Error('system status missing')
  return result.data
})

const workers = computed(() => {
  const status = polling.data.value
  if (!status) return []
  return [
    ['collection', 'Colección', status.collection],
    ['retention', 'Retención', status.retention],
    ['checks', 'Checks', status.checks],
    ['notifications', 'Notificaciones', status.notifications],
  ] as const
})

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let amount = value
  let unit = 'B'
  for (const next of units) {
    amount /= 1024
    unit = next
    if (amount < 1024) break
  }
  return `${amount.toFixed(amount >= 100 ? 0 : 1)} ${unit}`
}

function workerLabel(status: WorkerStatus) {
  if (status.state === 'healthy') return 'Operativo'
  if (status.state === 'degraded') return 'Degradado'
  if (status.state === 'idle') return 'Sin objetivos'
  return 'Desconocido'
}

function workerClass(status: WorkerStatus) {
  return `system__worker--${status.state}`
}
</script>

<template>
  <main id="main-content" class="system route-view" tabindex="-1">
    <section class="system__intro">
      <PageIntro
        index="06"
        eyebrow="Operación / estado de la instancia"
        title="Ver la torre por dentro."
        copy="Una lectura autenticada de versión, schema, almacenamiento y trabajadores. Los errores se expresan como códigos estables, nunca como trazas ni secretos."
      />
    </section>

    <section class="system__workspace" aria-labelledby="system-title">
      <div class="system__summary">
        <p class="system__eyebrow">Atalaya</p>
        <h2 id="system-title">Estado del sistema</h2>
        <p v-if="polling.loading.value" role="status">Consultando estado…</p>
        <p v-else-if="polling.error.value && !polling.data.value" role="alert">
          No pudimos leer el estado de la instancia.
        </p>
        <dl v-else-if="polling.data.value" class="system__facts">
          <div>
            <dt>Versión</dt>
            <dd>{{ polling.data.value.version }}</dd>
          </div>
          <div>
            <dt>Schema</dt>
            <dd>{{ polling.data.value.schema_version }}</dd>
          </div>
          <div>
            <dt>SQLite aprox.</dt>
            <dd>{{ formatBytes(polling.data.value.database_size_bytes) }}</dd>
          </div>
          <div>
            <dt>Último heartbeat</dt>
            <dd>{{ polling.data.value.last_heartbeat_at ?? 'Sin datos' }}</dd>
          </div>
        </dl>
      </div>

      <div class="system__workers" aria-label="Trabajadores del sistema">
        <article
          v-for="[, name, status] in workers"
          :key="name"
          class="system__worker"
          :class="workerClass(status)"
        >
          <div class="system__worker-heading">
            <h3>{{ name }}</h3>
            <span>{{ workerLabel(status) }}</span>
          </div>
          <p class="system__worker-code">
            {{ status.error_code || 'sin errores' }}
          </p>
          <p class="system__worker-heartbeat">
            {{
              status.last_heartbeat_at
                ? `Latido ${status.last_heartbeat_at}`
                : 'Todavía sin heartbeat'
            }}
          </p>
        </article>
      </div>
    </section>
  </main>
</template>

<style scoped>
.system {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}

.system__intro {
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}

.system__workspace {
  display: grid;
  grid-template-columns: minmax(16rem, 0.72fr) minmax(0, 1.28fr);
  padding: clamp(2rem, 6vw, 5rem) var(--page-gutter) clamp(4rem, 9vw, 8rem);
}

.system__summary,
.system__workers {
  min-height: 30rem;
  border: 1px solid var(--line-strong);
  padding: clamp(1.25rem, 3vw, 2rem);
}

.system__summary {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  background: var(--accent-surface);
}

.system__eyebrow,
.system__worker-code {
  margin: 0;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.system__summary h2 {
  margin: 1rem 0;
  font: 500 clamp(2.5rem, 5vw, 5rem) / 0.9 var(--font-display);
  letter-spacing: -0.05em;
}

.system__facts {
  display: grid;
  gap: 1rem;
  margin: 0;
}

.system__facts div {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  border-top: 1px solid currentColor;
  padding-top: 0.65rem;
}

.system__facts dt,
.system__facts dd {
  margin: 0;
}

.system__facts dt,
.system__facts dd,
.system__worker-heading span,
.system__worker-heartbeat {
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.system__facts dd {
  text-align: right;
}

.system__workers {
  display: grid;
  align-content: start;
  gap: 0.8rem;
  border-left: 0;
  background: var(--surface);
}

.system__worker {
  border: 1px solid var(--line-strong);
  padding: 1rem;
  background: var(--paper);
}

.system__worker-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
}

.system__worker-heading h3 {
  margin: 0;
  font: 600 1.5rem / 1 var(--font-display);
}

.system__worker-code {
  margin-top: 1.2rem;
}

.system__worker-heartbeat {
  margin: 0.7rem 0 0;
  color: var(--ink-muted);
}

.system__worker--healthy .system__worker-heading span {
  color: var(--signal);
}

.system__worker--degraded .system__worker-heading span,
.system__worker--unknown .system__worker-heading span {
  color: var(--accent-text);
}

@media (max-width: 54rem) {
  .system__workspace {
    grid-template-columns: 1fr;
  }

  .system__summary,
  .system__workers {
    min-height: auto;
  }

  .system__workers {
    border-top: 0;
    border-left: 1px solid var(--line-strong);
  }
}
</style>
