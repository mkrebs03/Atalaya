<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <a class="skip-link" href="#main-content">Saltar al contenido</a>

  <header class="masthead" aria-label="Cabecera de Atalaya">
    <RouterLink class="wordmark" to="/" aria-label="Atalaya, inicio">
      <span class="wordmark__tower" aria-hidden="true"> <i></i><i></i><i></i> </span>
      <span>ATALAYA</span>
    </RouterLink>
    <p class="masthead__phase">Fase 00 / Base reproducible</p>
    <p class="system-state"><span aria-hidden="true"></span> Bootstrap operativo</p>
  </header>
</template>

<style scoped>
.skip-link {
  position: fixed;
  z-index: 20;
  top: 0.75rem;
  left: 0.75rem;
  padding: 0.7rem 1rem;
  transform: translateY(-180%);
  background: var(--ink);
  color: var(--paper);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  text-decoration: none;
}

.skip-link:focus {
  transform: translateY(0);
}

.masthead {
  min-height: 5.25rem;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 1.5rem;
  border-bottom: 1px solid var(--line-strong);
  padding: 0 clamp(1rem, 4vw, 4.5rem);
}

.wordmark {
  justify-self: start;
  display: inline-flex;
  align-items: center;
  gap: 0.75rem;
  color: var(--ink);
  font-family: var(--font-mono);
  font-size: 0.9rem;
  font-weight: 900;
  letter-spacing: 0.18em;
  text-decoration: none;
}

.wordmark__tower {
  width: 1.45rem;
  height: 1.55rem;
  display: grid;
  place-items: center;
  align-content: space-between;
}

.wordmark__tower i {
  height: 0.22rem;
  display: block;
  background: var(--accent-surface);
}

.wordmark__tower i:nth-child(1) {
  width: 100%;
}

.wordmark__tower i:nth-child(2) {
  width: 68%;
}

.wordmark__tower i:nth-child(3) {
  width: 35%;
  height: 0.6rem;
}

.masthead__phase,
.system-state {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 0.65rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.masthead__phase {
  justify-self: center;
  color: var(--ink-muted);
}

.system-state {
  justify-self: end;
  display: flex;
  align-items: center;
  gap: 0.55rem;
}

.system-state span {
  width: 0.55rem;
  height: 0.55rem;
  border-radius: 50%;
  background: var(--signal);
  box-shadow: 0 0 0 0.2rem color-mix(in srgb, var(--signal) 20%, transparent);
}

@media (max-width: 60rem) {
  .masthead {
    grid-template-columns: 1fr auto;
  }

  .masthead__phase {
    display: none;
  }
}

@media (max-width: 38rem) {
  .masthead {
    min-height: 4.5rem;
    padding-inline: 1rem;
  }

  .system-state {
    font-size: 0.56rem;
    letter-spacing: 0.06em;
  }
}
</style>
