<script setup lang="ts">
import { shallowRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { APIError } from '@/auth/api'
import { useAuth } from '@/auth/useAuth'
import LoginForm from '@/components/auth/LoginForm.vue'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const busy = shallowRef(false)
const error = shallowRef('')
function redirectTarget() {
  const value = route.query.redirect
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '/'
}
async function submit(username: string, password: string) {
  busy.value = true
  error.value = ''
  try {
    await auth.signIn(username, password)
    await router.replace(redirectTarget())
  } catch (reason) {
    error.value =
      reason instanceof APIError && reason.code === 'invalid_credentials'
        ? 'Usuario o contraseña incorrectos.'
        : 'No fue posible iniciar sesión.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main id="main-content" class="login-page" tabindex="-1">
    <section class="login-panel">
      <p class="eyebrow">Administración local</p>
      <h1>Acceso a Atalaya</h1>
      <p>Entra para consultar y administrar esta instancia.</p>
      <LoginForm :busy="busy" :error="error" @submit="submit" />
    </section>
  </main>
</template>
<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 1.25rem;
}
.login-panel {
  width: min(100%, 30rem);
  padding: clamp(1.5rem, 5vw, 3rem);
  border: 2px solid var(--line-strong);
  box-shadow: 0.7rem 0.7rem 0 var(--ink);
  background: var(--surface);
}
.login-panel h1 {
  font: 700 clamp(2rem, 7vw, 3.5rem)/0.95 var(--font-display);
  margin: 0.5rem 0 1rem;
}
.eyebrow {
  font: 700 0.78rem var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--accent-text);
}
</style>
