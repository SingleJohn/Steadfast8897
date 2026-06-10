package dto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
)

// strmItemPathResolvedFlag 控制 strm 条目「item 级 Path」字段的取值:
//   - false(默认)= 返回 .strm 文件本身的路径(对齐 Emby:item.Path 永远是 .strm,
//     解析后的真实地址只出现在 MediaSources.Path)
//   - true        = 返回解析 .strm 后的内层真实路径(FYMS 旧行为)
//
// 无论该开关取值如何,strm 的 item.Container 都解析为真实内层容器(如 mp4),与 Emby 一致。
// 由 main.go 启动时按 system_config 'strm_item_path_mode' 设置,postConfiguration 保存时实时刷新。
var strmItemPathResolvedFlag atomic.Bool

// SetStrmItemPathMode 设置 strm item.Path 模式。mode=="resolved" 时返回解析后真实路径,
// 其它值(含默认空值 / "strm")返回 .strm 文件路径。
func SetStrmItemPathMode(mode string) {
	strmItemPathResolvedFlag.Store(strings.EqualFold(strings.TrimSpace(mode), "resolved"))
}

func strmItemPathResolved() bool {
	return strmItemPathResolvedFlag.Load()
}

// StrmItemPathResolved 导出当前模式:true=item.Path 取解析后真实路径;false=取 .strm 文件路径(默认)。
// 供 handler 层判断是否应把 item 级 Path 覆盖为 MediaSources 的解析路径。
func StrmItemPathResolved() bool {
	return strmItemPathResolvedFlag.Load()
}

// studioNamespace 用于由 studio name 生成稳定 UUID。Emby/Jellyfin 客户端
// （包括 VidHub）要求 Studios[].Id 必填且为 UUID 形式。
var studioNamespace = uuid.MustParse("b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e")

func studioStableID(name string) string {
	return uuid.NewSHA1(studioNamespace, []byte(name)).String()
}

// normalizeProviderIDs 把 DB 里统一的小写 provider key（tmdb/imdb/tvdb...）在出口处
// 补出 Emby 官方大小写 key（Tmdb/Imdb/Tvdb...），同时保留小写 key。
// 这样取 "Imdb" 的聚合 app 和取 "imdb" 的 app 都能拿到值，多余 key 主流客户端忽略。
// DB 存储层保持小写不变，只在序列化时转换。任何解析/序列化失败都原样返回，绝不丢数据。
func normalizeProviderIDs(raw *json.RawMessage) *json.RawMessage {
	if raw == nil || len(*raw) == 0 {
		return raw
	}
	var m map[string]string
	if err := json.Unmarshal(*raw, &m); err != nil || len(m) == 0 {
		return raw
	}
	out := make(map[string]string, len(m)*2)
	for k, v := range m {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" || v == "" {
			continue
		}
		out[lk] = v                    // 保留小写
		out[canonicalProvider(lk)] = v // 补官方大小写
	}
	if len(out) == 0 {
		return raw
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	rm := json.RawMessage(b)
	return &rm
}

// canonicalProvider 已知 provider 用 Emby 官方写法，未知的首字母大写兜底。
func canonicalProvider(lower string) string {
	switch lower {
	case "tmdb":
		return "Tmdb"
	case "imdb":
		return "Imdb"
	case "tvdb":
		return "Tvdb"
	case "tmdbcollection":
		return "TmdbCollection"
	default: // bangumi→Bangumi, douban→Douban 等
		if lower == "" {
			return lower
		}
		return strings.ToUpper(lower[:1]) + lower[1:]
	}
}

// FormatItemDtoList 列表场景：跳过 strm 文件解析（避免大量磁盘 IO）
func FormatItemDtoList(item *ItemRow, serverID string, userData *UserDataRow) BaseItemDto {
	return formatItemDto(item, serverID, userData, true)
}

func FormatItemDto(item *ItemRow, serverID string, userData *UserDataRow) BaseItemDto {
	return formatItemDto(item, serverID, userData, false)
}

