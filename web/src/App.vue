<script setup lang="ts">
import { provide, watch } from 'vue'
import { useRoute } from 'vue-router'
import { auth, authKey } from '@/auth/useAuth'

const route = useRoute()
provide(authKey, auth)

watch(
  () => route.path,
  () => {
    document.querySelector<HTMLElement>('#main-content')?.focus({ preventScroll: true })
  },
  { flush: 'post' },
)
</script>

<template>
  <RouterView />
</template>
