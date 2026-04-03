package dto

import (
	"os"
	"path/filepath"
	"strings"
)

func FormatItemDto(item *ItemRow, serverID string, userData *UserDataRow) BaseItemDto {
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
	case "Series", "Season", "CollectionFolder":
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
	dto.ProviderIDs = item.ProviderIDs

	var displayPath *string
	if item.ResolvedPath != nil {
		displayPath = item.ResolvedPath
	} else if item.FilePath != nil {
		if strings.HasSuffix(*item.FilePath, ".strm") {
			if resolved := resolveStrmForDisplay(*item.FilePath); resolved != nil {
				displayPath = resolved
			} else {
				displayPath = item.FilePath
			}
		} else {
			displayPath = item.FilePath
		}
	}
	dto.Path = displayPath

	if item.Container != nil {
		if *item.Container != "strm" {
			dto.Container = item.Container
		} else if displayPath != nil {
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(*displayPath), "."))
			if ext == "" {
				ext = "mkv"
			}
			dto.Container = &ext
		} else {
			dto.Container = item.Container
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
