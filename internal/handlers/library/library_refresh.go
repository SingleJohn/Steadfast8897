package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/repository"
	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

func refreshAll(c *gin.Context) {
	state := GetState(c)
	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	scanStarted := shouldRunLibraryScan(req, hasBody, true)
	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}
	if len(scopes) > 0 && state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	resp := gin.H{"status": "accepted", "scan_started": scanStarted}
	queueRefreshAfterScan := scanStarted && len(scopes) > 0
	if queueRefreshAfterScan {
		resp["refresh_queued_after_scan"] = true
		resp["refresh_scopes"] = refreshScopeNames(scopes)
	}
	if len(scopes) > 0 && !queueRefreshAfterScan {
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(c.Request.Context(), state, nil, scopes, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		resp["queued_tasks"] = queuedTasks
		resp["scope_items"] = scopeItems
		resp["allow_remote"] = opts.AllowRemote
		resp["validate_only"] = opts.ValidateOnly
	}

	if scanStarted {
		go func() {
			ctx := context.Background()
			if queueRefreshAfterScan {
				services.ScanAllLibrariesWithOptions(ctx, state.DB, state.Cache, state.ScanProgress, state.Ingest, func(lib models.Library) services.ScanLibraryOptions {
					libID := lib.ID
					return services.ScanLibraryOptions{
						AfterComplete: enqueueLibraryRefreshScopesAfterScan(state, &libID, scopes, opts),
					}
				})
				return
			}
			services.ScanAllLibraries(ctx, state.DB, state.Cache, state.ScanProgress, state.Ingest)
		}()
	}
	c.JSON(http.StatusAccepted, resp)
}

