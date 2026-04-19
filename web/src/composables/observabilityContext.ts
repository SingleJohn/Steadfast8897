import type { InjectionKey } from 'vue'
import type { useGatewayObservability } from './useGatewayObservability'

/**
 * 网关观测容器（GatewayObsLayout）持有 useGatewayObservability 的结果，
 * 通过 provide 注入给 TrafficTab / RedirectTab / IpStatsTab。
 *
 * 使用方式：
 *   // 父容器
 *   const obs = useGatewayObservability(message)
 *   provide(GW_OBS_KEY, obs)
 *
 *   // 子组件
 *   const obs = inject(GW_OBS_KEY)
 *   const { isLive, logsItems, refreshLogs } = obs!
 */
export type GatewayObservabilityContext = ReturnType<typeof useGatewayObservability>

export const GW_OBS_KEY: InjectionKey<GatewayObservabilityContext> = Symbol('gateway-observability')
