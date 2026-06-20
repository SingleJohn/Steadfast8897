package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/models"
	"fyms/internal/repository"
	"fyms/internal/services/coverart"

	"github.com/disintegration/imaging"
)

func uploadLibraryImage(c *gin.Context) {
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

	var data []byte
	file, ferr := c.FormFile("file")
	if ferr != nil {
		raw, rerr := io.ReadAll(c.Request.Body)
		if rerr != nil || len(raw) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "file required"})
			return
		}
		data = raw
	} else {
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		defer src.Close()
		data, err = io.ReadAll(src)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	if len(data) > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "File too large (max 20MB)"})
		return
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	tag := uuid.New().String()
	if err := state.Repo.Libraries.UpdateLibraryImage(ctx, id, fpath, tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag})
}

func setLibraryImageFromURL(c *gin.Context) {
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

	var body struct {
		Url string `json:"Url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Url is required"})
		return
	}

	if !strings.HasPrefix(body.Url, "http://") && !strings.HasPrefix(body.Url, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Url must start with http:// or https://"})
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(body.Url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Failed to fetch image: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Remote server returned %d", resp.StatusCode)})
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "URL does not point to an image (Content-Type: " + ct + ")"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to read image data"})
		return
	}
	if len(data) > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Image too large (max 20MB)"})
		return
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	tag := uuid.New().String()
	if err := state.Repo.Libraries.UpdateLibraryImage(ctx, id, fpath, tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag})
}

func deleteLibraryImage(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	if err := state.Repo.Libraries.DeleteLibraryImage(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	imgPath := filepath.Join(state.Config.CacheDir, "images", "lib_"+idStr+".jpg")
	_ = os.Remove(imgPath)
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// listCoverArtStyles 返回所有注册的封面生成风格,供前端下拉展示。
func listCoverArtStyles(c *gin.Context) {
	items := coverart.List()
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		w, h := g.AspectRatio()
		out = append(out, map[string]any{
			"name":        g.Name(),
			"label":       g.Label(),
			"aspectRatio": fmt.Sprintf("%d:%d", w, h),
		})
	}
	c.JSON(http.StatusOK, out)
}

type generateLibraryCoverBody struct {
	Style   string         `json:"Style"`
	Options map[string]any `json:"Options"`
}

// generateLibraryCover 调用 coverart 风格生成封面,写入磁盘并更新 DB。
// POST /Library/VirtualFolders/:id/Image/Generate
// body: { "Style": string, "Options"?: map[string]any }
func generateLibraryCover(c *gin.Context) {
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

	var body generateLibraryCoverBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style: " + strings.TrimSpace(body.Style)})
		return
	}

	tag, err := renderAndSaveLibraryCover(ctx, state, lib, gen, body.Options)
	if err != nil {
		switch {
		case err == coverart.ErrBusy:
			c.JSON(http.StatusConflict, gin.H{"message": "generation already in progress"})
			return
		case err == coverart.ErrNoPosters:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "媒体库暂无可用海报素材,请先扫描入库"})
			return
		case err == coverart.ErrFontMissing:
			c.JSON(http.StatusFailedDependency, gin.H{"message": err.Error()})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"message": "render failed: " + err.Error()})
			return
		}
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag, "Style": style})
}

func renderAndSaveLibraryCover(ctx context.Context, state *AppState, lib *repository.Library, gen coverart.Generator, options map[string]any) (string, error) {
	release, err := coverart.AcquireBusy(lib.ID)
	if err != nil {
		return "", err
	}
	defer release()

	idStr := lib.ID.String()
	materials, err := coverart.PickMaterials(ctx, state.DB, lib.ID)
	if err != nil {
		return "", err
	}

	itemCount, _ := models.GetLibraryDisplayItemCount(ctx, state.DB, idStr)
	out, err := gen.Render(ctx, coverart.Input{
		LibraryID:      lib.ID,
		LibraryName:    lib.Name,
		CollectionType: lib.CollectionType,
		ItemCount:      int(itemCount),
		PosterPaths:    coverart.PosterPathsFromMaterials(materials),
		Materials:      materials,
		Options:        options,
		OutputWidth:    1920,
		OutputHeight:   1080,
	})
	if err != nil {
		return "", err
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return "", err
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	f, err := os.Create(fpath)
	if err != nil {
		return "", err
	}
	if err := imaging.Encode(f, out.Image, imaging.JPEG, imaging.JPEGQuality(out.Quality)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("encode failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	tag := uuid.New().String()
	if err := state.Repo.Libraries.UpdateLibraryImage(ctx, lib.ID, fpath, tag); err != nil {
		return "", err
	}
	return tag, nil
}

// renderAndSaveVirtualCover 为多维度虚拟库生成并保存封面,复用 coverart 生成器。
func renderAndSaveVirtualCover(ctx context.Context, state *AppState, p *models.PlatformLibrary, gen coverart.Generator, options map[string]any) (string, error) {
	pid, perr := uuid.Parse(p.ID)
	if perr != nil {
		return "", perr
	}
	release, err := coverart.AcquireBusy(pid)
	if err != nil {
		return "", err
	}
	defer release()

	cond, ok := models.VirtualDimensionCondition(p.Dimension)
	if !ok {
		return "", fmt.Errorf("unknown dimension: %s", p.Dimension)
	}
	materials, err := coverart.PickMaterialsForVirtual(ctx, state.DB, cond, p.Values())
	if err != nil {
		return "", err
	}

	itemCount, _ := models.CountItemsForVirtual(ctx, state.DB, p.Dimension, p.Values())
	out, err := gen.Render(ctx, coverart.Input{
		LibraryID:      pid,
		LibraryName:    p.EffectiveDisplayName(),
		CollectionType: models.PlatformCollectionType(ctx, state.DB, p.Dimension, p.Values()),
		ItemCount:      int(itemCount),
		PosterPaths:    coverart.PosterPathsFromMaterials(materials),
		Materials:      materials,
		Options:        options,
		OutputWidth:    1920,
		OutputHeight:   1080,
	})
	if err != nil {
		return "", err
	}

	vid := models.PlatformVirtualID(p.Dimension, p.MatchValue)
	imgDir := filepath.Join("data", "library-images", "virtual")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return "", err
	}
	fpath := filepath.Join(imgDir, vid+".jpg")
	f, err := os.Create(fpath)
	if err != nil {
		return "", err
	}
	if err := imaging.Encode(f, out.Image, imaging.JPEG, imaging.JPEGQuality(out.Quality)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("encode failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	tag := uuid.New().String()
	if err := models.SetPlatformCover(ctx, state.DB, p.ID, fpath, tag); err != nil {
		return "", err
	}
	return tag, nil
}

// generatePlatformCover POST /Library/Platforms/:id/Image/Generate  body: {Style?, Options?}
func generatePlatformCover(c *gin.Context, state *AppState) {
	id := c.Param("id")
	p, err := models.GetPlatformByID(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "platform not found"})
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
	tag, err := renderAndSaveVirtualCover(c.Request.Context(), state, p, gen, body.Options)
	if err != nil {
		if errors.Is(err, coverart.ErrNoPosters) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "该虚拟库下没有可用海报素材,无法生成封面"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag, "Style": style})
}

// generateAllPlatformCovers POST /Library/Platforms/CoverArt/GenerateAll  body: {Style?, Options?}
// 给所有已启用虚拟库批量生成封面(无素材的跳过)。
func generateAllPlatformCovers(c *gin.Context, state *AppState) {
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
	ctx := c.Request.Context()
	platforms, err := models.GetEnabledPlatforms(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	generated, skipped := 0, 0
	for i := range platforms {
		if _, err := renderAndSaveVirtualCover(ctx, state, &platforms[i], gen, body.Options); err != nil {
			skipped++
			if !errors.Is(err, coverart.ErrNoPosters) {
				slog.Warn("[Platform] cover gen failed", "platform", platforms[i].PlatformName, "error", err)
			}
			continue
		}
		generated++
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"generated": generated, "skipped": skipped, "style": style})
}

func resolveCoverGenerator(style string) (coverart.Generator, string, bool) {
	style = strings.TrimSpace(style)
	if style == "" {
		if list := coverart.List(); len(list) > 0 {
			style = list[0].Name()
		}
	}
	gen, ok := coverart.Get(style)
	return gen, style, ok
}

// generateAllLibraryCovers 统一生成所有普通媒体库封面。
// POST /Library/CoverArt/GenerateAll
// body: { "Style": string, "Options"?: map[string]any }
func generateAllLibraryCovers(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()

	var body generateLibraryCoverBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style: " + strings.TrimSpace(body.Style)})
		return
	}

	libs, err := state.Repo.Libraries.ListLibraries(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type item struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		Status  string `json:"Status"`
		Message string `json:"Message,omitempty"`
	}
	items := make([]item, 0, len(libs))
	success, skipped, failed := 0, 0, 0
	for i := range libs {
		lib := &libs[i]
		tag, err := renderAndSaveLibraryCover(ctx, state, lib, gen, body.Options)
		if err == nil {
			success++
			items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "success", Message: tag})
			continue
		}
		if err == coverart.ErrFontMissing {
			c.JSON(http.StatusFailedDependency, gin.H{"message": err.Error()})
			return
		}
		if err == coverart.ErrNoPosters || err == coverart.ErrBusy {
			skipped++
			items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "skipped", Message: coverBatchMessage(err)})
			continue
		}
		failed++
		items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "failed", Message: err.Error()})
	}

	if success > 0 {
		invalidateViewsCache(c, state)
	}
	c.JSON(http.StatusOK, gin.H{
		"Style":   style,
		"Total":   len(libs),
		"Success": success,
		"Skipped": skipped,
		"Failed":  failed,
		"Items":   items,
	})
}

func coverBatchMessage(err error) string {
	switch err {
	case coverart.ErrNoPosters:
		return "媒体库暂无可用海报素材"
	case coverart.ErrBusy:
		return "已有生成任务进行中"
	default:
		return err.Error()
	}
}
