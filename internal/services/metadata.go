package services

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"fyms/internal/utils"
)

type ProbeResult struct {
	DurationTicks int64          `json:"DurationTicks"`
	Streams       []StreamInfo   `json:"Streams"`
	Container     string         `json:"Container"`
	Chapters      []ProbeChapter `json:"Chapters"`
}

type ProbeChapter struct {
	StartPositionTicks int64  `json:"StartPositionTicks"`
	Name               string `json:"Name"`
}

type StreamInfo struct {
	Index        int32   `json:"Index"`
	StreamType   string  `json:"Type"`
	Codec        string  `json:"Codec"`
	Language     *string `json:"Language,omitempty"`
	Title        *string `json:"Title,omitempty"`
	IsDefault    bool    `json:"IsDefault"`
	IsForced     bool    `json:"IsForced"`
	Width        *int32  `json:"Width,omitempty"`
	Height       *int32  `json:"Height,omitempty"`
	BitRate      *int64  `json:"BitRate,omitempty"`
	Channels     *int32  `json:"Channels,omitempty"`
	SampleRate   *int32  `json:"SampleRate,omitempty"`
	BitDepth     *int32  `json:"BitDepth,omitempty"`
	PixelFormat  *string `json:"PixelFormat,omitempty"`
	DisplayTitle *string `json:"DisplayTitle,omitempty"`
}

type ffprobeOutput struct {
	Format   *ffprobeFormat   `json:"format"`
	Streams  []ffprobeStream  `json:"streams"`
	Chapters []ffprobeChapter `json:"chapters"`
}

type ffprobeChapter struct {
	StartTime *string           `json:"start_time"`
	Tags      map[string]string `json:"tags"`
}

type ffprobeFormat struct {
	Duration   *string `json:"duration"`
	FormatName *string `json:"format_name"`
}

type ffprobeStream struct {
	Index             *int32                 `json:"index"`
	CodecType         *string                `json:"codec_type"`
	CodecName         *string                `json:"codec_name"`
	Width             *int32                 `json:"width"`
	Height            *int32                 `json:"height"`
	BitRate           *string                `json:"bit_rate"`
	Channels          *int32                 `json:"channels"`
	SampleRate        *string                `json:"sample_rate"`
	BitsPerRawSample  *string                `json:"bits_per_raw_sample"`
	PixFmt            *string                `json:"pix_fmt"`
	Disposition       *ffprobeDisposition    `json:"disposition"`
	Tags              map[string]string      `json:"tags"`
}

type ffprobeDisposition struct {
	Default *int `json:"default"`
	Forced  *int `json:"forced"`
}

// ProbeFile 探测本地文件路径。
func ProbeFile(filePath string) (*ProbeResult, error) {
	return runFFProbe(filePath, false)
}

// ProbeRemote 探测远程 URL(strm 解析出的直链)。ffprobe 原生支持 http(s),
// 额外加读超时与 User-Agent,避免远程卡死并兼容 115/Alist 等需要 UA 的源。
func ProbeRemote(url string) (*ProbeResult, error) {
	return runFFProbe(url, true)
}

func runFFProbe(target string, remote bool) (*ProbeResult, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
	}
	if remote {
		// 输入协议选项需置于目标 URL 之前。-rw_timeout 单位为微秒。
		args = append(args,
			"-user_agent", "FYMS/1.0",
			"-rw_timeout", "30000000",
		)
	}
	args = append(args, target)

	cmd := exec.Command("ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe exec error: %w", err)
	}

	var data ffprobeOutput
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("ffprobe JSON parse error: %w", err)
	}

	duration := 0.0
	container := ""
	if data.Format != nil {
		if data.Format.Duration != nil {
			fmt.Sscanf(*data.Format.Duration, "%f", &duration)
		}
		if data.Format.FormatName != nil {
			parts := strings.SplitN(*data.Format.FormatName, ",", 2)
			container = parts[0]
		}
	}

	var streams []StreamInfo
	for _, s := range data.Streams {
		if s.CodecType == nil {
			continue
		}
		var streamType string
		switch *s.CodecType {
		case "video":
			streamType = "Video"
		case "audio":
			streamType = "Audio"
		case "subtitle":
			streamType = "Subtitle"
		default:
			continue
		}

		codec := ""
		if s.CodecName != nil {
			codec = *s.CodecName
		}

		isDefault := s.Disposition != nil && s.Disposition.Default != nil && *s.Disposition.Default == 1
		isForced := s.Disposition != nil && s.Disposition.Forced != nil && *s.Disposition.Forced == 1

		var lang, title *string
		if s.Tags != nil {
			if v, ok := s.Tags["language"]; ok {
				lang = &v
			}
			if v, ok := s.Tags["title"]; ok {
				title = &v
			}
		}

		idx := int32(0)
		if s.Index != nil {
			idx = *s.Index
		}

		info := StreamInfo{
			Index:      idx,
			StreamType: streamType,
			Codec:      codec,
			Language:   lang,
			Title:      title,
			IsDefault:  isDefault,
			IsForced:   isForced,
		}

		switch streamType {
		case "Video":
			info.Width = s.Width
			info.Height = s.Height
			info.BitRate = parseOptInt64(s.BitRate)
			info.PixelFormat = s.PixFmt
			w, h := int32(0), int32(0)
			if s.Width != nil {
				w = *s.Width
			}
			if s.Height != nil {
				h = *s.Height
			}
			dt := fmt.Sprintf("%s %dx%d", strings.ToUpper(codec), w, h)
			info.DisplayTitle = &dt
		case "Audio":
			info.Channels = s.Channels
			info.SampleRate = parseOptInt32(s.SampleRate)
			info.BitRate = parseOptInt64(s.BitRate)
			info.BitDepth = parseOptInt32(s.BitsPerRawSample)
			l := "und"
			if lang != nil {
				l = *lang
			}
			ch := int32(0)
			if s.Channels != nil {
				ch = *s.Channels
			}
			dt := fmt.Sprintf("%s %dch %s", strings.ToUpper(codec), ch, l)
			info.DisplayTitle = &dt
		case "Subtitle":
			l := "und"
			if lang != nil {
				l = *lang
			}
			dt := fmt.Sprintf("%s (%s)", l, codec)
			info.DisplayTitle = &dt
		}

		streams = append(streams, info)
	}

	var chapters []ProbeChapter
	for i, ch := range data.Chapters {
		startSec := 0.0
		if ch.StartTime != nil {
			fmt.Sscanf(*ch.StartTime, "%f", &startSec)
		}
		name := fmt.Sprintf("章节 %d", i+1)
		if v, ok := ch.Tags["title"]; ok && strings.TrimSpace(v) != "" {
			name = v
		}
		chapters = append(chapters, ProbeChapter{
			StartPositionTicks: utils.SecondsToTicks(startSec),
			Name:               name,
		})
	}

	return &ProbeResult{
		DurationTicks: utils.SecondsToTicks(duration),
		Streams:       streams,
		Container:     container,
		Chapters:      chapters,
	}, nil
}

func parseOptInt64(s *string) *int64 {
	if s == nil {
		return nil
	}
	var v int64
	if _, err := fmt.Sscanf(*s, "%d", &v); err == nil {
		return &v
	}
	return nil
}

func parseOptInt32(s *string) *int32 {
	if s == nil {
		return nil
	}
	var v int32
	if _, err := fmt.Sscanf(*s, "%d", &v); err == nil {
		return &v
	}
	return nil
}
