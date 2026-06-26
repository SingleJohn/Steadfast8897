package source

import (
	"time"
)

const (
	ProviderHealthStatusOK       = "ok"
	ProviderHealthStatusPartial  = "partial"
	ProviderHealthStatusSkipped  = "skipped"
	ProviderHealthStatusError    = "error"
	ProviderHealthStatusUnknown  = "unknown"
	ProviderHealthStatusUnusable = "unhealthy"
)

type ProviderHealthSummary struct {
	RuntimeStatus   string                      `json:"runtime_status"`
	HomeStatus      string                      `json:"home_status"`
	CategoryStatus  string                      `json:"category_status"`
	SearchStatus    string                      `json:"search_status"`
	PlayReadyStatus string                      `json:"play_ready_status"`
	OverallStatus   string                      `json:"overall_status"`
	Message         string                      `json:"message,omitempty"`
	CheckedAt       time.Time                   `json:"checked_at"`
	Home            ProviderHealthMethodSummary `json:"home"`
	Category        ProviderHealthMethodSummary `json:"category"`
	Search          ProviderHealthMethodSummary `json:"search"`
}

type ProviderHealthMethodSummary struct {
	Status          string `json:"status"`
	ErrorType       string `json:"error_type,omitempty"`
	Message         string `json:"message,omitempty"`
	CategoriesCount int    `json:"categories_count,omitempty"`
	FiltersCount    int    `json:"filters_count,omitempty"`
	ItemsCount      int    `json:"items_count,omitempty"`
	LatencyMS       int64  `json:"latency_ms,omitempty"`
}

func providerHealthOverall(summary ProviderHealthSummary) string {
	if isProviderHealthFailed(summary.RuntimeStatus) {
		return ProviderHealthStatusError
	}
	if isProviderHealthUsable(summary.HomeStatus) {
		if summary.CategoryStatus == ProviderHealthStatusOK && !isProviderHealthFailed(summary.SearchStatus) {
			return ProviderHealthStatusOK
		}
		return ProviderHealthStatusPartial
	}
	if summary.CategoryStatus == ProviderHealthStatusOK {
		return ProviderHealthStatusPartial
	}
	if summary.SearchStatus == ProviderHealthStatusOK {
		return ProviderHealthStatusPartial
	}
	if isProviderHealthFailed(summary.HomeStatus) || isProviderHealthFailed(summary.CategoryStatus) {
		return ProviderHealthStatusError
	}
	return ProviderHealthStatusUnknown
}

func isProviderHealthUsable(status string) bool {
	return status == ProviderHealthStatusOK || status == ProviderHealthStatusPartial
}

func isProviderHealthFailed(status string) bool {
	return status == ProviderHealthStatusError || status == ProviderHealthStatusUnusable
}

func providerHealthMessage(summary ProviderHealthSummary) string {
	switch summary.OverallStatus {
	case ProviderHealthStatusOK:
		return ""
	case ProviderHealthStatusPartial:
		if isProviderHealthUsable(summary.HomeStatus) {
			return "Provider 首页可用，但部分能力未完全通过。"
		}
		return "Provider 部分能力可用。"
	case ProviderHealthStatusError:
		if summary.Home.Message != "" {
			return summary.Home.Message
		}
		if summary.Category.Message != "" {
			return summary.Category.Message
		}
		if summary.Search.Message != "" {
			return summary.Search.Message
		}
		return "Provider 分项探活失败。"
	default:
		return "Provider 分项探活无可用结果。"
	}
}
