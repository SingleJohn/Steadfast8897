package scraper

import "strings"

// ConfigOverride 是库级 scrape_config JSONB 的反序列化目标。
// 指针 / nil map 表示"未设置,继承全局";非 nil 表示覆盖。
//
// 合并语义由 MergeOverride 实现:
//   - 列表字段(ProvidersEnabled):非 nil 整体替换
//   - map 字段(ProviderPriority / FieldPriority):per-key merge
//   - 指针标量:非 nil 覆盖
//
// Provider 凭据(TVDB/Fanart/Douban Cookie 等)不在此结构内 —— 永远全局。
type ConfigOverride struct {
	ProvidersEnabled    *[]string           `json:"providers_enabled,omitempty"`
	ProviderPriority    map[string]int      `json:"provider_priority,omitempty"`
	FieldPriority       map[string][]string `json:"field_priority,omitempty"`
	ConfidenceThreshold *float64            `json:"confidence_threshold,omitempty"`
	AutoApply           *bool               `json:"auto_apply,omitempty"`
}

// IsEmpty 判断 override 是否全为 nil(= 完全继承)。
func (o *ConfigOverride) IsEmpty() bool {
	if o == nil {
		return true
	}
	return o.ProvidersEnabled == nil &&
		len(o.ProviderPriority) == 0 &&
		len(o.FieldPriority) == 0 &&
		o.ConfidenceThreshold == nil &&
		o.AutoApply == nil
}

// MergeOverride 把库级 override 合并到全局 cfg,返回最终生效配置。
// global 不被修改;返回的 RuntimeConfig 中的 map/slice 与 global 相互独立。
func MergeOverride(global RuntimeConfig, override *ConfigOverride) RuntimeConfig {
	out := global
	if override == nil {
		return out
	}

	if override.ProvidersEnabled != nil {
		out.ProvidersEnabled = append([]string(nil), (*override.ProvidersEnabled)...)
	}

	if len(override.ProviderPriority) > 0 {
		merged := make(map[string]int, len(global.ProviderPriority)+len(override.ProviderPriority))
		for k, v := range global.ProviderPriority {
			merged[k] = v
		}
		for k, v := range override.ProviderPriority {
			merged[strings.ToLower(strings.TrimSpace(k))] = v
		}
		out.ProviderPriority = merged
	}

	if len(override.FieldPriority) > 0 {
		merged := make(map[string][]string, len(global.FieldPriority)+len(override.FieldPriority))
		for k, v := range global.FieldPriority {
			merged[k] = append([]string(nil), v...)
		}
		for k, v := range override.FieldPriority {
			key := strings.ToLower(strings.TrimSpace(k))
			if key == "" || len(v) == 0 {
				continue
			}
			merged[key] = append([]string(nil), v...)
		}
		out.FieldPriority = merged
	}

	if override.ConfidenceThreshold != nil {
		t := *override.ConfidenceThreshold
		if t > 0 && t <= 1 {
			out.ConfidenceThreshold = t
		}
	}

	if override.AutoApply != nil {
		out.AutoApply = *override.AutoApply
	}

	return out
}
