<script setup lang="ts">
import { shallowRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { APIError } from '@/auth/api'
import { useAuth } from '@/auth/useAuth'
import LoginForm from '@/components/auth/LoginForm.vue'
import AtalayaMark from '@/components/ui/AtalayaMark.vue'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const busy = shallowRef(false)
const error = shallowRef('')
function redirectTarget() {
  const value = route.query.redirect
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '/'
}
async function submit(username: string, password: string) {
  busy.value = true
  error.value = ''
  try {
    await auth.signIn(username, password)
    await router.replace(redirectTarget())
  } catch (reason) {
    error.value =
      reason instanceof APIError && reason.code === 'invalid_credentials'
        ? 'Usuario o contraseña incorrectos.'
        : 'No fue posible iniciar sesión.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main id="main-content" class="login route-view" tabindex="-1">
    <section class="login__story" aria-labelledby="login-title">
      <div class="login__masthead">
        <AtalayaMark />
        <p>Monitorización privada</p>
      </div>
      <div class="login__intro">
        <p class="login__kicker">Atalaya</p>
        <h1 id="login-title" class="login__title">Observa tu infraestructura.</h1>
        <p class="login__copy">
          Inicia sesión para consultar el estado de tus hosts y servicios desde tu red privada.
        </p>
      </div>
      <p class="login__classification">Acceso local protegido</p>
    </section>

    <section class="login__access" aria-label="Acceso a Atalaya">
      <div class="login__access-header"><span>Acceso seguro</span><span>Cuenta local</span></div>
      <div class="login__form-panel">
        <p class="login__eyebrow">Administración local</p>
        <h2 class="login__form-title">Iniciar sesión</h2>
        <p class="login__form-copy">Tu sesión se mantiene de forma segura en este navegador.</p>
        <LoginForm :busy="busy" :error="error" @submit="submit" />
      </div>
      <div class="login__access-footer">
        <span>Sesión protegida</span><span>Red privada Tailscale</span>
      </div>
    </section>
  </main>
</template>
<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(25rem, 0.75fr);
  background: var(--paper);
}
.login__story {
  min-height: 100svh;
  display: grid;
  grid-template-columns: 1fr;
  grid-template-rows: auto 1fr auto;
  position: relative;
  border-right: 1px solid var(--line-strong);
}
.login__story::after {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 0;
  opacity: 0.28;
  background-image: repeating-linear-gradient(
    0deg,
    transparent 0,
    transparent 3.9rem,
    var(--line-soft) 4rem
  );
  pointer-events: none;
}
.login__masthead,
.login__intro,
.login__classification,
.login__index {
  position: relative;
  z-index: 1;
}
.login__masthead {
  grid-column: 1 / -1;
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 5.25rem;
  padding-inline: var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.login__masthead p,
.login__classification {
  margin: 0;
  color: var(--ink-muted);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.login__index {
  display: flex;
  align-items: flex-end;
  border-right: 1px solid var(--line-soft);
  padding: 0 1rem 2rem;
  color: var(--accent-surface);
  font: 900 clamp(5rem, 10vw, 10rem) / 0.72 var(--font-mono);
  letter-spacing: -0.12em;
  writing-mode: vertical-rl;
  transform: rotate(180deg);
}
.login__intro {
  align-self: center;
  padding: clamp(4rem, 8vw, 8rem) clamp(1.5rem, 6vw, 6rem);
}
.login__kicker,
.login__eyebrow {
  margin: 0 0 1.2rem;
  color: var(--accent-text);
  font: 800 0.7rem var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.login__title {
  max-width: 11ch;
  margin: 0;
  font: 600 clamp(3.5rem, 6.8vw, 7.5rem) / 0.84 var(--font-display);
  letter-spacing: -0.06em;
  overflow-wrap: anywhere;
}
.login__copy {
  max-width: 38rem;
  margin: clamp(2rem, 4vw, 3.5rem) 0 0 auto;
  padding-left: clamp(1rem, 4vw, 3.5rem);
  border-left: 1px solid var(--line-strong);
  color: var(--ink-muted);
  font-size: clamp(1rem, 1.5vw, 1.15rem);
  line-height: 1.75;
}
.login__classification {
  grid-column: 1 / -1;
  padding: 1.25rem var(--page-gutter);
  border-top: 1px solid var(--line-soft);
}
.login__access {
  min-height: 100svh;
  display: flex;
  flex-direction: column;
  padding: clamp(1.25rem, 3vw, 2.25rem);
  background: var(--ink);
  color: var(--paper);
}
.login__access-header,
.login__access-footer {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding-bottom: 0.8rem;
  border-bottom: 1px solid color-mix(in srgb, var(--paper) 30%, transparent);
  font: 800 0.62rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.login__form-panel {
  width: min(100%, 30rem);
  margin: auto;
  padding-block: clamp(3rem, 8vh, 6rem);
}
.login__eyebrow {
  color: var(--accent-on-dark);
}
.login__form-title {
  max-width: 8ch;
  margin: 0;
  font: 500 clamp(3rem, 5vw, 5.2rem) / 0.88 var(--font-display);
  letter-spacing: -0.05em;
}
.login__form-copy {
  margin: 1.4rem 0 2rem;
  color: color-mix(in srgb, var(--paper) 72%, transparent);
  line-height: 1.65;
}
.login__access-footer {
  padding: 0.8rem 0 0;
  border-top: 1px solid color-mix(in srgb, var(--paper) 30%, transparent);
  border-bottom: 0;
  color: color-mix(in srgb, var(--paper) 70%, transparent);
  font-size: 0.55rem;
}

@media (max-width: 64rem) {
  .login {
    grid-template-columns: 1fr;
  }
  .login__story,
  .login__access {
    min-height: auto;
  }
  .login__story {
    min-height: 40rem;
    border-right: 0;
    border-bottom: 1px solid var(--line-strong);
  }
  .login__access {
    min-height: 38rem;
  }
}

@media (max-width: 38rem) {
  .login__story {
    min-height: 36rem;
    grid-template-columns: 1fr;
  }
  .login__masthead {
    min-height: 4.5rem;
  }
  .login__masthead p,
  .login__index,
  .login__classification {
    display: none;
  }
  .login__intro {
    align-self: end;
    padding: 4rem 1.25rem 3rem;
  }
  .login__title {
    font-size: clamp(2.9rem, 13vw, 4.6rem);
  }
  .login__copy {
    padding-left: 1rem;
  }
  .login__access {
    min-height: 100svh;
    padding: 1.25rem;
  }
}
</style>
