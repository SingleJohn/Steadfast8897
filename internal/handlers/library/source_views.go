package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/repository"
	"fyms/internal/services/coverart"
	sourcebridge "fyms/internal/source"
)

type sourceViewRequest struct {
	Name           string         `json:"Name"`
	DisplayName    *string        `json:"DisplayName"`
	Dimension      string         `json:"Dimension"`
	MatchValue     string         `json:"MatchValue"`
	MatchValues    []string       `json:"MatchValues"`
	CollectionType string         `json:"CollectionType"`
	ProviderIDs    []int64        `json:"ProviderIds"`
	Filter         map[string]any `json:"Filter"`
	Enabled        *bool          `json:"Enabled"`
	ExposeToEmby   *bool          `json:"ExposeToEmby"`
	SortOrder      *int32         `json:"SortOrder"`
	Config         map[string]any `json:"Config"`
}

func listSourceViews(c *gin.Context, state *AppState) {
	views, err := state.Repo.Source.ListLibraryViews(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(views))
	for _, view := range views {
		items = append(items, sourceViewAdminDTO(view))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func createSourceView(c *gin.Context, state *AppState) {
	var req sourceViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	view, err := state.Repo.Source.UpsertLibraryView(c.Request.Context(), sourceViewUpsertFromRequest(req, nil))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sourceViewAdminDTO(*view))
}

func updateSourceView(c *gin.Context, state *AppState) {
	id, ok := parseSourceViewID(c)
	if !ok {
		return
	}
	existing, err := state.Repo.Source.GetLibraryViewByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source view not found"})
		return
	}
	var req sourceViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	view, err := state.Repo.Source.UpsertLibraryView(c.Request.Context(), sourceViewUpsertFromRequest(req, existing))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sourceViewAdminDTO(*view))
}

