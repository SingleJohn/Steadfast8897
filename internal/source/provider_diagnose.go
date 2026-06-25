package source

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	ProviderDiagnoseStatusOK          = "ok"
	ProviderDiagnoseStatusEmpty       = "empty"
	ProviderDiagnoseStatusError       = "error"
	ProviderDiagnoseStatusUnsupported = "unsupported"
	ProviderDiagnoseStatusSkipped     = "skipped"

	providerDiagnoseDefaultTimeout = 30 * time.Second
	providerDiagnoseMaxTimeout     = 60 * time.Second
	providerDiagnoseSampleLimit    = 5
)

type ProviderDiagnoseRequest struct {
	Methods      []string `json:"methods"`
	CategoryID   string   `json:"category_id"`
	Keyword      string   `json:"keyword"`
	SourceItemID string   `json:"source_item_id"`
	DetailID     string   `json:"detail_id"`
	TimeoutMS    int      `json:"timeout_ms"`
}

type ProviderDiagnoseResponse struct {
	ProviderID    int64                          `json:"provider_id"`
	ProviderName  string                         `json:"provider_name"`
	SourceKey     string                         `json:"source_key"`
	RuntimeKind   string                         `json:"runtime_kind"`
	ProviderKind  string                         `json:"provider_kind"`
	OverallStatus string                         `json:"overall_status"`
	Results       []ProviderDiagnoseMethodResult `json:"results"`
	DurationMS    int64                          `json:"duration_ms"`
}

type ProviderDiagnoseMethodResult struct {
	Method          string                       `json:"method"`
	Status          string                       `json:"status"`
	ErrorType       string                       `json:"error_type,omitempty"`
	Message         string                       `json:"message,omitempty"`
	LatencyMS       int64                        `json:"latency_ms"`
	CategoriesCount int                          `json:"categories_count"`
	FiltersCount    int                          `json:"filters_count"`
	ItemsCount      int                          `json:"items_count"`
	SampleItems     []ProviderDiagnoseSampleItem `json:"sample_items,omitempty"`
	Metrics         map[string]any               `json:"metrics,omitempty"`
}

type ProviderDiagnoseSampleItem struct {
	SourceItemID string `json:"source_item_id,omitempty"`
	Title        string `json:"title,omitempty"`
	ItemType     string `json:"item_type,omitempty"`
	Year         *int32 `json:"year,omitempty"`
	PosterHash   string `json:"poster_hash,omitempty"`
	Remarks      string `json:"remarks,omitempty"`
}

type providerDiagnoseHomePayload struct {
	Class   []providerDiagnoseCategory `json:"class"`
	Classes []providerDiagnoseCategory `json:"classes"`
	List    []cmsVOD                   `json:"list"`
	Filters json.RawMessage            `json:"filters"`
}

type providerDiagnoseCategory struct {
	TypeID   string `json:"type_id"`
	TypeName string `json:"type_name"`
}

func (m *ProviderRuntimeManager) Diagnose(ctx context.Context, providerID int64, req ProviderDiagnoseRequest) (*ProviderDiagnoseResponse, error) {
	start := time.Now()
	provider, row, err := m.provider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "diagnose", err)
		return nil, err
	}
	methods := normalizeProviderDiagnoseMethods(req.Methods)
	timeout := providerDiagnoseTimeout(req.TimeoutMS, row.TimeoutMS)
	resp := &ProviderDiagnoseResponse{
		ProviderID:   row.ID,
		ProviderName: row.Name,
		SourceKey:    row.SourceKey,
		RuntimeKind:  row.RuntimeKind,
		ProviderKind: row.ProviderKind,
		Results:      make([]ProviderDiagnoseMethodResult, 0, len(methods)),
	}
	for _, method := range methods {
		result := m.diagnoseMethod(ctx, provider, row, method, req, timeout)
		resp.Results = append(resp.Results, result)
	}
	resp.DurationMS = time.Since(start).Milliseconds()
	resp.OverallStatus = providerDiagnoseOverallStatus(resp.Results)
	LogProviderAction(m.logger, start, row.ID, "diagnose", nil, "status", resp.OverallStatus, "methods", len(resp.Results))
	return resp, nil
}

