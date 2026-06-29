import { computed, ref } from 'vue'

import {
  addPlatformLibrary,
  addPlatformsBatch,
  addPlatformValues,
  deletePlatformCover,
  deletePlatformLibrary,
  discoverPlatformDimension,
  generateAllPlatformCovers,
  generatePlatformCover,
  getPlatforms,
  getTaskSummary,
  removePlatformValue,
  renamePlatform,
  rescrapeMissingStudio,
  scanPlatformByFilename,
  scanPlatformStudios,
  setPlatformEnable,
  updatePlatformSortOrder,
  updateSystemConfig,
} from '@/api/client'
import { useVisibleInterval } from '@/composables/useVisibleInterval'

type ToastFn = (message: string, type?: any) => void

export function usePlatformLibraries(
  showToast: ToastFn,
  ensureCoverStylesLoaded: () => Promise<void>,
  coverStyles: any,
) {
  const platformsData = ref<{ GlobalEnabled: boolean; Platforms: any[] }>({ GlobalEnabled: false, Platforms: [] })
  const newPlatformName = ref('')
  const platformScanning = ref(false)
  const platformTask = ref<any>(null)
  const platformPosition = ref<'before' | 'after'>('after')
  const showLibraryItemCount = ref(true)
  const showPlatformCover = ref(false)
  const platformCoverTargetId = ref<string | null>(null)
  const platformCoverStyle = ref('')
  const platformShowcaseIcon = ref('auto')
  const platformShowcaseShowPosterTitles = ref(true)
  const platformShowcaseShowCount = ref(true)
  const generatingPlatformCover = ref(false)

  const dimensionOptions = [
    { label: '片商 (studio)', value: 'studio' },
    { label: '番号前缀 (num_prefix)', value: 'num_prefix' },
    { label: '演员 (actor)', value: 'actor' },
  ]
  const discoverDimension = ref<'studio' | 'num_prefix' | 'actor'>('studio')
  const discoverSearch = ref('')
  const discoverMinCount = ref(1)
  const discoverLoading = ref(false)
  const discoverResults = ref<{ Value: string; Count: number; AlreadyAdded: boolean }[]>([])
  const discoverSelected = ref<string[]>([])

  const showRename = ref(false)
  const renameTargetId = ref<string | null>(null)
  const renameValue = ref('')
  const showAlias = ref(false)
  const aliasTarget = ref<any>(null)
  const aliasValues = ref<string[]>([])
  const aliasSearch = ref('')
  const aliasResults = ref<{ Value: string; Count: number; AlreadyAdded: boolean }[]>([])
  const aliasSelected = ref<string[]>([])
  const aliasLoading = ref(false)
  const filenameScanning = ref(false)
  const rescraping = ref(false)
  const rescrapeStatus = ref<any>(null)
  let rescrapeRefreshing = false
  const rescrapePollingEnabled = computed(() => Boolean(rescrapeStatus.value?.running))
  const rescrapePolling = useVisibleInterval(pollRescrapeProgress, 2000, {
    enabled: rescrapePollingEnabled,
  })

  const platformCoverIsShowcase = computed(() => platformCoverStyle.value === 'showcase')

  async function loadPlatforms() {
    try {
      platformsData.value = await getPlatforms()
    } catch {}
  }

  async function loadTaskSummary() {
    try {
      const summary = await getTaskSummary()
      platformTask.value = summary.platform
      rescrapeStatus.value = summary.platform?.rescrape || null
    } catch {}
  }

  async function toggleGlobalPlatform(enabled: boolean) {
    try {
      await updateSystemConfig({ platform_libraries_enabled: String(enabled) })
      platformsData.value.GlobalEnabled = enabled
      showToast(enabled ? '平台库已启用' : '平台库已禁用', 'success')
    } catch {
      showToast('操作失败', 'error')
    }
  }

  async function togglePlatform(id: string, enabled: boolean) {
    try {
      await setPlatformEnable(id, enabled)
      await loadPlatforms()
    } catch {
      showToast('操作失败', 'error')
    }
  }

  async function handleAddPlatform() {
    const name = newPlatformName.value.trim()
    if (!name) return
    try {
      await addPlatformLibrary(name)
      newPlatformName.value = ''
      await loadPlatforms()
      showToast('平台已添加', 'success')
    } catch {
      showToast('添加失败', 'error')
    }
  }

  async function runDiscover() {
    discoverLoading.value = true
    discoverSelected.value = []
    try {
      const res = await discoverPlatformDimension(discoverDimension.value, discoverSearch.value.trim(), discoverMinCount.value)
      discoverResults.value = res.values || []
      if (discoverResults.value.length === 0) showToast('没有扫描到可分类的值', 'info')
    } catch (e: any) {
      showToast(e?.message || '扫描失败', 'error')
    } finally {
      discoverLoading.value = false
    }
  }

  async function addSelectedDimension() {
    if (discoverSelected.value.length === 0) return
    try {
      const res = await addPlatformsBatch(discoverDimension.value, discoverSelected.value)
      const skippedText = res.skipped > 0 ? `，已存在 ${res.skipped} 个` : ''
      const failedText = res.failed?.length ? `，失败 ${res.failed.length} 个` : ''
      showToast(`已添加 ${res.added} 个虚拟库${skippedText}${failedText}(默认关闭，可在下方启用)`, res.failed?.length ? 'warning' : 'success')
      discoverSelected.value = []
      await runDiscover()
      await loadPlatforms()
    } catch (e: any) {
      showToast(e?.message || '添加失败', 'error')
    }
  }

  async function openPlatformCover(id: string | null) {
    platformCoverTargetId.value = id
    showPlatformCover.value = true
    await ensureCoverStylesLoaded()
    if (!platformCoverStyle.value && coverStyles.value.length > 0) {
      platformCoverStyle.value = coverStyles.value.find((s: any) => s.name === 'showcase')?.name || coverStyles.value[0].name
    }
  }

  function platformCoverOptions() {
    if (!platformCoverIsShowcase.value) return undefined
    const o: Record<string, any> = {
      ShowPosterTitles: platformShowcaseShowPosterTitles.value,
      ShowCount: platformShowcaseShowCount.value,
    }
    if (platformShowcaseIcon.value !== 'auto') o.Icon = platformShowcaseIcon.value
    return o
  }

  async function confirmPlatformCover() {
    if (!platformCoverStyle.value) return
    generatingPlatformCover.value = true
    try {
      const opts = platformCoverOptions()
      if (platformCoverTargetId.value) {
        await generatePlatformCover(platformCoverTargetId.value, platformCoverStyle.value, opts)
        showToast('封面已生成', 'success')
      } else {
        const res = await generateAllPlatformCovers(platformCoverStyle.value, opts)
        showToast(`封面生成完成：成功 ${res.generated}，跳过 ${res.skipped}`, 'success')
      }
      showPlatformCover.value = false
      await loadPlatforms()
    } catch (e: any) {
      showToast(e?.message || '生成失败(可能没有海报素材)', 'error')
    } finally {
      generatingPlatformCover.value = false
    }
  }

  async function handleRestoreCover(id: string) {
    try {
      await deletePlatformCover(id)
      await loadPlatforms()
      showToast('已恢复默认封面', 'success')
    } catch {
      showToast('恢复失败', 'error')
    }
  }

  function openRename(p: any) {
    renameTargetId.value = p.Id
    renameValue.value = p.CustomName || ''
    showRename.value = true
  }

  async function confirmRename() {
    if (!renameTargetId.value) return
    try {
      await renamePlatform(renameTargetId.value, renameValue.value.trim())
      showRename.value = false
      await loadPlatforms()
      showToast('名称已更新', 'success')
    } catch {
      showToast('重命名失败', 'error')
    }
  }

  function syncAliasTarget() {
    const updated = (platformsData.value?.Platforms || []).find((x: any) => x.Id === aliasTarget.value?.Id)
    if (updated) {
      aliasTarget.value = updated
      aliasValues.value = [...(updated.MatchValues || [updated.MatchValue])]
    }
  }

  function openAlias(p: any) {
    aliasTarget.value = p
    aliasValues.value = [...(p.MatchValues || [p.MatchValue])]
    aliasSearch.value = ''
    aliasResults.value = []
    aliasSelected.value = []
    showAlias.value = true
  }

  async function runAliasDiscover() {
    if (!aliasTarget.value) return
    aliasLoading.value = true
    aliasSelected.value = []
    try {
      const res = await discoverPlatformDimension(aliasTarget.value.Dimension, aliasSearch.value.trim(), 1)
      aliasResults.value = (res.values || []).filter((v) => !aliasValues.value.includes(v.Value))
    } catch {
      showToast('扫描失败', 'error')
    } finally {
      aliasLoading.value = false
    }
  }

  async function addAliasSelected() {
    if (!aliasTarget.value || aliasSelected.value.length === 0) return
    try {
      await addPlatformValues(aliasTarget.value.Id, aliasSelected.value)
      showToast(`已合并 ${aliasSelected.value.length} 个值`, 'success')
      await loadPlatforms()
      syncAliasTarget()
      aliasSelected.value = []
      await runAliasDiscover()
    } catch {
      showToast('合并失败', 'error')
    }
  }

  async function removeAlias(value: string) {
    if (!aliasTarget.value) return
    if (value === aliasTarget.value.MatchValue) {
      showToast('主匹配值不可移除', 'info')
      return
    }
    try {
      await removePlatformValue(aliasTarget.value.Id, value)
      await loadPlatforms()
      syncAliasTarget()
    } catch {
      showToast('移除失败', 'error')
    }
  }

  async function handleDeletePlatform(id: string) {
    try {
      await deletePlatformLibrary(id)
      await loadPlatforms()
      showToast('平台已删除', 'success')
    } catch {
      showToast('删除失败', 'error')
    }
  }

  async function movePlatform(idx: number, dir: number) {
    const list = platformsData.value?.Platforms || []
    const j = idx + dir
    if (j < 0 || j >= list.length) return
    const ids = list.map((p: any) => p.Id)
    ;[ids[idx], ids[j]] = [ids[j], ids[idx]]
    try {
      await updatePlatformSortOrder(ids)
      await loadPlatforms()
    } catch {
      showToast('排序失败', 'error')
    }
  }

  async function handleScanStudios() {
    platformScanning.value = true
    try {
      const res = await scanPlatformStudios()
      await loadTaskSummary()
      showToast(`正在从 TMDB 扫描 ${res.total} 个项目`, 'success')
    } catch {
      showToast('扫描失败', 'error')
    }
    setTimeout(() => {
      platformScanning.value = false
      void loadPlatforms()
      void loadTaskSummary()
    }, 5000)
  }

  async function handleScanFilename() {
    filenameScanning.value = true
    try {
      const res = await scanPlatformByFilename()
      showToast(`从文件名识别完成，更新了 ${res.updated} 个项目`, 'success')
      await loadPlatforms()
      await loadTaskSummary()
    } catch {
      showToast('扫描失败', 'error')
    }
    filenameScanning.value = false
  }

  async function pollRescrapeProgress() {
    if (rescrapeRefreshing) return
    rescrapeRefreshing = true
    try {
      await loadTaskSummary()
      const p = rescrapeStatus.value
      if (!p?.running) {
        rescraping.value = false
        void loadPlatforms()
      }
    } catch {
    } finally {
      rescrapeRefreshing = false
    }
  }

  async function handleRescrape() {
    rescraping.value = true
    rescrapeStatus.value = null
    try {
      const res = await rescrapeMissingStudio()
      showToast(`正在重新刮削 ${res.total} 个项目`, 'success')
      rescrapeStatus.value = { running: true, total: res.total }
      void pollRescrapeProgress()
    } catch {
      showToast('刮削失败', 'error')
      rescraping.value = false
    }
  }

  function resumeRescrapePolling() {
    if (rescrapeStatus.value?.running) {
      rescraping.value = true
    }
  }

  function clearRescrapeTimer() {
    rescrapePolling.pause()
  }

  return {
    platformsData,
    newPlatformName,
    platformScanning,
    platformTask,
    platformPosition,
    showLibraryItemCount,
    showPlatformCover,
    platformCoverTargetId,
    platformCoverStyle,
    platformShowcaseIcon,
    platformShowcaseShowPosterTitles,
    platformShowcaseShowCount,
    generatingPlatformCover,
    dimensionOptions,
    discoverDimension,
    discoverSearch,
    discoverMinCount,
    discoverLoading,
    discoverResults,
    discoverSelected,
    showRename,
    renameValue,
    showAlias,
    aliasTarget,
    aliasValues,
    aliasSearch,
    aliasResults,
    aliasSelected,
    aliasLoading,
    filenameScanning,
    rescraping,
    rescrapeStatus,
    loadPlatforms,
    loadTaskSummary,
    toggleGlobalPlatform,
    togglePlatform,
    handleAddPlatform,
    runDiscover,
    addSelectedDimension,
    openPlatformCover,
    confirmPlatformCover,
    handleRestoreCover,
    openRename,
    confirmRename,
    openAlias,
    runAliasDiscover,
    addAliasSelected,
    removeAlias,
    handleDeletePlatform,
    movePlatform,
    handleScanStudios,
    handleScanFilename,
    handleRescrape,
    resumeRescrapePolling,
    clearRescrapeTimer,
  }
}