func refreshSingle(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	lib, err := state.Repo.ScanIngest.GetLibraryByItemID(ctx, *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	go func() {
		bg := context.Background()
		services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func getVirtualFolders(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	var scope *shared.UserLibraryScope
	if authUser := middleware.GetAuthUser(c); authUser != nil && !authUser.IsAdmin {
		var err error
		scope, err = shared.LoadUserLibraryScope(ctx, state, authUser.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	libs, err := state.Repo.Libraries.ListLibraries(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(libs))
	for _, lib := range libs {
		idStr := lib.ID.String()
		if scope != nil && !scope.AllowsLibrary(idStr) {
			continue
		}
		locations := lib.Paths
		if locations == nil {
			locations = []string{}
		}

		var itemCount int64
		itemCount, _ = state.Repo.Playback.CountItemsByLibrary(ctx, lib.ID)

		entry := gin.H{
			"Name":               lib.Name,
			"Locations":          locations,
			"CollectionType":     lib.CollectionType,
			"ItemId":             idStr,
			"Guid":               idStr,
			"RecursiveItemCount": itemCount,
		}
		if lib.PrimaryImageTag != nil {
			entry["ImageTag"] = *lib.PrimaryImageTag
		}
		out = append(out, entry)
	}
	c.JSON(http.StatusOK, out)
}

func getScanProgress(c *gin.Context) {
	state := GetState(c)
	all := state.ScanProgress.GetAll()
	items := make([]gin.H, 0, len(all))
	for _, p := range all {
		entry := gin.H{
			"LibraryId":      p.LibraryID,
			"LibraryName":    p.LibraryName,
			"Status":         p.Status,
			"TotalItems":     p.TotalItems,
			"ProcessedItems": p.ProcessedItems,
			"Percentage":     p.Percentage,
			"StartedAt":      time.UnixMilli(p.StartedAt).UTC().Format(time.RFC3339),
		}
		if p.CurrentItem != nil {
			entry["CurrentItem"] = *p.CurrentItem
		}
		if p.CompletedAt != nil {
			entry["CompletedAt"] = time.UnixMilli(*p.CompletedAt).UTC().Format(time.RFC3339)
		}
		if p.Error != nil {
			entry["Error"] = *p.Error
		}
		items = append(items, entry)
	}
	c.JSON(http.StatusOK, gin.H{"Items": items})
}

func startProbe(c *gin.Context) {
	state := GetState(c)
	threads := 5
	if s := strings.TrimSpace(c.Query("threads")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		threads = n
	}

	if t := state.TaskCenter.Get(taskcenter.KindProbe); t != nil {
		_, err := t.Start(c.Request.Context(), taskcenter.StartParams{"threads": threads}, taskcenter.TriggerManual)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
		return
	}

	if err := state.ProbeTask.Start(state.DB, threads); err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

func stopProbe(c *gin.Context) {
	state := GetState(c)
	if t := state.TaskCenter.Get(taskcenter.KindProbe); t != nil {
		_ = t.Stop()
	} else {
		state.ProbeTask.Stop()
	}
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

type rescrapeProgressResponse struct {
	Running      bool  `json:"running"`
	Total        int64 `json:"total"`
	Success      int64 `json:"success"`
	NotFound     int64 `json:"not_found"`
	FetchError   int64 `json:"fetch_error"`
	Processed    int64 `json:"processed"`
	PendingTotal int64 `json:"pending_total"`
	Percentage   int   `json:"percentage"`
}

type platformTaskSummary struct {
	ScanRunning      bool                     `json:"scan_running"`
	PendingTotal     int64                    `json:"pending_total"`
	PendingTMDBReady int64                    `json:"pending_tmdb_ready_total"`
	PendingMetadata  int64                    `json:"pending_metadata_total"`
	ItemsTotal       int64                    `json:"items_total"`
	Rescrape         rescrapeProgressResponse `json:"rescrape"`
}

// scrapeProgressResponse 替代已删除的 services.ScrapeProgress。
// 方案 C 后刮削由 scrape_queue + ScrapeWorker 持续消费驱动,
// 这里的字段是从 scrape_queue.Stats 派生出的兼容 shape,
// 供 /Library/Scrape/Progress 和 /Library/Tasks/Summary 保持旧契约。
type scrapeProgressResponse struct {
	Status         string `json:"status"`
	TotalItems     int64  `json:"total_items"`
	ProcessedItems int64  `json:"processed_items"`
	SuccessItems   int64  `json:"success_items"`
	FailedItems    int64  `json:"failed_items"`
	CurrentItem    string `json:"current_item,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	Percentage     int    `json:"percentage"`
	MissingCount   int64  `json:"missing_count"`
	ItemsTotal     int64  `json:"items_total"`
}

type taskSummaryResponse struct {
	Scrape   scrapeProgressResponse `json:"scrape"`
	Probe    services.ProbeProgress `json:"probe"`
	Platform platformTaskSummary    `json:"platform"`
}

func buildEffectiveProbeProgress(ctx context.Context, state *AppState) services.ProbeProgress {
	prog := state.ProbeTask.GetProgress()
	if prog.Status != "running" && prog.Status != "stopping" {
		if cnt, err := services.GetMissingMediainfoCount(ctx, state.DB); err == nil {
			prog.MissingCount = cnt
		}
		if prog.MissingCount > 0 {
			prog.Status = "idle"
		}
	}
	if total, err := services.GetTotalMediaVersionsCount(ctx, state.DB); err == nil {
		prog.VersionsTotal = total
	}
	return prog
}

// buildEffectiveScrapeProgress 从 scrape_queue 派生刮削整体进度。
// 方案 C 后不再有 legacy ScrapeTask 的单一运行态:
//   - status: pending+running > 0 → running;否则 idle
//   - processed = done;total = done + pending + running + failed
//   - success/failed 分别映射到 done/failed
//   - missing_count 仍从 items 表(缺 overview 的 Movie/Series)查,
//     和 scrape_queue 各自独立,用于 UI 提示"还有多少待刮"
func buildEffectiveScrapeProgress(ctx context.Context, state *AppState) scrapeProgressResponse {
	resp := scrapeProgressResponse{Status: "idle"}
	if state.ScrapeQueue != nil {
		if stats, err := state.ScrapeQueue.Stats(ctx); err == nil {
			resp.SuccessItems = stats.Done
			resp.FailedItems = stats.Failed
			resp.ProcessedItems = stats.Done
			resp.TotalItems = stats.Done + stats.Pending + stats.Running + stats.Failed
			if stats.Pending+stats.Running > 0 {
				resp.Status = "running"
			}
			if resp.TotalItems > 0 {
				resp.Percentage = int(resp.ProcessedItems * 100 / resp.TotalItems)
			}
		}
	}
	if cnt, err := services.GetMissingScrapeCount(ctx, state.DB); err == nil {
		resp.MissingCount = cnt
	}
	if total, err := services.GetTopLevelItemCount(ctx, state.DB); err == nil {
		resp.ItemsTotal = total
	}
	return resp
}

func buildRescrapeProgressResponse(ctx context.Context, state *AppState) rescrapeProgressResponse {
	rescrapeProgress.mu.Lock()
	running := rescrapeProgress.Running
	rescrapeProgress.mu.Unlock()

	processed := atomic.LoadInt64(&rescrapeProgress.Processed)
	success := atomic.LoadInt64(&rescrapeProgress.Success)
	notFound := atomic.LoadInt64(&rescrapeProgress.NotFound)
	fetchError := atomic.LoadInt64(&rescrapeProgress.FetchError)
	total := atomic.LoadInt64(&rescrapeProgress.Total)
	pendingTotal := int64(0)
	if !running {
		if cnt, err := models.CountItemsPendingPlatformScan(ctx, state.DB, false, false); err == nil {
			pendingTotal = cnt
		}
	}
	pct := 0
	if total > 0 {
		pct = int(processed * 100 / total)
	}

	return rescrapeProgressResponse{
		Running:      running,
		Total:        total,
		Success:      success,
		NotFound:     notFound,
		FetchError:   fetchError,
		Processed:    processed,
		PendingTotal: pendingTotal,
		Percentage:   pct,
	}
}

func buildPlatformTaskSummary(ctx context.Context, state *AppState) platformTaskSummary {
	pendingTotal, _ := models.CountItemsPendingPlatformScan(ctx, state.DB, false, false)
	pendingTMDBReady, _ := models.CountItemsPendingPlatformScan(ctx, state.DB, true, false)
	pendingMetadata, _ := models.CountItemsPendingPlatformMetadataScrape(ctx, state.DB)
	itemsTotal, _ := services.GetTopLevelItemCount(ctx, state.DB)

	platformScanState.mu.Lock()
	scanRunning := platformScanState.running
	platformScanState.mu.Unlock()

	return platformTaskSummary{
		ScanRunning:      scanRunning,
		PendingTotal:     pendingTotal,
		PendingTMDBReady: pendingTMDBReady,
		PendingMetadata:  pendingMetadata,
		ItemsTotal:       itemsTotal,
		Rescrape:         buildRescrapeProgressResponse(ctx, state),
	}
}

func getProbeProgress(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, buildEffectiveProbeProgress(c.Request.Context(), state))
}

type itemRefreshRequest struct {
	Scope              string `json:"scope"`
	Metadata           *bool  `json:"metadata"`
	Images             *bool  `json:"images"`
	ReplaceAllMetadata bool   `json:"replace_all_metadata"`
	ReplaceAllImages   bool   `json:"replace_all_images"`
	ValidateOnly       bool   `json:"validate_only"`
	AllowRemote        *bool  `json:"allow_remote"`
	RefreshSubtree     bool   `json:"refresh_subtree"`
}

type libraryRefreshRequest struct {
	Scan               *bool  `json:"scan"`
	Scope              string `json:"scope"`
	Metadata           *bool  `json:"metadata"`
	Images             *bool  `json:"images"`
	ReplaceAllMetadata bool   `json:"replace_all_metadata"`
	ReplaceAllImages   bool   `json:"replace_all_images"`
	ValidateOnly       bool   `json:"validate_only"`
	AllowRemote        *bool  `json:"allow_remote"`
	RefreshSubtree     bool   `json:"refresh_subtree"`
}

func parseItemRefreshRequest(c *gin.Context) (itemRefreshRequest, error) {
	var req itemRefreshRequest
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return req, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, err
	}
	return req, nil
}

func parseLibraryRefreshRequest(c *gin.Context) (libraryRefreshRequest, bool, error) {
	var req libraryRefreshRequest
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return req, false, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return req, false, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, true, err
	}
	return req, true, nil
}

func (r libraryRefreshRequest) toItemRefreshRequest() itemRefreshRequest {
	return itemRefreshRequest{
		Scope:              r.Scope,
		Metadata:           r.Metadata,
		Images:             r.Images,
		ReplaceAllMetadata: r.ReplaceAllMetadata,
		ReplaceAllImages:   r.ReplaceAllImages,
		ValidateOnly:       r.ValidateOnly,
		AllowRemote:        r.AllowRemote,
		RefreshSubtree:     r.RefreshSubtree,
	}
}

func hasExplicitLibraryRefresh(req libraryRefreshRequest) bool {
	return strings.TrimSpace(req.Scope) != "" ||
		req.Metadata != nil ||
		req.Images != nil ||
		req.ReplaceAllMetadata ||
		req.ReplaceAllImages ||
		req.ValidateOnly ||
		req.AllowRemote != nil ||
		req.RefreshSubtree
}

func isSubtreeRefreshScope(scope string) bool {
	return strings.EqualFold(strings.TrimSpace(scope), string(services.RefreshScopeSubtree))
}

func shouldRunLibraryScan(req libraryRefreshRequest, hasBody bool, defaultScan bool) bool {
	if req.Scan != nil {
		return *req.Scan
	}
	if hasBody && hasExplicitLibraryRefresh(req) {
		return false
	}
	return defaultScan
}

func resolveLibraryRefreshScopes(req libraryRefreshRequest, hasBody bool, defaultScopes []services.RefreshScope) ([]services.RefreshScope, error) {
	if !hasBody || !hasExplicitLibraryRefresh(req) {
		return defaultScopes, nil
	}
	return resolveItemRefreshScopes(req.toItemRefreshRequest())
}

func buildLibraryRefreshOptions(req libraryRefreshRequest) services.RefreshOptions {
	opts := services.DefaultRefreshOptionsForSource(services.RefreshSourceManual)
	opts.AllowRemote = false
	opts.ReplaceAllMetadata = req.ReplaceAllMetadata
	opts.ReplaceAllImages = req.ReplaceAllImages
	opts.ValidateOnly = req.ValidateOnly
	opts.RefreshSubtree = req.RefreshSubtree || isSubtreeRefreshScope(req.Scope)
	if req.AllowRemote != nil {
		opts.AllowRemote = *req.AllowRemote
	}
	return opts
}

func refreshItemTypesForScope(scope services.RefreshScope) []string {
	switch scope {
	case services.RefreshScopeMetadata:
		return []string{"Movie", "Series", "Episode"}
	case services.RefreshScopeImages:
		return []string{"Movie", "Series", "Season", "Episode"}
	case services.RefreshScopeSubtree:
		return []string{"Series"}
	default:
		return nil
	}
}

func loadLibraryRefreshTargetIDs(ctx context.Context, pool *pgxpool.Pool, libraryID *uuid.UUID, scope services.RefreshScope) ([]string, error) {
	types := refreshItemTypesForScope(scope)
	if len(types) == 0 {
		return nil, fmt.Errorf("unsupported batch refresh scope: %s", scope)
	}

	return repository.NewScanIngestRepository(pool).ListRefreshTargetIDs(ctx, libraryID, types)
}

func enqueueLibraryRefreshScopes(ctx context.Context, state *AppState, libraryID *uuid.UUID, scopes []services.RefreshScope, opts services.RefreshOptions) (map[string]int, int64, error) {
	scopeItems := make(map[string]int, len(scopes))
	var queuedTasks int64
	for _, scope := range scopes {
		itemIDs, err := loadLibraryRefreshTargetIDs(ctx, state.DB, libraryID, scope)
		if err != nil {
			return nil, 0, err
		}
		scopeItems[string(scope)] = len(itemIDs)
		n, err := state.RefreshQueue.EnqueueBatch(ctx, itemIDs, scope, services.RefreshSourceManual, services.RefreshPriorityManual, opts)
		if err != nil {
			return nil, 0, err
		}
		queuedTasks += n
	}
	return scopeItems, queuedTasks, nil
}

func enqueueLibraryRefreshScopesAfterScan(state *AppState, libraryID *uuid.UUID, scopes []services.RefreshScope, opts services.RefreshOptions) func(context.Context) {
	return func(ctx context.Context) {
		libraryLabel := "all"
		if libraryID != nil {
			libraryLabel = libraryID.String()
		}
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(ctx, state, libraryID, scopes, opts)
		if err != nil {
			slog.Warn("[LibraryRefresh] enqueue after scan failed", "library", libraryLabel, "error", err)
			return
		}
		slog.Info("[LibraryRefresh] queued after scan", "library", libraryLabel, "scopes", refreshScopeNames(scopes), "items", scopeItems, "tasks", queuedTasks)
	}
}

func refreshScopeNames(scopes []services.RefreshScope) []string {
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, string(scope))
	}
	return names
}

func resolveItemRefreshScopes(req itemRefreshRequest) ([]services.RefreshScope, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	switch scope {
	case "", string(services.RefreshScopeMetadata), string(services.RefreshScopeImages):
	case string(services.RefreshScopeSubtree):
		req.RefreshSubtree = true
	default:
		return nil, fmt.Errorf("unsupported refresh scope: %s", req.Scope)
	}

	if req.RefreshSubtree {
		return []services.RefreshScope{services.RefreshScopeSubtree}, nil
	}

	wantMetadata := scope == string(services.RefreshScopeMetadata)
	wantImages := scope == string(services.RefreshScopeImages)
	explicitSelection := scope != ""
	if req.Metadata != nil {
		wantMetadata = *req.Metadata
		explicitSelection = true
	}
	if req.Images != nil {
		wantImages = *req.Images
		explicitSelection = true
	}
	if !wantMetadata && !wantImages {
		if explicitSelection {
			return nil, fmt.Errorf("no refresh scope selected")
		}
		wantMetadata = true
	}

	scopes := make([]services.RefreshScope, 0, 2)
	if wantMetadata {
		scopes = append(scopes, services.RefreshScopeMetadata)
	}
	if wantImages {
		scopes = append(scopes, services.RefreshScopeImages)
	}
	return scopes, nil
}

func refreshItem(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	itemID := c.Param("itemId")
	ctx := c.Request.Context()
	item, err := models.GetItemByID(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	req, err := parseItemRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}

	scopes, err := resolveItemRefreshScopes(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	for _, scope := range scopes {
		if scope == services.RefreshScopeSubtree && item.ItemType != "Series" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "subtree refresh 仅支持 Series"})
			return
		}
	}

	opts := services.DefaultRefreshOptionsForSource(services.RefreshSourceManual)
	opts.ReplaceAllMetadata = req.ReplaceAllMetadata
	opts.ReplaceAllImages = req.ReplaceAllImages
	opts.ValidateOnly = req.ValidateOnly
	opts.RefreshSubtree = req.RefreshSubtree || isSubtreeRefreshScope(req.Scope)
	if req.AllowRemote != nil {
		opts.AllowRemote = *req.AllowRemote
	}
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}

	for _, scope := range scopes {
		if err := state.RefreshQueue.Enqueue(ctx, itemID, scope, services.RefreshSourceManual, services.RefreshPriorityManual, opts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	scopeNames := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scopeNames = append(scopeNames, string(scope))
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"queued":        true,
		"item_id":       itemID,
		"item_type":     item.ItemType,
		"scopes":        scopeNames,
		"allow_remote":  opts.AllowRemote,
		"validate_only": opts.ValidateOnly,
		"message":       "已加入刷新队列，稍后自动生效",
	})
}

func refreshSingleLibrary(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := state.Repo.Libraries.GetLibraryByID(ctx, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	scanStarted := shouldRunLibraryScan(req, hasBody, true)
	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}
	if len(scopes) > 0 && state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	resp := gin.H{"status": "accepted", "scan_started": scanStarted, "library_id": lib.ID.String()}
	queueRefreshAfterScan := scanStarted && len(scopes) > 0
	if queueRefreshAfterScan {
		resp["refresh_queued_after_scan"] = true
		resp["refresh_scopes"] = refreshScopeNames(scopes)
	}
	if len(scopes) > 0 && !queueRefreshAfterScan {
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(ctx, state, &lib.ID, scopes, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		resp["queued_tasks"] = queuedTasks
		resp["scope_items"] = scopeItems
		resp["allow_remote"] = opts.AllowRemote
		resp["validate_only"] = opts.ValidateOnly
	}

	if scanStarted {
		go func() {
			bg := context.Background()
			if queueRefreshAfterScan {
				services.ScanLibraryWithOptions(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name, services.ScanLibraryOptions{
					AfterComplete: enqueueLibraryRefreshScopesAfterScan(state, &lib.ID, scopes, opts),
				})
				return
			}
			services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
		}()
	}
	c.JSON(http.StatusAccepted, resp)
}