func formatItemDto(item *ItemRow, serverID string, userData *UserDataRow, skipStrmResolve bool) BaseItemDto {
	sortName := item.Name
	if item.SortName != nil {
		sortName = *item.SortName
	}

	dto := BaseItemDto{
		ID:       item.ID,
		Name:     item.Name,
		ServerID: serverID,
		Type:     item.ItemType,
		SortName: &sortName,
	}

	switch item.ItemType {
	case "Movie", "Episode":
		mediaType := "Video"
		isFolder := false
		dto.MediaType = &mediaType
		dto.IsFolder = &isFolder
	case "Series", "Season", "CollectionFolder", "Folder":
		isFolder := true
		dto.IsFolder = &isFolder
	}

	dto.CollectionType = item.CollectionType
	dto.Overview = item.Overview
	dto.ProductionYear = item.ProductionYear
	if item.PremiereDate != nil {
		s := item.PremiereDate.UTC().Format("2006-01-02T15:04:05.0000000Z")
		dto.PremiereDate = &s
	}
	dto.CommunityRating = item.CommunityRating
	dto.OfficialRating = item.OfficialRating
	dto.RunTimeTicks = item.RuntimeTicks
	dto.IndexNumber = item.IndexNumber
	dto.ParentIndexNumber = item.ParentIndexNumber
	dto.ParentID = item.ParentID
	dto.SeriesID = item.SeriesID
	dto.SeriesName = item.SeriesName
	dto.SeasonID = item.SeasonID
	dto.ProviderIDs = normalizeProviderIDs(item.ProviderIDs)

	// displayPath = item 级 Path 字段；resolvedStrm = 解析 .strm 得到的内层真实路径
	// (仅非列表模式下读盘获得，用于推导真实 Container)。
	var displayPath *string
	var resolvedStrm *string
	if item.ResolvedPath != nil {
		displayPath = item.ResolvedPath
	} else if item.FilePath != nil {
		isStrm := strings.HasSuffix(strings.ToLower(*item.FilePath), ".strm")
		switch {
		case skipStrmResolve:
			// 列表模式：跳过磁盘 IO，item.Path 直接用 file_path(.strm 时即对齐 Emby)。
			displayPath = item.FilePath
		case isStrm:
			// 读一次 .strm 取内层真实路径，供 Container 推导；
			// item.Path 用 .strm 还是解析后路径由 strmItemPathResolved() 决定。
			resolvedStrm = resolveStrmForDisplay(*item.FilePath)
			if strmItemPathResolved() && resolvedStrm != nil {
				displayPath = resolvedStrm
			} else {
				displayPath = item.FilePath
			}
		default:
			displayPath = item.FilePath
		}
	}
	dto.Path = displayPath

	if item.Container != nil {
		if *item.Container != "strm" {
			dto.Container = item.Container
		} else {
			// items.container 对 strm 入库时存的是字面 "strm"，这里推真实内层容器(对齐 Emby 的 mp4)。
			// 优先用解析后的内层路径扩展名；列表模式没解析时退回 displayPath，避免对外暴露 "strm" 容器。
			src := resolvedStrm
			if src == nil {
				src = displayPath
			}
			if src != nil {
				ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(*src), "."))
				if ext == "" || ext == "strm" {
					ext = "mkv"
				}
				dto.Container = &ext
			} else {
				dto.Container = item.Container
			}
		}
	}

	seriesItemID := item.SeriesFallbackID
	if seriesItemID == nil {
		seriesItemID = item.SeriesID
	}
	if seriesItemID == nil {
		seriesItemID = item.ParentID
	}

	imageTags := make(map[string]string)
	if item.PrimaryImageTag != nil {
		imageTags["Primary"] = *item.PrimaryImageTag
	}
	if len(imageTags) > 0 {
		dto.ImageTags = imageTags
	}

	if item.BackdropImageTag != nil {
		dto.BackdropImageTags = []string{*item.BackdropImageTag}
	} else if item.ItemType == "Episode" || item.ItemType == "Season" {
		if item.SeriesBackdropImageTag != nil {
			dto.ParentBackdropItemID = seriesItemID
			dto.ParentBackdropImageTags = []string{*item.SeriesBackdropImageTag}
		}
	}

	if item.ItemType == "Episode" || item.ItemType == "Season" {
		if item.SeriesPrimaryImageTag != nil {
			dto.SeriesPrimaryImageTag = item.SeriesPrimaryImageTag
			if dto.SeriesPrimaryImageItemID == nil {
				dto.SeriesPrimaryImageItemID = seriesItemID
			}
			dto.ParentPrimaryImageItemID = seriesItemID
			dto.ParentPrimaryImageTag = item.SeriesPrimaryImageTag
			dto.ParentThumbItemID = seriesItemID
			dto.ParentThumbImageTag = item.SeriesPrimaryImageTag
		}
		if item.SeriesBackdropImageTag != nil {
			if dto.ParentBackdropItemID == nil {
				dto.ParentBackdropItemID = seriesItemID
				dto.ParentBackdropImageTags = []string{*item.SeriesBackdropImageTag}
			}
		}
	}

	dto.ChildCount = item.ChildCount
	dto.RecursiveItemCount = item.RecursiveItemCount

	// Supplemental fields for bot/search compatibility
	if item.Tagline != nil && *item.Tagline != "" {
		dto.Taglines = []string{*item.Tagline}
	}
	if item.CreatedAt != nil {
		t := item.CreatedAt.UTC().Format("2006-01-02T15:04:05.0000000Z")
		dto.DateCreated = &t
	}
	if item.Studio != nil && *item.Studio != "" {
		dto.Studios = []StudioItem{{Name: *item.Studio, ID: studioStableID(*item.Studio)}}
	}
	dto.ProductionLocations = []string{}

	if userData != nil {
		position := int64(0)
		if userData.PlaybackPositionTicks != nil {
			position = *userData.PlaybackPositionTicks
		}
		playCount := int32(0)
		if userData.PlayCount != nil {
			playCount = *userData.PlayCount
		}
		isFav := false
		if userData.IsFavorite != nil {
			isFav = *userData.IsFavorite
		}
		played := false
		if userData.Played != nil {
			played = *userData.Played
		}

		var percentage *float64
		if dto.RunTimeTicks != nil && *dto.RunTimeTicks > 0 && position > 0 {
			p := float64(position) / float64(*dto.RunTimeTicks) * 100.0
			percentage = &p
		}

		var lastPlayed *string
		if userData.LastPlayedDate != nil {
			s := userData.LastPlayedDate.UTC().Format("2006-01-02T15:04:05.0000000Z")
			lastPlayed = &s
		}

		dto.UserData = &UserItemDataDto{
			PlaybackPositionTicks: position,
			PlayCount:             playCount,
			IsFavorite:            isFav,
			Played:                played,
			LastPlayedDate:        lastPlayed,
			PlayedPercentage:      percentage,
		}
	} else {
		dto.UserData = &UserItemDataDto{}
	}

	return dto
}

