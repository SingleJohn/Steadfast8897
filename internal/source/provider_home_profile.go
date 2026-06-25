package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ProviderHomeSourceStatusOK          = "ok"
	ProviderHomeSourceStatusEmpty       = "empty"
	ProviderHomeSourceStatusError       = "error"
	ProviderHomeSourceStatusUnsupported = "unsupported"
	ProviderHomeSourceStatusSkipped     = "skipped"
)

type HomeProfiler interface {
	HomeProfile(ctx context.Context) (*ProviderHomeProfile, error)
}

type ProviderHomeProfile struct {
	ProviderID     int64                `json:"provider_id"`
	RuntimeKind    string               `json:"runtime_kind"`
	Categories     []ProviderCategory   `json:"categories"`
	Filters        json.RawMessage      `json:"filters,omitempty"`
	FiltersCount   int                  `json:"filters_count"`
	HomeItems      []SourceItemSnapshot `json:"home_items"`
	HomeItemSource string               `json:"home_item_source"`
	Sources        ProviderHomeSources  `json:"sources"`
}

type ProviderHomeSources struct {
	HomeContent      ProviderRuntimeSlice `json:"home_content"`
	HomeVideoContent ProviderRuntimeSlice `json:"home_video_content"`
}

type ProviderRuntimeSlice struct {
	Method          string `json:"method"`
	Status          string `json:"status"`
	OK              bool   `json:"ok"`
	ErrorType       string `json:"error_type,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	CategoriesCount int    `json:"categories_count"`
	FiltersCount    int    `json:"filters_count"`
	ItemsCount      int    `json:"items_count"`
	DurationMS      int64  `json:"duration_ms"`
}

type providerHomePayload struct {
	Class   []providerHomeCategory `json:"class"`
	Classes []providerHomeCategory `json:"classes"`
	List    []cmsVOD               `json:"list"`
	Filters json.RawMessage        `json:"filters"`
}

type providerHomeCategory struct {
	TypeID   string `json:"type_id"`
	TypeName string `json:"type_name"`
}

func newProviderRuntimeSlice(method string, start time.Time, categoriesCount, filtersCount, itemsCount int) ProviderRuntimeSlice {
	status := ProviderHomeSourceStatusEmpty
	if categoriesCount > 0 || filtersCount > 0 || itemsCount > 0 {
		status = ProviderHomeSourceStatusOK
	}
	return ProviderRuntimeSlice{
		Method:          method,
		Status:          status,
		OK:              status == ProviderHomeSourceStatusOK,
		CategoriesCount: categoriesCount,
		FiltersCount:    filtersCount,
		ItemsCount:      itemsCount,
		DurationMS:      time.Since(start).Milliseconds(),
	}
}

func providerRuntimeSliceError(method string, start time.Time, err error) ProviderRuntimeSlice {
	return ProviderRuntimeSlice{
		Method:       method,
		Status:       ProviderHomeSourceStatusError,
		OK:           false,
		ErrorType:    ErrorType(err),
		ErrorMessage: err.Error(),
		DurationMS:   time.Since(start).Milliseconds(),
	}
}

func providerRuntimeSliceUnsupported(method, message string) ProviderRuntimeSlice {
	return ProviderRuntimeSlice{
		Method:       method,
		Status:       ProviderHomeSourceStatusUnsupported,
		OK:           false,
		ErrorType:    "unsupported",
		ErrorMessage: strings.TrimSpace(message),
	}
}

func decodeProviderHomePayload(raw json.RawMessage, decoderName string) (providerHomePayload, error) {
	var payload providerHomePayload
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return payload, fmt.Errorf("%s runtime 数据为空", decoderName)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("解析 %s runtime 首页数据失败: %w", decoderName, err)
	}
	return payload, nil
}

func providerHomeCategories(payload providerHomePayload) []ProviderCategory {
	rows := payload.Class
	if len(rows) == 0 {
		rows = payload.Classes
	}
	out := make([]ProviderCategory, 0, len(rows))
	for _, row := range rows {
		id := cleanCMSValue(row.TypeID)
		name := cleanCMSValue(row.TypeName)
		if id == "" || name == "" {
			continue
		}
		out = append(out, ProviderCategory{ID: id, Name: name})
	}
	return out
}

func providerHomeFilters(raw json.RawMessage) (json.RawMessage, int) {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return nil, 0
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		return raw, len(obj)
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		return raw, len(arr)
	}
	return raw, 0
}

func providerHomeItems(imageBase string, payload providerHomePayload, providerFormat string) []SourceItemSnapshot {
	cmsPayload := cmsResponse{List: payload.List}
	for i := range cmsPayload.List {
		if cmsPayload.List[i].Raw == nil {
			cmsPayload.List[i].Raw = map[string]any{}
		}
		if providerFormat != "" {
			cmsPayload.List[i].Raw["provider_format"] = providerFormat
		}
	}
	return parseCMSPage(imageBase, cmsPayload, false).Items
}
