<script setup lang="ts">
import type { Filesystem } from '@/hosts/api'
import type { DeepReadonly } from 'vue'
import { formatBytes, formatPercent } from '@/hosts/format'

defineProps<{
  filesystems: readonly DeepReadonly<Filesystem>[]
  loading: boolean
  error: boolean
  editable?: boolean
  busyId?: string
}>()

const emit = defineEmits<{
  select: [filesystem: Filesystem, selected: boolean]
}>()

function onSelectionChange(filesystem: Filesystem, event: Event) {
  emit('select', filesystem, (event.target as HTMLInputElement).checked)
}
</script>

<template>
  <section class="filesystems" aria-labelledby="filesystems-title">
    <header>
      <p>Almacenamiento</p>
      <h2 id="filesystems-title">Puntos de montaje</h2>
    </header>
    <p v-if="loading" role="status">Consultando almacenamiento…</p>
    <p v-else-if="error" role="alert">No se pudo leer el almacenamiento.</p>
    <p v-else-if="filesystems.length === 0">No hay puntos de montaje observados.</p>
    <div v-else class="filesystems__content">
      <div
        class="filesystems__table"
        role="region"
        aria-label="Almacenamiento observado"
        tabindex="0"
      >
        <table>
          <thead>
            <tr>
              <th>Montaje</th>
              <th>Tipo</th>
              <th>Estado</th>
              <th v-if="editable">Monitorear</th>
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
              <td v-if="editable">
                <label class="filesystems__selection">
                  <input
                    type="checkbox"
                    :checked="filesystem.selected"
                    :disabled="busyId === filesystem.id || !filesystem.present"
                    @change="onSelectionChange(filesystem, $event)"
                  />
                  <span>{{ busyId === filesystem.id ? 'Guardando…' : 'Incluido' }}</span>
                </label>
              </td>
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
      <p class="filesystems__scroll-hint">
        En pantallas pequeñas, desliza para ver todos los datos.
      </p>
      <div class="filesystems__cards">
        <article v-for="filesystem in filesystems" :key="filesystem.id" class="filesystem-card">
          <header>
            <div>
              <strong>{{ filesystem.mount_point }}</strong>
              <small>{{ filesystem.device }} · {{ filesystem.fs_type }}</small>
            </div>
            <span>{{ filesystem.present ? 'Presente' : 'Ausente' }}</span>
          </header>
          <dl>
            <div>
              <dt>Uso</dt>
              <dd>
                {{
                  filesystem.used_bytes !== null && filesystem.total_bytes
                    ? formatPercent((filesystem.used_bytes / filesystem.total_bytes) * 100)
                    : 'Sin dato'
                }}
              </dd>
            </div>
            <div>
              <dt>Disponible</dt>
              <dd>{{ formatBytes(filesystem.available_bytes) }}</dd>
            </div>
          </dl>
          <label v-if="editable" class="filesystems__selection">
            <input
              type="checkbox"
              :checked="filesystem.selected"
              :disabled="busyId === filesystem.id || !filesystem.present"
              @change="onSelectionChange(filesystem, $event)"
            />
            <span>{{ busyId === filesystem.id ? 'Guardando…' : 'Incluir en monitoreo' }}</span>
          </label>
        </article>
      </div>
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
.filesystems__scroll-hint {
  margin: 0.6rem 0 0;
  color: var(--ink-muted);
  font-size: 0.8rem;
}
.filesystems__cards {
  display: none;
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
.filesystems__selection {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  color: var(--ink);
  font: 800 0.62rem var(--font-mono);
  white-space: nowrap;
}
.filesystems__selection input {
  min-height: 1rem;
  accent-color: var(--accent-text);
}
@media (max-width: 42rem) {
  .filesystems__table,
  .filesystems__scroll-hint {
    display: none;
  }
  .filesystems__cards {
    display: grid;
    gap: 0.75rem;
  }
  .filesystem-card {
    border: 1px solid var(--line-strong);
    padding: 1rem;
    background: var(--surface);
  }
  .filesystem-card header,
  .filesystem-card dl,
  .filesystem-card dl div {
    display: flex;
    justify-content: space-between;
    gap: 1rem;
  }
  .filesystem-card header {
    align-items: start;
    padding-bottom: 0.8rem;
    border-bottom: 1px solid var(--line-soft);
  }
  .filesystem-card header strong,
  .filesystem-card header small {
    display: block;
  }
  .filesystem-card header small {
    margin-top: 0.3rem;
    color: var(--ink-muted);
  }
  .filesystem-card header span,
  .filesystem-card dt {
    color: var(--ink-muted);
    font: 800 0.62rem var(--font-mono);
    text-transform: uppercase;
  }
  .filesystem-card dl {
    margin: 0;
    padding-block: 0.8rem;
  }
  .filesystem-card dt,
  .filesystem-card dd {
    margin: 0;
  }
  .filesystem-card .filesystems__selection {
    padding-top: 0.8rem;
    border-top: 1px solid var(--line-soft);
  }
}
</style>
