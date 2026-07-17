<script setup lang="ts">
import { shallowRef } from 'vue'
import { APIError } from '@/auth/api'
import { useAuth } from '@/auth/useAuth'
import ChangePasswordForm from '@/components/auth/ChangePasswordForm.vue'
const auth = useAuth()
const busy = shallowRef(false)
const error = shallowRef('')
const success = shallowRef('')
async function submit(currentPassword: string, newPassword: string) {
  busy.value = true
  error.value = ''
  success.value = ''
  try {
    await auth.updatePassword(currentPassword, newPassword)
    success.value = 'Contraseña actualizada. Las demás sesiones fueron cerradas.'
  } catch (reason) {
    if (reason instanceof APIError && reason.code === 'current_password_invalid')
      error.value = 'La contraseña actual no es correcta.'
    else if (reason instanceof APIError && reason.status === 401) error.value = ''
    else error.value = 'No se pudo cambiar la contraseña.'
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <main id="main-content" class="account-page" tabindex="-1">
    <p class="eyebrow">Cuenta</p>
    <h1>{{ auth.administrator.value?.username }}</h1>
    <h2>Cambiar contraseña</h2>
    <p>Usa al menos 12 caracteres. El cambio cerrará las demás sesiones.</p>
    <ChangePasswordForm :busy="busy" :error="error" :success="success" @submit="submit" />
  </main>
</template>
<style scoped>
.account-page {
  max-width: 58rem;
  margin: 0 auto;
  padding: clamp(2rem, 6vw, 5rem) 1rem;
}
.account-page h1 {
  font: 700 clamp(2.5rem, 7vw, 5rem)/1 var(--font-display);
  margin: 0.4rem 0 3rem;
}
.account-page h2 {
  font-size: 1.5rem;
}
.eyebrow {
  font: 700 0.8rem var(--font-mono);
  color: var(--accent-text);
  text-transform: uppercase;
}
</style>
