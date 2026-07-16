<script setup lang="ts">
import { watch } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

watch(
  () => route.path,
  () => {
    document.querySelector<HTMLElement>('#main-content')?.focus()
  },
  { flush: 'post' },
)
</script>

<template>
  <RouterView />
</template>
