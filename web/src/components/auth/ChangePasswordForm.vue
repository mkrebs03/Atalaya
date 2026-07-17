<script setup lang="ts">
import { reactive, shallowRef, watch } from 'vue'

const props = defineProps<{ busy: boolean; error: string; success: string }>()
const emit = defineEmits<{ submit: [currentPassword: string, newPassword: string] }>()
const fields = reactive({ current: '', next: '', confirm: '' })
const validation = shallowRef('')

function submit() {
  if (!fields.current || !fields.next) validation.value = 'Completa todos los campos.'
  else if (fields.next !== fields.confirm) validation.value = 'Las contraseñas nuevas no coinciden.'
  else validation.value = ''
  if (!validation.value) emit('submit', fields.current, fields.next)
}

watch(
  () => props.success,
  (success) => {
    if (!success) return
    fields.current = ''
    fields.next = ''
    fields.confirm = ''
  },
)
</script>

<template>
  <form class="password-form" @submit.prevent="submit">
    <p v-if="validation || error" class="form-error" role="alert">{{ validation || error }}</p>
    <p v-if="success" class="form-success" role="status">{{ success }}</p>
    <label class="field"
      >Contraseña actual<input
        v-model="fields.current"
        type="password"
        autocomplete="current-password"
        required
    /></label>
    <label class="field"
      >Nueva contraseña<input
        v-model="fields.next"
        type="password"
        autocomplete="new-password"
        required
        minlength="12"
    /></label>
    <label class="field"
      >Confirmar nueva contraseña<input
        v-model="fields.confirm"
        type="password"
        autocomplete="new-password"
        required
        minlength="12"
    /></label>
    <button class="primary-button" type="submit" :disabled="busy">
      {{ busy ? 'Actualizando…' : 'Cambiar contraseña' }}
    </button>
  </form>
</template>

<style scoped>
.password-form {
  display: grid;
  gap: 1.1rem;
  max-width: 34rem;
}
.field {
  display: grid;
  gap: 0.45rem;
  font-weight: 700;
}
.field input {
  min-height: 3rem;
  border: 2px solid var(--line-strong);
  background: white;
  padding: 0.7rem;
  font: inherit;
}
.form-error,
.form-success {
  margin: 0;
  padding: 0.75rem;
  border-left: 0.35rem solid;
}
.form-error {
  border-color: var(--accent-text);
  color: var(--accent-text);
}
.form-success {
  border-color: var(--signal);
}
</style>
