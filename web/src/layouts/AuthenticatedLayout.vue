<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/auth/useAuth'

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
  <header class="app-header">
    <RouterLink class="wordmark" to="/">ATALAYA</RouterLink>
    <nav class="navigation" aria-label="Principal">
      <RouterLink to="/">Resumen</RouterLink><RouterLink to="/account">Cuenta</RouterLink>
    </nav>
    <div class="account-actions">
      <span>{{ auth.administrator.value?.username }}</span
      ><button type="button" :disabled="busy" @click="signOut">Salir</button>
    </div>
  </header>
  <p v-if="auth.alert.value" class="global-alert" role="alert">{{ auth.alert.value }}</p>
  <RouterView />
</template>

<style scoped>
.skip-link {
  position: fixed;
  left: 1rem;
  top: -5rem;
  z-index: 10;
  background: var(--ink);
  color: white;
  padding: 0.75rem;
}
.skip-link:focus {
  top: 1rem;
}
.app-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 2rem;
  padding: 1rem clamp(1rem, 4vw, 4rem);
  border-bottom: 2px solid var(--line-strong);
  background: var(--surface);
}
.wordmark {
  color: var(--ink);
  font: 800 1.2rem var(--font-mono);
  letter-spacing: 0.13em;
  text-decoration: none;
}
.navigation,
.account-actions {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.navigation a {
  color: var(--ink);
}
.navigation .router-link-exact-active {
  text-decoration-thickness: 0.2rem;
}
.account-actions button {
  border: 1px solid var(--line-strong);
  background: transparent;
  padding: 0.55rem 0.8rem;
  font: inherit;
}
.global-alert {
  margin: 1rem;
  padding: 0.8rem;
  border: 2px solid var(--accent-text);
}
@media (max-width: 42rem) {
  .app-header {
    grid-template-columns: 1fr auto;
    gap: 0.8rem;
  }
  .navigation {
    grid-row: 2;
    grid-column: 1 / -1;
  }
  .account-actions span {
    display: none;
  }
}
</style>
