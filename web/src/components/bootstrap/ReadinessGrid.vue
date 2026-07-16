<template>
  <section class="foundation" aria-labelledby="foundation-title">
    <div class="section-heading">
      <p class="section-heading__number">01—03</p>
      <div>
        <p class="kicker">Plano fundacional</p>
        <h2 id="foundation-title" class="section-heading__title">Tres contratos para avanzar.</h2>
      </div>
    </div>

    <ol class="foundation-grid" role="list">
      <li class="foundation-card">
        <div class="foundation-card__topline">
          <span>01</span>
          <span>Runtime</span>
        </div>
        <h3 class="foundation-card__title">Un artefacto</h3>
        <p class="foundation-card__copy">
          El build web queda embebido en Go. Node participa en la construcción, nunca en la
          operación.
        </p>
        <p class="foundation-card__tag">GO × VUE</p>
      </li>
      <li class="foundation-card foundation-card--dark">
        <div class="foundation-card__topline">
          <span>02</span>
          <span>Superficie</span>
        </div>
        <h3 class="foundation-card__title">Mismo origen</h3>
        <p class="foundation-card__copy">
          UI, API y salud comparten un perímetro sencillo, listo para endurecerse en las siguientes
          fases.
        </p>
        <p class="foundation-card__tag">/HEALTHZ</p>
      </li>
      <li class="foundation-card foundation-card--accent">
        <div class="foundation-card__topline">
          <span>03</span>
          <span>Entrega</span>
        </div>
        <h3 class="foundation-card__title">Dos arquitecturas</h3>
        <p class="foundation-card__copy">
          La misma fuente apunta a Linux amd64 y arm64 con verificaciones repetibles desde el primer
          cambio.
        </p>
        <p class="foundation-card__tag">CGO_ENABLED=0</p>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.foundation {
  padding: clamp(4rem, 9vw, 8rem) clamp(1rem, 4vw, 4rem);
  border-bottom: 1px solid var(--line-strong);
}

.section-heading {
  display: grid;
  grid-template-columns: minmax(7rem, 0.4fr) 2fr;
  align-items: end;
  margin-bottom: clamp(2rem, 5vw, 4rem);
}

.section-heading__number {
  margin: 0;
  color: var(--ink-muted);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.1em;
}

.kicker {
  margin: 0 0 1.4rem;
  color: var(--accent-text);
  font-family: var(--font-mono);
  font-size: clamp(0.65rem, 1.2vw, 0.78rem);
  font-weight: 800;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.section-heading__title {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(2.5rem, 5vw, 5.25rem);
  font-weight: 500;
  letter-spacing: -0.045em;
  line-height: 0.95;
}

.foundation-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin: 0;
  padding: 0;
  list-style: none;
}

.foundation-card {
  min-height: 25rem;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--line-strong);
  padding: clamp(1.25rem, 3vw, 2.25rem);
  background: var(--surface);
}

.foundation-card + .foundation-card {
  border-left: 0;
}

.foundation-card--dark {
  background: var(--ink);
  color: var(--paper);
}

.foundation-card--accent {
  background: var(--accent-surface);
}

.foundation-card__topline {
  display: flex;
  justify-content: space-between;
  padding-bottom: 0.8rem;
  border-bottom: 1px solid currentColor;
  font-family: var(--font-mono);
  font-size: 0.65rem;
  font-weight: 800;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.foundation-card__title {
  max-width: 7ch;
  margin: 4rem 0 1.25rem;
  font-family: var(--font-display);
  font-size: clamp(2.5rem, 4vw, 4.3rem);
  font-weight: 500;
  letter-spacing: -0.05em;
  line-height: 0.88;
}

.foundation-card__copy {
  max-width: 30rem;
  margin: 0;
  color: currentColor;
  font-size: 0.95rem;
  line-height: 1.65;
  opacity: 0.78;
}

.foundation-card--accent .foundation-card__copy {
  opacity: 1;
}

.foundation-card__tag {
  margin: auto 0 0;
  font-family: var(--font-mono);
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.14em;
}

@media (max-width: 60rem) {
  .foundation-grid {
    grid-template-columns: 1fr;
  }

  .foundation-card {
    min-height: 20rem;
  }

  .foundation-card + .foundation-card {
    border-top: 0;
    border-left: 1px solid var(--line-strong);
  }
}

@media (max-width: 38rem) {
  .section-heading {
    grid-template-columns: 1fr;
    gap: 1.5rem;
  }

  .foundation-card__title {
    margin-top: 3rem;
  }
}
</style>
