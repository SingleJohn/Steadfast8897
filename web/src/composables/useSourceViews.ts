import { computed, reactive, ref, shallowRef } from 'vue'
import { listCoverStyles, type CoverStyle } from '@/api/client'
import {
  createSourceView,
  deleteSourceView,
  deleteSourceViewCover,
  discoverSourceViewValues,
  fetchSourceViewDimensionMeta,
  generateSourceViewCover,
  listSourceViews,
  previewSourceView,
  renameSourceView,
  updateSourceView,
  updateSourceViewDisplayOrder,
  type DimensionValue,
  type SourceViewDimensionMeta,
  type SourceViewPreview,
  type SourceView,
} from '@/api/source'

type ToastFn = (message: string, type?: any) => void

export function useSourceViews(showToast: ToastFn) {
  const views = ref<SourceView[]>([])
  const coverStyles = ref<CoverStyle[]>([])
  const coverStylesLoaded = shallowRef(false)

  const viewDraft = reactive({
    id: null as number | null,
    Name: '',
    DisplayName: '',
    Dimension: 'normalized_kind',
    MatchValue: '',
    MatchValues: [] as string[],
    CollectionType: 'mixed',
    ProviderIds: [] as number[],
    SortOrder: 0,
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
  const showcaseIcon = shallowRef('auto')
  const showcaseShowPosterTitles = shallowRef(true)
  const showcaseShowCount = shallowRef(true)
  const generatingCover = shallowRef(false)
  const viewPreview = shallowRef<SourceViewPreview | null>(null)
  const previewLoading = shallowRef(false)
  const matchValueError = shallowRef('')
  const dimensionMeta = ref<SourceViewDimensionMeta[]>([])
  const dimensionMetaLoaded = shallowRef(false)

  const activeDimensionMeta = computed(() => dimensionMeta.value.find((m) => m.value === viewDraft.Dimension) || null)

  const coverStyleOptions = computed(() => coverStyles.value.map((s) => ({ label: s.label, value: s.name })))
  const coverIsShowcase = computed(() => coverStyle.value === 'showcase')
  const showcaseIconOptions = [
    { label: '自动', value: 'auto' },
    { label: '电影', value: 'movie' },
    { label: '电视', value: 'tv' },
    { label: '音乐', value: 'music' },
    { label: '动漫', value: 'anime' },
    { label: '纪录片', value: 'documentary' },
    { label: '少儿', value: 'kids' },
    { label: '媒体', value: 'media' },
  ]

  async function refreshViews() {
    views.value = await listSourceViews()
  }

  async function loadDimensionMeta() {
    if (dimensionMetaLoaded.value) return
    try {
      dimensionMeta.value = await fetchSourceViewDimensionMeta()
      dimensionMetaLoaded.value = true
    } catch {
      // 静默失败：前端面板仍有兜底示例
    }
  }

  function fillMatchValue(value: string) {
    viewDraft.MatchValue = value
    if (!viewDraft.Name) viewDraft.Name = value
    matchValueError.value = ''
    viewPreview.value = null
  }

  async function ensureCoverStyles() {
    if (coverStylesLoaded.value) return
    try {
      coverStyles.value = await listCoverStyles()
      coverStyle.value = coverStyles.value.find((s) => s.name === 'showcase')?.name || coverStyles.value[0]?.name || ''
    } catch {
      coverStylesLoaded.value = false
      showToast('加载封面风格失败', 'error')
      return
    } finally {
      if (coverStyles.value.length > 0) coverStylesLoaded.value = true
    }
  }

  function editView(view?: SourceView) {
    viewDraft.id = view?.Id || null
    viewDraft.Name = view?.Name || ''
    viewDraft.DisplayName = view?.CustomName || ''
    viewDraft.Dimension = view?.Dimension || 'normalized_kind'
    viewDraft.MatchValue = view?.MatchValue || ''
    viewDraft.MatchValues = [...(view?.MatchValues || [])]
    viewDraft.CollectionType = view?.CollectionType || 'mixed'
    viewDraft.ProviderIds = [...(view?.ProviderIds || [])]
    viewDraft.SortOrder = view?.SortOrder || 0
    viewDraft.Enabled = view?.Enabled ?? true
    viewDraft.ExposeToEmby = view?.ExposeToEmby ?? false
    viewPreview.value = null
    matchValueError.value = ''
  }

  function buildViewPayload() {
    if (!viewDraft.MatchValue.trim()) {
      matchValueError.value = '请填写主匹配值'
      return null
    }
    matchValueError.value = ''
    return {
      Name: viewDraft.Name || viewDraft.MatchValue,
      DisplayName: viewDraft.DisplayName || undefined,
      Dimension: viewDraft.Dimension,
      MatchValue: viewDraft.MatchValue,
      MatchValues: viewDraft.MatchValues.length > 0 ? viewDraft.MatchValues : [viewDraft.MatchValue],
      CollectionType: viewDraft.CollectionType,
      ProviderIds: viewDraft.ProviderIds,
      SortOrder: viewDraft.SortOrder,
      Enabled: viewDraft.Enabled,
      ExposeToEmby: viewDraft.ExposeToEmby,
    }
  }

  async function saveView() {
    const payload = buildViewPayload()
    if (!payload) {
      showToast('请填写匹配值', 'info')
      return
    }
    if (viewDraft.id) await updateSourceView(viewDraft.id, payload)
    else await createSourceView(payload)
    showToast('在线虚拟库已保存', 'success')
    editView()
    await refreshViews()
  }

  async function previewView() {
    const payload = buildViewPayload()
    if (!payload) {
      showToast('请填写匹配值后再预览', 'info')
      return
    }
    previewLoading.value = true
    try {
      viewPreview.value = await previewSourceView(payload)
    } catch (e: any) {
      showToast(e?.message || '在线虚拟库预览失败', 'error')
    } finally {
      previewLoading.value = false
    }
  }

  async function removeView(id: number) {
    await deleteSourceView(id)
    showToast('在线虚拟库已删除', 'success')
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
    viewPreview.value = null
    matchValueError.value = ''
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

  function closeCover() {
    if (generatingCover.value) return
    coverTargetId.value = null
  }

  function coverOptions() {
    if (!coverIsShowcase.value) return undefined
    const options: Record<string, any> = {
      ShowPosterTitles: showcaseShowPosterTitles.value,
      ShowCount: showcaseShowCount.value,
    }
    if (showcaseIcon.value !== 'auto') {
      options.Icon = showcaseIcon.value
    }
    return options
  }

  async function confirmCover() {
    if (!coverTargetId.value || !coverStyle.value) return
    generatingCover.value = true
    try {
      await generateSourceViewCover(coverTargetId.value, coverStyle.value, coverOptions())
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
    coverTargetId.value = null
    showToast('已恢复默认封面', 'success')
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
    coverStylesLoaded,
    showcaseIconOptions,
    showcaseIcon,
    showcaseShowPosterTitles,
    showcaseShowCount,
    generatingCover,
    viewPreview,
    previewLoading,
    matchValueError,
    dimensionMeta,
    activeDimensionMeta,
    refreshViews,
    loadDimensionMeta,
    fillMatchValue,
    editView,
    saveView,
    previewView,
    removeView,
    renameView,
    runDiscover,
    applyDiscoverSelection,
    moveView,
    openCover,
    closeCover,
    confirmCover,
    restoreCover,
  }
}
