<script setup lang="ts">
import { reactive, shallowRef } from 'vue'

defineProps<{ busy: boolean; error: string }>()
const emit = defineEmits<{ submit: [username: string, password: string] }>()
const fields = reactive({ username: '', password: '' })
const validation = shallowRef('')

function submit() {
  validation.value =
    fields.username.trim() && fields.password ? '' : 'Ingresa usuario y contraseña.'
  if (!validation.value) emit('submit', fields.username, fields.password)
}
</script>

<template>
  <form class="login-form" @submit.prevent="submit">
    <p v-if="validation || error" class="form-error" role="alert">{{ validation || error }}</p>
    <label class="field"
      >Usuario<input v-model="fields.username" name="username" autocomplete="username" required
    /></label>
    <label class="field"
      >Contraseña<input
        v-model="fields.password"
        name="password"
        type="password"
        autocomplete="current-password"
        required
    /></label>
    <button class="primary-button" type="submit" :disabled="busy">
      {{ busy ? 'Ingresando…' : 'Ingresar' }}
    </button>
  </form>
</template>

<style scoped>
.login-form {
  display: grid;
  gap: 1.25rem;
}
.field {
  display: grid;
  gap: 0.45rem;
  color: var(--paper);
  font: 800 0.68rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.field input {
  min-height: 3.25rem;
  border: 1px solid color-mix(in srgb, var(--paper) 55%, transparent);
  background: var(--paper);
  color: var(--ink);
  padding: 0.75rem;
  font: 1rem var(--font-body);
  letter-spacing: normal;
  text-transform: none;
}
.login-form :deep(.primary-button) {
  border-color: var(--accent-surface);
  background: var(--accent-surface);
  color: var(--ink);
}
.login-form :deep(.primary-button:hover:not(:disabled)) {
  background: var(--paper);
}
.form-error {
  margin: 0;
  padding: 0.75rem;
  border-left: 0.35rem solid var(--accent-on-dark);
  color: var(--paper);
  line-height: 1.5;
}
</style>
