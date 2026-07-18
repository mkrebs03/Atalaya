<script setup lang="ts">
import type { Filesystem } from '@/hosts/api'
import type { DeepReadonly } from 'vue'
import { formatBytes, formatPercent } from '@/hosts/format'

defineProps<{
  filesystems: readonly DeepReadonly<Filesystem>[]
  loading: boolean
  error: boolean
}>()
</script>

<template>
  <section class="filesystems" aria-labelledby="filesystems-title">
    <header>
      <p>Almacenamiento seleccionado</p>
      <h2 id="filesystems-title">Filesystems</h2>
    </header>
    <p v-if="loading" role="status">Consultando filesystems…</p>
    <p v-else-if="error" role="alert">No se pudo leer el almacenamiento.</p>
    <p v-else-if="filesystems.length === 0">No hay filesystems observados.</p>
    <div
      v-else
      class="filesystems__table"
      role="region"
      aria-label="Filesystems observados"
      tabindex="0"
    >
      <table>
        <thead>
          <tr>
            <th>Montaje</th>
            <th>Tipo</th>
            <th>Estado</th>
            <th>Uso</th>
            <th>Disponible</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="filesystem in filesystems" :key="filesystem.id">
            <th scope="row">
              <strong>{{ filesystem.mount_point }}</strong
              ><small>{{ filesystem.device }}</small>
            </th>
            <td>{{ filesystem.fs_type }}</td>
            <td>{{ filesystem.present ? 'Presente' : 'Ausente' }}</td>
            <td>
              {{
                filesystem.used_bytes !== null && filesystem.total_bytes
                  ? formatPercent((filesystem.used_bytes / filesystem.total_bytes) * 100)
                  : 'Sin dato'
              }}
            </td>
            <td>{{ formatBytes(filesystem.available_bytes) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.filesystems {
  padding: clamp(3rem, 7vw, 6rem) var(--page-gutter);
}
.filesystems header p {
  margin: 0 0 0.75rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.filesystems h2 {
  margin: 0 0 2rem;
  font: 500 clamp(2.5rem, 5vw, 5rem)/0.9 var(--font-display);
  letter-spacing: -0.045em;
}
.filesystems__table {
  overflow-x: auto;
  border: 1px solid var(--line-strong);
  background: var(--surface);
}
.filesystems table {
  width: 100%;
  min-width: 44rem;
  border-collapse: collapse;
  text-align: left;
}
.filesystems th,
.filesystems td {
  padding: 1rem;
  border-bottom: 1px solid var(--line-soft);
}
.filesystems thead th {
  background: var(--ink);
  color: var(--paper);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.filesystems tbody th strong,
.filesystems tbody th small {
  display: block;
}
.filesystems tbody th small {
  margin-top: 0.3rem;
  color: var(--ink-muted);
  font-weight: 400;
}
</style>
