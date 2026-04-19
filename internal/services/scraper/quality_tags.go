package scraper

import (
	"strings"
)

// QualityFromMediainfo 从 mediainfo JSON(ReadMediainfoJSON 的返回值)抽取画质标签。
// 只读最常见的键,找不到留空字符串。
// 支持两种结构:
//   - 顶级 Width/Height/VideoRange/HDR_Format/CodecID/Format(Emby/Jellyfin MediaSourceInfo 风格)
//   - Streams 数组,取第一个 Video 流的 Width/Height/Codec
func QualityFromMediainfo(mi map[string]any) QualityTags {
	var q QualityTags
	if mi == nil {
		return q
	}

	width, height := readDimensions(mi)
	q.Resolution = resolutionFromDimensions(width, height)
	q.HDRFormat = hdrFromMediainfo(mi)
	q.VideoCodec = videoCodecFromMediainfo(mi)
	q.AudioCodec = audioCodecFromMediainfo(mi)
	return q
}

func readDimensions(mi map[string]any) (int, int) {
	if w, h := readIntField(mi, "Width"), readIntField(mi, "Height"); w > 0 && h > 0 {
		return w, h
	}
	streams, _ := mi["Streams"].([]any)
	for _, raw := range streams {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := s["Type"].(string); !strings.EqualFold(t, "Video") {
			continue
		}
		if w, h := readIntField(s, "Width"), readIntField(s, "Height"); w > 0 && h > 0 {
			return w, h
		}
	}
	return 0, 0
}

func resolutionFromDimensions(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	shortEdge := height
	if width < shortEdge {
		shortEdge = width
	}
	switch {
	case longEdge >= 7000 || shortEdge >= 4000:
		return "8k"
	case longEdge >= 3200 || shortEdge >= 1800:
		return "4k"
	case longEdge >= 2500 || shortEdge >= 1300:
		return "1440p"
	case longEdge >= 1800 || shortEdge >= 1000:
		return "1080p"
	case longEdge >= 1200 || shortEdge >= 700:
		return "720p"
	default:
		return "sd"
	}
}

func hdrFromMediainfo(mi map[string]any) string {
	candidates := []string{
		readStringField(mi, "HDR_Format"),
		readStringField(mi, "HdrFormat"),
		readStringField(mi, "VideoRange"),
		readStringField(mi, "VideoRangeType"),
		readStringField(mi, "ColorTransfer"),
	}
	streams, _ := mi["Streams"].([]any)
	for _, raw := range streams {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := s["Type"].(string); !strings.EqualFold(t, "Video") {
			continue
		}
		candidates = append(candidates,
			readStringField(s, "HDR_Format"),
			readStringField(s, "VideoRange"),
			readStringField(s, "VideoRangeType"),
			readStringField(s, "ColorTransfer"),
		)
	}

	var best string
	for _, c := range candidates {
		hdr := hdrFromString(c)
		if hdr != "" && hdrRank[hdr] > hdrRank[best] {
			best = hdr
		}
	}
	return best
}

func hdrFromString(s string) string {
	if s == "" {
		return ""
	}
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "dolby vision") || strings.Contains(l, "dolbyvision") || strings.Contains(l, "dovi"):
		return "dv"
	case strings.Contains(l, "hdr10+"):
		return "hdr10+"
	case strings.Contains(l, "hdr"):
		return "hdr10"
	case strings.Contains(l, "smpte2084"), strings.Contains(l, "pq"):
		return "hdr10"
	case strings.Contains(l, "hlg"):
		return "hdr10"
	case l == "sdr":
		return "sdr"
	}
	return ""
}

func videoCodecFromMediainfo(mi map[string]any) string {
	candidates := []string{
		readStringField(mi, "VideoCodec"),
		readStringField(mi, "CodecID"),
		readStringField(mi, "Format"),
		readStringField(mi, "Codec"),
	}
	streams, _ := mi["Streams"].([]any)
	for _, raw := range streams {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := s["Type"].(string); !strings.EqualFold(t, "Video") {
			continue
		}
		candidates = append(candidates,
			readStringField(s, "CodecID"),
			readStringField(s, "Format"),
			readStringField(s, "Codec"),
			readStringField(s, "CodecName"),
		)
	}

	var best string
	for _, c := range candidates {
		codec := videoCodecFromString(c)
		if codec != "" && codecRank[codec] > codecRank[best] {
			best = codec
		}
	}
	return best
}

func videoCodecFromString(s string) string {
	if s == "" {
		return ""
	}
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "hevc"), strings.Contains(l, "h.265"), strings.Contains(l, "h265"), strings.Contains(l, "x265"), strings.Contains(l, "v_mpegh/iso/hevc"):
		return "x265"
	case strings.Contains(l, "av1"):
		return "av1"
	case strings.Contains(l, "avc"), strings.Contains(l, "h.264"), strings.Contains(l, "h264"), strings.Contains(l, "x264"), strings.Contains(l, "v_mpeg4/iso/avc"):
		return "x264"
	}
	return ""
}

