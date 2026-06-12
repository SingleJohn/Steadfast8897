import { ref, type Ref } from 'vue'

import { addLibrary, browseDirectories, getLibraries } from '@/api/client'

export function useLibraryCreate(libraries: Ref<any[]>, showToast: (message: string, type?: any) => void) {
  const showAddLib = ref(false)
  const newLibName = ref('')
  const newLibType = ref('movies')
  const newLibPaths = ref<string[]>([])
  const newLibPathInput = ref('')
  const showDirBrowser = ref(false)
  const dirBrowserPath = ref('/mnt')
  const dirBrowserDirs = ref<{ Name: string; Path: string }[]>([])
  const dirBrowserLoading = ref(false)

  async function loadDirBrowser(path: string) {
    dirBrowserLoading.value = true
    try {
      const res = await browseDirectories(path)
      dirBrowserPath.value = res.Path
      dirBrowserDirs.value = res.Directories || []
    } catch {
      showToast('无法读取目录', 'error')
    } finally {
      dirBrowserLoading.value = false
    }
  }

  function openDirBrowser() {
    showDirBrowser.value = true
    void loadDirBrowser('/mnt')
  }

  function dirParentPath() {
    const p = dirBrowserPath.value
    if (p === '/') return
    void loadDirBrowser(p.substring(0, p.lastIndexOf('/')) || '/')
  }

  function addPathToList(path: string) {
    const p = path.trim()
    if (p && !newLibPaths.value.includes(p)) newLibPaths.value = [...newLibPaths.value, p]
  }

  function removePathFromList(index: number) {
    newLibPaths.value = newLibPaths.value.filter((_, i) => i !== index)
  }

  function handleAddPathManual() {
    addPathToList(newLibPathInput.value)
    newLibPathInput.value = ''
  }

  async function handleAddLibrary(e?: Event) {
    e?.preventDefault?.()
    if (!newLibName.value || newLibPaths.value.length === 0) return
    try {
      await addLibrary(newLibName.value, newLibType.value, newLibPaths.value)
      showAddLib.value = false
      newLibName.value = ''
      newLibPaths.value = []
      newLibPathInput.value = ''
      showToast('媒体库添加成功', 'success')
      libraries.value = await getLibraries()
    } catch {
      showToast('添加媒体库失败', 'error')
    }
  }

  return {
    showAddLib,
    newLibName,
    newLibType,
    newLibPaths,
    newLibPathInput,
    showDirBrowser,
    dirBrowserPath,
    dirBrowserDirs,
    dirBrowserLoading,
    loadDirBrowser,
    openDirBrowser,
    dirParentPath,
    addPathToList,
    removePathFromList,
    handleAddPathManual,
    handleAddLibrary,
  }
}
