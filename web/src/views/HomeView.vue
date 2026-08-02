<script setup lang="ts">
import PageIntro from '@/components/ui/PageIntro.vue'
import HostSummaryList from '@/components/hosts/HostSummaryList.vue'
import { useHostPolling } from '@/composables/useHostPolling'
import { listAllHosts, type HostSummary } from '@/hosts/api'

const polling = useHostPolling<HostSummary[]>(async (signal) => {
  return listAllHosts(signal)
})
</script>

<template>
  <main id="main-content" class="home route-view" tabindex="-1">
    <section class="home__hero">
      <PageIntro
        eyebrow="Resumen"
        title="Estado de la infraestructura."
        copy="Consulta el estado actual de tus hosts. Los datos ausentes se muestran como desconocidos y los datos vencidos como desactualizados."
      />
      <aside class="home__meta" aria-label="Información de actualización">
        <span>Actualización automática</span><strong>Cada 30 segundos</strong> <span>Red</span
        ><strong>Tailnet privada</strong>
      </aside>
    </section>
    <HostSummaryList
      :hosts="polling.data.value ?? []"
      :loading="polling.loading.value"
      :error="polling.error.value"
      :refreshing="polling.refreshing.value"
      @retry="polling.refresh"
    />
  </main>
</template>

<style scoped>
.home {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.home__hero {
  min-height: min(28rem, calc(100svh - 8rem));
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(13rem, 0.28fr);
  align-items: center;
  border-bottom: 1px solid var(--line-strong);
}
.home__hero :deep(.page-intro) {
  padding: clamp(4rem, 9vw, 8rem) var(--page-gutter);
}
.home__meta {
  align-self: stretch;
  display: grid;
  align-content: end;
  gap: 0.25rem;
  border-left: 1px solid var(--line-soft);
  padding: 2rem 1.25rem;
  font-family: var(--font-mono);
}
.home__meta span {
  color: var(--ink-muted);
  font-size: 0.58rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.home__meta strong {
  font-size: 0.7rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
@media (max-width: 60rem) {
  .home__hero {
    grid-template-columns: 1fr;
  }
  .home__meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.4rem 0.8rem;
    border-top: 1px solid var(--line-soft);
    border-left: 0;
    padding: 1rem var(--page-gutter);
  }
  .home__meta span:nth-of-type(2) {
    margin-left: 1rem;
  }
}
@media (max-width: 42rem) {
  .home__hero {
    min-height: auto;
    align-items: start;
  }
  .home__hero :deep(.page-intro) {
    padding-block: 3.5rem 3rem;
  }
}
</style>
