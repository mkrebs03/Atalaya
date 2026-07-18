<script setup lang="ts">
withDefaults(
  defineProps<{
    index: string
    eyebrow: string
    title: string
    copy?: string
    compact?: boolean
  }>(),
  {
    copy: '',
    compact: false,
  },
)
</script>

<template>
  <header class="page-intro" :class="{ 'page-intro--compact': compact }">
    <p class="page-intro__index" aria-hidden="true">{{ index }}</p>
    <div class="page-intro__body">
      <p class="page-intro__eyebrow">{{ eyebrow }}</p>
      <h1 class="page-intro__title">{{ title }}</h1>
      <p v-if="copy" class="page-intro__copy">{{ copy }}</p>
    </div>
  </header>
</template>

<style scoped>
.page-intro {
  display: grid;
  grid-template-columns: minmax(6rem, 0.35fr) minmax(0, 2fr);
  align-items: start;
}

.page-intro__index {
  margin: 0;
  color: var(--accent-surface);
  font: 900 clamp(4rem, 8vw, 8.5rem) / 0.72 var(--font-mono);
  letter-spacing: -0.12em;
  writing-mode: vertical-rl;
  transform: rotate(180deg);
}

.page-intro__body {
  max-width: 70rem;
}

.page-intro__eyebrow {
  margin: 0 0 1rem;
  color: var(--accent-text);
  font: 800 0.7rem var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.page-intro__title {
  max-width: 13ch;
  margin: 0;
  font: 600 clamp(3.5rem, 7vw, 7.5rem) / 0.84 var(--font-display);
  letter-spacing: -0.06em;
}

.page-intro__copy {
  max-width: 43rem;
  margin: clamp(2rem, 4vw, 3.5rem) 0 0 auto;
  padding-left: clamp(1rem, 5vw, 4rem);
  border-left: 1px solid var(--line-strong);
  color: var(--ink-muted);
  font-size: clamp(1rem, 1.6vw, 1.2rem);
  line-height: 1.75;
}

.page-intro--compact .page-intro__index {
  font-size: clamp(3.5rem, 7vw, 6.5rem);
}

.page-intro--compact .page-intro__title {
  font-size: clamp(3rem, 6vw, 5.7rem);
}

@media (max-width: 42rem) {
  .page-intro {
    grid-template-columns: 1fr;
  }

  .page-intro__index {
    display: none;
  }

  .page-intro__title,
  .page-intro--compact .page-intro__title {
    font-size: clamp(3.1rem, 15vw, 5.25rem);
  }

  .page-intro__copy {
    margin-top: 2rem;
    padding-left: 1rem;
  }
}
</style>
