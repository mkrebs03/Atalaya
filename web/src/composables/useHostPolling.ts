import { onMounted, onUnmounted, readonly, shallowRef } from 'vue'

export function useHostPolling<T>(loader: (signal: AbortSignal) => Promise<T>) {
  const data = shallowRef<T | null>(null)
  const loading = shallowRef(true)
  const refreshing = shallowRef(false)
  const error = shallowRef(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined
  let disposed = false
  let pending = false

  function schedule() {
    clearTimeout(timer)
    if (!disposed && document.visibilityState === 'visible')
      timer = setTimeout(() => void refresh(), 30_000)
  }

  async function refresh() {
    if (disposed || document.visibilityState !== 'visible') return
    if (controller) {
      pending = true
      return
    }
    pending = false
    const activeController = new AbortController()
    controller = activeController
    if (data.value !== null) refreshing.value = true
    try {
      data.value = await loader(activeController.signal)
      error.value = false
    } catch (caught: unknown) {
      if (!(caught instanceof DOMException && caught.name === 'AbortError')) error.value = true
    } finally {
      if (controller === activeController) controller = undefined
      loading.value = false
      refreshing.value = false
      if (pending && document.visibilityState === 'visible') void refresh()
      else schedule()
    }
  }

  function visibilityChanged() {
    clearTimeout(timer)
    if (document.visibilityState === 'visible') {
      pending = true
      void refresh()
    } else controller?.abort()
  }

  function restart(clearData = false) {
    pending = true
    if (clearData) {
      data.value = null
      loading.value = true
      error.value = false
    }
    if (controller) controller.abort()
    else void refresh()
  }

  onMounted(() => {
    disposed = false
    document.addEventListener('visibilitychange', visibilityChanged)
    void refresh()
  })
  onUnmounted(() => {
    disposed = true
    clearTimeout(timer)
    controller?.abort()
    document.removeEventListener('visibilitychange', visibilityChanged)
  })

  return {
    data: readonly(data),
    loading: readonly(loading),
    refreshing: readonly(refreshing),
    error: readonly(error),
    refresh,
    restart,
  }
}