func FormatMediaStreamDto(stream *StreamRow) MediaStreamInfo {
	codec := ""
	if stream.Codec != nil {
		codec = *stream.Codec
	}
	return MediaStreamInfo{
		Codec:        codec,
		Type:         stream.StreamType,
		Index:        stream.StreamIndex,
		Language:     stream.Language,
		Title:        stream.Title,
		IsDefault:    stream.IsDefault != nil && *stream.IsDefault,
		IsForced:     stream.IsForced != nil && *stream.IsForced,
		Width:        stream.Width,
		Height:       stream.Height,
		BitRate:      stream.BitRate,
		Channels:     stream.Channels,
		SampleRate:   stream.SampleRate,
		BitDepth:     stream.BitDepth,
		PixelFormat:  stream.PixelFormat,
		DisplayTitle: stream.DisplayTitle,
	}
}

func resolveStrmForDisplay(strmPath string) *string {
	data, err := os.ReadFile(strmPath)
	if err != nil {
		return nil
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) == 0 {
		return nil
	}
	line := strings.TrimSpace(lines[0])
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	resolved := line
	if strings.HasPrefix(resolved, "http") {
		return &resolved
	}
	if !strings.HasPrefix(resolved, "/") {
		return nil
	}

	if _, err := os.Stat(resolved); err != nil {
		mntPath := "/mnt" + resolved
		if _, err := os.Stat(mntPath); err == nil {
			return &mntPath
		}
		fixed := strings.Replace(resolved, "/CloudNAS", "/mnt/CloudNAS", 1)
		if fixed != resolved {
			if _, err := os.Stat(fixed); err == nil {
				return &fixed
			}
		}
	}
	return &resolved
}
