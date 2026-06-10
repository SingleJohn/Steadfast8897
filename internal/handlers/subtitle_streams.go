package handlers

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/models"
)

func appendExternalSubtitleStreams(ctx context.Context, pool *pgxpool.Pool, itemID, mediaSourceID string, streams []dto.MediaStreamInfo) []dto.MediaStreamInfo {
	subs, err := models.GetExternalSubtitlesForMediaVersion(ctx, pool, mediaSourceID)
	if err != nil || len(subs) == 0 {
		return streams
	}
	next := nextMediaStreamIndex(streams)
	for i := range subs {
		sub := subs[i]
		index := next
		next++
		streams = append(streams, externalSubtitleStream(itemID, mediaSourceID, sub, index))
	}
	return streams
}

func externalSubtitleStream(itemID, mediaSourceID string, sub dto.ExternalSubtitleRow, index int32) dto.MediaStreamInfo {
	deliveryMethod := "External"
	deliveryURL := externalSubtitleDeliveryURL(itemID, mediaSourceID, index, sub.Codec)
	displayTitle := externalSubtitleDisplayTitle(sub)
	return dto.MediaStreamInfo{
		Codec:          sub.Codec,
		Type:           "Subtitle",
		Index:          index,
		Language:       sub.Language,
		Title:          sub.Title,
		IsDefault:      sub.IsDefault,
		IsForced:       sub.IsForced,
		IsExternal:     true,
		Path:           &sub.FilePath,
		DeliveryMethod: &deliveryMethod,
		DeliveryUrl:    &deliveryURL,
		DisplayTitle:   displayTitle,
	}
}

func externalSubtitleDeliveryURL(itemID, mediaSourceID string, index int32, codec string) string {
	ext := strings.TrimPrefix(strings.ToLower(codec), ".")
	if ext == "" {
		ext = "srt"
	}
	return fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/Stream.%s",
		url.PathEscape(itemID), url.PathEscape(mediaSourceID), index, ext)
}

func externalSubtitleDisplayTitle(sub dto.ExternalSubtitleRow) *string {
	parts := make([]string, 0, 3)
	if sub.Language != nil && *sub.Language != "" {
		parts = append(parts, *sub.Language)
	}
	if sub.Title != nil && *sub.Title != "" {
		parts = append(parts, *sub.Title)
	}
	if sub.IsForced {
		parts = append(parts, "Forced")
	}
	if len(parts) == 0 {
		title := strings.TrimSuffix(filepath.Base(sub.FilePath), filepath.Ext(sub.FilePath))
		if title != "" {
			return &title
		}
		return nil
	}
	title := strings.Join(parts, " - ")
	return &title
}

func nextMediaStreamIndex(streams []dto.MediaStreamInfo) int32 {
	var next int32
	for i := range streams {
		if streams[i].Index >= next {
			next = streams[i].Index + 1
		}
	}
	return next
}

func externalSubtitleByIndex(ctx context.Context, pool *pgxpool.Pool, mediaSourceID string, streamIndex int32, embeddedStreams []dto.MediaStreamInfo) (*dto.ExternalSubtitleRow, error) {
	subs, err := models.GetExternalSubtitlesForMediaVersion(ctx, pool, mediaSourceID)
	if err != nil {
		return nil, err
	}
	next := nextMediaStreamIndex(embeddedStreams)
	for i := range subs {
		if next == streamIndex {
			return &subs[i], nil
		}
		next++
	}
	return nil, nil
}