func (m *ProviderRuntimeManager) diagnoseMethod(ctx context.Context, provider Provider, row *repository.SourceProvider, method string, req ProviderDiagnoseRequest, timeout time.Duration) ProviderDiagnoseMethodResult {
	start := time.Now()
	result := ProviderDiagnoseMethodResult{Method: method, Status: ProviderDiagnoseStatusSkipped}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		return providerDiagnoseErrorResult(method, start, err)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch method {
	case "home":
		return diagnoseProviderHome(callCtx, provider, start)
	case "homeVideo":
		return diagnoseProviderHomeVideo(callCtx, provider, start)
	case "category":
		categoryID := strings.TrimSpace(req.CategoryID)
		if categoryID == "" {
			categories, err := provider.Categories(callCtx)
			if err != nil {
				return providerDiagnoseErrorResult(method, start, err)
			}
			if len(categories) == 0 {
				return ProviderDiagnoseMethodResult{
					Method:    method,
					Status:    ProviderDiagnoseStatusSkipped,
					Message:   "未提供 category_id，且 homeContent 未返回可用分类。",
					LatencyMS: time.Since(start).Milliseconds(),
				}
			}
			categoryID = categories[0].ID
		}
		page, err := provider.Category(callCtx, CategoryRequest{CategoryID: categoryID, Page: 1})
		if err != nil {
			return providerDiagnoseErrorResult(method, start, err)
		}
		result = providerDiagnosePageResult(method, start, page)
		if result.Metrics == nil {
			result.Metrics = map[string]any{}
		}
		result.Metrics["category_id"] = categoryID
		return result
	case "search":
		keyword := strings.TrimSpace(req.Keyword)
		if keyword == "" {
			keyword = "test"
		}
		page, err := provider.Search(callCtx, SearchRequest{Keyword: keyword, Page: 1})
		if err != nil {
			return providerDiagnoseErrorResult(method, start, err)
		}
		result = providerDiagnosePageResult(method, start, page)
		if result.Metrics == nil {
			result.Metrics = map[string]any{}
		}
		result.Metrics["keyword_len"] = len(keyword)
		return result
	case "detail":
		sourceItemID := strings.TrimSpace(req.SourceItemID)
		if sourceItemID == "" {
			sourceItemID = strings.TrimSpace(req.DetailID)
		}
		if sourceItemID == "" {
			return ProviderDiagnoseMethodResult{
				Method:    method,
				Status:    ProviderDiagnoseStatusSkipped,
				Message:   "未提供 source_item_id/detail_id，detail 诊断已跳过。",
				LatencyMS: time.Since(start).Milliseconds(),
			}
		}
		detail, err := provider.Detail(callCtx, sourceItemID)
		if err != nil {
			return providerDiagnoseErrorResult(method, start, err)
		}
		items := []SourceItemSnapshot{}
		if detail != nil {
			items = append(items, detail.Item)
		}
		return providerDiagnoseItemsResult(method, start, items, map[string]any{
			"source_item_hash":  URLHash(sourceItemID),
			"play_source_count": providerDiagnosePlaySourceCount(detail),
		})
	default:
		return ProviderDiagnoseMethodResult{
			Method:    method,
			Status:    ProviderDiagnoseStatusUnsupported,
			Message:   "暂不支持的诊断 method。",
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}
}

func diagnoseProviderHomeVideo(ctx context.Context, provider Provider, start time.Time) ProviderDiagnoseMethodResult {
	switch p := provider.(type) {
	case *CSPProvider:
		raw, err := p.runData(ctx, CSPRuntimeMethodHomeVideo, nil)
		if err != nil {
			return providerDiagnoseErrorResult("homeVideo", start, err)
		}
		result := providerDiagnoseHomeResult("homeVideo", start, raw, p.baseForImages())
		result.CategoriesCount = 0
		result.FiltersCount = 0
		result.Status = providerDiagnoseStatus(0, 0, result.ItemsCount)
		return result
	case *JSProvider:
		return ProviderDiagnoseMethodResult{
			Method:    "homeVideo",
			Status:    ProviderDiagnoseStatusUnsupported,
			Message:   "JS runtime 当前未提供独立 homeVideoContent，首页列表来自 home。",
			LatencyMS: time.Since(start).Milliseconds(),
		}
	case *CMSProvider:
		return ProviderDiagnoseMethodResult{
			Method:    "homeVideo",
			Status:    ProviderDiagnoseStatusUnsupported,
			Message:   "native CMS 没有独立 homeVideoContent，首页列表来自 CMS ac=list。",
			LatencyMS: time.Since(start).Milliseconds(),
		}
	default:
		return ProviderDiagnoseMethodResult{
			Method:    "homeVideo",
			Status:    ProviderDiagnoseStatusUnsupported,
			Message:   "当前 Provider 不支持 homeVideoContent。",
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}
}

func diagnoseProviderHome(ctx context.Context, provider Provider, start time.Time) ProviderDiagnoseMethodResult {
	switch p := provider.(type) {
	case *JSProvider:
		raw, err := p.runData(ctx, JSRuntimeMethodHome, nil)
		if err != nil {
			return providerDiagnoseErrorResult("home", start, err)
		}
		return providerDiagnoseHomeResult("home", start, raw, p.baseForImages())
	case *CSPProvider:
		raw, err := p.runData(ctx, CSPRuntimeMethodHome, map[string]any{"filter": true})
		if err != nil {
			return providerDiagnoseErrorResult("home", start, err)
		}
		return providerDiagnoseHomeResult("home", start, raw, p.baseForImages())
	case *CMSProvider:
		var payload cmsResponse
		if err := p.getCMS(ctx, map[string]string{"ac": p.categoryAC}, &payload); err != nil {
			return providerDiagnoseErrorResult("home", start, err)
		}
		page := parseCMSPage(p.api, payload, false)
		return providerDiagnoseItemsResult("home", start, page.Items, map[string]any{
			"page":       page.Page,
			"page_count": page.PageCount,
			"total":      page.Total,
		}).withCounts(len(payload.Class), 0, len(page.Items))
	default:
		categories, err := provider.Categories(ctx)
		if err != nil {
			return providerDiagnoseErrorResult("home", start, err)
		}
		return ProviderDiagnoseMethodResult{
			Method:          "home",
			Status:          providerDiagnoseStatus(len(categories), 0, 0),
			CategoriesCount: len(categories),
			LatencyMS:       time.Since(start).Milliseconds(),
		}
	}
}

func providerDiagnoseHomeResult(method string, start time.Time, raw json.RawMessage, imageBase string) ProviderDiagnoseMethodResult {
	var payload providerDiagnoseHomePayload
	if err := decodeRuntimeData(raw, &payload); err != nil {
		return providerDiagnoseErrorResult(method, start, err)
	}
	categoriesCount := len(payload.Class)
	if categoriesCount == 0 {
		categoriesCount = len(payload.Classes)
	}
	cmsPayload := cmsResponse{List: payload.List}
	page := parseCMSPage(imageBase, cmsPayload, false)
	result := providerDiagnoseItemsResult(method, start, page.Items, nil)
	result.CategoriesCount = categoriesCount
	result.FiltersCount = providerDiagnoseFiltersCount(payload.Filters)
	result.ItemsCount = len(page.Items)
	result.Status = providerDiagnoseStatus(result.CategoriesCount, result.FiltersCount, result.ItemsCount)
	return result
}

func providerDiagnosePageResult(method string, start time.Time, page *ProviderPage) ProviderDiagnoseMethodResult {
	if page == nil {
		return ProviderDiagnoseMethodResult{Method: method, Status: ProviderDiagnoseStatusEmpty, LatencyMS: time.Since(start).Milliseconds()}
	}
	return providerDiagnoseItemsResult(method, start, page.Items, map[string]any{
		"page":       page.Page,
		"page_count": page.PageCount,
		"total":      page.Total,
	})
}

func providerDiagnoseItemsResult(method string, start time.Time, items []SourceItemSnapshot, metrics map[string]any) ProviderDiagnoseMethodResult {
	return ProviderDiagnoseMethodResult{
		Method:      method,
		Status:      providerDiagnoseStatus(0, 0, len(items)),
		LatencyMS:   time.Since(start).Milliseconds(),
		ItemsCount:  len(items),
		SampleItems: providerDiagnoseSampleItems(items),
		Metrics:     metrics,
	}
}

func (r ProviderDiagnoseMethodResult) withCounts(categoriesCount, filtersCount, itemsCount int) ProviderDiagnoseMethodResult {
	r.CategoriesCount = categoriesCount
	r.FiltersCount = filtersCount
	r.ItemsCount = itemsCount
	r.Status = providerDiagnoseStatus(categoriesCount, filtersCount, itemsCount)
	return r
}

func providerDiagnoseErrorResult(method string, start time.Time, err error) ProviderDiagnoseMethodResult {
	return ProviderDiagnoseMethodResult{
		Method:    method,
		Status:    ProviderDiagnoseStatusError,
		ErrorType: ErrorType(err),
		Message:   err.Error(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
}

func providerDiagnoseStatus(categoriesCount, filtersCount, itemsCount int) string {
	if categoriesCount > 0 || filtersCount > 0 || itemsCount > 0 {
		return ProviderDiagnoseStatusOK
	}
	return ProviderDiagnoseStatusEmpty
}

func providerDiagnoseFiltersCount(raw json.RawMessage) int {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return 0
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		return len(obj)
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	return 0
}

func providerDiagnoseSampleItems(items []SourceItemSnapshot) []ProviderDiagnoseSampleItem {
	limit := min(len(items), providerDiagnoseSampleLimit)
	out := make([]ProviderDiagnoseSampleItem, 0, limit)
	for i := 0; i < limit; i++ {
		item := items[i]
		posterHash := ""
		if item.PosterURL != nil {
			posterHash = URLHash(*item.PosterURL)
		}
		remarks := ""
		if item.Remarks != nil {
			remarks = *item.Remarks
		}
		out = append(out, ProviderDiagnoseSampleItem{
			SourceItemID: item.SourceItemID,
			Title:        item.Title,
			ItemType:     item.ItemType,
			Year:         item.Year,
			PosterHash:   posterHash,
			Remarks:      remarks,
		})
	}
	return out
}

func providerDiagnosePlaySourceCount(detail *ProviderDetail) int {
	if detail == nil {
		return 0
	}
	return len(detail.PlaySources)
}

func normalizeProviderDiagnoseMethods(methods []string) []string {
	if len(methods) == 0 {
		return []string{"home", "homeVideo", "category", "search"}
	}
	out := make([]string, 0, len(methods))
	seen := map[string]struct{}{}
	for _, method := range methods {
		normalized := normalizeProviderDiagnoseMethod(method)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return []string{"home", "homeVideo", "category", "search"}
	}
	return out
}

func normalizeProviderDiagnoseMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "home", "homecontent":
		return "home"
	case "homevideo", "homevideocontent", "home_video":
		return "homeVideo"
	case "category", "categorycontent":
		return "category"
	case "search", "searchcontent":
		return "search"
	case "detail", "detailcontent":
		return "detail"
	default:
		return strings.TrimSpace(method)
	}
}

func providerDiagnoseTimeout(requestTimeoutMS int, providerTimeoutMS int32) time.Duration {
	timeout := providerDiagnoseDefaultTimeout
	if providerTimeoutMS > 0 {
		timeout = time.Duration(providerTimeoutMS) * time.Millisecond
	}
	if requestTimeoutMS > 0 {
		timeout = time.Duration(requestTimeoutMS) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = providerDiagnoseDefaultTimeout
	}
	if timeout > providerDiagnoseMaxTimeout {
		timeout = providerDiagnoseMaxTimeout
	}
	return timeout
}

func providerDiagnoseOverallStatus(results []ProviderDiagnoseMethodResult) string {
	hasOK := false
	hasError := false
	for _, result := range results {
		switch result.Status {
		case ProviderDiagnoseStatusOK:
			hasOK = true
		case ProviderDiagnoseStatusError:
			hasError = true
		}
	}
	if hasOK && hasError {
		return "partial_ok"
	}
	if hasOK {
		return ProviderDiagnoseStatusOK
	}
	if hasError {
		return ProviderDiagnoseStatusError
	}
	return ProviderDiagnoseStatusEmpty
}
