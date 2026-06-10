package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	".srt": true,
	".ass": true,
	".ssa": true,
	".vtt": true,
}

func IsSubtitleExt(ext string) bool {
	return subtitleExtSet[strings.ToLower(ext)]
}

func findExternalSubtitlesCached(videoPath string, cache DirCache) []externalSubtitleCandidate {
	videoName := strings.ToLower(filepath.Base(videoPath))
	videoStem := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	if videoStem == "" {
		return nil
	}

	var out []externalSubtitleCandidate
	for _, entry := range cache {
		name, path := entry[0], entry[1]
		ext := strings.ToLower(filepath.Ext(name))
		if !IsSubtitleExt(ext) {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		if !subtitleStemMatchesVideo(stem, videoStem) {
			continue
		}
		out = append(out, buildExternalSubtitleCandidate(path, stem, videoStem, ext))
	}
	return out
}

func subtitleStemMatchesVideo(subtitleStem, videoStem string) bool {
	if subtitleStem == videoStem {
		return true
	}
	if !strings.HasPrefix(subtitleStem, videoStem) {
		return false
	}
	rest := strings.TrimPrefix(subtitleStem, videoStem)
	if rest == "" {
		return true
	}
	switch rest[0] {
	case '.', '-', '_', ' ':
		return true
	default:
		return false
	}
}

func buildExternalSubtitleCandidate(path, subtitleStem, videoStem, ext string) externalSubtitleCandidate {
	suffix := strings.TrimPrefix(subtitleStem, videoStem)
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
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' ' || r == '[' || r == ']'
	})
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
		case "zh", "chi", "chs", "sc", "simplified", "cn":
			lang := "chi"
			if i+1 < len(tokens) && (tokens[i+1] == "tw" || tokens[i+1] == "hk" || tokens[i+1] == "cht" || tokens[i+1] == "tc" || tokens[i+1] == "traditional") {
				lang = "cht"
			}
			return &lang
		case "cht", "tc", "traditional":
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
		if _, err := pool.Exec(ctx,
			`INSERT INTO external_subtitles (item_id, media_version_id, file_path, codec, language, title, is_default, is_forced, updated_at)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, NOW())
			 ON CONFLICT (media_version_id, file_path) DO UPDATE SET
			 	item_id = EXCLUDED.item_id,
			 	codec = EXCLUDED.codec,
			 	language = EXCLUDED.language,
			 	title = EXCLUDED.title,
			 	is_default = EXCLUDED.is_default,
			 	is_forced = EXCLUDED.is_forced,
			 	updated_at = NOW()`,
			itemID, mediaVersionID, sub.Path, sub.Codec, derefStr(sub.Language), derefStr(sub.Title), sub.IsDefault, sub.IsForced,
		); err != nil {
			slog.Warn("[Scan] Failed to upsert external subtitle", "itemId", itemID, "mediaVersion", mediaVersionID, "path", sub.Path, "error", err)
		}
	}

	if len(paths) == 0 {
		if _, err := pool.Exec(ctx, "DELETE FROM external_subtitles WHERE media_version_id = $1::uuid", mediaVersionID); err != nil {
			slog.Warn("[Scan] Failed to clear external subtitles", "mediaVersion", mediaVersionID, "error", err)
		}
		return
	}
	if _, err := pool.Exec(ctx,
		"DELETE FROM external_subtitles WHERE media_version_id = $1::uuid AND NOT (file_path = ANY($2::text[]))",
		mediaVersionID, paths); err != nil {
		slog.Warn("[Scan] Failed to prune external subtitles", "mediaVersion", mediaVersionID, "error", err)
	}
}

func RefreshExternalSubtitlesForSidecar(ctx context.Context, pool *pgxpool.Pool, subtitlePath string) {
	if !IsSubtitleExt(filepath.Ext(subtitlePath)) {
		return
	}
	dir := filepath.Dir(subtitlePath)
	cache := CacheDir(dir)
	if cache == nil {
		cache = DirCache{}
	}
	subStem := strings.ToLower(strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath)))

	type version struct {
		itemID uuid.UUID
		mvID   uuid.UUID
		path   string
	}
	var versions []version
	for _, entry := range cache {
		if !IsVideoExt(filepath.Ext(entry[0])) {
			continue
		}
		videoStem := strings.TrimSuffix(entry[0], filepath.Ext(entry[0]))
		if !subtitleStemMatchesVideo(subStem, videoStem) {
			continue
		}
		rows, err := pool.Query(ctx,
			`SELECT item_id, id, file_path
			   FROM media_versions
			  WHERE file_path = $1
			     OR file_path = $2`,
			entry[1], filepath.Clean(entry[1]))
		if err != nil {
			continue
		}
		for rows.Next() {
			var v version
			if rows.Scan(&v.itemID, &v.mvID, &v.path) == nil {
				versions = append(versions, v)
			}
		}
		rows.Close()
	}

	for _, v := range versions {
		SyncExternalSubtitles(ctx, pool, v.itemID, v.mvID, v.path, cache)
	}
	if len(versions) > 0 {
		slog.Info("[Ingest] External subtitles refreshed", "subtitle", subtitlePath, "versions", len(versions))
	}
}

func RefreshExternalSubtitlesForVideoPath(ctx context.Context, pool *pgxpool.Pool, videoPath string) {
	if !IsVideoExt(filepath.Ext(videoPath)) {
		return
	}
	rows, err := pool.Query(ctx,
		`SELECT item_id, id, file_path
		   FROM media_versions
		  WHERE file_path = $1 OR file_path = $2`,
		videoPath, filepath.Clean(videoPath))
	if err != nil {
		return
	}
	defer rows.Close()

	cache := CacheDir(filepath.Dir(videoPath))
	var count int
	for rows.Next() {
		var itemID, mvID uuid.UUID
		var path string
		if rows.Scan(&itemID, &mvID, &path) != nil {
			continue
		}
		SyncExternalSubtitles(ctx, pool, itemID, mvID, path, cache)
		count++
	}
	if count > 0 {
		slog.Info("[Ingest] External subtitles refreshed", "video", videoPath, "versions", count)
	}
}