func deleteSourceView(c *gin.Context, state *AppState) {
	id, ok := parseSourceViewID(c)
	if !ok {
		return
	}
	if err := state.Repo.Source.DeleteLibraryView(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func renameSourceView(c *gin.Context, state *AppState) {
	id, ok := parseSourceViewID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"Name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	view, err := state.Repo.Source.RenameLibraryView(c.Request.Context(), id, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, sourceViewAdminDTO(*view))
}

func updateSourceViewDisplayOrder(c *gin.Context, state *AppState) {
	var req struct {
		OrderedIDs []int64  `json:"OrderedIds"`
		OrderedIds []int64  `json:"ordered_ids"`
		IDs        []int64  `json:"ids"`
		StringIDs  []string `json:"OrderedIdsText"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	ids := req.OrderedIDs
	if len(ids) == 0 {
		ids = req.OrderedIds
	}
	if len(ids) == 0 {
		ids = req.IDs
	}
	for _, raw := range req.StringIDs {
		if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "OrderedIds required"})
		return
	}
	if err := state.Repo.Source.UpdateLibraryViewSortOrder(c.Request.Context(), ids); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func discoverSourceViewValues(c *gin.Context, state *AppState) {
	dimension := strings.TrimSpace(c.Query("dimension"))
	if dimension == "" {
		dimension = "normalized_kind"
	}
	minCount := int64(1)
	if raw := strings.TrimSpace(c.Query("minCount")); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
			minCount = value
		}
	}
	values, err := state.Repo.Source.DiscoverLibraryViewValues(c.Request.Context(), dimension, c.Query("search"), minCount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dimension": dimension, "values": values})
}

func generateSourceViewCover(c *gin.Context, state *AppState) {
	id, ok := parseSourceViewID(c)
	if !ok {
		return
	}
	view, err := state.Repo.Source.GetLibraryViewByID(c.Request.Context(), id)
	if err != nil || view == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source view not found"})
		return
	}
	var body struct {
		Style   string         `json:"Style"`
		Options map[string]any `json:"Options"`
	}
	_ = c.ShouldBindJSON(&body)
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style"})
		return
	}
	tag, err := renderAndSaveSourceViewCover(c, state, *view, gen, body.Options)
	if err != nil {
		if errors.Is(err, coverart.ErrNoPosters) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "该在线库下没有可用海报素材,无法生成封面"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag, "Style": style})
}

func deleteSourceViewCover(c *gin.Context, state *AppState) {
	id, ok := parseSourceViewID(c)
	if !ok {
		return
	}
	oldPath, err := state.Repo.Source.ClearLibraryViewCover(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if oldPath != "" {
		_ = os.Remove(oldPath)
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func renderAndSaveSourceViewCover(c *gin.Context, state *AppState, view repository.SourceLibraryView, gen coverart.Generator, options map[string]any) (string, error) {
	posters, err := state.Repo.Source.ListPosterURLsForLibraryView(c.Request.Context(), view, 36)
	if err != nil {
		return "", err
	}
	if len(posters) == 0 {
		return "", coverart.ErrNoPosters
	}
	materials := make([]coverart.Material, 0, len(posters))
	for _, poster := range posters {
		materials = append(materials, coverart.Material{Title: viewDisplayName(view), PosterPath: poster})
	}
	out, err := gen.Render(c.Request.Context(), coverart.Input{
		LibraryID:      uuid.MustParse(view.PublicUUID),
		LibraryName:    viewDisplayName(view),
		CollectionType: view.CollectionType,
		ItemCount:      int(view.ItemCount),
		PosterPaths:    posters,
		Materials:      materials,
		Options:        options,
		OutputWidth:    1920,
		OutputHeight:   1080,
	})
	if err != nil {
		return "", err
	}
	imgDir := filepath.Join("data", "library-images", "source")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return "", err
	}
	fpath := filepath.Join(imgDir, view.PublicUUID+".jpg")
	f, err := os.Create(fpath)
	if err != nil {
		return "", err
	}
	if err := imaging.Encode(f, out.Image, imaging.JPEG, imaging.JPEGQuality(out.Quality)); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	tag := uuid.New().String()
	if _, err := state.Repo.Source.SetLibraryViewCover(c.Request.Context(), view.ID, fpath, tag); err != nil {
		return "", err
	}
	return tag, nil
}

func sourceViewUpsertFromRequest(req sourceViewRequest, existing *repository.SourceLibraryView) repository.SourceLibraryViewUpsert {
	dimension := strings.TrimSpace(req.Dimension)
	matchValue := strings.TrimSpace(req.MatchValue)
	if existing != nil {
		if dimension == "" {
			dimension = existing.Dimension
		}
		if matchValue == "" {
			matchValue = existing.MatchValue
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = matchValue
	}
	if existing != nil && name == "" {
		name = existing.Name
	}
	matchValues := compactSourceStrings(req.MatchValues)
	if len(matchValues) == 0 && matchValue != "" {
		matchValues = []string{matchValue}
	}
	enabled := true
	expose := false
	var sortOrder int32
	if existing != nil {
		enabled = existing.Enabled
		expose = existing.ExposeToEmby
		sortOrder = existing.SortOrder
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.ExposeToEmby != nil {
		expose = *req.ExposeToEmby
	}
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	collectionType := strings.TrimSpace(req.CollectionType)
	if collectionType == "" && existing != nil {
		collectionType = existing.CollectionType
	}
	if collectionType == "" {
		collectionType = "mixed"
	}
	return repository.SourceLibraryViewUpsert{
		PublicUUID:     sourcebridge.SourceLibraryViewPublicUUID(dimension, matchValue),
		Name:           name,
		DisplayName:    req.DisplayName,
		Dimension:      dimension,
		MatchValue:     matchValue,
		MatchValues:    matchValues,
		CollectionType: collectionType,
		ProviderIDs:    req.ProviderIDs,
		Filter:         sourcebridgeJSON(req.Filter),
		Enabled:        enabled,
		ExposeToEmby:   expose,
		SortOrder:      sortOrder,
		Config:         sourcebridgeJSON(req.Config),
	}
}

func sourceViewAdminDTO(view repository.SourceLibraryView) gin.H {
	coverURL := ""
	if view.CoverImagePath != nil && *view.CoverImagePath != "" {
		tag := ""
		if view.CoverImageTag != nil {
			tag = *view.CoverImageTag
		}
		coverURL = "/Items/" + view.PublicUUID + "/Images/Primary?tag=" + tag
	}
	return gin.H{
		"Id":             view.ID,
		"PublicUUID":     view.PublicUUID,
		"Name":           view.Name,
		"DisplayName":    viewDisplayName(view),
		"CustomName":     view.DisplayName,
		"Dimension":      view.Dimension,
		"MatchValue":     view.MatchValue,
		"MatchValues":    view.MatchValues,
		"CollectionType": view.CollectionType,
		"ProviderIds":    view.ProviderIDs,
		"Filter":         view.Filter,
		"Enabled":        view.Enabled,
		"ExposeToEmby":   view.ExposeToEmby,
		"SortOrder":      view.SortOrder,
		"ItemCount":      view.ItemCount,
		"HasCover":       coverURL != "",
		"CoverUrl":       coverURL,
	}
}

func viewDisplayName(view repository.SourceLibraryView) string {
	if view.DisplayName != nil && strings.TrimSpace(*view.DisplayName) != "" {
		return strings.TrimSpace(*view.DisplayName)
	}
	return view.Name
}

func parseSourceViewID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return 0, false
	}
	return id, true
}

func compactSourceStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sourcebridgeJSON(value map[string]any) []byte {
	if len(value) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(value)
	if err != nil || !json.Valid(raw) {
		return []byte("{}")
	}
	return raw
}
