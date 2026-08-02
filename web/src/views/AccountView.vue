<script setup lang="ts">
import { shallowRef } from 'vue'
import { APIError } from '@/auth/api'
import { useAuth } from '@/auth/useAuth'
import ChangePasswordForm from '@/components/auth/ChangePasswordForm.vue'
import PageIntro from '@/components/ui/PageIntro.vue'
const auth = useAuth()
const busy = shallowRef(false)
const error = shallowRef('')
const success = shallowRef('')
async function submit(currentPassword: string, newPassword: string) {
  busy.value = true
  error.value = ''
  success.value = ''
  try {
    await auth.updatePassword(currentPassword, newPassword)
    success.value = 'Contraseña actualizada. Las demás sesiones fueron cerradas.'
  } catch (reason) {
    if (reason instanceof APIError && reason.code === 'current_password_invalid')
      error.value = 'La contraseña actual no es correcta.'
    else if (reason instanceof APIError && reason.status === 401) error.value = ''
    else error.value = 'No se pudo cambiar la contraseña.'
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <main id="main-content" class="account route-view" tabindex="-1">
    <section class="account__intro">
      <PageIntro
        eyebrow="Cuenta"
        :title="auth.administrator.value?.username ?? ''"
        copy="Esta instalación tiene una única cuenta de administrador. Sus credenciales permanecen dentro de Atalaya y no dependen de la identidad de Tailscale."
        compact
      />
    </section>

    <section class="account__workspace" aria-labelledby="password-title">
      <aside class="account__identity">
        <div class="account__identity-topline"><span>Cuenta</span><span>Administrador</span></div>
        <div class="account__identity-body">
          <p class="account__status"><span aria-hidden="true"></span>Sesión activa</p>
          <h2 class="account__identity-name">{{ auth.administrator.value?.username }}</h2>
          <p class="account__identity-copy">
            Cambiar la contraseña revoca las demás sesiones y mantiene esta conexión activa.
          </p>
        </div>
        <p class="account__identity-tag">Cuenta de administrador</p>
      </aside>

      <div class="account__form-panel">
        <div class="account__form-heading">
          <p>Seguridad de la cuenta</p>
          <h2 id="password-title">Cambiar contraseña</h2>
          <p>Usa al menos 12 caracteres. El cambio cerrará las demás sesiones.</p>
        </div>
        <ChangePasswordForm :busy="busy" :error="error" :success="success" @submit="submit" />
      </div>
    </section>
  </main>
</template>
<style scoped>
.account {
  width: min(100%, var(--content-max));
  margin: 0 auto;
  border-inline: 1px solid var(--line-soft);
}
.account__intro {
  padding: clamp(4rem, 8vw, 7rem) var(--page-gutter);
  border-bottom: 1px solid var(--line-strong);
}
.account__workspace {
  display: grid;
  grid-template-columns: minmax(19rem, 0.72fr) minmax(0, 1.28fr);
  padding: clamp(2rem, 6vw, 5rem) var(--page-gutter) clamp(4rem, 9vw, 8rem);
}
.account__identity,
.account__form-panel {
  min-height: 35rem;
  border: 1px solid var(--line-strong);
}
.account__identity {
  display: flex;
  flex-direction: column;
  padding: clamp(1.25rem, 3vw, 2rem);
  background: var(--accent-surface);
}
.account__identity-topline {
  display: flex;
  justify-content: space-between;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid currentColor;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.account__identity-body {
  margin-block: auto;
}
.account__status {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  margin: 0 0 2rem;
  font: 800 0.67rem var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.account__status span {
  width: 0.58rem;
  height: 0.58rem;
  border-radius: 50%;
  background: var(--signal);
  box-shadow: 0 0 0 0.2rem color-mix(in srgb, var(--signal) 25%, transparent);
}
.account__identity-name {
  margin: 0;
  overflow-wrap: anywhere;
  font: 500 clamp(3rem, 5vw, 5rem) / 0.9 var(--font-display);
  letter-spacing: -0.05em;
}
.account__identity-copy {
  max-width: 28rem;
  margin: 1.5rem 0 0;
  line-height: 1.65;
}
.account__identity-tag {
  margin: 0;
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
}
.account__form-panel {
  border-left: 0;
  padding: clamp(2rem, 5vw, 4.5rem);
  background: var(--surface);
}
.account__form-heading {
  margin-bottom: 2.5rem;
}
.account__form-heading > p:first-child {
  margin: 0 0 1rem;
  color: var(--accent-text);
  font: 800 0.68rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.account__form-heading h2 {
  margin: 0;
  font: 500 clamp(2.7rem, 4.5vw, 4.5rem) / 0.92 var(--font-display);
  letter-spacing: -0.05em;
}
.account__form-heading > p:last-child {
  max-width: 34rem;
  margin: 1.25rem 0 0;
  color: var(--ink-muted);
  line-height: 1.6;
}

@media (max-width: 54rem) {
  .account__workspace {
    grid-template-columns: 1fr;
  }
  .account__identity,
  .account__form-panel {
    min-height: auto;
  }
  .account__identity {
    min-height: 24rem;
  }
  .account__form-panel {
    border-top: 0;
    border-left: 1px solid var(--line-strong);
  }
}

@media (max-width: 38rem) {
  .account__intro {
    padding-block: 3.5rem;
  }
  .account__workspace {
    padding: 1rem 1rem 4rem;
  }
  .account__identity {
    min-height: 21rem;
  }
  .account__form-panel {
    padding: 1.5rem;
  }
}
</style>
