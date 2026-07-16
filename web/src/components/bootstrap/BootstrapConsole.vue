<script setup lang="ts">
import BootstrapHeader from './BootstrapHeader.vue'
import BootstrapHero from './BootstrapHero.vue'
import BootstrapManifest from './BootstrapManifest.vue'
import ReadinessGrid from './ReadinessGrid.vue'
</script>

<template>
  <div class="console-shell">
    <BootstrapHeader />

    <main id="main-content" class="bootstrap-main" tabindex="-1">
      <BootstrapHero />
      <ReadinessGrid />
      <BootstrapManifest />
    </main>

    <footer class="footer">
      <p>ATALAYA / PRIVATE OBSERVABILITY</p>
      <p>GO 1.26.5 · NODE 24.18.0 · NPM 11.16.0</p>
    </footer>
  </div>
</template>

<style scoped>
.console-shell {
  min-height: 100svh;
  overflow-x: hidden;
}

.bootstrap-main {
  width: min(100%, 100rem);
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}

.footer {
  display: flex;
  justify-content: space-between;
  gap: 2rem;
  border-top: 1px solid color-mix(in srgb, var(--paper) 25%, transparent);
  padding: 1.5rem clamp(1rem, 4vw, 4.5rem);
  background: var(--ink);
  color: var(--paper);
  font-family: var(--font-mono);
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.1em;
}

.footer p {
  margin: 0;
}

@media (max-width: 38rem) {
  .footer {
    flex-direction: column;
    gap: 0.6rem;
  }
}
</style>
