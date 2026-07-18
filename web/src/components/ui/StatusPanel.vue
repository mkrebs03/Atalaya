<script setup lang="ts">
withDefaults(
  defineProps<{
    index: string
    label: string
    title: string
    copy: string
    tag: string
    tone?: 'light' | 'dark' | 'accent'
  }>(),
  {
    tone: 'light',
  },
)
</script>

<template>
  <article class="status-panel" :class="`status-panel--${tone}`">
    <div class="status-panel__topline">
      <span>{{ index }}</span>
      <span>{{ label }}</span>
    </div>
    <h2 class="status-panel__title">{{ title }}</h2>
    <p class="status-panel__copy">{{ copy }}</p>
    <p class="status-panel__tag">{{ tag }}</p>
  </article>
</template>

<style scoped>
.status-panel {
  min-height: 23rem;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--line-strong);
  padding: clamp(1.25rem, 3vw, 2.25rem);
  background: var(--surface);
  color: var(--ink);
}

.status-panel--dark {
  background: var(--ink);
  color: var(--paper);
}

.status-panel--accent {
  background: var(--accent-surface);
}

.status-panel__topline {
  display: flex;
  justify-content: space-between;
  padding-bottom: 0.8rem;
  border-bottom: 1px solid currentColor;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.status-panel__title {
  max-width: 8ch;
  margin: 3.5rem 0 1.25rem;
  font: 500 clamp(2.4rem, 4vw, 4.1rem) / 0.88 var(--font-display);
  letter-spacing: -0.05em;
}

.status-panel__copy {
  max-width: 30rem;
  margin: 0;
  color: currentColor;
  font-size: 0.95rem;
  line-height: 1.65;
  opacity: 0.78;
}

.status-panel--accent .status-panel__copy {
  opacity: 1;
}

.status-panel__tag {
  margin: auto 0 0;
  font: 800 0.68rem var(--font-mono);
  letter-spacing: 0.14em;
}

@media (max-width: 60rem) {
  .status-panel {
    min-height: 19rem;
  }

  .status-panel__title {
    margin-top: 2.75rem;
  }
}
</style>
