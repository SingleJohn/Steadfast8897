package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"fyms/internal/services/scraper"
)

// extrasDirNames 是 Emby/Jellyfin 约定的"附属内容"目录名:里面的视频是预告片/花絮等,
// 不是独立影片,扫库时不应单独入库。
var extrasDirNames = map[string]bool{
	"trailers": true, "extras": true, "featurettes": true, "behind the scenes": true,
	"deleted scenes": true, "interviews": true, "scenes": true, "samples": true,
	"shorts": true, "theme-music": true, "backdrops": true,
}

// IsExtrasDirName 判断目录名是否为 extras 类目录。
func IsExtrasDirName(name string) bool {
	return extrasDirNames[strings.ToLower(strings.TrimSpace(name))]
}

// IsInExtrasFolder 判断文件路径的直接父目录是否为 extras 类目录(如 .../trailers/trailer.mp4)。
func IsInExtrasFolder(filePath string) bool {
	return IsExtrasDirName(filepath.Base(filepath.Dir(filePath)))
}

// catalogNumberRe 匹配常见番号:可选前导数字(厂牌/频道码) + 字母 + 连字符 + 数字。
// 例:IPZZ-857 / 300MIUM-1328 / 326IAV-002 / 336KNB-406。
var catalogNumberRe = regexp.MustCompile(`(?i)(\d{0,4}[A-Za-z]{2,8})-(\d{2,6})`)

// ExtractCatalogNumber 从名称/文件名提取番号,规范成大写带连字符(300MIUM-1328)。无匹配返回 ""。
func ExtractCatalogNumber(name string) string {
	m := catalogNumberRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}

const scanConcurrency = 10

var videoExtSet = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true, ".flv": true,
	".webm": true, ".m4v": true, ".mov": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".iso": true, ".bdmv": true, ".m2ts": true, ".vob": true,
	".rmvb": true, ".rm": true, ".3gp": true, ".ogv": true, ".strm": true,
}

var (
	posterImagePrefixes   = []string{"poster", "cover", "folder", "thumb"}
	backdropImagePrefixes = []string{"fanart", "backdrop", "background", "landscape"}
)

func IsVideoExt(ext string) bool {
	return videoExtSet[strings.ToLower(ext)]
}

// ============ Filename Parsing ============

type ParsedMovie struct {
	Name string
	Year *int32
}

func ParseMovieName(name string) ParsedMovie {
	p := scraper.Parse(name, scraper.ModeMovie)
	title := preferTitle(p, name)
	return ParsedMovie{Name: title, Year: p.Year}
}

// preferTitle 在 Title 为空时回落到 OriginalTitle 或原始名。
func preferTitle(p scraper.ParsedName, raw string) string {
	if p.Title != "" {
		return p.Title
	}
	if p.OriginalTitle != "" {
		return p.OriginalTitle
	}
	return raw
}

type ParsedEpisode struct {
	Season  int32
	Episode *int32
	Title   *string
}

func ParseEpisodeInfo(filename string) *ParsedEpisode {
	p := scraper.Parse(filename, scraper.ModeEpisode)
	if p.Episode == nil {
		return nil
	}
	season := int32(1)
	if p.Season != nil {
		season = *p.Season
	}
	return &ParsedEpisode{Season: season, Episode: p.Episode}
}

// ============ JSON / Null Helpers ============

func getJSONInt64(m map[string]interface{}, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		i := int64(n)
		return &i
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return &i
		}
	}
	return nil
}

func nullableJSON(data []byte) interface{} {
	if data == nil {
		return nil
	}
	return string(data)
}

// NullableStr 为空字符串时返回 nil,保证对应列写入 NULL。
func NullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ComputeMediaVersionQuality 组合 mediainfo(优先)与文件名 NameParser(兜底)推导 QualityTags,
// 并给出短标签(如 "4K HDR BluRay")。是所有 media_versions INSERT 路径的共用入口。
func ComputeMediaVersionQuality(fileName string, mi map[string]interface{}) (scraper.QualityTags, string) {
	q := scraper.MergeQualityTags(
		scraper.QualityFromMediainfo(mi),
		scraper.QualityFromParsed(scraper.Parse(fileName, scraper.ModeEpisode)),
	)
	return q, scraper.QualityLabel(q)
}

func ptrAndThen(p *string, f func(string) *string) *string {
	if p == nil {
		return nil
	}
	return f(*p)
}

func derefStr(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// fileMtimeOrNil returns the mtime of the file at path, or nil if stat fails.
// Used as the created_at timestamp for new items so FYMS's "latest" list
// mirrors Emby's DateCreated (= file mtime on disk).
func fileMtimeOrNil(path string) interface{} {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return info.ModTime().UTC()
}
