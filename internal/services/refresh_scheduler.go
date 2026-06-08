package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const refreshSchedulerDebounce = 2 * time.Second

type RefreshScheduler struct {
	pool  *pgxpool.Pool
	queue *RefreshQueue

	mu      sync.Mutex
	pending map[string]*scheduledRefresh
}

type scheduledRefresh struct {
	dir           string
	source        RefreshSource
	metadataPaths map[string]struct{}
	imagePaths    map[string]struct{}
	timer         *time.Timer
}

func NewRefreshScheduler(pool *pgxpool.Pool, queue *RefreshQueue) *RefreshScheduler {
	return &RefreshScheduler{
		pool:    pool,
		queue:   queue,
		pending: make(map[string]*scheduledRefresh),
	}
}

func (s *RefreshScheduler) OnSidecarChange(path string) {
	kind := classifySidecarPath(path)
	if kind == "" {
		return
	}

	dir := filepath.Dir(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pending[dir]
	if !ok {
		p = &scheduledRefresh{
			dir:           dir,
			source:        RefreshSourceSidecar,
			metadataPaths: make(map[string]struct{}),
			imagePaths:    make(map[string]struct{}),
		}
		s.pending[dir] = p
	}

	switch kind {
	case "metadata":
		p.metadataPaths[path] = struct{}{}
	case "images":
		p.imagePaths[path] = struct{}{}
	}

	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(refreshSchedulerDebounce, func() {
		s.flush(dir)
	})
}

func (s *RefreshScheduler) flush(dir string) {
	s.mu.Lock()
	p, ok := s.pending[dir]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.pending, dir)
	s.mu.Unlock()

	ctx := context.Background()
	opts := DefaultRefreshOptionsForSource(p.source)

	metadataTargets := make(map[string]struct{})
	for path := range p.metadataPaths {
		for _, id := range s.resolveMetadataTargets(ctx, path) {
			metadataTargets[id] = struct{}{}
		}
	}
	imageTargets := make(map[string]struct{})
	for path := range p.imagePaths {
		for _, id := range s.resolveImageTargets(ctx, path) {
			imageTargets[id] = struct{}{}
		}
	}

	for itemID := range metadataTargets {
		if err := s.queue.Enqueue(ctx, itemID, RefreshScopeMetadata, p.source, RefreshPriorityFS, opts); err != nil {
			slog.Warn("[RefreshScheduler] enqueue metadata refresh failed", "item", itemID, "dir", dir, "error", err)
		}
	}
	for itemID := range imageTargets {
		if err := s.queue.Enqueue(ctx, itemID, RefreshScopeImages, p.source, RefreshPriorityFS, opts); err != nil {
			slog.Warn("[RefreshScheduler] enqueue image refresh failed", "item", itemID, "dir", dir, "error", err)
		}
	}

	if len(metadataTargets) > 0 || len(imageTargets) > 0 {
		slog.Info("[RefreshScheduler] enqueued refresh tasks",
			"dir", dir,
			"metadata_items", len(metadataTargets),
			"image_items", len(imageTargets))
	}
}

func (s *RefreshScheduler) resolveMetadataTargets(ctx context.Context, path string) []string {
	dir := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(path))
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	if base == "tvshow.nfo" {
		return s.loadSeriesByDir(ctx, dir)
	}
	if base == "movie.nfo" {
		return extractMovieIDs(s.loadMoviesUnderDir(ctx, dir))
	}

	if ids := extractEpisodeIDsByStem(s.loadEpisodesUnderDir(ctx, dir), stem); len(ids) > 0 {
		return ids
	}
	if ids := extractMovieIDsByStem(s.loadMoviesUnderDir(ctx, dir), stem); len(ids) > 0 {
		return ids
	}
	return nil
}

func (s *RefreshScheduler) resolveImageTargets(ctx context.Context, path string) []string {
	dir := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(path))
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	if ids := extractEpisodeIDsByThumbStem(s.loadEpisodesUnderDir(ctx, dir), stem); len(ids) > 0 {
		return ids
	}
	if ids := extractMovieIDsByImageStem(s.loadMoviesUnderDir(ctx, dir), stem); len(ids) > 0 {
		return ids
	}
	if ids := s.loadSeriesByDir(ctx, dir); len(ids) > 0 {
		return ids
	}
	if ids := extractMovieIDs(s.loadMoviesUnderDir(ctx, dir)); len(ids) > 0 {
		return ids
	}
	return extractSeasonIDs(s.loadEpisodesUnderDir(ctx, dir))
}

