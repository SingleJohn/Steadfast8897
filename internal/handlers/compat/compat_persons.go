package compat

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	"fyms/internal/handlers/shared"
	"fyms/internal/models"
)

func getPersons(c *gin.Context, state *AppState) {
	start := int64(0)
	// 对齐 Emby：未显式传 Limit 时返回全部 person，不做默认分页。
	// gfriends-inputer 等外部头像工具依赖一次性拿到全量演职人员来判断谁缺头像；
	// 旧实现默认 50 会让它们只看到前 50 个（按名排序），误判“没有需要下载的头像”。
	limit := int64(0)
	if v := c.Query("StartIndex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = n
		}
	}
	if v := c.Query("Limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}

	search := strings.TrimSpace(c.Query("SearchTerm"))
	nameStartsWith := strings.TrimSpace(c.Query("NameStartsWith"))
	filters := parseCSVQuery(c.Query("Filters"))
	userID := shared.ResolveUserID(c)
	favoriteOnly := hasCSVValue(filters, "IsFavorite")
	if favoriteOnly {
		if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
			c.JSON(http.StatusOK, gin.H{"Items": []gin.H{}, "TotalRecordCount": 0})
			return
		}
	}
	persons, total, err := models.ListPersons(c.Request.Context(), state.DB, models.PersonListOptions{
		Search:         search,
		NameStartsWith: nameStartsWith,
		UserID:         userID,
		Filters:        filters,
		Limit:          limit,
		Offset:         start,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	items := make([]gin.H, 0, len(persons))
	personIDs := make([]string, 0, len(persons))
	for i := range persons {
		personIDs = append(personIDs, persons[i].ID)
	}
	favoriteMap := map[string]bool{}
	if userID != "" {
		if m, merr := models.GetUserPersonFavoriteMap(c.Request.Context(), state.DB, userID, personIDs); merr == nil {
			favoriteMap = m
		}
	}
	for i := range persons {
		var ud *dto.UserDataRow
		if favoriteOnly {
			ud = models.PersonUserDataRow(true)
		} else if fav, ok := favoriteMap[persons[i].ID]; ok {
			ud = models.PersonUserDataRow(fav)
		}
		items = append(items, personItemDTO(state, &persons[i], ud))
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": total})
}

func parseCSVQuery(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func hasCSVValue(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

// getPersonByName 对齐 Emby `GET /Persons/{Name}`（Items-by-Name 单演员详情）。
// 第三方刮削工具（mdc-ng 等）先用它拿到演员详情/Id，再回传头像；缺这个路由会报“未找到详情页”。
func getPersonByName(c *gin.Context, state *AppState) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	p, err := models.GetPersonByName(c.Request.Context(), state.DB, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	userID := shared.ResolveUserID(c)
	var ud *dto.UserDataRow
	if userID != "" {
		if u, uerr := models.GetUserPersonData(c.Request.Context(), state.DB, userID, p.ID); uerr == nil {
			ud = u
		}
	}
	c.JSON(http.StatusOK, PersonDetailDTO(state, p, ud))
}

// personDetailDTO 严格镜像真实 Emby `GET /Persons/{Name}` 的返回字段集（依据官方
// Emby 服务器实测样本对齐，不多不少）。第三方 Rust 客户端（mdc-ng）的演员详情结构体
// 按真实 Emby 建模并 deny_unknown_fields——多出 Emby 不返回的字段（IsFolder /
// LocationType / Overview / PrimaryImageTag 等）会触发 "error decoding response body"，
// 故此处刻意不复用更宽松的 personItemDTO，也不附加任何 Emby 不返回的字段。
func PersonDetailDTO(state *AppState, p *models.Person, userData *dto.UserDataRow) gin.H {
	ts := embyTimestampFromEpoch(p.ImageTag)
	etag := p.ImageTag
	if etag == "" {
		etag = p.ID
	}
	providerIDs := personProviderIDMap(p)
	item := gin.H{
		"Name":                  p.Name,
		"ServerId":              state.Config.ServerID,
		"Id":                    p.ID,
		"Etag":                  etag,
		"DateCreated":           ts,
		"DateModified":          ts,
		"CanDelete":             false,
		"CanDownload":           false,
		"PresentationUniqueKey": p.ID,
		"SortName":              p.Name,
		"ForcedSortName":        p.Name,
		"ExternalUrls":          personExternalUrls(providerIDs),
		"ProductionLocations":   strOrEmpty(p.ProductionLocations),
		"Taglines":              strOrEmpty(p.Taglines),
		"RemoteTrailers":        []gin.H{},
		"ProviderIds":           providerIDs,
		"Type":                  "Person",
		"DisplayPreferencesId":  p.ID,
		"ImageTags":             gin.H{},
		"BackdropImageTags":     []string{},
		"LockedFields":          []string{},
		"LockData":              false,
		"UserData":              personUserDataDTO(userData),
	}
	if p.Overview != nil && *p.Overview != "" {
		item["Overview"] = *p.Overview
	}
	if pd := personPremiereDate(p); pd != "" {
		item["PremiereDate"] = pd
	}
	if p.ProductionYear != nil {
		item["ProductionYear"] = *p.ProductionYear
	}
	// Genres/Tags 仅在有值时输出：空时与真实 Emby 详情一致(不返回该键，规避客户端
	// deny_unknown 风险)；有值时如实暴露给其它客户端(三围/身高/罩杯等存在 Tags 里)。
	if len(p.Genres) > 0 {
		item["Genres"] = p.Genres
	}
	if len(p.Tags) > 0 {
		item["Tags"] = p.Tags
	}
	if p.ImagePath != nil && *p.ImagePath != "" {
		tag := imageTagOr(p, p.ImageTag)
		item["ImageTags"] = gin.H{"Primary": tag}
		item["PrimaryImageAspectRatio"] = 0.6666666666666666
	}
	if p.BackdropPath != nil && *p.BackdropPath != "" {
		item["BackdropImageTags"] = []string{imageTagOr(p, p.ImageTag)}
	}
	return item
}

// personProviderIDMap 合并完整外部 id 映射 + Tmdb 兜底(键 "Tmdb",Emby 习惯)。
func personProviderIDMap(p *models.Person) map[string]string {
	out := map[string]string{}
	for k, v := range p.ProviderIDs {
		out[k] = v
	}
	if p.TmdbPersonID != nil {
		if _, ok := out["Tmdb"]; !ok {
			out["Tmdb"] = strconv.FormatInt(int64(*p.TmdbPersonID), 10)
		}
	}
	return out
}

// personExternalUrls 依据外部 id 生成 Emby 风格 ExternalUrls(IMDb / TheMovieDb)。
func personExternalUrls(ids map[string]string) []gin.H {
	out := []gin.H{}
	get := func(want string) string {
		for k, v := range ids {
			if strings.EqualFold(k, want) && strings.TrimSpace(v) != "" {
				return v
			}
		}
		return ""
	}
	if v := get("Imdb"); v != "" {
		out = append(out, gin.H{"Name": "IMDb", "Url": "https://www.imdb.com/name/" + v})
	}
	if v := get("Tmdb"); v != "" {
		out = append(out, gin.H{"Name": "TheMovieDb", "Url": "https://www.themoviedb.org/person/" + v})
	}
	return out
}

// personPremiereDate 把存储的 "YYYY-MM-DD"(或已含 T 的串)转 Emby 时间串;空则 ""。
func personPremiereDate(p *models.Person) string {
	if p.PremiereDate == nil {
		return ""
	}
	s := strings.TrimSpace(*p.PremiereDate)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "T") {
		return s
	}
	return s + "T00:00:00.0000000Z"
}

// strOrEmpty 把可能为 nil 的切片渲染成 JSON 数组([] 而非 null)。
func strOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// imageTagFallback 取图片 tag(updated_at epoch),为空时退回 person id。
func imageTagOr(p *models.Person, tag string) string {
	if tag != "" {
		return tag
	}
	return p.ID
}

func personUserDataDTO(userData *dto.UserDataRow) gin.H {
	isFavorite := false
	if userData != nil && userData.IsFavorite != nil {
		isFavorite = *userData.IsFavorite
	}
	return gin.H{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            isFavorite,
		"Played":                false,
	}
}

// embyTimestampFromEpoch 把 Unix 秒 epoch 字符串格式化成 Emby 的时间串（7 位小数 + Z）。
// 用于 DateCreated / DateModified —— mdc-ng 会按 DateTime 解析，必须是合法格式。
func embyTimestampFromEpoch(epoch string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(epoch), 10, 64)
	if err != nil || n <= 0 {
		return "2020-01-01T00:00:00.0000000Z"
	}
	return time.Unix(n, 0).UTC().Format("2006-01-02T15:04:05.0000000") + "Z"
}

// personItemDTO 把全局 person 渲染成 Emby `/Persons` 列表项(对齐真实 Emby:Name/
// ServerId/Id/DateCreated/Type/UserData/ImageTags/BackdropImageTags,Overview 在有值时附带)。
// 仅当 person 实际有头像时才带 ImageTags.Primary —— 客户端据此判断谁缺头像。
func personItemDTO(state *AppState, p *models.Person, userData *dto.UserDataRow) gin.H {
	item := gin.H{
		"Name":              p.Name,
		"ServerId":          state.Config.ServerID,
		"Id":                p.ID,
		"DateCreated":       embyTimestampFromEpoch(p.ImageTag),
		"Type":              "Person",
		"ImageTags":         gin.H{},
		"BackdropImageTags": []string{},
		"ProviderIds":       personProviderIDMap(p),
		"UserData":          personUserDataDTO(userData),
	}
	if p.ImagePath != nil && *p.ImagePath != "" {
		item["ImageTags"] = gin.H{"Primary": imageTagOr(p, p.ImageTag)}
		item["PrimaryImageAspectRatio"] = 0.6666666666666666
	}
	if p.BackdropPath != nil && *p.BackdropPath != "" {
		item["BackdropImageTags"] = []string{imageTagOr(p, p.ImageTag)}
	}
	if p.Overview != nil && *p.Overview != "" {
		item["Overview"] = *p.Overview
	}
	return item
}
