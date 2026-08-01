<script setup lang="ts">
import { ref, watch } from 'vue'
import PageIntro from '@/components/ui/PageIntro.vue'
import { useHostPolling } from '@/composables/useHostPolling'
import { useAuth } from '@/auth/useAuth'
import { getRetentionSettings, updateRetentionSettings } from '@/settings/api'

const auth = useAuth()
const rawHours = ref(48)
const rollupDays = ref(90)
const checkInterval = ref(60)
const checkTimeout = ref(10)
const privateCIDRs = ref('')
const busy = ref(false)
const error = ref('')
const success = ref('')
const settings = useHostPolling(async (signal) => {
  const result = await getRetentionSettings(signal)
  if (!result.data) throw new Error('settings missing')
  return result.data
})

watch(
  () => settings.data.value,
  (value) => {
    if (!value) return
    rawHours.value = value.raw_retention_seconds / 3600
    rollupDays.value = value.rollup_retention_seconds / 86400
    checkInterval.value = value.check_interval_seconds
    checkTimeout.value = value.check_timeout_seconds
    privateCIDRs.value = value.allowed_private_cidrs.join('\n')
  },
  { immediate: true },
)

async function save() {
  if (!settings.data.value) return
  busy.value = true
  error.value = ''
  success.value = ''
  try {
    await updateRetentionSettings(auth.mutate, settings.data.value, {
      raw_retention_seconds: Math.round(rawHours.value * 3600),
      rollup_retention_seconds: Math.round(rollupDays.value * 86400),
      check_interval_seconds: Number(checkInterval.value),
      check_timeout_seconds: Number(checkTimeout.value),
      allowed_private_cidrs: privateCIDRs.value
        .split(/[\s,]+/)
        .map((value) => value.trim())
        .filter(Boolean),
    })
    success.value = 'Retención guardada. Se aplicará en el siguiente ciclo de mantenimiento.'
    settings.restart()
  } catch {
    error.value = 'No se pudo guardar. Revisa los rangos o vuelve a cargar la configuración.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main id="main-content" class="settings route-view" tabindex="-1">
    <section class="settings__intro">
      <PageIntro
        index="04"
        eyebrow="Configuración / política de señal"
        title="Conservar la señal justa."
        copy="Define retención, cadencia y redes privadas autorizadas. Los checks nunca salen a destinos públicos por defecto; los cambios de red se validan antes de cada conexión."
      />
    </section>
    <section class="settings__workspace" aria-labelledby="retention-title">
      <aside class="settings__aside">
        <p>Política de datos</p>
        <h2>Retención y egress.</h2>
        <p>
          La base almacena UTC. Los gráficos eligen automáticamente la resolución y conservan los
          huecos de recepción.
        </p>
      </aside>
      <div class="settings__panel">
        <p class="settings__eyebrow">01 / Retención</p>
        <h2 id="retention-title">Ventanas históricas</h2>
        <p v-if="settings.loading.value" role="status">Consultando configuración…</p>
        <p v-else-if="settings.error.value && !settings.data.value" role="alert">
          No pudimos leer la configuración.
        </p>
        <form v-else @submit.prevent="save">
          <label>
            Muestras crudas (horas)
            <input v-model.number="rawHours" type="number" min="24" max="720" step="1" required />
            <small>Entre 24 horas y 30 días.</small>
          </label>
          <label>
            Intervalo por defecto de checks (segundos)
            <input
              v-model.number="checkInterval"
              type="number"
              min="15"
              max="3600"
              step="1"
              required
            />
            <small>Entre 15 segundos y 1 hora. Se usa al crear un check.</small>
          </label>
          <label>
            Timeout por defecto de checks (segundos)
            <input v-model.number="checkTimeout" type="number" min="1" max="60" step="1" required />
            <small>Debe ser menor que el intervalo.</small>
          </label>
          <label>
            CIDRs privados autorizados
            <textarea v-model="privateCIDRs" rows="4" placeholder="192.168.1.0/24\n10.20.0.0/16" />
            <small>Uno por línea. RFC1918 solamente; Tailscale se permite por defecto.</small>
          </label>
          <label>
            Agregados horarios (días)
            <input v-model.number="rollupDays" type="number" min="7" max="730" step="1" required />
            <small>Debe ser igual o mayor que la retención raw.</small>
          </label>
          <p v-if="error" class="settings__message settings__message--error" role="alert">
            {{ error }}
          </p>
          <p v-if="success" class="settings__message settings__message--success" role="status">
            {{ success }}
          </p>
          <button type="submit" :disabled="busy">
            {{ busy ? 'Guardando…' : 'Guardar retención' }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>

<style scoped>
.settings {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.settings__intro {
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.settings__workspace {
  display: grid;
  grid-template-columns: minmax(16rem, 0.72fr) minmax(0, 1.28fr);
  padding: clamp(2rem, 6vw, 5rem) var(--page-gutter) clamp(4rem, 9vw, 8rem);
}
.settings__aside,
.settings__panel {
  min-height: 31rem;
  border: 1px solid var(--line-strong);
  padding: clamp(1.25rem, 3vw, 2rem);
}
.settings__aside {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  background: var(--accent-surface);
}
.settings__aside p,
.settings__eyebrow {
  margin: 0;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.settings__aside h2,
.settings__panel h2 {
  margin: 1rem 0;
  font: 500 clamp(2.5rem, 5vw, 5rem) / 0.9 var(--font-display);
  letter-spacing: -0.05em;
}
.settings__aside p:last-child {
  max-width: 28rem;
  line-height: 1.6;
}
.settings__panel {
  border-left: 0;
  background: var(--surface);
}
.settings__eyebrow {
  color: var(--accent-text);
}
.settings form {
  display: grid;
  gap: 1.5rem;
  max-width: 34rem;
  margin-top: 2.5rem;
}
.settings label {
  display: grid;
  gap: 0.45rem;
  font-weight: 800;
}
.settings input,
.settings textarea {
  min-height: 3rem;
  border: 1px solid var(--line-strong);
  padding: 0.7rem;
  background: var(--paper);
  color: var(--ink);
  font: inherit;
  resize: vertical;
}
.settings small {
  color: var(--ink-muted);
  font-weight: 400;
}
.settings button {
  justify-self: start;
  min-height: 2.9rem;
  border: 1px solid var(--ink);
  padding: 0.7rem 1rem;
  background: var(--ink);
  color: var(--paper);
  font-weight: 800;
  cursor: pointer;
}
.settings button:disabled {
  cursor: wait;
  opacity: 0.6;
}
.settings__message {
  margin: 0;
  padding: 0.8rem 1rem;
  border-left: 0.3rem solid currentColor;
}
.settings__message--error {
  color: var(--accent-text);
}
.settings__message--success {
  color: #476800;
}
@media (max-width: 54rem) {
  .settings__workspace {
    grid-template-columns: 1fr;
  }
  .settings__aside,
  .settings__panel {
    min-height: auto;
  }
  .settings__panel {
    border-top: 0;
    border-left: 1px solid var(--line-strong);
  }
}
</style>
