package services

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type scrapeSaveTargets struct {
	PosterPath   string
	BackdropPath string
	NfoPath      string
}

func getScrapeSaveMode(ctx context.Context, pool *pgxpool.Pool) string {
	mode := "database"
	if val, ok, err := repository.NewSystemConfigRepository(pool).GetString(ctx, "scrape_save_mode"); err == nil && ok {
		switch strings.TrimSpace(val) {
		case "database", "media_dir", "both":
			mode = strings.TrimSpace(val)
		}
	}
	return mode
}

func resolveScrapeSaveTargets(ctx context.Context, pool *pgxpool.Pool, itemID, itemType string) scrapeSaveTargets {
	targets := scrapeSaveTargets{}

	switch itemType {
	case "Movie":
		if filePath, err := repository.NewScanIngestRepository(pool).GetItemFilePath(ctx, itemID); err == nil && filePath != nil && *filePath != "" {
			if !strings.HasPrefix(strings.ToLower(*filePath), "http") {
				dir := filepath.Dir(*filePath)
				targets.PosterPath = filepath.Join(dir, "poster.jpg")
				targets.BackdropPath = filepath.Join(dir, "fanart.jpg")
				targets.NfoPath = filepath.Join(dir, "movie.nfo")
			}
		}
	case "Series":
		if episodePath, err := repository.NewScanIngestRepository(pool).GetFirstSeriesEpisodeFilePath(ctx, itemID); err == nil && episodePath != nil && *episodePath != "" {
			showDir := filepath.Dir(filepath.Dir(*episodePath))
			targets.PosterPath = filepath.Join(showDir, "poster.jpg")
			targets.BackdropPath = filepath.Join(showDir, "fanart.jpg")
			targets.NfoPath = filepath.Join(showDir, "tvshow.nfo")
		}
	}

	if targets.PosterPath != "" {
		slog.Debug("[TMDB] Resolved media save targets", "item_id", itemID, "poster", targets.PosterPath, "backdrop", targets.BackdropPath, "nfo", targets.NfoPath)
	} else {
		slog.Debug("[TMDB] No media directory target resolved, will use data/metadata/", "item_id", itemID, "type", itemType)
	}

	return targets
}

func resolveSeasonPosterMediaPath(ctx context.Context, pool *pgxpool.Pool, seasonID string) string {
	episodePath, err := repository.NewScanIngestRepository(pool).GetFirstSeasonEpisodeFilePath(ctx, seasonID)
	if err != nil || episodePath == nil || *episodePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(*episodePath), "poster.jpg")
}

// resolveEpisodeThumbMediaPath 返回 Episode 对应媒体目录内的 thumb 路径,形如
// `<视频同目录>/<视频 basename>-thumb.jpg`。这是 Emby/Jellyfin 的 thumb 命名约定,
// 也是 scanner 端 FindEpisodeThumbCached 首要识别的 pattern。
// file_path 为 http URL 或空时返回空串,调用方回退到 data/metadata。
func resolveEpisodeThumbMediaPath(ctx context.Context, pool *pgxpool.Pool, episodeID string) string {
	filePath, err := repository.NewScanIngestRepository(pool).GetEpisodeFilePath(ctx, episodeID)
	if err != nil || filePath == nil || *filePath == "" {
		return ""
	}
	p := *filePath
	if strings.HasPrefix(strings.ToLower(p), "http") {
		return ""
	}
	stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	if stem == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), stem+"-thumb.jpg")
}

func writeNfoFile(path string, itemType string, nfo *NfoData) bool {
	if path == "" || nfo == nil {
		return false
	}
	root := "movie"
	if itemType != "Movie" {
		root = "tvshow"
	}

	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<" + root + ">\n")
	writeNfoTag := func(name string, value *string) {
		if value == nil || *value == "" {
			return
		}
		b.WriteString("  <" + name + ">")
		xml.EscapeText(&b, []byte(*value))
		b.WriteString("</" + name + ">\n")
	}
	writeNfoTag("title", nfo.Title)
	writeNfoTag("originaltitle", nfo.OriginalTitle)
	writeNfoTag("plot", nfo.Plot)
	writeNfoTag("premiered", nfo.Premiered)
	writeNfoTag("imdbid", nfo.ImdbID)
	writeNfoTag("tagline", nfo.Tagline)

	if nfo.Year != nil {
		fmt.Fprintf(&b, "  <year>%d</year>\n", *nfo.Year)
	}
	if nfo.Rating != nil {
		fmt.Fprintf(&b, "  <rating>%.1f</rating>\n", *nfo.Rating)
	}
	if nfo.TmdbID != nil {
		fmt.Fprintf(&b, "  <tmdbid>%d</tmdbid>\n", *nfo.TmdbID)
	}
	if nfo.TvdbID != nil {
		fmt.Fprintf(&b, "  <tvdbid>%d</tvdbid>\n", *nfo.TvdbID)
	}
	for _, genre := range nfo.Genres {
		g := strings.TrimSpace(genre)
		if g == "" {
			continue
		}
		b.WriteString("  <genre>")
		xml.EscapeText(&b, []byte(g))
		b.WriteString("</genre>\n")
	}
	for _, director := range nfo.Directors {
		d := strings.TrimSpace(director)
		if d == "" {
			continue
		}
		b.WriteString("  <director>")
		xml.EscapeText(&b, []byte(d))
		b.WriteString("</director>\n")
	}
	for _, actor := range nfo.Actors {
		if strings.TrimSpace(actor.Name) == "" {
			continue
		}
		b.WriteString("  <actor>\n")
		b.WriteString("    <name>")
		xml.EscapeText(&b, []byte(actor.Name))
		b.WriteString("</name>\n")
		if strings.TrimSpace(actor.Role) != "" {
			b.WriteString("    <role>")
			xml.EscapeText(&b, []byte(actor.Role))
			b.WriteString("</role>\n")
		}
		if actor.TmdbID != nil {
			fmt.Fprintf(&b, "    <tmdbid>%d</tmdbid>\n", *actor.TmdbID)
		}
		b.WriteString("    <type>Actor</type>\n")
		b.WriteString("  </actor>\n")
	}
	b.WriteString("</" + root + ">\n")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false
	}
	return os.WriteFile(path, []byte(b.String()), 0644) == nil
}
