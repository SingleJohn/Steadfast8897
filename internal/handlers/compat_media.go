package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	"fyms/internal/models"
)

func hideMediaSourceSizeForInfuse(c *gin.Context, sources []dto.MediaSourceInfo) {
	_, _ = c, sources
}

func buildItemMediaSources(ctx context.Context, state *AppState, itemID string, item *dto.ItemRow) []dto.MediaSourceInfo {
	rows, err := state.DB.Query(ctx,
		`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
		        resolution, hdr_format, video_codec, audio_codec, source, quality_label
		 FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC`, itemID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var versions []mediaVersionRow
	for rows.Next() {
		var v mediaVersionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.FilePath, &v.Container, &v.IsPrimary, &v.RuntimeTicks, &v.Bitrate, &v.Size, &v.MediaInfo,
			&v.Resolution, &v.HDRFormat, &v.VideoCodec, &v.AudioCodec, &v.Source, &v.QualityLabel); err != nil {
			continue
		}
		versions = append(versions, v)
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

	streamRows, _ := models.GetMediaStreams(ctx, state.DB, itemID)
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
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
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
	sibRows, err := state.DB.Query(ctx,
		`SELECT s.id::text, l.name AS lib_name
		 FROM items s JOIN libraries l ON s.library_id = l.id
		 WHERE s.merged_to_id = $1::uuid AND l.deleted_at IS NULL`, itemID)
	if err != nil {
		return nil
	}
	defer sibRows.Close()

	type sibInfo struct{ ID, LibName string }
	var siblings []sibInfo
	for sibRows.Next() {
		var si sibInfo
		if err := sibRows.Scan(&si.ID, &si.LibName); err != nil {
			continue
		}
		siblings = append(siblings, si)
	}
	if len(siblings) == 0 {
		return nil
	}

	var merged []dto.MediaSourceInfo
	for _, sib := range siblings {
		mvRows, err := state.DB.Query(ctx,
			`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
			        resolution, hdr_format, video_codec, audio_codec, source, quality_label
			 FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at ASC`, sib.ID)
		if err != nil {
			continue
		}
		for mvRows.Next() {
			var mv mediaVersionRow
			if err := mvRows.Scan(&mv.ID, &mv.Name, &mv.FilePath, &mv.Container, &mv.IsPrimary, &mv.RuntimeTicks, &mv.Bitrate, &mv.Size, &mv.MediaInfo,
				&mv.Resolution, &mv.HDRFormat, &mv.VideoCodec, &mv.AudioCodec, &mv.Source, &mv.QualityLabel); err != nil {
				continue
			}
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
			}
			if mv.Bitrate != nil {
				b := int64(*mv.Bitrate)
				src.Bitrate = &b
			}
			merged = append(merged, src)
		}
		mvRows.Close()
	}
	return merged
}
