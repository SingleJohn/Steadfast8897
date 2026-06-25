package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
	sourcebridge "fyms/internal/source"
)

func testSourceRuntimeJS(c *gin.Context, state *AppState) {
	var req sourcebridge.JSRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if state == nil || state.JSRuntime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "JS runtime 未初始化"})
		return
	}
	resp, err := state.JSRuntime.Run(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func testSourceRuntimeCSP(c *gin.Context, state *AppState) {
	var req sourcebridge.CSPRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if state == nil || state.CSPRuntime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "CSP runtime 未初始化"})
		return
	}
	resp, err := state.CSPRuntime.Run(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func listSourceRuntimeArtifacts(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	items, err := state.Repo.Source.ListRuntimeArtifacts(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": sourceRuntimeArtifactDTOs(items)})
}

func trustSourceRuntimeArtifact(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid artifact id"})
		return
	}
	item, err := state.Repo.Source.TrustRuntimeArtifact(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sourceRuntimeArtifactDTOFromRepository(*item))
}

func listSourceRuntimeInvocations(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	providerID, ok := queryInt64Ptr(c, "provider_id")
	if !ok {
		return
	}
	startTime, ok := queryTimePtr(c, "start_time")
	if !ok {
		return
	}
	endTime, ok := queryTimePtr(c, "end_time")
	if !ok {
		return
	}
	items, err := state.Repo.Source.ListRuntimeInvocations(c.Request.Context(), repository.SourceRuntimeInvocationListOptions{
		Limit:       int64(queryInt(c, "limit", 100)),
		Offset:      int64(queryInt(c, "offset", 0)),
		ProviderID:  providerID,
		Method:      strings.TrimSpace(c.Query("method")),
		Status:      strings.TrimSpace(c.Query("status")),
		ErrorType:   strings.TrimSpace(c.Query("error_type")),
		RuntimeKind: strings.TrimSpace(c.Query("runtime_kind")),
		StartTime:   startTime,
		EndTime:     endTime,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	names := sourceProviderNameLookup(c.Request.Context(), state, items)
	c.JSON(http.StatusOK, gin.H{"items": sourceRuntimeInvocationSummaryDTOs(items, names)})
}

func getSourceRuntimeInvocation(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	item, err := state.Repo.Source.GetRuntimeInvocationByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "runtime invocation not found"})
		return
	}
	names := sourceProviderNameLookup(c.Request.Context(), state, []repository.SourceRuntimeInvocation{*item})
	c.JSON(http.StatusOK, sourceRuntimeInvocationDetailDTOFromRepository(*item, names))
}

type sourceRuntimeInvocationSummaryDTO struct {
	ID           int64
	ProviderID   *int64
	ProviderName string
	RuntimeKind  string
	Method       string
	Status       string
	ErrorType    *string
	DurationMS   int64
	InvokedAt    time.Time
	URLHash      *string
}

type sourceRuntimeInvocationDetailDTO struct {
	sourceRuntimeInvocationSummaryDTO
	ErrorMessage *string
	EngineOK     *bool
	WorkerPID    *int32
	ArtifactIDs  []int64
	Raw          json.RawMessage
}

type sourceRuntimeArtifactDTO struct {
	ID            int64
	ProviderID    *int64
	SourceType    string
	ArtifactKind  string
	Name          string
	SourceURL     string
	SourceURLHash string
	BaseURL       *string
	RelativePath  *string
	MD5           string
	SHA256        string
	ByteSize      int64
	ContentType   *string
	TrustStatus   string
	Status        string
	LastFetchedAt time.Time
	VerifiedAt    *time.Time
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func sourceRuntimeInvocationSummaryDTOs(items []repository.SourceRuntimeInvocation, names map[int64]string) []sourceRuntimeInvocationSummaryDTO {
	out := make([]sourceRuntimeInvocationSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, sourceRuntimeInvocationSummaryDTOFromRepository(item, names))
	}
	return out
}

func sourceRuntimeInvocationSummaryDTOFromRepository(item repository.SourceRuntimeInvocation, names map[int64]string) sourceRuntimeInvocationSummaryDTO {
	providerName := ""
	if item.ProviderID != nil {
		providerName = names[*item.ProviderID]
	}
	return sourceRuntimeInvocationSummaryDTO{
		ID:           item.ID,
		ProviderID:   item.ProviderID,
		ProviderName: providerName,
		RuntimeKind:  item.RuntimeKind,
		Method:       item.Method,
		Status:       item.Status,
		ErrorType:    item.ErrorType,
		DurationMS:   item.DurationMS,
		InvokedAt:    item.InvokedAt,
		URLHash:      item.URLHash,
	}
}

func sourceRuntimeInvocationDetailDTOFromRepository(item repository.SourceRuntimeInvocation, names map[int64]string) sourceRuntimeInvocationDetailDTO {
	return sourceRuntimeInvocationDetailDTO{
		sourceRuntimeInvocationSummaryDTO: sourceRuntimeInvocationSummaryDTOFromRepository(item, names),
		ErrorMessage:                      item.ErrorMessage,
		EngineOK:                          item.EngineOK,
		WorkerPID:                         item.WorkerPID,
		ArtifactIDs:                       item.ArtifactIDs,
		Raw:                               item.Raw,
	}
}

func sourceRuntimeArtifactDTOs(items []repository.SourceRuntimeArtifact) []sourceRuntimeArtifactDTO {
	out := make([]sourceRuntimeArtifactDTO, 0, len(items))
	for _, item := range items {
		out = append(out, sourceRuntimeArtifactDTOFromRepository(item))
	}
	return out
}

func sourceRuntimeArtifactDTOFromRepository(item repository.SourceRuntimeArtifact) sourceRuntimeArtifactDTO {
	return sourceRuntimeArtifactDTO{
		ID:            item.ID,
		ProviderID:    item.ProviderID,
		SourceType:    item.SourceType,
		ArtifactKind:  item.ArtifactKind,
		Name:          item.Name,
		SourceURL:     redactedRuntimeURL(item.SourceURL),
		SourceURLHash: sourcebridge.URLHash(item.SourceURL),
		BaseURL:       redactedRuntimeURLPtr(item.BaseURL),
		RelativePath:  item.RelativePath,
		MD5:           item.MD5,
		SHA256:        item.SHA256,
		ByteSize:      item.ByteSize,
		ContentType:   item.ContentType,
		TrustStatus:   item.TrustStatus,
		Status:        item.Status,
		LastFetchedAt: item.LastFetchedAt,
		VerifiedAt:    item.VerifiedAt,
		LastError:     item.LastError,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func sourceProviderNameLookup(ctx context.Context, state *AppState, items []repository.SourceRuntimeInvocation) map[int64]string {
	names := map[int64]string{}
	for _, item := range items {
		if item.ProviderID == nil {
			continue
		}
		id := *item.ProviderID
		if _, ok := names[id]; ok {
			continue
		}
		provider, err := state.Repo.Source.GetProviderByID(ctx, id)
		if err == nil && provider != nil {
			names[id] = provider.Name
		}
	}
	return names
}

func queryTimePtr(c *gin.Context, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid " + name})
		return nil, false
	}
	return &value, true
}

func redactedRuntimeURLPtr(value *string) *string {
	if value == nil {
		return nil
	}
	redacted := redactedRuntimeURL(*value)
	return &redacted
}

func redactedRuntimeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx] + "?..."
		}
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
