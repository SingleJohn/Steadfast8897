package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
	"fyms/internal/services/scraper"
)

type externalSubtitleCandidate struct {
	Path      string
	Codec     string
	Language  *string
	Title     *string
	IsDefault bool
	IsForced  bool
}

var subtitleExtSet = map[string]bool{
	".srt":  true,
	".ass":  true,
	".ssa":  true,
	".vtt":  true,
	".smi":  true,
	".sami": true,
	".ttml": true,
	".dfxp": true,
}

// VobSub/PGS 不能按普通文本字幕直出，保留识别用于给出明确诊断。
var deferredSubtitleExtSet = map[string]bool{
	".sub": true,
	".idx": true,
	".sup": true,
}

var subtitleDirNames = map[string]bool{
	"subs":      true,
	"subtitle":  true,
	"subtitles": true,
}

var subtitleQualifierTokens = map[string]bool{
	"default": true, "forced": true, "foreign": true,
	"sdh": true, "hi": true, "cc": true, "commentary": true,
	"sign": true, "signs": true, "song": true, "songs": true,
	"zh": true, "zho": true, "chi": true, "chs": true, "cht": true,
	"sc": true, "tc": true, "cn": true, "tw": true, "hk": true,
	"hans": true, "hant": true, "simplified": true, "traditional": true,
	"中文": true, "简中": true, "繁中": true, "简体": true, "繁体": true,
	"简繁": true, "双语": true, "中英": true, "中日": true, "简英": true, "繁英": true,
	"en": true, "eng": true, "english": true,
	"ja": true, "jpn": true, "jp": true, "japanese": true,
	"ko": true, "kor": true, "kr": true, "korean": true,
	"fr": true, "fre": true, "fra": true, "french": true,
	"de": true, "ger": true, "deu": true, "german": true,
	"es": true, "spa": true, "spanish": true,
	"it": true, "ita": true, "italian": true,
	"ru": true, "rus": true, "russian": true,
	"pt": true, "por": true, "portuguese": true,
	"th": true, "tha": true, "thai": true,
	"vi": true, "vie": true, "vietnamese": true,
	"ar": true, "ara": true, "arabic": true,
	"und": true,
}

func IsSubtitleExt(ext string) bool {
	return subtitleExtSet[strings.ToLower(ext)]
}

func IsDeferredSubtitleExt(ext string) bool {
	return deferredSubtitleExtSet[strings.ToLower(ext)]
}

