package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
)

func hideMediaSourceSizeForInfuse(c *gin.Context, sources []dto.MediaSourceInfo) {
	_, _ = c, sources
}

func parseChaptersJSON(data []byte) []dto.ChapterInfo {
	if len(data) == 0 {
		return nil
	}
	type storedChapter struct {
		StartPositionTicks int64  `json:"StartPositionTicks"`
		Name               string `json:"Name"`
	}
	var stored []storedChapter
	if json.Unmarshal(data, &stored) != nil || len(stored) == 0 {
		return nil
	}
	chapters := make([]dto.ChapterInfo, len(stored))
	for i, c := range stored {
		chapters[i] = dto.ChapterInfo{
			ChapterIndex:       i,
			MarkerType:         "Chapter",
			Name:               c.Name,
			StartPositionTicks: c.StartPositionTicks,
		}
	}
	return chapters
}

func applyMediaSourceCompatDefaults(src *dto.MediaSourceInfo, itemID string) {
	if src == nil {
		return
	}
	src.ItemID = itemID
	src.SupportsProbing = true
	src.HasMixedProtocols = false
	src.IsInfiniteStream = false
	src.RequiresOpening = false
	src.RequiresClosing = false
	src.RequiresLooping = false
	src.ReadAtNativeFramerate = false
	if src.RequiredHTTPHeaders == nil {
		src.RequiredHTTPHeaders = map[string]string{}
	}
	if src.Formats == nil {
		src.Formats = []string{}
	}
	if src.Chapters == nil {
		src.Chapters = []dto.ChapterInfo{}
	}
	if src.MediaStreams == nil {
		src.MediaStreams = []dto.MediaStreamInfo{}
	}
	if src.MediaAttachments == nil {
		src.MediaAttachments = []interface{}{}
	}
	if src.VideoType == nil {
		videoType := "VideoFile"
		src.VideoType = &videoType
	}
}

func buildItemMediaSources(ctx context.Context, state *AppState, itemID string, item *dto.ItemRow) []dto.MediaSourceInfo {
	versions, err := loadMediaVersions(ctx, state, itemID)
	if err != nil {
		return nil
	}

	if len(versions) == 0 && item.FilePath != nil && *item.FilePath != "" {
		versions = append(versions, mediaVersionRow{
			ID:           uuid.Nil,
			Name:         "Default",
			FilePath:     *item.FilePath,
			Container:    item.Container,
			IsPrimary:    true,
			RuntimeTicks: item.RuntimeTicks,
		})
	}

	streamRows, _ := state.Repo.Playback.ListMediaStreamsForItem(ctx, itemID)
	baseStreams := make([]dto.MediaStreamInfo, 0, len(streamRows))
	for i := range streamRows {
		baseStreams = append(baseStreams, dto.FormatMediaStreamDto(&streamRows[i]))
	}

	var sources []dto.MediaSourceInfo
	for idx, mv := range versions {
		msid := mv.ID.String()
		if mv.ID == uuid.Nil {
			msid = itemID
		}

		actualPath := mv.FilePath
		actualContainer := ""
		if mv.Container != nil {
			actualContainer = *mv.Container
		}
		protocol := "File"
		isRemote := false

		if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
			if rp := resolveStrmPath(mv.FilePath); rp != nil {
				actualPath = rp.filePath
				actualContainer = rp.container
				isRemote = rp.isRemote
				if isRemote {
					protocol = "Http"
				}
			}
		} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
			protocol = "Http"
			isRemote = true
		}

		if actualContainer == "" && item.Container != nil {
			actualContainer = *item.Container
		}

		versionStreams := baseStreams
		if len(mv.MediaInfo) > 0 {
			var mi map[string]json.RawMessage
			if json.Unmarshal(mv.MediaInfo, &mi) == nil {
				if msRaw, ok := mi["MediaStreams"]; ok {
					var miStreams []dto.MediaStreamInfo
					if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
						versionStreams = miStreams
					}
				}
			}
		}
		if len(versionStreams) == 0 && idx == 0 {
			versionStreams = baseStreams
		}
		versionStreams = appendExternalSubtitleStreams(ctx, state.DB, itemID, msid, versionStreams)

		src := dto.MediaSourceInfo{
			ID:                   msid,
			Path:                 actualPath,
			Protocol:             protocol,
			Type:                 "Default",
			Container:            actualContainer,
			Name:                 mv.Name,
			IsRemote:             isRemote,
			RunTimeTicks:         mv.RuntimeTicks,
			SupportsDirectPlay:   true,
			SupportsDirectStream: true,
			SupportsTranscoding:  false,
			MediaStreams:         versionStreams,
			DirectStreamURL:      fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, actualContainer, msid),
			ETag:                 msid,
			Size:                 mv.Size,
			Formats:              []string{},
			FymsResolution:       mv.Resolution,
			FymsHdrFormat:        mv.HDRFormat,
			FymsVideoCodec:       mv.VideoCodec,
			FymsAudioCodec:       mv.AudioCodec,
			FymsSource:           mv.Source,
			FymsQualityLabel:     mv.QualityLabel,
			Chapters:             parseChaptersJSON(mv.ChaptersJSON),
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		applyMediaSourceCompatDefaults(&src, itemID)
		sources = append(sources, src)
	}

	mergedSources := collectMergedVersionSources(ctx, state, itemID, baseStreams)
	if len(mergedSources) > 0 {
		sources = append(sources, mergedSources...)
	}

	return sources
}

