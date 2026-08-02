<script setup lang="ts">
import { nextTick, onMounted, ref, useId, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    copy: string
    confirmLabel?: string
    cancelLabel?: string
    danger?: boolean
  }>(),
  {
    confirmLabel: 'Confirmar',
    cancelLabel: 'Cancelar',
    danger: false,
  },
)

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
const dialogID = useId()
const titleID = 'confirm-dialog-title-' + dialogID
const copyID = 'confirm-dialog-copy-' + dialogID

function syncDialog() {
  if (!dialog.value) return
  if (props.open && !dialog.value.open) {
    dialog.value.showModal()
    void nextTick(() => cancelButton.value?.focus())
  } else if (!props.open && dialog.value.open) {
    dialog.value.close()
  }
}

watch(() => props.open, syncDialog)
onMounted(syncDialog)

function cancel() {
  emit('cancel')
}
</script>

<template>
  <dialog
    ref="dialog"
    class="confirm-dialog"
    :aria-labelledby="titleID"
    :aria-describedby="copyID"
    @cancel.prevent="cancel"
    @click.self="cancel"
  >
    <div class="confirm-dialog__panel">
      <p class="confirm-dialog__eyebrow">Confirmación</p>
      <h2 :id="titleID">{{ title }}</h2>
      <p :id="copyID">{{ copy }}</p>
      <div class="confirm-dialog__actions">
        <button ref="cancelButton" type="button" @click="cancel">{{ cancelLabel }}</button>
        <button
          type="button"
          class="confirm-dialog__confirm"
          :class="{ 'confirm-dialog__confirm--danger': danger }"
          @click="emit('confirm')"
        >
          {{ confirmLabel }}
        </button>
      </div>
    </div>
  </dialog>
</template>

<style scoped>
.confirm-dialog {
  width: min(calc(100% - 2rem), 34rem);
  border: 1px solid var(--line-strong);
  padding: 0;
  background: var(--surface);
  color: var(--ink);
  box-shadow: 0.6rem 0.6rem 0 var(--ink);
}
.confirm-dialog::backdrop {
  background: rgb(22 27 25 / 55%);
}
.confirm-dialog__panel {
  padding: clamp(1.25rem, 4vw, 2rem);
}
.confirm-dialog__eyebrow {
  margin: 0 0 0.6rem;
  color: var(--accent-text);
  font: 800 0.65rem var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.confirm-dialog h2 {
  margin: 0;
  font: 500 clamp(2rem, 5vw, 3.2rem) / 0.95 var(--font-display);
  letter-spacing: -0.05em;
}
.confirm-dialog p:not(.confirm-dialog__eyebrow) {
  margin: 1rem 0 0;
  color: var(--ink-muted);
  line-height: 1.6;
}
.confirm-dialog__actions {
  display: flex;
  justify-content: end;
  flex-wrap: wrap;
  gap: 0.6rem;
  margin-top: 1.5rem;
}
.confirm-dialog__actions button {
  min-height: 2.75rem;
  border: 1px solid var(--line-strong);
  padding: 0.6rem 0.9rem;
  background: transparent;
  color: var(--ink);
  font-weight: 800;
  cursor: pointer;
}
.confirm-dialog__actions .confirm-dialog__confirm {
  background: var(--ink);
  color: var(--paper);
}
.confirm-dialog__actions .confirm-dialog__confirm--danger {
  border-color: var(--accent-text);
  background: var(--accent-text);
}
@media (max-width: 30rem) {
  .confirm-dialog__actions {
    flex-direction: column-reverse;
  }
  .confirm-dialog__actions button {
    width: 100%;
  }
}
</style>
