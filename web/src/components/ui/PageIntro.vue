<script setup lang="ts">
withDefaults(
  defineProps<{
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
    <div class="page-intro__body">
      <p class="page-intro__eyebrow">{{ eyebrow }}</p>
      <h1 class="page-intro__title">{{ title }}</h1>
      <p v-if="copy" class="page-intro__copy">{{ copy }}</p>
    </div>
  </header>
</template>

<style scoped>
.page-intro {
  display: block;
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
  overflow-wrap: anywhere;
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

.page-intro--compact .page-intro__title {
  font-size: clamp(3rem, 6vw, 5.7rem);
}

@media (max-width: 42rem) {
  .page-intro__title,
  .page-intro--compact .page-intro__title {
    font-size: clamp(2.8rem, 13vw, 4.8rem);
  }

  .page-intro__copy {
    margin-top: 2rem;
    padding-left: 1rem;
  }
}
</style>