// collectMergedVersionSources finds items merged into itemID (via merged_to_id)
// and returns their media_versions as additional MediaSourceInfo entries.
func collectMergedVersionSources(ctx context.Context, state *AppState, itemID string, fallbackStreams []dto.MediaStreamInfo) []dto.MediaSourceInfo {
	siblings, err := state.Repo.Playback.ListMergedSiblingItems(ctx, itemID)
	if err != nil {
		return nil
	}
	if len(siblings) == 0 {
		return nil
	}

	var merged []dto.MediaSourceInfo
	for _, sib := range siblings {
		versions, err := loadMediaVersions(ctx, state, sib.ID)
		if err != nil {
			continue
		}
		for _, mv := range versions {
			msid := mv.ID.String()
			actualPath := mv.FilePath
			actualContainer := ""
			if mv.Container != nil {
				actualContainer = *mv.Container
			}
			protocol := "File"
			isRemote := false
			if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
				if rp := resolveStrmPath(mv.FilePath); rp != nil {
					actualPath = rp.filePath
					actualContainer = rp.container
					isRemote = rp.isRemote
					if isRemote {
						protocol = "Http"
					}
				}
			} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
				protocol = "Http"
				isRemote = true
			}

			versionStreams := fallbackStreams
			if len(mv.MediaInfo) > 0 {
				var mi map[string]json.RawMessage
				if json.Unmarshal(mv.MediaInfo, &mi) == nil {
					if msRaw, ok := mi["MediaStreams"]; ok {
						var miStreams []dto.MediaStreamInfo
						if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
							versionStreams = miStreams
						}
					}
				}
			}
			versionStreams = appendExternalSubtitleStreams(ctx, state.DB, itemID, msid, versionStreams)

			srcName := sib.LibName + " - " + mv.Name
			src := dto.MediaSourceInfo{
				ID:                   msid,
				Path:                 actualPath,
				Protocol:             protocol,
				Type:                 "Default",
				Container:            actualContainer,
				Name:                 srcName,
				IsRemote:             isRemote,
				RunTimeTicks:         mv.RuntimeTicks,
				SupportsDirectPlay:   true,
				SupportsDirectStream: true,
				SupportsTranscoding:  false,
				MediaStreams:         versionStreams,
				DirectStreamURL:      fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, actualContainer, msid),
				ETag:                 msid,
				Size:                 mv.Size,
				Formats:              []string{},
				FymsResolution:       mv.Resolution,
				FymsHdrFormat:        mv.HDRFormat,
				FymsVideoCodec:       mv.VideoCodec,
				FymsAudioCodec:       mv.AudioCodec,
				FymsSource:           mv.Source,
				FymsQualityLabel:     mv.QualityLabel,
				Chapters:             parseChaptersJSON(mv.ChaptersJSON),
			}
			if mv.Bitrate != nil {
				b := int64(*mv.Bitrate)
				src.Bitrate = &b
			}
			applyMediaSourceCompatDefaults(&src, itemID)
			merged = append(merged, src)
		}
	}
	return merged
}
