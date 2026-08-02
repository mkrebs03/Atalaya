<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <main id="main-content" class="not-found route-view" tabindex="-1">
    <div class="not-found__frame">
      <p class="not-found__code" aria-hidden="true">404</p>
      <p class="not-found__eyebrow">Coordenada fuera de alcance</p>
      <h1 class="not-found__title">Esta ruta no está en el mapa.</h1>
      <p class="not-found__copy">
        La dirección solicitada no corresponde a una vista disponible en Atalaya.
      </p>
      <RouterLink class="not-found__link" to="/">Volver al resumen</RouterLink>
    </div>
  </main>
</template>

<style scoped>
.not-found {
  min-height: calc(100svh - 8rem);
  display: grid;
  place-items: center;
  padding: clamp(1.25rem, 5vw, 5rem);
}

.not-found__frame {
  width: min(100%, 56rem);
  border: 1px solid var(--line-strong);
  padding: clamp(1.5rem, 6vw, 5rem);
  position: relative;
  background: var(--surface);
  box-shadow: 0.75rem 0.75rem 0 var(--ink);
}

.not-found__code {
  margin: 0;
  color: var(--accent-surface);
  font-family: var(--font-mono);
  font-size: clamp(5rem, 18vw, 11rem);
  font-weight: 800;
  letter-spacing: -0.08em;
  line-height: 0.72;
}

.not-found__eyebrow {
  margin: 2.5rem 0 0.75rem;
  font-family: var(--font-mono);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.not-found__title {
  max-width: 12ch;
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(2.25rem, 7vw, 5rem);
  font-weight: 600;
  letter-spacing: -0.045em;
  line-height: 0.94;
}

.not-found__copy {
  max-width: 38rem;
  margin: 1.5rem 0 2rem;
  color: var(--ink-muted);
  font-size: 1.05rem;
  line-height: 1.7;
}

.not-found__link {
  display: inline-block;
  border-bottom: 2px solid var(--accent-text);
  color: var(--ink);
  font-family: var(--font-mono);
  font-size: 0.78rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-decoration: none;
  text-transform: uppercase;
}

@media (max-width: 36rem) {
  .not-found__frame {
    box-shadow: 0.4rem 0.4rem 0 var(--ink);
  }
}
</style>
