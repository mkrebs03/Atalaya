<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AtalayaMark from './AtalayaMark.vue'

defineProps<{
  username: string
  busy: boolean
}>()

defineEmits<{
  signOut: []
}>()

const route = useRoute()
const administrationOpen = ref(false)

function isPrimaryActive(section: 'home' | 'hosts' | 'checks' | 'alerts'): boolean {
  if (section === 'home') return route.path === '/'
  if (section === 'hosts') return route.path.startsWith('/hosts/') || route.path === '/manage'
  if (section === 'checks') return route.path.startsWith('/checks')
  return route.path.startsWith('/alerts')
}

watch(
  () => route.path,
  () => {
    administrationOpen.value = false
  },
)
</script>

<template>
  <header class="masthead">
    <div class="masthead__identity">
      <RouterLink class="masthead__brand" to="/" aria-label="Atalaya, resumen">
        <AtalayaMark />
      </RouterLink>
      <p class="masthead__privacy">Red privada Tailscale</p>
      <div class="masthead__session">
        <RouterLink class="masthead__user" to="/account">
          <span class="masthead__signal" aria-hidden="true"></span>
          <span>{{ username }}</span>
        </RouterLink>
        <button class="masthead__sign-out" type="button" :disabled="busy" @click="$emit('signOut')">
          {{ busy ? 'Saliendo…' : 'Cerrar sesión' }}
        </button>
      </div>
    </div>

    <div class="masthead__navigation-row">
      <nav class="masthead__navigation" aria-label="Principal">
        <RouterLink
          to="/"
          :class="{ 'masthead__link--active': isPrimaryActive('home') }"
          :aria-current="isPrimaryActive('home') ? 'page' : undefined"
          >Resumen</RouterLink
        >
        <RouterLink
          to="/manage"
          :class="{ 'masthead__link--active': isPrimaryActive('hosts') }"
          :aria-current="isPrimaryActive('hosts') ? 'page' : undefined"
          >Hosts</RouterLink
        >
        <RouterLink
          to="/checks"
          :class="{ 'masthead__link--active': isPrimaryActive('checks') }"
          :aria-current="isPrimaryActive('checks') ? 'page' : undefined"
          >Servicios</RouterLink
        >
        <RouterLink
          to="/alerts"
          :class="{ 'masthead__link--active': isPrimaryActive('alerts') }"
          :aria-current="isPrimaryActive('alerts') ? 'page' : undefined"
          >Alertas</RouterLink
        >
      </nav>

      <div class="masthead__administration">
        <button
          class="masthead__administration-toggle"
          type="button"
          :aria-expanded="administrationOpen"
          aria-controls="administration-menu"
          @click="administrationOpen = !administrationOpen"
        >
          Administración <span aria-hidden="true">{{ administrationOpen ? '−' : '+' }}</span>
        </button>
        <div
          v-if="administrationOpen"
          id="administration-menu"
          class="masthead__administration-menu"
        >
          <RouterLink to="/notifications" @click="administrationOpen = false"
            >Notificaciones</RouterLink
          >
          <RouterLink to="/settings" @click="administrationOpen = false">Configuración</RouterLink>
          <RouterLink to="/system" @click="administrationOpen = false">Sistema</RouterLink>
          <RouterLink to="/account" @click="administrationOpen = false">Cuenta</RouterLink>
        </div>
      </div>
    </div>
  </header>
</template>

<style scoped>
.masthead {
  position: relative;
  z-index: 5;
  border-bottom: 1px solid var(--line-strong);
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(0.7rem);
}

.masthead__identity {
  min-height: 4.75rem;
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 2rem;
  padding-inline: var(--page-gutter);
}

.masthead__brand {
  justify-self: start;
  text-decoration: none;
}

.masthead__privacy,
.masthead__user,
.masthead__administration-toggle,
.masthead__navigation a,
.masthead__administration-menu a {
  font-family: var(--font-mono);
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.masthead__privacy {
  margin: 0;
  color: var(--accent-text);
}

.masthead__session {
  justify-self: end;
  display: flex;
  align-items: center;
  gap: 1rem;
}

.masthead__user {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ink);
  text-decoration: none;
}

.masthead__signal {
  width: 0.52rem;
  height: 0.52rem;
  border-radius: 50%;
  background: var(--signal);
  box-shadow: 0 0 0 0.2rem color-mix(in srgb, var(--signal) 20%, transparent);
}

.masthead__sign-out,
.masthead__administration-toggle {
  min-height: 2.75rem;
  border: 1px solid var(--line-strong);
  background: transparent;
  padding: 0.6rem 0.85rem;
  color: var(--ink);
  cursor: pointer;
}

.masthead__sign-out:hover:not(:disabled),
.masthead__administration-toggle:hover {
  background: var(--ink);
  color: var(--paper);
}

.masthead__navigation-row {
  min-height: 3.5rem;
  display: flex;
  align-items: stretch;
  gap: 1rem;
  border-top: 1px solid var(--line-soft);
  padding-inline: var(--page-gutter);
}

.masthead__navigation {
  display: flex;
  flex: 1;
  justify-content: center;
}

.masthead__navigation a {
  min-width: 7.5rem;
  display: grid;
  place-items: center;
  border-inline: 1px solid var(--line-soft);
  color: var(--ink);
  text-decoration: none;
  white-space: nowrap;
}

.masthead__navigation a + a {
  border-left: 0;
}

.masthead__navigation a:hover,
.masthead__navigation .masthead__link--active {
  background: var(--ink);
  color: var(--paper);
}

.masthead__administration {
  position: relative;
  display: flex;
  align-items: center;
}

.masthead__administration-toggle span {
  margin-left: 0.45rem;
  color: var(--accent-text);
  font-size: 1rem;
}

.masthead__administration-menu {
  position: absolute;
  z-index: 10;
  top: calc(100% + 0.5rem);
  right: 0;
  width: 15rem;
  display: grid;
  border: 1px solid var(--line-strong);
  background: var(--surface);
  box-shadow: 0.4rem 0.4rem 0 var(--ink);
}

.masthead__administration-menu a {
  padding: 0.85rem 1rem;
  border-bottom: 1px solid var(--line-soft);
  color: var(--ink);
  text-decoration: none;
}

.masthead__administration-menu a:last-child {
  border-bottom: 0;
}

.masthead__administration-menu a:hover,
.masthead__administration-menu .router-link-active {
  background: var(--ink);
  color: var(--paper);
}

@media (max-width: 60rem) {
  .masthead__identity {
    gap: 1rem;
  }

  .masthead__privacy {
    display: none;
  }

  .masthead__navigation-row {
    flex-wrap: wrap;
    gap: 0;
    padding-inline: 0;
  }

  .masthead__navigation {
    flex: 1 1 100%;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
  }

  .masthead__navigation a {
    min-width: 0;
    min-height: 3rem;
    border-bottom: 1px solid var(--line-soft);
  }

  .masthead__navigation a:nth-child(even) {
    border-left: 0;
  }

  .masthead__administration {
    flex: 1 1 100%;
    padding: 0.6rem var(--page-gutter);
  }

  .masthead__administration-toggle {
    width: 100%;
    display: flex;
    justify-content: space-between;
  }

  .masthead__administration-menu {
    position: static;
    width: 100%;
    margin-top: 0.6rem;
    box-shadow: none;
  }
}

@media (max-width: 38rem) {
  .masthead__identity {
    min-height: 4.25rem;
  }

  .masthead__user {
    display: none;
  }

  .masthead__sign-out {
    min-height: 2.75rem;
  }
}
</style>
