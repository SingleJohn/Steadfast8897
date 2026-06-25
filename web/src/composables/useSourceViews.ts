import { computed, reactive, ref, shallowRef } from 'vue'
import { listCoverStyles, type CoverStyle } from '@/api/client'
import {
  createSourceView,
  deleteSourceView,
  deleteSourceViewCover,
  discoverSourceViewValues,
  generateSourceViewCover,
  listSourceViews,
  renameSourceView,
  updateSourceView,
  updateSourceViewDisplayOrder,
  type DimensionValue,
  type SourceView,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceViews(showToast: ToastFn) {
  const views = ref<SourceView[]>([])
  const coverStyles = ref<CoverStyle[]>([])

  const viewDraft = reactive({
    id: null as number | null,
    Name: '',
    DisplayName: '',
    Dimension: 'normalized_kind',
    MatchValue: '',
    MatchValues: [] as string[],
    CollectionType: 'mixed',
    Enabled: true,
    ExposeToEmby: false,
  })
  const discoverDimension = shallowRef('normalized_kind')
  const discoverSearch = shallowRef('')
  const discoverValues = ref<DimensionValue[]>([])
  const discoverSelected = ref<string[]>([])
  const discoverLoading = shallowRef(false)
  const coverTargetId = shallowRef<number | null>(null)
  const coverStyle = shallowRef('')
  const generatingCover = shallowRef(false)

  const coverStyleOptions = computed(() => coverStyles.value.map((s) => ({ label: s.label, value: s.name })))

  async function refreshViews() {
    views.value = await listSourceViews()
  }

  async function ensureCoverStyles() {
    if (coverStyles.value.length > 0) return
    coverStyles.value = await listCoverStyles()
    coverStyle.value = coverStyles.value.find((s) => s.name === 'showcase')?.name || coverStyles.value[0]?.name || ''
  }

  function editView(view?: SourceView) {
    viewDraft.id = view?.Id || null
    viewDraft.Name = view?.Name || ''
    viewDraft.DisplayName = view?.CustomName || ''
    viewDraft.Dimension = view?.Dimension || 'normalized_kind'
    viewDraft.MatchValue = view?.MatchValue || ''
    viewDraft.MatchValues = [...(view?.MatchValues || [])]
    viewDraft.CollectionType = view?.CollectionType || 'mixed'
    viewDraft.Enabled = view?.Enabled ?? true
    viewDraft.ExposeToEmby = view?.ExposeToEmby ?? false
  }

  async function saveView() {
    if (!viewDraft.MatchValue.trim()) {
      showToast('请填写匹配值', 'info')
      return
    }
    const payload = {
      Name: viewDraft.Name || viewDraft.MatchValue,
      DisplayName: viewDraft.DisplayName || undefined,
      Dimension: viewDraft.Dimension,
      MatchValue: viewDraft.MatchValue,
      MatchValues: viewDraft.MatchValues.length > 0 ? viewDraft.MatchValues : [viewDraft.MatchValue],
      CollectionType: viewDraft.CollectionType,
      Enabled: viewDraft.Enabled,
      ExposeToEmby: viewDraft.ExposeToEmby,
    }
    if (viewDraft.id) await updateSourceView(viewDraft.id, payload)
    else await createSourceView(payload)
    showToast('在线库已保存', 'success')
    editView()
    await refreshViews()
  }

  async function removeView(id: number) {
    await deleteSourceView(id)
    showToast('在线库已删除', 'success')
    await refreshViews()
  }

  async function renameView(id: number, name: string) {
    await renameSourceView(id, name)
    await refreshViews()
  }

  async function runDiscover() {
    discoverLoading.value = true
    discoverSelected.value = []
    try {
      discoverValues.value = await discoverSourceViewValues(discoverDimension.value, discoverSearch.value.trim(), 1)
    } catch (e: any) {
      showToast(e?.message || '维度发现失败', 'error')
    } finally {
      discoverLoading.value = false
    }
  }

  function applyDiscoverSelection() {
    if (discoverSelected.value.length === 0) return
    viewDraft.Dimension = discoverDimension.value
    viewDraft.MatchValue = discoverSelected.value[0]
    viewDraft.MatchValues = [...discoverSelected.value]
    if (!viewDraft.Name) viewDraft.Name = discoverSelected.value[0]
  }

  async function moveView(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= views.value.length) return
    const ids = views.value.map((v) => v.Id)
    ;[ids[index], ids[target]] = [ids[target], ids[index]]
    await updateSourceViewDisplayOrder(ids)
    await refreshViews()
  }

  async function openCover(id: number) {
    coverTargetId.value = id
    await ensureCoverStyles()
  }

  async function confirmCover() {
    if (!coverTargetId.value || !coverStyle.value) return
    generatingCover.value = true
    try {
      await generateSourceViewCover(coverTargetId.value, coverStyle.value)
      coverTargetId.value = null
      showToast('封面已生成', 'success')
      await refreshViews()
    } catch (e: any) {
      showToast(e?.message || '生成失败', 'error')
    } finally {
      generatingCover.value = false
    }
  }

  async function restoreCover(id: number) {
    await deleteSourceViewCover(id)
    await refreshViews()
  }

  return {
    views,
    viewDraft,
    discoverDimension,
    discoverSearch,
    discoverValues,
    discoverSelected,
    discoverLoading,
    coverTargetId,
    coverStyle,
    coverStyleOptions,
    generatingCover,
    refreshViews,
    editView,
    saveView,
    removeView,
    renameView,
    runDiscover,
    applyDiscoverSelection,
    moveView,
    openCover,
    confirmCover,
    restoreCover,
  }
}
