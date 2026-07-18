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
    <section class="home__hero" aria-labelledby="home-title">
      <PageIntro
        index="02"
        eyebrow="Estado actual"
        title="La torre recibe señal."
        copy="Lecturas privadas de CPU, memoria, almacenamiento y conectividad. Una vista operativa sin convertir la ausencia de datos en un falso cero."
      />
      <aside class="home__rail" aria-label="Cadencia de actualización">
        <span>Actualización</span><strong>30 segundos</strong><span>Alcance</span
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
  min-height: min(38rem, calc(100svh - 8rem));
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(8rem, 0.16fr);
  align-items: center;
  border-bottom: 1px solid var(--line-strong);
}
.home__hero :deep(.page-intro) {
  padding: clamp(4rem, 9vw, 8rem) var(--page-gutter);
}
.home__rail {
  align-self: stretch;
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
.home__rail span {
  color: var(--ink-muted);
  font-size: 0.58rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.home__rail strong {
  font-size: 0.7rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
@media (max-width: 60rem) {
  .home__hero {
    grid-template-columns: 1fr;
  }
  .home__rail {
    display: none;
  }
}
@media (max-width: 42rem) {
  .home__hero {
    min-height: calc(100svh - 7.25rem);
    align-items: end;
  }
  .home__hero :deep(.page-intro) {
    padding-block: 5rem 3.5rem;
  }
}
</style>