func audioCodecFromMediainfo(mi map[string]any) string {
	candidates := []string{
		readStringField(mi, "AudioCodec"),
		readStringField(mi, "AudioFormat"),
	}
	streams, _ := mi["Streams"].([]any)
	for _, raw := range streams {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := s["Type"].(string); !strings.EqualFold(t, "Audio") {
			continue
		}
		candidates = append(candidates,
			readStringField(s, "CodecID"),
			readStringField(s, "Format"),
			readStringField(s, "Codec"),
			readStringField(s, "CodecName"),
			readStringField(s, "Profile"),
			readStringField(s, "DisplayTitle"),
		)
	}

	var best string
	for _, c := range candidates {
		codec := audioCodecFromString(c)
		if codec != "" && audioRank[codec] > audioRank[best] {
			best = codec
		}
	}
	return best
}

func audioCodecFromString(s string) string {
	if s == "" {
		return ""
	}
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "atmos"):
		return "atmos"
	case strings.Contains(l, "truehd"):
		return "truehd"
	case strings.Contains(l, "dts-hd"), strings.Contains(l, "dtshd"), strings.Contains(l, "dts hd"), strings.Contains(l, "dts:x"):
		return "dts-hd"
	case strings.Contains(l, "dts"):
		return "dts"
	case strings.Contains(l, "eac3"), strings.Contains(l, "e-ac-3"), strings.Contains(l, "e-ac3"):
		return "eac3"
	case strings.Contains(l, "ac3"), strings.Contains(l, "ac-3"):
		return "ac3"
	case strings.Contains(l, "flac"):
		return "flac"
	case strings.Contains(l, "aac"):
		return "aac"
	}
	return ""
}

// QualityFromParsed 从 NameParser 的解析结果中取出画质标签。
func QualityFromParsed(p ParsedName) QualityTags {
	return p.Quality
}

// MergeQualityTags 按字段补空:primary 为空时用 fallback 的对应字段。
func MergeQualityTags(primary, fallback QualityTags) QualityTags {
	if primary.Resolution == "" {
		primary.Resolution = fallback.Resolution
	}
	if primary.HDRFormat == "" {
		primary.HDRFormat = fallback.HDRFormat
	}
	if primary.VideoCodec == "" {
		primary.VideoCodec = fallback.VideoCodec
	}
	if primary.AudioCodec == "" {
		primary.AudioCodec = fallback.AudioCodec
	}
	if primary.Source == "" {
		primary.Source = fallback.Source
	}
	return primary
}

// QualityLabel 按 resolution → hdr → source → video_codec → audio_codec 顺序拼接短标签,
// 仅保留前 3 段,例如 "4K HDR BluRay"。
func QualityLabel(t QualityTags) string {
	parts := []string{}
	add := func(s string) {
		if s == "" {
			return
		}
		parts = append(parts, s)
	}
	add(formatResolutionLabel(t.Resolution))
	add(formatHDRLabel(t.HDRFormat))
	add(formatSourceLabel(t.Source))
	add(formatVideoCodecLabel(t.VideoCodec))
	add(formatAudioCodecLabel(t.AudioCodec))
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, " ")
}

func formatResolutionLabel(r string) string {
	switch r {
	case "4k":
		return "4K"
	case "8k":
		return "8K"
	case "1440p":
		return "1440p"
	case "1080p":
		return "1080p"
	case "720p":
		return "720p"
	case "sd":
		return "SD"
	}
	return ""
}

func formatHDRLabel(h string) string {
	switch h {
	case "hdr10+":
		return "HDR10+"
	case "hdr10":
		return "HDR"
	case "dv":
		return "DV"
	}
	return ""
}

func formatVideoCodecLabel(c string) string {
	switch c {
	case "x265":
		return "x265"
	case "x264":
		return "x264"
	case "av1":
		return "AV1"
	}
	return ""
}

func formatAudioCodecLabel(a string) string {
	switch a {
	case "atmos":
		return "Atmos"
	case "truehd":
		return "TrueHD"
	case "dts-hd":
		return "DTS-HD"
	case "dts":
		return "DTS"
	case "eac3":
		return "EAC3"
	case "ac3":
		return "AC3"
	case "flac":
		return "FLAC"
	case "aac":
		return "AAC"
	}
	return ""
}

func formatSourceLabel(s string) string {
	switch s {
	case "remux":
		return "Remux"
	case "bluray":
		return "BluRay"
	case "bdrip":
		return "BDRip"
	case "web-dl":
		return "WEB-DL"
	case "webrip":
		return "WEBRip"
	case "hdtv":
		return "HDTV"
	case "dvdrip":
		return "DVDRip"
	}
	return ""
}

func readStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func readIntField(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case string:
		if n == "" {
			return 0
		}
		var out int
		for _, r := range n {
			if r < '0' || r > '9' {
				return out
			}
			out = out*10 + int(r-'0')
		}
		return out
	}
	return 0
}
