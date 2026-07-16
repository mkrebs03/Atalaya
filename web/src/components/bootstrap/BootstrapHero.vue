<template>
  <section class="hero" aria-labelledby="bootstrap-title">
    <div class="hero__index" aria-hidden="true">00</div>
    <div class="hero__content">
      <p class="kicker">Puesto de observación · infraestructura Linux</p>
      <h1 id="bootstrap-title" class="hero__title">
        La torre de vigilancia<br />
        <em>ya está en pie.</em>
      </h1>
      <p class="hero__lede">
        Atalaya inicia con una base determinista: interfaz Vue, servidor Go y controles de entrega
        preparados para convivir en un único ejecutable.
      </p>
    </div>
    <aside class="hero__rail" aria-label="Identificación del bootstrap">
      <span>BUILD CLASS</span>
      <strong>FOUNDATION</strong>
      <span>RUNTIME</span>
      <strong>SELF-CONTAINED</strong>
    </aside>
  </section>
</template>

<style scoped>
.hero {
  min-height: min(47rem, calc(100svh - 5.25rem));
  display: grid;
  grid-template-columns: minmax(8rem, 0.42fr) minmax(0, 2fr) minmax(9rem, 0.5fr);
  position: relative;
  border-bottom: 1px solid var(--line-strong);
}

.hero::after {
  content: '';
  position: absolute;
  inset: 0;
  z-index: -1;
  opacity: 0.22;
  background-image: repeating-linear-gradient(
    0deg,
    transparent 0,
    transparent 3.9rem,
    var(--line-soft) 4rem
  );
  pointer-events: none;
}

.hero__index {
  display: flex;
  align-items: flex-end;
  border-right: 1px solid var(--line-soft);
  padding: 0 1rem 2.2rem;
  color: var(--accent-surface);
  font-family: var(--font-mono);
  font-size: clamp(4.5rem, 10vw, 10rem);
  font-weight: 900;
  letter-spacing: -0.12em;
  line-height: 0.75;
  writing-mode: vertical-rl;
  transform: rotate(180deg);
}

.hero__content {
  align-self: center;
  padding: clamp(4.5rem, 10vw, 9rem) clamp(1.5rem, 6vw, 6.5rem);
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

.hero__title {
  max-width: 14ch;
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(3.5rem, 7.3vw, 8rem);
  font-weight: 600;
  letter-spacing: -0.06em;
  line-height: 0.84;
}

.hero__title em {
  color: var(--accent-text);
  font-weight: 500;
}

.hero__lede {
  max-width: 41rem;
  margin: clamp(2rem, 4vw, 3.5rem) 0 0 auto;
  padding-left: clamp(1rem, 5vw, 4rem);
  border-left: 1px solid var(--line-strong);
  color: var(--ink-muted);
  font-size: clamp(1rem, 1.6vw, 1.2rem);
  line-height: 1.75;
}

.hero__rail {
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  gap: 0.25rem;
  border-left: 1px solid var(--line-soft);
  padding: 2rem 1.25rem;
  font-family: var(--font-mono);
  writing-mode: vertical-rl;
  transform: rotate(180deg);
}

.hero__rail span {
  color: var(--ink-muted);
  font-size: 0.58rem;
  letter-spacing: 0.12em;
}

.hero__rail strong {
  font-size: 0.7rem;
  letter-spacing: 0.1em;
}

@media (max-width: 60rem) {
  .hero {
    grid-template-columns: 5.5rem 1fr;
  }

  .hero__rail {
    display: none;
  }
}

@media (max-width: 38rem) {
  .hero {
    min-height: calc(100svh - 4.5rem);
    grid-template-columns: 1fr;
  }

  .hero__index {
    display: none;
  }

  .hero__content {
    align-self: end;
    padding: 5rem 1.25rem 3.5rem;
  }

  .hero__title {
    font-size: clamp(3.15rem, 16vw, 5.25rem);
  }

  .hero__lede {
    padding-left: 1rem;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .hero__content {
    animation: reveal 640ms cubic-bezier(0.2, 0.7, 0.2, 1) both;
  }

  .hero__index,
  .hero__rail {
    animation: fade-in 640ms ease-out both;
  }

  .hero__index {
    animation-delay: 80ms;
  }

  .hero__rail {
    animation-delay: 160ms;
  }
}

@keyframes fade-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes reveal {
  from {
    opacity: 0;
    transform: translateY(1.25rem);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
