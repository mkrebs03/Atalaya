<script setup lang="ts">
import AtalayaMark from './AtalayaMark.vue'

defineProps<{
  username: string
  busy: boolean
}>()

defineEmits<{
  signOut: []
}>()
</script>

<template>
  <header class="masthead">
    <div class="masthead__identity">
      <RouterLink class="masthead__brand" to="/" aria-label="Atalaya, resumen">
        <AtalayaMark />
      </RouterLink>
      <p class="masthead__phase">Fase 05 <span>/</span> Alertas y entregas</p>
      <div class="masthead__session">
        <p class="masthead__user">
          <span class="masthead__signal" aria-hidden="true"></span>
          <span>{{ username }}</span>
        </p>
        <button class="masthead__sign-out" type="button" :disabled="busy" @click="$emit('signOut')">
          {{ busy ? 'Saliendo…' : 'Cerrar sesión' }}
        </button>
      </div>
    </div>

    <div class="masthead__navigation-row">
      <p class="masthead__section-label">Puesto de observación</p>
      <nav class="masthead__navigation" aria-label="Principal">
        <RouterLink to="/">Resumen</RouterLink>
        <RouterLink to="/checks">Checks</RouterLink>
        <RouterLink to="/manage">Gestionar</RouterLink>
        <RouterLink to="/settings">Configurar</RouterLink>
        <RouterLink to="/alerts">Alertas</RouterLink>
        <RouterLink to="/notifications">Notificar</RouterLink>
        <RouterLink to="/account">Cuenta</RouterLink>
      </nav>
      <p class="masthead__privacy">Tailnet / privado</p>
    </div>
  </header>
</template>

<style scoped>
.masthead {
  position: relative;
  z-index: 5;
  border-bottom: 1px solid var(--line-strong);
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(0.7rem);
}

.masthead__identity,
.masthead__navigation-row {
  min-height: 4.75rem;
  display: grid;
  grid-template-columns: minmax(13rem, 1fr) auto minmax(13rem, 1fr);
  align-items: center;
  gap: 2rem;
  padding-inline: var(--page-gutter);
}

.masthead__brand {
  justify-self: start;
  text-decoration: none;
}

.masthead__phase,
.masthead__privacy,
.masthead__section-label,
.masthead__user {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 0.64rem;
  font-weight: 800;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.masthead__phase {
  justify-self: center;
  color: var(--ink-muted);
}

.masthead__phase span {
  margin-inline: 0.35rem;
  color: var(--accent-text);
}

.masthead__session {
  justify-self: end;
  display: flex;
  align-items: center;
  gap: 1rem;
}

.masthead__user {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.masthead__signal {
  width: 0.52rem;
  height: 0.52rem;
  border-radius: 50%;
  background: var(--signal);
  box-shadow: 0 0 0 0.2rem color-mix(in srgb, var(--signal) 20%, transparent);
}

.masthead__sign-out {
  min-height: 2.5rem;
  border: 1px solid var(--line-strong);
  background: transparent;
  padding: 0.55rem 0.8rem;
  color: var(--ink);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  cursor: pointer;
}

.masthead__sign-out:hover:not(:disabled) {
  background: var(--ink);
  color: var(--paper);
}

.masthead__navigation-row {
  min-height: 3.25rem;
  border-top: 1px solid var(--line-soft);
}

.masthead__section-label {
  justify-self: start;
  color: var(--ink-muted);
}

.masthead__navigation {
  min-width: 0;
  justify-self: center;
  align-self: stretch;
  display: flex;
  align-items: stretch;
}

.masthead__navigation a {
  min-width: 7.5rem;
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  border-inline: 1px solid var(--line-soft);
  color: var(--ink);
  font-family: var(--font-mono);
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-decoration: none;
  text-transform: uppercase;
  white-space: nowrap;
}

.masthead__navigation a + a {
  border-left: 0;
}

.masthead__navigation a:hover,
.masthead__navigation .router-link-exact-active {
  background: var(--ink);
  color: var(--paper);
}

.masthead__privacy {
  justify-self: end;
  color: var(--accent-text);
}

@media (max-width: 56rem) {
  .masthead__identity,
  .masthead__navigation-row {
    grid-template-columns: 1fr auto;
    gap: 1rem;
  }

  .masthead__phase,
  .masthead__section-label,
  .masthead__privacy {
    display: none;
  }

  .masthead__navigation {
    grid-column: 1 / -1;
    justify-self: stretch;
    width: 100%;
    max-width: 100%;
    overflow-x: auto;
    justify-content: flex-start;
    scrollbar-width: thin;
  }

  .masthead__navigation a {
    min-width: max-content;
  }
}

@media (max-width: 38rem) {
  .masthead__identity {
    min-height: 4.25rem;
  }

  .masthead__navigation-row {
    min-height: 3rem;
    padding-inline: 0;
  }

  .masthead__user {
    display: none;
  }

  .masthead__sign-out {
    min-height: 2.75rem;
  }
}
</style>
