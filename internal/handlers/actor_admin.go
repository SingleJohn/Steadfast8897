package handlers

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/models"
)

// 演员管理(后台「媒体内容 → 演员」)。人工核对/编辑演员资料、头像、清理垃圾。
// 列表/编辑/删除走独立的 /Library/Actors 路由族(adminMW);头像上传/删除复用
// /Items/{id}/Images/...(uploadImage 按 person 分流到 handlePersonImageUpload)。

func listActorsAdmin(c *gin.Context) {
	state := GetState(c)
	f := models.ActorAdminFilter{
		Search: c.Query("q"),
		Filter: c.Query("filter"),
		Sort:   c.Query("sort"),
		Order:  c.Query("order"),
		Limit:  50,
		Offset: 0,
	}
	if v, err := strconv.ParseInt(c.Query("limit"), 10, 64); err == nil && v > 0 && v <= 200 {
		f.Limit = v
	}
	if v, err := strconv.ParseInt(c.Query("offset"), 10, 64); err == nil && v >= 0 {
		f.Offset = v
	}
	rows, total, err := models.ListActorsAdmin(c.Request.Context(), state.DB, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Items": rows, "TotalRecordCount": total})
}

func getActorAdmin(c *gin.Context) {
	state := GetState(c)
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	p, err := models.GetPersonByID(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	c.JSON(http.StatusOK, actorAdminDetailDTO(c, state, p))
}

// actorAdminDetailDTO 抽屉编辑用的原始字段视图(区别于 Emby 风格的 personDetailDTO)。
func actorAdminDetailDTO(c *gin.Context, state *AppState, p *models.Person) gin.H {
	works, _ := models.CountPersonWorks(c.Request.Context(), state.DB, p.ID)
	providerIDs := p.ProviderIDs
	if providerIDs == nil {
		providerIDs = map[string]string{}
	}
	dto := gin.H{
		"Id":                  p.ID,
		"Name":                p.Name,
		"Overview":            strPtrVal(p.Overview),
		"PremiereDate":        strPtrVal(p.PremiereDate),
		"ProductionYear":      p.ProductionYear,
		"ProductionLocations": strOrEmpty(p.ProductionLocations),
		"Genres":              strOrEmpty(p.Genres),
		"Tags":                strOrEmpty(p.Tags),
		"Taglines":            strOrEmpty(p.Taglines),
		"ProviderIds":         providerIDs,
		"HasImage":            p.ImagePath != nil && *p.ImagePath != "",
		"HasBackdrop":         p.BackdropPath != nil && *p.BackdropPath != "",
		"ImageLocked":         p.ImageLocked,
		"ImageTag":            p.ImageTag,
		"WorkCount":           works,
	}
	return dto
}

func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func updateActorAdmin(c *gin.Context) {
	state := GetState(c)
	id := c.Param("id")
	ctx := c.Request.Context()
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	p, err := models.GetPersonByID(ctx, state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}

	// 管理端 PATCH:字段缺省=保留;给出(含空串/空数组)=显式覆盖。
	var body struct {
		Overview            *string           `json:"Overview"`
		PremiereDate        *string           `json:"PremiereDate"`
		ProductionYear      *int32            `json:"ProductionYear"`
		ProductionLocations []string          `json:"ProductionLocations"`
		Genres              []string          `json:"Genres"`
		Tags                []string          `json:"Tags"`
		Taglines            []string          `json:"Taglines"`
		ProviderIds         map[string]string `json:"ProviderIds"`
		ImageLocked         *bool             `json:"ImageLocked"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	upd := models.PersonMetadataUpdate{
		Overview:            body.Overview,
		PremiereDate:        body.PremiereDate,
		ProductionYear:      body.ProductionYear,
		ProductionLocations: body.ProductionLocations,
		Genres:              body.Genres,
		Tags:                body.Tags,
		Taglines:            body.Taglines,
		ProviderIDs:         body.ProviderIds,
		TmdbPersonID:        tmdbFromProviderIds(body.ProviderIds),
	}
	if err := models.UpdatePersonMetadata(ctx, state.DB, id, upd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if body.ImageLocked != nil {
		if err := models.SetPersonImageLocked(ctx, state.DB, id, *body.ImageLocked); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	fresh, err := models.GetPersonByID(ctx, state.DB, id)
	if err != nil || fresh == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, actorAdminDetailDTO(c, state, fresh))
}

func deleteActor(c *gin.Context) {
	state := GetState(c)
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	paths, err := models.DeletePersons(c.Request.Context(), state.DB, []string{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	removePersonFiles(state, paths)
	c.Status(http.StatusNoContent)
}

func bulkDeleteActors(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	var body struct {
		Ids     []string `json:"Ids"`
		AllJunk bool     `json:"AllJunk"` // true=删除全部垃圾名演员(忽略 Ids)
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	if body.AllJunk {
		paths, n, err := models.DeleteJunkPersons(ctx, state.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		removePersonFiles(state, paths)
		c.JSON(http.StatusOK, gin.H{"Deleted": n})
		return
	}

	// 过滤非法 id,避免 ::uuid[] 转换报错。
	ids := make([]string, 0, len(body.Ids))
	for _, id := range body.Ids {
		if _, err := uuid.Parse(id); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "no valid ids"})
		return
	}
	paths, err := models.DeletePersons(ctx, state.DB, ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	removePersonFiles(state, paths)
	c.JSON(http.StatusOK, gin.H{"Deleted": len(ids)})
}

// removePersonFiles 只删 data/metadata/persons 下我们自己写的头像/背景图,绝不碰挂载盘/NFO 原图。
func removePersonFiles(state *AppState, paths []string) {
	for _, p := range paths {
		if isUnderPersonsDir(state, p) {
			_ = os.Remove(p)
		}
	}
}
