import { onBeforeUnmount, onMounted, toValue, watch, type MaybeRefOrGetter } from 'vue'

type VisibleIntervalCallback = () => void | Promise<void>

interface UseVisibleIntervalOptions {
  enabled?: MaybeRefOrGetter<boolean>
  immediate?: boolean
  immediateOnVisible?: boolean
}

export function useVisibleInterval(
  callback: VisibleIntervalCallback,
  delay: number,
  options: UseVisibleIntervalOptions = {},
) {
  let timer: ReturnType<typeof window.setInterval> | null = null
  let mounted = false

  const isClient = typeof window !== 'undefined' && typeof document !== 'undefined'
  const shouldRun = () => toValue(options.enabled ?? true)
  const isVisible = () => !isClient || document.visibilityState === 'visible'
  const shouldRunNow = () => shouldRun() && isVisible()

  function run() {
    if (shouldRunNow()) void callback()
  }

  function stop() {
    if (!timer || !isClient) return
    window.clearInterval(timer)
    timer = null
  }

  function start() {
    if (!mounted || !isClient || timer || !shouldRunNow()) return
    timer = window.setInterval(run, delay)
  }

  function sync(runImmediate = false) {
    stop()
    if (!shouldRunNow()) return
    if (runImmediate) run()
    start()
  }

  function handleVisibilityChange() {
    if (document.visibilityState === 'visible') {
      sync(options.immediateOnVisible ?? true)
      return
    }
    stop()
  }

  onMounted(() => {
    mounted = true
    if (!isClient) return
    document.addEventListener('visibilitychange', handleVisibilityChange)
    sync(options.immediate ?? false)
  })

  watch(
    () => toValue(options.enabled ?? true),
    (enabled, previous) => {
      if (!mounted) return
      if (enabled) {
        sync(previous === false ? options.immediateOnVisible ?? true : false)
        return
      }
      stop()
    },
  )

  onBeforeUnmount(() => {
    mounted = false
    stop()
    if (isClient) {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  })

  return {
    refresh: run,
    pause: stop,
    resume: () => sync(options.immediateOnVisible ?? true),
  }
}
