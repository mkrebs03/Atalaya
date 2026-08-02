<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/auth/useAuth'
import AppMasthead from '@/components/ui/AppMasthead.vue'
import { listAlertRules, listIncidents, type Incident } from '@/alerts/api'
import { useHostPolling } from '@/composables/useHostPolling'

const auth = useAuth()
const router = useRouter()
const busy = shallowRef(false)
const incidentPolling = useHostPolling(async (signal) => {
  const [incidentsResult, rulesResult] = await Promise.all([
    listIncidents(signal, 'open'),
    listAlertRules(signal),
  ])
  if (!incidentsResult.data || !rulesResult.data) throw new Error('alert state missing')
  return { incidents: incidentsResult.data.items ?? [], rules: rulesResult.data.items ?? [] }
})
const openIncidents = computed<readonly Incident[]>(
  () => incidentPolling.data.value?.incidents ?? [],
)
const pendingRules = computed(
  () =>
    incidentPolling.data.value?.rules.filter((rule) => rule.evaluation?.state === 'pending') ?? [],
)
const criticalIncidentCount = computed(
  () => openIncidents.value.filter((incident) => incident.severity === 'critical').length,
)
const warningIncidentCount = computed(
  () => openIncidents.value.filter((incident) => incident.severity === 'warning').length,
)
const incidentTone = computed<'critical' | 'warning'>(() =>
  criticalIncidentCount.value > 0 ? 'critical' : 'warning',
)
const incidentSummary = computed(() => {
  const parts: string[] = []
  if (criticalIncidentCount.value)
    parts.push(
      `${criticalIncidentCount.value} alerta${criticalIncidentCount.value === 1 ? '' : 's'} crítica${criticalIncidentCount.value === 1 ? '' : 's'}`,
    )
  if (warningIncidentCount.value)
    parts.push(
      `${warningIncidentCount.value} advertencia${warningIncidentCount.value === 1 ? '' : 's'}`,
    )
  if (pendingRules.value.length) parts.push(`${pendingRules.value.length} pendientes`)
  return `${parts.join(' · ')} requieren atención`
})

watch(auth.isAuthenticated, (authenticated) => {
  if (!authenticated) void router.replace({ name: 'login' })
})

async function signOut() {
  busy.value = true
  try {
    await auth.signOut()
  } catch {
    auth.clear('No se pudo cerrar la sesión limpiamente.')
  } finally {
    busy.value = false
    await router.replace({ name: 'login' })
  }
}
</script>

<template>
  <a class="skip-link" href="#main-content">Saltar al contenido</a>
  <div class="authenticated-shell">
    <AppMasthead
      :username="auth.administrator.value?.username ?? ''"
      :busy="busy"
      @sign-out="signOut"
    />
    <p v-if="auth.alert.value" class="global-alert" role="alert">
      <span>Alerta de sesión</span>{{ auth.alert.value }}
    </p>
    <RouterLink
      v-if="openIncidents.length || pendingRules.length"
      class="incident-alert"
      :class="`incident-alert--${incidentTone}`"
      to="/alerts"
      role="status"
      aria-live="polite"
    >
      <span>{{ incidentTone === 'critical' ? 'Atención crítica' : 'Atención' }}</span
      >{{ incidentSummary }}
    </RouterLink>
    <RouterView />
    <footer class="shell-footer">
      <p>Atalaya · monitorización privada</p>
      <p>Datos protegidos dentro de tu red Tailscale</p>
    </footer>
  </div>
</template>

<style scoped>
.skip-link {
  position: fixed;
  z-index: 20;
  top: 0.75rem;
  left: 0.75rem;
  background: var(--ink);
  color: var(--paper);
  padding: 0.7rem 1rem;
  transform: translateY(-180%);
  font: 800 0.72rem var(--font-mono);
  text-decoration: none;
}
.skip-link:focus {
  transform: translateY(0);
}
.authenticated-shell {
  min-height: 100svh;
  display: flex;
  flex-direction: column;
}
.global-alert {
  width: min(calc(100% - 2rem), 70rem);
  margin: 1rem auto 0;
  padding: 0.9rem 1rem;
  border: 1px solid var(--accent-text);
  background: var(--surface);
  color: var(--accent-text);
}
.global-alert span {
  margin-right: 0.75rem;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.incident-alert {
  display: block;
  width: min(calc(100% - 2rem), 70rem);
  margin: 1rem auto 0;
  border: 1px solid var(--accent-text);
  padding: 0.9rem 1rem;
  background: var(--accent-surface);
  color: var(--ink);
  text-decoration: none;
}
.incident-alert--critical {
  border-color: var(--accent-text);
  background: color-mix(in srgb, var(--accent-surface) 20%, var(--paper));
}
.incident-alert--warning {
  --incident-warning: #8a5a00;
  border-color: var(--incident-warning);
  background: color-mix(in srgb, #e5b94d 20%, var(--paper));
}
.incident-alert span {
  margin-right: 0.75rem;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.incident-alert--warning span {
  color: var(--incident-warning);
}
.shell-footer {
  display: flex;
  justify-content: space-between;
  gap: 2rem;
  margin-top: auto;
  border-top: 1px solid color-mix(in srgb, var(--paper) 25%, transparent);
  padding: 1.35rem var(--page-gutter);
  background: var(--ink);
  color: var(--paper);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.shell-footer p {
  margin: 0;
}
@media (max-width: 38rem) {
  .global-alert span {
    display: block;
    margin: 0 0 0.35rem;
  }
  .shell-footer {
    flex-direction: column;
    gap: 0.5rem;
  }
}
</style>