func (s *RefreshScheduler) loadSeriesByDir(ctx context.Context, dir string) []string {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text
		   FROM items
		  WHERE type = 'Series'
		    AND file_path = $1`,
		dir)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

type moviePathItem struct {
	id       string
	filePath string
}

func (s *RefreshScheduler) loadMoviesUnderDir(ctx context.Context, dir string) []moviePathItem {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, file_path
		   FROM items
		  WHERE type = 'Movie'
		    AND (file_path = $1 OR file_path LIKE $2)`,
		dir, dir+string(filepath.Separator)+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var items []moviePathItem
	for rows.Next() {
		var it moviePathItem
		if rows.Scan(&it.id, &it.filePath) == nil {
			items = append(items, it)
		}
	}
	return items
}

type episodePathItem struct {
	id       string
	seasonID string
	filePath string
}

func (s *RefreshScheduler) loadEpisodesUnderDir(ctx context.Context, dir string) []episodePathItem {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, season_id::text, file_path
		   FROM items
		  WHERE type = 'Episode'
		    AND file_path LIKE $1`,
		dir+string(filepath.Separator)+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var items []episodePathItem
	for rows.Next() {
		var it episodePathItem
		if rows.Scan(&it.id, &it.seasonID, &it.filePath) == nil {
			items = append(items, it)
		}
	}
	return items
}

func classifySidecarPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(base))
	if ext == ".nfo" {
		return "metadata"
	}
	if !isImageExt(ext) {
		return ""
	}

	stem := strings.TrimSuffix(base, ext)
	if strings.HasSuffix(stem, "-thumb") || strings.HasSuffix(stem, ".thumb") {
		return "images"
	}
	for _, prefix := range posterImagePrefixes {
		if stem == prefix {
			return "images"
		}
	}
	for _, prefix := range backdropImagePrefixes {
		if stem == prefix {
			return "images"
		}
	}
	if movieImageSidecarBaseStem(stem) != "" {
		return "images"
	}
	return ""
}

func isImageExt(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func extractMovieIDs(items []moviePathItem) []string {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.id)
	}
	return ids
}

func extractMovieIDsByStem(items []moviePathItem, stem string) []string {
	if stem == "" {
		return nil
	}
	var ids []string
	for _, it := range items {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(it.filePath), filepath.Ext(it.filePath)))
		if base == stem {
			ids = append(ids, it.id)
		}
	}
	return ids
}

func extractMovieIDsByImageStem(items []moviePathItem, stem string) []string {
	baseStem := movieImageSidecarBaseStem(stem)
	if baseStem == "" {
		baseStem = stem
	}
	var ids []string
	for _, it := range items {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(it.filePath), filepath.Ext(it.filePath)))
		if base == baseStem {
			ids = append(ids, it.id)
		}
	}
	return ids
}

func movieImageSidecarBaseStem(stem string) string {
	stem = strings.ToLower(strings.TrimSpace(stem))
	if stem == "" {
		return ""
	}
	for _, prefixes := range [][]string{posterImagePrefixes, backdropImagePrefixes} {
		for _, prefix := range prefixes {
			for _, sep := range []string{"-", ".", "_"} {
				suffix := sep + prefix
				if strings.HasSuffix(stem, suffix) && len(stem) > len(suffix) {
					return strings.TrimSuffix(stem, suffix)
				}
			}
		}
	}
	return ""
}

func extractEpisodeIDsByStem(items []episodePathItem, stem string) []string {
	if stem == "" {
		return nil
	}
	var ids []string
	for _, it := range items {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(it.filePath), filepath.Ext(it.filePath)))
		if base == stem {
			ids = append(ids, it.id)
		}
	}
	return ids
}

func extractEpisodeIDsByThumbStem(items []episodePathItem, stem string) []string {
	if stem == "" {
		return nil
	}
	var ids []string
	for _, it := range items {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(it.filePath), filepath.Ext(it.filePath)))
		if stem == base || stem == base+"-thumb" || stem == base+".thumb" {
			ids = append(ids, it.id)
		}
	}
	return ids
}

func extractSeasonIDs(items []episodePathItem) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, it := range items {
		if it.seasonID == "" {
			continue
		}
		if _, ok := seen[it.seasonID]; ok {
			continue
		}
		seen[it.seasonID] = struct{}{}
		ids = append(ids, it.seasonID)
	}
	return ids
}
