import { ref, type Ref } from 'vue'

import { getLibraries, updateLibrarySortOrder } from '@/api/client'

export function useLibraryCardOrder(libraries: Ref<any[]>, showToast: (message: string, type?: any) => void) {
  const draggingLibraryId = ref<string | null>(null)
  const dragOverLibraryId = ref<string | null>(null)
  const dragStartLibraries = ref<any[]>([])
  const libraryDragChanged = ref(false)
  const libraryDragCommitted = ref(false)
  const savingLibraryOrder = ref(false)

  function reorderLibrary(fromIndex: number, toIndex: number) {
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= libraries.value.length || toIndex >= libraries.value.length) {
      return false
    }
    const arr = [...libraries.value]
    const [moved] = arr.splice(fromIndex, 1)
    arr.splice(toIndex, 0, moved)
    libraries.value = arr
    return true
  }

  async function persistLibraryOrder() {
    const orders = libraries.value.map((lib: any, i: number) => ({ Id: lib.ItemId, SortOrder: i }))
    savingLibraryOrder.value = true
    try {
      await updateLibrarySortOrder(orders)
    } catch {
      showToast('排序保存失败，已恢复服务器顺序', 'error')
      try {
        libraries.value = await getLibraries()
      } catch {
        if (dragStartLibraries.value.length > 0) libraries.value = dragStartLibraries.value
      }
    } finally {
      savingLibraryOrder.value = false
    }
  }

  async function moveLibrary(index: number, direction: 'up' | 'down') {
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (!reorderLibrary(index, targetIndex)) return
    await persistLibraryOrder()
  }

  function handleLibraryDragStart(index: number, e: DragEvent) {
    const lib = libraries.value[index]
    if (!lib || libraries.value.length <= 1 || savingLibraryOrder.value) return
    draggingLibraryId.value = lib.ItemId
    dragOverLibraryId.value = lib.ItemId
    dragStartLibraries.value = [...libraries.value]
    libraryDragChanged.value = false
    libraryDragCommitted.value = false
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', lib.ItemId)
    }
  }

  function handleLibraryDragOver(index: number, e: DragEvent) {
    if (!draggingLibraryId.value) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const target = libraries.value[index]
    if (!target) return
    dragOverLibraryId.value = target.ItemId
    const fromIndex = libraries.value.findIndex((lib: any) => lib.ItemId === draggingLibraryId.value)
    if (reorderLibrary(fromIndex, index)) libraryDragChanged.value = true
  }

  async function finishLibraryDrag(commit: boolean) {
    if (!draggingLibraryId.value) return
    if (!commit) {
      if (libraryDragChanged.value && dragStartLibraries.value.length > 0) {
        libraries.value = dragStartLibraries.value
      }
      resetLibraryDrag()
      return
    }
    if (libraryDragChanged.value) await persistLibraryOrder()
    resetLibraryDrag()
  }

  function handleLibraryDrop(e: DragEvent) {
    if (!draggingLibraryId.value) return
    e.preventDefault()
    libraryDragCommitted.value = true
    void finishLibraryDrag(true)
  }

  function handleLibraryDragEnd() {
    if (libraryDragCommitted.value) return
    void finishLibraryDrag(false)
  }

  function resetLibraryDrag() {
    draggingLibraryId.value = null
    dragOverLibraryId.value = null
    dragStartLibraries.value = []
    libraryDragChanged.value = false
    libraryDragCommitted.value = false
  }

  function onLibraryDragHandleKeydown(index: number, e: KeyboardEvent) {
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      void moveLibrary(index, 'up')
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      void moveLibrary(index, 'down')
    }
  }

  return {
    draggingLibraryId,
    dragOverLibraryId,
    savingLibraryOrder,
    handleLibraryDragStart,
    handleLibraryDragOver,
    handleLibraryDrop,
    handleLibraryDragEnd,
    onLibraryDragHandleKeydown,
  }
}
