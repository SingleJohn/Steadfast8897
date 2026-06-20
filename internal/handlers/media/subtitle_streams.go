package media

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

func AppendExternalSubtitleStreams(ctx context.Context, pool *pgxpool.Pool, itemID, mediaSourceID string, streams []dto.MediaStreamInfo) []dto.MediaStreamInfo {
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
	protocol := "File"
	none := "None"

	stream := dto.MediaStreamInfo{
		Codec:                           sub.Codec,
		Type:                            "Subtitle",
		Index:                           index,
		Language:                        sub.Language,
		Title:                           sub.Title,
		IsDefault:                       sub.IsDefault,
		IsForced:                        sub.IsForced,
		IsExternal:                      true,
		Path:                            &sub.FilePath,
		DeliveryMethod:                  &deliveryMethod,
		DeliveryUrl:                     &deliveryURL,
		DisplayTitle:                    displayTitle,
		Protocol:                        &protocol,
		IsInterlaced:                    ptr(false),
		IsHearingImpaired:               ptr(false),
		IsExternalUrl:                   ptr(false),
		IsChunkedResponse:               ptr(false),
		IsTextSubtitleStream:            ptr(true),
		SupportsExternalStream:          ptr(true),
		ExtendedVideoType:               &none,
		ExtendedVideoSubType:            &none,
		ExtendedVideoSubTypeDescription: &none,
		AttachmentSize:                  ptr(int64(0)),
	}
	if sub.Language != nil {
		if name := subtitleDisplayLanguage(*sub.Language); name != "" {
			stream.DisplayLanguage = &name
		}
	}
	return stream
}

func externalSubtitleDeliveryURL(itemID, mediaSourceID string, index int32, codec string) string {
	ext := strings.TrimPrefix(strings.ToLower(codec), ".")
	if ext == "" {
		ext = "srt"
	}
	// 对齐 Emby:/Videos/{item}/{source}/Subtitles/{index}/{startPositionTicks}/Stream.{ext}
	// startPositionTicks 固定为 0(外挂字幕整文件直出,不转码),AllowChunkedResponse 供客户端分块读取。
	return fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.%s?AllowChunkedResponse=true",
		url.PathEscape(itemID), url.PathEscape(mediaSourceID), index, ext)
}

// externalSubtitleDisplayTitle 生成 Emby 风格标题,如 "Chinese (SRT)"、"English (ASS) - Forced"。
func externalSubtitleDisplayTitle(sub dto.ExternalSubtitleRow) *string {
	var langName string
	if sub.Language != nil {
		langName = subtitleDisplayLanguage(*sub.Language)
	}
	codec := strings.ToUpper(strings.TrimPrefix(strings.ToLower(sub.Codec), "."))

	var base string
	switch {
	case langName != "" && codec != "":
		base = fmt.Sprintf("%s (%s)", langName, codec)
	case langName != "":
		base = langName
	case codec != "":
		base = codec
	default:
		base = strings.TrimSuffix(filepath.Base(sub.FilePath), filepath.Ext(sub.FilePath))
	}

	extras := make([]string, 0, 2)
	if sub.IsForced {
		extras = append(extras, "Forced")
	}
	if sub.IsDefault {
		extras = append(extras, "Default")
	}
	if len(extras) > 0 {
		base = strings.TrimSpace(base + " - " + strings.Join(extras, " - "))
	}
	if base == "" {
		return nil
	}
	return &base
}

// subtitleLanguageNames 把存储的语言码(ISO 639-2/B 为主)映射为 Emby 展示用英文全名。
var subtitleLanguageNames = map[string]string{
	"chi": "Chinese",
	"zho": "Chinese",
	"zh":  "Chinese",
	"chs": "Chinese",
	"cht": "Chinese",
	"eng": "English",
	"en":  "English",
	"jpn": "Japanese",
	"ja":  "Japanese",
	"kor": "Korean",
	"ko":  "Korean",
	"fre": "French",
	"fra": "French",
	"fr":  "French",
	"ger": "German",
	"deu": "German",
	"de":  "German",
	"spa": "Spanish",
	"es":  "Spanish",
	"ita": "Italian",
	"it":  "Italian",
	"rus": "Russian",
	"ru":  "Russian",
	"por": "Portuguese",
	"pt":  "Portuguese",
	"tha": "Thai",
	"vie": "Vietnamese",
	"ara": "Arabic",
	"und": "Undetermined",
}

// subtitleDisplayLanguage 返回语言码对应的展示名,未知码原样返回(去空白)。
func subtitleDisplayLanguage(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return ""
	}
	if name, ok := subtitleLanguageNames[c]; ok {
		return name
	}
	return strings.TrimSpace(code)
}

func ptr[T any](v T) *T { return &v }

func nextMediaStreamIndex(streams []dto.MediaStreamInfo) int32 {
	var next int32
	for i := range streams {
		if streams[i].Index >= next {
			next = streams[i].Index + 1
		}
	}
	return next
}

func ExternalSubtitleByIndex(ctx context.Context, pool *pgxpool.Pool, mediaSourceID string, streamIndex int32, embeddedStreams []dto.MediaStreamInfo) (*dto.ExternalSubtitleRow, error) {
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
