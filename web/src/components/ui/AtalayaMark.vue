<script setup lang="ts">
withDefaults(
  defineProps<{
    inverse?: boolean
    compact?: boolean
  }>(),
  {
    inverse: false,
    compact: false,
  },
)
</script>

<template>
  <span class="atalaya-mark" :class="{ 'atalaya-mark--inverse': inverse }">
    <span class="atalaya-mark__tower" aria-hidden="true"><i></i><i></i><i></i></span>
    <span v-if="!compact" class="atalaya-mark__name">Atalaya</span>
  </span>
</template>

<style scoped>
.atalaya-mark {
  display: inline-flex;
  align-items: center;
  gap: 0.75rem;
  color: var(--ink);
}

.atalaya-mark--inverse {
  color: var(--paper);
}

.atalaya-mark__tower {
  width: 1.45rem;
  height: 1.55rem;
  display: grid;
  place-items: center;
  align-content: space-between;
}

.atalaya-mark__tower i {
  height: 0.22rem;
  display: block;
  background: var(--accent-surface);
}

.atalaya-mark__tower i:nth-child(1) {
  width: 100%;
}

.atalaya-mark__tower i:nth-child(2) {
  width: 68%;
}

.atalaya-mark__tower i:nth-child(3) {
  width: 35%;
  height: 0.6rem;
}

.atalaya-mark__name {
  font-family: var(--font-mono);
  font-size: 0.9rem;
  font-weight: 900;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}
</style>
