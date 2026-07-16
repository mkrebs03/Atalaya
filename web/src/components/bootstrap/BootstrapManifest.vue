<template>
  <section class="manifest" aria-labelledby="manifest-title">
    <div class="manifest__intro">
      <p class="kicker">Manifiesto de arranque</p>
      <h2 id="manifest-title" class="manifest__title">La base está lista para recibir señal.</h2>
    </div>
    <dl class="manifest__list">
      <div class="manifest__row">
        <dt>Interfaz</dt>
        <dd>Vue 3 · TypeScript estricto · Router</dd>
      </div>
      <div class="manifest__row">
        <dt>Servidor</dt>
        <dd>Go · assets embebidos · cierre controlado</dd>
      </div>
      <div class="manifest__row">
        <dt>Verificación</dt>
        <dd>Vitest · Playwright · lint reproducible</dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.manifest {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: clamp(3rem, 8vw, 8rem);
  padding: clamp(4rem, 9vw, 8rem) clamp(1rem, 4vw, 4rem);
  background: var(--ink);
  color: var(--paper);
}

.kicker {
  margin: 0 0 1.4rem;
  color: var(--accent-on-dark);
  font-family: var(--font-mono);
  font-size: clamp(0.65rem, 1.2vw, 0.78rem);
  font-weight: 800;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.manifest__title {
  max-width: 12ch;
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(2.75rem, 5.5vw, 5.5rem);
  font-weight: 500;
  letter-spacing: -0.05em;
  line-height: 0.92;
}

.manifest__list {
  align-self: end;
  margin: 0;
}

.manifest__row {
  display: grid;
  grid-template-columns: 7rem 1fr;
  gap: 1.5rem;
  padding: 1.25rem 0;
  border-top: 1px solid color-mix(in srgb, var(--paper) 30%, transparent);
}

.manifest__row:last-child {
  border-bottom: 1px solid color-mix(in srgb, var(--paper) 30%, transparent);
}

.manifest__row dt {
  color: var(--accent-surface);
  font-family: var(--font-mono);
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.manifest__row dd {
  margin: 0;
  font-size: 0.98rem;
  line-height: 1.5;
}

@media (max-width: 60rem) {
  .manifest {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 38rem) {
  .manifest__row {
    grid-template-columns: 1fr;
    gap: 0.4rem;
  }
}
</style>
