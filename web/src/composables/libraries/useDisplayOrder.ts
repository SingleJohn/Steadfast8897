import { ref, watch, type Ref } from 'vue'

import { getViews, setLibraryDisplayOrder } from '@/api/client'

type ActiveView = 'libraries' | 'scan' | 'platforms' | 'order'
type OrderEntry = { kind: 'library' | 'platform'; id: string; name: string; type: string }

export function useDisplayOrder(activeView: Ref<ActiveView>, showToast: (message: string, type?: any) => void) {
  const orderList = ref<OrderEntry[]>([])
  const savingOrder = ref(false)
  const draggingOrderKey = ref<string | null>(null)
  const dragOverOrderKey = ref<string | null>(null)
  const orderDragStart = ref<OrderEntry[]>([])
  const orderDragChanged = ref(false)
  const orderDragCommitted = ref(false)

  async function loadOrderList() {
    try {
      const res = await getViews()
      orderList.value = (res.Items || []).map((it: any) => ({
        kind: it.PlatformLibrary ? 'platform' : 'library',
        id: it.Id,
        name: it.Name,
        type: it.PlatformLibrary ? '虚拟库' : '媒体库',
      }))
    } catch {
      showToast('加载顺序失败', 'error')
    }
  }

  function orderKey(e: { kind: string; id: string }) {
    return e.kind + ':' + e.id
  }

  function reorderOrderList(fromIndex: number, toIndex: number) {
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= orderList.value.length || toIndex >= orderList.value.length) {
      return false
    }
    const arr = [...orderList.value]
    const [moved] = arr.splice(fromIndex, 1)
    arr.splice(toIndex, 0, moved)
    orderList.value = arr
    return true
  }

  async function persistOrder() {
    savingOrder.value = true
    try {
      await setLibraryDisplayOrder(orderList.value.map((e) => ({ Kind: e.kind, Id: e.id })))
    } catch {
      showToast('排序保存失败，已恢复服务器顺序', 'error')
      await loadOrderList()
    } finally {
      savingOrder.value = false
    }
  }

  function handleOrderDragStart(index: number, e: DragEvent) {
    const item = orderList.value[index]
    if (!item || orderList.value.length <= 1 || savingOrder.value) return
    draggingOrderKey.value = orderKey(item)
    dragOverOrderKey.value = orderKey(item)
    orderDragStart.value = [...orderList.value]
    orderDragChanged.value = false
    orderDragCommitted.value = false
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', orderKey(item))
    }
  }

  function handleOrderDragOver(index: number, e: DragEvent) {
    if (!draggingOrderKey.value) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const target = orderList.value[index]
    if (!target) return
    dragOverOrderKey.value = orderKey(target)
    const fromIndex = orderList.value.findIndex((x) => orderKey(x) === draggingOrderKey.value)
    if (reorderOrderList(fromIndex, index)) orderDragChanged.value = true
  }

  async function finishOrderDrag(commit: boolean) {
    if (!draggingOrderKey.value) return
    if (!commit) {
      if (orderDragChanged.value && orderDragStart.value.length > 0) orderList.value = orderDragStart.value
      resetOrderDrag()
      return
    }
    if (orderDragChanged.value) await persistOrder()
    resetOrderDrag()
  }

  function handleOrderDrop(e: DragEvent) {
    if (!draggingOrderKey.value) return
    e.preventDefault()
    orderDragCommitted.value = true
    void finishOrderDrag(true)
  }

  function handleOrderDragEnd() {
    if (orderDragCommitted.value) return
    void finishOrderDrag(false)
  }

  function resetOrderDrag() {
    draggingOrderKey.value = null
    dragOverOrderKey.value = null
    orderDragStart.value = []
    orderDragChanged.value = false
    orderDragCommitted.value = false
  }

  async function moveOrderKeyboard(index: number, dir: number) {
    if (savingOrder.value) return
    if (reorderOrderList(index, index + dir)) await persistOrder()
  }

  function onOrderDragHandleKeydown(index: number, e: KeyboardEvent) {
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      void moveOrderKeyboard(index, -1)
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      void moveOrderKeyboard(index, 1)
    }
  }

  watch(activeView, (v) => {
    if (v === 'order') void loadOrderList()
  })

  return {
    orderList,
    savingOrder,
    draggingOrderKey,
    dragOverOrderKey,
    handleOrderDragStart,
    handleOrderDragOver,
    handleOrderDrop,
    handleOrderDragEnd,
    onOrderDragHandleKeydown,
    loadOrderList,
  }
}
