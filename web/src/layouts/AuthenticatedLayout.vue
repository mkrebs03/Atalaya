<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/auth/useAuth'
import AppMasthead from '@/components/ui/AppMasthead.vue'

const auth = useAuth()
const router = useRouter()
const busy = shallowRef(false)

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
    <RouterView />
    <footer class="shell-footer">
      <p>Atalaya / observabilidad privada</p>
      <p>Fase 02 · estado actual y gestión de hosts</p>
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
