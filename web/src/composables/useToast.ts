import { useMessage } from 'naive-ui'

export type ToastType = 'success' | 'error' | 'info'

export function useToast() {
  const message = useMessage()

  function showToast(msg: string, type: ToastType = 'info') {
    if (type === 'success') message.success(msg)
    else if (type === 'error') message.error(msg)
    else message.info(msg)
  }

  return { showToast }
}
