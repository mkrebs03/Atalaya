<template>
  <main id="main-content" class="page" tabindex="-1">
    <p class="eyebrow">Fase 1</p>
    <h1>Servidor preparado.</h1>
    <p class="lede">
      Persistencia cifrada y autenticación local están operativas. La incorporación de hosts
      comienza en la fase siguiente.
    </p>
    <section class="status-card">
      <span class="status-dot" aria-hidden="true"></span>
      <div>
        <strong>Acceso protegido</strong>
        <p>Sesión activa y rutas privadas habilitadas.</p>
      </div>
    </section>
  </main>
</template>
<style scoped>
.page {
  max-width: 70rem;
  margin: 0 auto;
  padding: clamp(2rem, 7vw, 6rem) clamp(1rem, 4vw, 4rem);
}
.page h1 {
  font: 700 clamp(2.8rem, 9vw, 6rem)/0.9 var(--font-display);
  margin: 0.4rem 0 1.5rem;
}
.eyebrow {
  font: 700 0.8rem var(--font-mono);
  color: var(--accent-text);
  text-transform: uppercase;
}
.lede {
  max-width: 45rem;
  font-size: 1.2rem;
  line-height: 1.6;
}
.status-card {
  margin-top: 3rem;
  display: flex;
  gap: 1rem;
  padding: 1.3rem;
  border: 2px solid var(--line-strong);
  background: var(--surface);
}
.status-card p {
  margin: 0.35rem 0 0;
}
.status-dot {
  width: 0.8rem;
  height: 0.8rem;
  margin-top: 0.25rem;
  border-radius: 50%;
  background: var(--signal);
}
</style>