func findExternalSubtitlesCached(videoPath string, cache DirCache) []externalSubtitleCandidate {
	videoStems := externalSubtitleVideoStems(videoPath)
	if len(videoStems) == 0 {
		return nil
	}

	var out []externalSubtitleCandidate
	for _, entry := range externalSubtitleSearchCache(videoPath, cache) {
		name, path := entry[0], entry[1]
		ext := strings.ToLower(filepath.Ext(name))
		if !IsSubtitleExt(ext) {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		matchedVideoStem := ""
		for _, videoStem := range videoStems {
			if subtitleStemMatchesVideo(stem, videoStem) {
				matchedVideoStem = videoStem
				break
			}
		}
		if matchedVideoStem == "" {
			continue
		}
		out = append(out, buildExternalSubtitleCandidate(path, stem, matchedVideoStem, ext))
	}
	return out
}

func externalSubtitleVideoStems(videoPath string) []string {
	videoName := strings.ToLower(filepath.Base(videoPath))
	videoStem := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	if videoStem == "" {
		return nil
	}
	stems := []string{videoStem}
	if root := findBdmvMovieRoot(videoPath); root != "" {
		rootStem := strings.ToLower(filepath.Base(root))
		if rootStem != "" && rootStem != videoStem {
			stems = append(stems, rootStem)
		}
	}
	return stems
}

func externalSubtitleSearchCache(videoPath string, cache DirCache) DirCache {
	result := make(DirCache, 0, len(cache)+8)
	seenPaths := make(map[string]struct{}, len(cache)+8)
	coveredDirs := make(map[string]struct{}, 3)

	addCache := func(entries DirCache) {
		for _, entry := range entries {
			key := filepath.Clean(entry[1])
			if _, ok := seenPaths[key]; ok {
				continue
			}
			seenPaths[key] = struct{}{}
			coveredDirs[filepath.Clean(filepath.Dir(entry[1]))] = struct{}{}
			result = append(result, entry)
		}
	}
	addDir := func(dir string) {
		dir = filepath.Clean(dir)
		if _, ok := coveredDirs[dir]; ok {
			return
		}
		entries := CacheDir(dir)
		coveredDirs[dir] = struct{}{}
		addCache(entries)
	}

	// 调用方缓存可能是电影根目录（BDMV 场景），因此始终补齐实际视频目录。
	addCache(cache)
	addDir(filepath.Dir(videoPath))
	if root := findBdmvMovieRoot(videoPath); root != "" {
		addDir(root)
	}

	baseEntries := append(DirCache(nil), result...)
	for _, entry := range baseEntries {
		if subtitleDirNames[entry[0]] {
			addDir(entry[1])
		}
	}
	return result
}

func subtitleStemMatchesVideo(subtitleStem, videoStem string) bool {
	subtitleStem = strings.ToLower(strings.TrimSpace(subtitleStem))
	videoStem = strings.ToLower(strings.TrimSpace(videoStem))
	if subtitleStem == videoStem {
		return true
	}
	if strings.HasPrefix(subtitleStem, videoStem) {
		rest := strings.TrimPrefix(subtitleStem, videoStem)
		if rest != "" {
			switch rest[0] {
			case '.', '-', '_', ' ':
				return true
			}
		}
	}
	return normalizedSubtitleStemMatchesVideo(subtitleStem, videoStem)
}

func normalizedSubtitleStemMatchesVideo(subtitleStem, videoStem string) bool {
	// 仅在严格 stem 匹配失败时去掉语言、画质和发布组标签；剧集仍要求集号明确对齐。
	subtitleBase, _ := splitSubtitleQualifierSuffix(subtitleStem)
	videoParsed := scraper.Parse(videoStem, scraper.ModeEpisode)
	subtitleParsed := scraper.Parse(subtitleBase, scraper.ModeEpisode)

	if (videoParsed.Episode == nil) != (subtitleParsed.Episode == nil) {
		return false
	}
	videoTitle := comparableSubtitleTitle(videoParsed)
	subtitleTitle := comparableSubtitleTitle(subtitleParsed)
	if videoParsed.Episode != nil {
		if *videoParsed.Episode != *subtitleParsed.Episode {
			return false
		}
		if (videoParsed.Season == nil) != (subtitleParsed.Season == nil) {
			return false
		}
		if videoParsed.Season != nil && *videoParsed.Season != *subtitleParsed.Season {
			return false
		}
		return videoTitle == subtitleTitle
	}

	if videoParsed.Year != nil && subtitleParsed.Year != nil && *videoParsed.Year != *subtitleParsed.Year {
		return false
	}
	return videoTitle != "" && videoTitle == subtitleTitle
}

func comparableSubtitleTitle(parsed scraper.ParsedName) string {
	title := parsed.Title
	if title == "" {
		title = parsed.OriginalTitle
	}
	title = strings.ToLower(strings.TrimSpace(title))
	title = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(title)
	return strings.Join(strings.Fields(title), " ")
}

func splitSubtitleQualifierSuffix(stem string) (string, string) {
	qualifierStart := len(stem)
	pos := len(stem)
	for {
		for pos > 0 && isSubtitleTokenDelimiter(rune(stem[pos-1])) {
			pos--
		}
		if pos == 0 {
			break
		}
		tokenEnd := pos
		for pos > 0 && !isSubtitleTokenDelimiter(rune(stem[pos-1])) {
			pos--
		}
		token := strings.ToLower(strings.TrimSpace(stem[pos:tokenEnd]))
		if !subtitleQualifierTokens[token] {
			break
		}
		qualifierStart = pos
	}
	base := strings.TrimRightFunc(stem[:qualifierStart], isSubtitleTokenDelimiter)
	suffix := strings.TrimFunc(stem[qualifierStart:], isSubtitleTokenDelimiter)
	return base, suffix
}

func isSubtitleTokenDelimiter(r rune) bool {
	switch r {
	case '.', '-', '_', ' ', '[', ']', '(', ')', '{', '}', '&', '+', ',':
		return true
	default:
		return false
	}
}

func buildExternalSubtitleCandidate(path, subtitleStem, videoStem, ext string) externalSubtitleCandidate {
	suffix := strings.TrimPrefix(subtitleStem, videoStem)
	if suffix == subtitleStem {
		_, suffix = splitSubtitleQualifierSuffix(subtitleStem)
	}
	suffix = strings.TrimLeft(suffix, ".-_ ")
	tokens := subtitleTokens(suffix)
	lang := detectSubtitleLanguage(tokens)
	isForced := containsToken(tokens, "forced")
	isDefault := containsToken(tokens, "default")

	var title *string
	if suffix != "" {
		t := strings.ReplaceAll(suffix, "_", " ")
		t = strings.ReplaceAll(t, ".", " ")
		t = strings.ReplaceAll(t, "-", " ")
		t = strings.Join(strings.Fields(t), " ")
		if t != "" {
			title = &t
		}
	}

	return externalSubtitleCandidate{
		Path:      path,
		Codec:     strings.TrimPrefix(ext, "."),
		Language:  lang,
		Title:     title,
		IsDefault: isDefault,
		IsForced:  isForced,
	}
}

func subtitleTokens(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(strings.ToLower(s), isSubtitleTokenDelimiter)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

func detectSubtitleLanguage(tokens []string) *string {
	for i, token := range tokens {
		switch token {
		case "zh", "zho", "chi", "chs", "sc", "hans", "simplified", "cn", "中文", "简中", "简体", "简繁", "双语", "中英", "中日", "简英", "繁英":
			lang := "chi"
			if i+1 < len(tokens) && (tokens[i+1] == "tw" || tokens[i+1] == "hk" || tokens[i+1] == "cht" || tokens[i+1] == "tc" || tokens[i+1] == "hant" || tokens[i+1] == "traditional") {
				lang = "cht"
			}
			return &lang
		case "cht", "tc", "hant", "traditional", "繁中", "繁体":
			lang := "cht"
			return &lang
		case "en", "eng", "english":
			lang := "eng"
			return &lang
		case "ja", "jpn", "jp", "japanese":
			lang := "jpn"
			return &lang
		case "ko", "kor", "kr", "korean":
			lang := "kor"
			return &lang
		case "fr", "fre", "fra", "french":
			lang := "fre"
			return &lang
		case "de", "ger", "deu", "german":
			lang := "ger"
			return &lang
		case "es", "spa", "spanish":
			lang := "spa"
			return &lang
		case "it", "ita", "italian":
			lang := "ita"
			return &lang
		case "ru", "rus", "russian":
			lang := "rus"
			return &lang
		case "pt", "por", "portuguese":
			lang := "por"
			return &lang
		case "th", "tha", "thai":
			lang := "tha"
			return &lang
		case "vi", "vie", "vietnamese":
			lang := "vie"
			return &lang
		case "ar", "ara", "arabic":
			lang := "ara"
			return &lang
		case "und":
			lang := "und"
			return &lang
		}
	}
	return nil
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

func SyncExternalSubtitles(ctx context.Context, pool *pgxpool.Pool, itemID uuid.UUID, mediaVersionID uuid.UUID, videoPath string, cache DirCache) {
	if mediaVersionID == uuid.Nil || strings.TrimSpace(videoPath) == "" {
		return
	}
	if cache == nil {
		cache = CacheDir(filepath.Dir(videoPath))
	}
	subs := findExternalSubtitlesCached(videoPath, cache)
	paths := make([]string, 0, len(subs))
	for _, sub := range subs {
		paths = append(paths, sub.Path)
		if err := repository.NewScanIngestRepository(pool).UpsertExternalSubtitle(ctx, repository.ExternalSubtitleUpsert{
			ItemID:         itemID,
			MediaVersionID: mediaVersionID,
			FilePath:       sub.Path,
			Codec:          sub.Codec,
			Language:       sub.Language,
			Title:          sub.Title,
			IsDefault:      sub.IsDefault,
			IsForced:       sub.IsForced,
		}); err != nil {
			slog.Warn("[Scan] Failed to upsert external subtitle", "itemId", itemID, "mediaVersion", mediaVersionID, "path", sub.Path, "error", err)
		}
	}

	if len(paths) == 0 {
		if err := repository.NewScanIngestRepository(pool).DeleteExternalSubtitlesForMediaVersion(ctx, mediaVersionID); err != nil {
			slog.Warn("[Scan] Failed to clear external subtitles", "mediaVersion", mediaVersionID, "error", err)
		}
		return
	}
	if err := repository.NewScanIngestRepository(pool).PruneExternalSubtitlesForMediaVersion(ctx, mediaVersionID, paths); err != nil {
		slog.Warn("[Scan] Failed to prune external subtitles", "mediaVersion", mediaVersionID, "error", err)
	}
}

func RefreshExternalSubtitlesForSidecar(ctx context.Context, pool *pgxpool.Pool, subtitlePath string) {
	if !IsSubtitleExt(filepath.Ext(subtitlePath)) {
		return
	}
	dir := filepath.Dir(subtitlePath)
	subStem := strings.ToLower(strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath)))

	type version struct {
		itemID uuid.UUID
		mvID   uuid.UUID
		path   string
	}
	versionsByID := make(map[uuid.UUID]version)
	for _, searchDir := range subtitleVideoSearchDirs(dir) {
		for _, entry := range CacheDir(searchDir) {
			if !IsVideoExt(filepath.Ext(entry[0])) {
				continue
			}
			matched := false
			for _, videoStem := range externalSubtitleVideoStems(entry[1]) {
				if subtitleStemMatchesVideo(subStem, videoStem) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			versionsForPath, err := repository.NewScanIngestRepository(pool).ListMediaVersionsByPath(ctx, entry[1], filepath.Clean(entry[1]))
			if err != nil {
				continue
			}
			for _, row := range versionsForPath {
				versionsByID[row.ID] = version{itemID: row.ItemID, mvID: row.ID, path: row.FilePath}
			}
		}
	}

	for _, v := range versionsByID {
		SyncExternalSubtitles(ctx, pool, v.itemID, v.mvID, v.path, nil)
	}
	if len(versionsByID) > 0 {
		slog.Info("[Ingest] External subtitles refreshed", "subtitle", subtitlePath, "versions", len(versionsByID))
	} else {
		slog.Debug("[Ingest] External subtitle did not match a media version", "subtitle", subtitlePath)
	}
}

func subtitleVideoSearchDirs(subtitleDir string) []string {
	dirs := []string{filepath.Clean(subtitleDir)}
	if subtitleDirNames[strings.ToLower(filepath.Base(subtitleDir))] {
		dirs = append(dirs, filepath.Clean(filepath.Dir(subtitleDir)))
	}
	baseDirs := append([]string(nil), dirs...)
	for _, dir := range baseDirs {
		if isBdmvMovieDir(dir) {
			dirs = append(dirs, filepath.Join(dir, "BDMV", "STREAM"))
		}
	}
	return dirs
}

func RefreshExternalSubtitlesForVideoPath(ctx context.Context, pool *pgxpool.Pool, videoPath string) {
	if !IsVideoExt(filepath.Ext(videoPath)) {
		return
	}
	rows, err := repository.NewScanIngestRepository(pool).ListMediaVersionsByPath(ctx, videoPath, filepath.Clean(videoPath))
	if err != nil {
		return
	}

	cache := CacheDir(filepath.Dir(videoPath))
	var count int
	for _, row := range rows {
		SyncExternalSubtitles(ctx, pool, row.ItemID, row.ID, row.FilePath, cache)
		count++
	}
	if count > 0 {
		slog.Info("[Ingest] External subtitles refreshed", "video", videoPath, "versions", count)
	}
}
