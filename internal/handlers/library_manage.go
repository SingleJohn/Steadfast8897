package handlers

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/middleware"
	"fyms/internal/models"
)

type virtualFolderBody struct {
	Name           string   `json:"Name"`
	CollectionType string   `json:"CollectionType"`
	Paths          []string `json:"Paths"`
}

func addLibrary(c *gin.Context) {
	state := GetState(c)
	var body virtualFolderBody
	_ = c.ShouldBindJSON(&body)

	if qn := c.Query("name"); qn != "" {
		body.Name = qn
	}
	if qct := c.Query("collectionType"); qct != "" {
		body.CollectionType = qct
	}
	if body.Name == "" || body.CollectionType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name and CollectionType required"})
		return
	}
	lib, err := models.CreateLibrary(c.Request.Context(), state.DB, body.Name, body.CollectionType, body.Paths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
	_ = lib
}

// deleteLibrary 把库标记为 deleted_at = NOW() 后立即返回 204,真正的 items
// 物理删除由 CleanupAdapter 后台分批跑(避免大库 DELETE 阻塞请求几分钟)。
// 完成后通过 task center SSE 推 succeeded snapshot,前端可据此 toast 通知。
func deleteLibrary(c *gin.Context) {
	state := GetState(c)
	idStr := strings.TrimSpace(c.Query("id"))
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}

	// 取库名给 cleanup snapshot 展示用;库不存在则当幂等成功处理。
	lib, err := models.GetLibraryByID(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.Status(http.StatusNoContent)
		return
	}

	marked, err := models.MarkLibraryDeleted(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)

	if marked && state.CleanupTask != nil {
		state.CleanupTask.Enqueue(id, lib.Name)
	}
	c.Status(http.StatusNoContent)
}

type renameLibraryBody struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

func renameLibrary(c *gin.Context) {
	state := GetState(c)
	var body renameLibraryBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name required"})
		return
	}
	name := strings.TrimSpace(body.Name)
	lib, err := models.UpdateLibrary(c.Request.Context(), state.DB, id, &name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, lib)
}

type libraryPathBody struct {
	ID   string `json:"Id"`
	Path string `json:"Path"`
}

func addLibraryPath(c *gin.Context) {
	state := GetState(c)
	var body libraryPathBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Path required"})
		return
	}
	if err := models.AddLibraryPath(c.Request.Context(), state.DB, id, body.Path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func removeLibraryPath(c *gin.Context) {
	state := GetState(c)
	idStr := strings.TrimSpace(c.Query("id"))
	path := strings.TrimSpace(c.Query("path"))
	if idStr == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id and path required"})
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	if err := models.RemoveLibraryPath(c.Request.Context(), state.DB, id, path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func uploadImage(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	imageType := strings.TrimSpace(c.Param("imageType"))
	ctx := c.Request.Context()

	// itemId 命中全局 persons → 演员头像上传(写 persons,全库同名生效)。
	if _, perr := uuid.Parse(itemID); perr == nil && models.PersonExists(ctx, state.DB, itemID) {
		handlePersonImageUpload(c, state, itemID)
		return
	}

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		data, rerr := io.ReadAll(c.Request.Body)
		if rerr != nil || len(data) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "file required (multipart field 'file')"})
			return
		}
		ext := ".bin"
		switch imageType {
		case "Primary", "Thumb":
			ext = ".jpg"
		}
		if err := saveItemImage(ctx, state, *resolved, imageType, ext, data); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	if err := saveItemImage(ctx, state, *resolved, imageType, ext, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func saveItemImage(ctx context.Context, state *AppState, itemUUID, imageType, ext string, data []byte) error {
	tag := uuid.New().String()
	dir := filepath.Join(state.Config.DataDir, "images", itemUUID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	safeType := strings.ReplaceAll(strings.ToLower(imageType), "/", "_")
	fpath := filepath.Join(dir, safeType+ext)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		return err
	}

	switch strings.ToLower(imageType) {
	case "primary", "thumb":
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	case "backdrop", "backdrops":
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	default:
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	}
}

func deleteImage(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	imageType := strings.TrimSpace(c.Param("imageType"))
	ctx := c.Request.Context()

	// itemId 命中全局 persons → 清除演员头像。
	if _, perr := uuid.Parse(itemID); perr == nil && models.PersonExists(ctx, state.DB, itemID) {
		handlePersonImageDelete(c, state, itemID)
		return
	}

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	switch strings.ToLower(imageType) {
	case "primary", "thumb":
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = NULL, primary_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	case "backdrop", "backdrops":
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET backdrop_image_path = NULL, backdrop_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	default:
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = NULL, primary_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func getVirtualFolderDetail(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	if authUser := middleware.GetAuthUser(c); authUser != nil && !authUser.IsAdmin {
		scope, err := loadUserLibraryScope(ctx, state, authUser.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if !scope.allowsLibrary(id.String()) {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
		}
	}
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	var itemCount int64
	itemCount, _ = models.GetLibraryDisplayItemCount(ctx, state.DB, id.String())

	locations := make([]string, 0)
	if lib.Paths != nil {
		locations = lib.Paths
	}

	imageTag := ""
	if lib.PrimaryImageTag != nil {
		imageTag = *lib.PrimaryImageTag
	}

	dateCreated := lib.CreatedAt.UTC().Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"Id":             lib.ID.String(),
		"Name":           lib.Name,
		"CollectionType": lib.CollectionType,
		"Locations":      locations,
		"ItemId":         lib.ID.String(),
		"ItemCount":      itemCount,
		"DateCreated":    dateCreated,
		"ImageTag":       imageTag,
	})
}

type updateLibraryInfoBody struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	CollectionType string `json:"CollectionType"`
}

func updateLibraryInfo(c *gin.Context) {
	state := GetState(c)
	var body updateLibraryInfoBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	ctx := c.Request.Context()
	if body.Name != "" {
		name := strings.TrimSpace(body.Name)
		if _, err := models.UpdateLibrary(ctx, state.DB, id, &name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if body.CollectionType != "" {
		_, err := state.DB.Exec(ctx, "UPDATE libraries SET collection_type = $1 WHERE id = $2", body.CollectionType, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	invalidateViewsCache(c, state)
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}
	c.JSON(http.StatusOK, lib)
}

func invalidateViewsCache(c *gin.Context, state *AppState) {
	state.Cache.DelPattern(c.Request.Context(), "views:*")
}

// ============ Library Sort Order ============

func updateLibrarySortOrder(c *gin.Context, state *AppState) {
	var body []models.LibrarySortItem
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if err := models.BatchUpdateLibrarySortOrder(c.Request.Context(), state.DB, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// updateDisplayOrder 整体重写实际库 + 虚拟库的统一展示顺序。
// POST /Library/DisplayOrder  body: [{Kind:"library"|"platform", Id}]
func updateDisplayOrder(c *gin.Context, state *AppState) {
	var body []models.DisplayOrderEntry
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if err := models.SetDisplayOrder(c.Request.Context(), state.DB, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}
