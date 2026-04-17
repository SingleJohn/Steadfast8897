import type { InjectionKey } from 'vue'
import type { useObservability } from './useObservability'

/**
 * Observability 父路由容器（ObservabilityPage）持有 useObservability 的结果，
 * 通过 provide 注入给所有子路由（TrafficTab / RedirectTab / IpStatsTab 等）。
 *
 * 使用方式：
 *   // 父容器
 *   const obs = useObservability(message)
 *   provide(OBS_KEY, obs)
 *
 *   // 子组件
 *   const obs = injectObservability()
 *   const { isLive, logsItems, refreshLogs } = obs  // 解构 ref，template 自动解包
 */
export type ObservabilityContext = ReturnType<typeof useObservability>

export const OBS_KEY: InjectionKey<ObservabilityContext> = Symbol('observability')
