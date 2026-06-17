package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BackgroundTaskRepository struct {
	pool *pgxpool.Pool
}

type ScrapeItemMeta struct {
	ItemType    string
	Name        string
	Year        *int32
	TmdbID      *int32
	ImdbID      *string
	FilePath    *string
	LibraryID   string
	ExternalIDs map[string]string
}

type ExternalIDRecord struct {
	Provider string
	Value    string
}

type IdentifyCandidateRow struct {
	ID         string
	ItemID     string
	Provider   string
	ExternalID string
	Title      string
	Year       *int32
	PosterURL  string
	Score      float64
	Payload    map[string]interface{}
	CreatedAt  time.Time
}

type UnmatchedItemRow struct {
	ID             string
	Name           string
	Type           string
	ProductionYear *int32
	FilePath       *string
	TmdbID         *int32
	ScanStatus     string
	ScanError      *string
	ScannedAt      *time.Time
	NextRetryAt    *time.Time
}

type CastImageBackfillMeta struct {
	ItemType string
	TmdbID   *int64
}

type CastImageBackfillTarget struct {
	ID       string
	Name     string
	TmdbID   *int32
	PersonID *string
}

type DirtyEpisodeNameRow struct {
	ID        string
	EpNum     int32
	SeasonNum *int32
	SeriesID  *string
}

type EpisodeImageBackfillCandidate struct {
	ID       string
	EpNum    int32
	FilePath string
	SeasonID string
}

type EpisodeStillTarget struct {
	ID    string
	EpNum int32
}

type QualityMediaVersionRow struct {
	ID        string
	Name      string
	MediaInfo map[string]interface{}
}

type ProbeTarget struct {
	MediaVersionID uuid.UUID
	ItemID         uuid.UUID
	FilePath       string
	Name           string
}

func NewBackgroundTaskRepository(pool *pgxpool.Pool) *BackgroundTaskRepository {
	return &BackgroundTaskRepository{pool: pool}
}

func (r *BackgroundTaskRepository) GetMissingScrapeCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM items
		WHERE (overview IS NULL OR overview = '')
		  AND type IN ('Movie', 'Series')`).Scan(&count)
	return count, err
}

func (r *BackgroundTaskRepository) GetTopLevelItemCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type IN ('Movie', 'Series')").Scan(&count)
	return count, err
}

func (r *BackgroundTaskRepository) LoadScrapeItemMeta(ctx context.Context, itemID string) (*ScrapeItemMeta, error) {
	meta := &ScrapeItemMeta{ExternalIDs: map[string]string{}}
	var providerIDsRaw []byte
	err := r.pool.QueryRow(ctx,
		"SELECT type, name, production_year, tmdb_id, imdb_id, file_path, library_id::text, provider_ids FROM items WHERE id = $1::uuid",
		itemID,
	).Scan(&meta.ItemType, &meta.Name, &meta.Year, &meta.TmdbID, &meta.ImdbID, &meta.FilePath, &meta.LibraryID, &providerIDsRaw)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	mergeProviderIDBytes(meta.ExternalIDs, providerIDsRaw)

	rows, err := r.pool.Query(ctx,
		"SELECT provider, external_id FROM item_external_ids WHERE item_id = $1::uuid",
		itemID)
	if err != nil {
		return nil, fmt.Errorf("query item external ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var provider, externalID string
		if err := rows.Scan(&provider, &externalID); err != nil {
			return nil, fmt.Errorf("scan item external ids: %w", err)
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		externalID = strings.TrimSpace(externalID)
		if provider != "" && externalID != "" {
			meta.ExternalIDs[provider] = externalID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item external ids: %w", err)
	}
	return meta, nil
}

func (r *BackgroundTaskRepository) MarkIdentifyAttempted(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx, "UPDATE items SET identify_attempted_at = NOW() WHERE id = $1::uuid", itemID)
	return err
}

func (r *BackgroundTaskRepository) UpsertExternalIDs(ctx context.Context, itemID string, ids []ExternalIDRecord) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	providerMap := make(map[string]string, len(ids))
	for _, rec := range ids {
		provider := strings.ToLower(strings.TrimSpace(rec.Provider))
		value := strings.TrimSpace(rec.Value)
		if provider == "" || value == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providerMap[provider] = value
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO item_external_ids (item_id, provider, external_id, updated_at)
			 VALUES ($1::uuid, $2, $3, NOW())
			 ON CONFLICT (item_id, provider)
			 DO UPDATE SET external_id = EXCLUDED.external_id,
			               updated_at = EXCLUDED.updated_at`,
			itemID, provider, value); err != nil {
			return err
		}
	}
	if len(providerMap) == 0 {
		return nil
	}
	raw, err := json.Marshal(providerMap)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, "UPDATE items SET provider_ids = $1::jsonb, updated_at = NOW() WHERE id = $2::uuid", string(raw), itemID)
	return err
}

func (r *BackgroundTaskRepository) ReplaceIdentifyCandidates(ctx context.Context, itemID string, candidates []IdentifyCandidateUpsert) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID); err != nil {
		return err
	}
	for _, cand := range candidates {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO identify_candidates (item_id, provider, external_id, title, year, poster_url, score, payload)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			itemID,
			cand.Provider,
			cand.ExternalID,
			cand.Title,
			cand.Year,
			cand.PosterURL,
			float32(cand.Score),
			cand.Payload,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *BackgroundTaskRepository) DeleteIdentifyCandidates(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID)
	return err
}

func (r *BackgroundTaskRepository) ResetMediaVersionQuality(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE media_versions
		 SET resolution = NULL, hdr_format = NULL, video_codec = NULL,
		     audio_codec = NULL, source = NULL, quality_label = NULL`)
	return err
}

func (r *BackgroundTaskRepository) ResetEpisodeStillImages(ctx context.Context) (int64, error) {
	res, err := r.pool.Exec(ctx,
		`UPDATE items
		 SET primary_image_path = NULL, primary_image_tag = NULL
		 WHERE type = 'Episode'
		   AND primary_image_path LIKE 'data/metadata/%/still.jpg'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

type IdentifyCandidateUpsert struct {
	Provider   string
	ExternalID string
	Title      string
	Year       any
	PosterURL  string
	Score      float64
	Payload    string
}

func (r *BackgroundTaskRepository) ListIdentifyCandidates(ctx context.Context, itemID string) ([]IdentifyCandidateRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, item_id::text, provider, external_id, COALESCE(title, ''), year, COALESCE(poster_url, ''), COALESCE(score, 0), payload, created_at
		   FROM identify_candidates
		  WHERE item_id = $1::uuid
		  ORDER BY score DESC, created_at DESC`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIdentifyCandidateRows(rows)
}

func (r *BackgroundTaskRepository) ListIdentifyCandidatesBatch(ctx context.Context, itemIDs []string, topN int) (map[string][]IdentifyCandidateRow, error) {
	if len(itemIDs) == 0 || topN <= 0 {
		return map[string][]IdentifyCandidateRow{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, item_id::text, provider, external_id, COALESCE(title, ''),
		       year, COALESCE(poster_url, ''), COALESCE(score, 0), payload, created_at
		  FROM (
		      SELECT *,
		             ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY score DESC, created_at DESC) AS rn
		        FROM identify_candidates
		       WHERE item_id = ANY($1::uuid[])
		  ) t
		 WHERE rn <= $2`, itemIDs, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rowsOut, err := scanIdentifyCandidateRows(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]IdentifyCandidateRow, len(itemIDs))
	for _, row := range rowsOut {
		out[row.ItemID] = append(out[row.ItemID], row)
	}
	return out, nil
}

func (r *BackgroundTaskRepository) ListUnmatchedItems(ctx context.Context, itemTypeFilter string, limit int) ([]UnmatchedItemRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var args []any
	where := `WHERE (i.platform_scan_status = 'unidentified' OR (sq.next_run_at IS NOT NULL AND sq.next_run_at > NOW()))`
	if strings.TrimSpace(itemTypeFilter) != "" {
		args = append(args, itemTypeFilter)
		where += fmt.Sprintf(" AND i.type = $%d", len(args))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT i.id::text, i.name, i.type, i.production_year, i.file_path, i.tmdb_id,
		       COALESCE(i.platform_scan_status, ''), i.platform_scan_error,
		       i.platform_scanned_at, sq.next_run_at
		  FROM items i
		  LEFT JOIN scrape_queue sq
		    ON sq.item_id = i.id
		   AND sq.task_type = 'identify'
		   AND sq.status IN ('pending', 'running', 'failed')
		  %s
		  ORDER BY COALESCE(sq.next_run_at, i.platform_scanned_at) DESC NULLS LAST
		  LIMIT $%d`, where, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UnmatchedItemRow, 0)
	for rows.Next() {
		var it UnmatchedItemRow
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.ProductionYear, &it.FilePath, &it.TmdbID, &it.ScanStatus, &it.ScanError, &it.ScannedAt, &it.NextRetryAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) GetItemMediaType(ctx context.Context, itemID string) (string, error) {
	var itemType string
	err := r.pool.QueryRow(ctx, "SELECT type FROM items WHERE id = $1::uuid", itemID).Scan(&itemType)
	return itemType, err
}

func (r *BackgroundTaskRepository) GetCastImageBackfillMeta(ctx context.Context, itemID string) (CastImageBackfillMeta, error) {
	var out CastImageBackfillMeta
	err := r.pool.QueryRow(ctx, "SELECT type, tmdb_id FROM items WHERE id = $1::uuid", itemID).Scan(&out.ItemType, &out.TmdbID)
	return out, err
}

func (r *BackgroundTaskRepository) CountMissingCastImages(ctx context.Context, itemID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cast_members
		  WHERE item_id = $1::uuid
		    AND (image_url IS NULL OR image_url = '')`,
		itemID).Scan(&count)
	return count, err
}

func (r *BackgroundTaskRepository) ListMissingCastImageTargets(ctx context.Context, itemID string) ([]CastImageBackfillTarget, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, tmdb_id, person_id::text
		   FROM cast_members
		  WHERE item_id = $1::uuid
		    AND (image_url IS NULL OR image_url = '')`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CastImageBackfillTarget
	for rows.Next() {
		var row CastImageBackfillTarget
		if err := rows.Scan(&row.ID, &row.Name, &row.TmdbID, &row.PersonID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) FillCastImageIfEmpty(ctx context.Context, castID, imageURL string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE cast_members SET image_url = $1
		  WHERE id = $2::uuid AND (image_url IS NULL OR image_url = '')`,
		imageURL, castID)
	return err
}

func (r *BackgroundTaskRepository) CountDirtyEpisodeNames(ctx context.Context) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM items e WHERE `+dirtyEpisodeNameWhereSQL).Scan(&total)
	return total, err
}

func (r *BackgroundTaskRepository) ListDirtyEpisodeNameBatch(ctx context.Context, lastID string, limit int) ([]DirtyEpisodeNameRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.id::text, e.index_number, se.index_number, e.series_id::text
		 FROM items e
		 LEFT JOIN items se ON se.id = e.season_id
		 WHERE `+dirtyEpisodeNameWhereSQL+`
		   AND e.id::text > $1
		 ORDER BY e.id
		 LIMIT $2`,
		lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DirtyEpisodeNameRow
	for rows.Next() {
		var row DirtyEpisodeNameRow
		if err := rows.Scan(&row.ID, &row.EpNum, &row.SeasonNum, &row.SeriesID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) RenameEpisode(ctx context.Context, itemID, name string) error {
	_, err := r.pool.Exec(ctx, "UPDATE items SET name = $1, updated_at = NOW() WHERE id = $2::uuid", name, itemID)
	return err
}

func (r *BackgroundTaskRepository) ListEpisodeImageBackfillCandidates(ctx context.Context) ([]EpisodeImageBackfillCandidate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.id::text,
		        e.index_number,
		        COALESCE(e.file_path, ''),
		        e.season_id::text
		 FROM items e
		 JOIN items se ON se.id = e.season_id
		 JOIN items sr ON sr.id = se.parent_id AND sr.type = 'Series'
		 WHERE e.type = 'Episode'
		   AND e.primary_image_path IS NULL
		   AND e.index_number IS NOT NULL
		   AND sr.tmdb_id IS NOT NULL AND sr.tmdb_id > 0
		 ORDER BY e.season_id, e.index_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EpisodeImageBackfillCandidate
	for rows.Next() {
		var row EpisodeImageBackfillCandidate
		if err := rows.Scan(&row.ID, &row.EpNum, &row.FilePath, &row.SeasonID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) SetEpisodeStillIfEmpty(ctx context.Context, episodeID, imagePath string, imageTag *string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW()
		  WHERE id = $3::uuid AND primary_image_path IS NULL`,
		imagePath, imageTag, episodeID)
	return err
}

func (r *BackgroundTaskRepository) GetSeasonSeriesTMDBID(ctx context.Context, seasonID string) (*int64, error) {
	var seriesTmdbID *int64
	err := r.pool.QueryRow(ctx,
		`SELECT sr.tmdb_id
		   FROM items se
		   JOIN items sr ON sr.id = se.parent_id AND sr.type = 'Series'
		  WHERE se.id = $1::uuid AND se.type = 'Season'`,
		seasonID).Scan(&seriesTmdbID)
	return seriesTmdbID, err
}

func (r *BackgroundTaskRepository) ListEpisodesMissingStill(ctx context.Context, seasonID string) ([]EpisodeStillTarget, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, index_number
		   FROM items
		  WHERE season_id = $1::uuid AND type = 'Episode'
		    AND primary_image_path IS NULL
		    AND index_number IS NOT NULL`,
		seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EpisodeStillTarget
	for rows.Next() {
		var row EpisodeStillTarget
		if err := rows.Scan(&row.ID, &row.EpNum); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) ListQualityBackfillItemIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT mv.item_id::text
		   FROM media_versions mv
		  WHERE mv.resolution IS NULL
		  ORDER BY mv.item_id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringRows(rows)
}

func (r *BackgroundTaskRepository) ListQualityMediaVersions(ctx context.Context, itemID string) ([]QualityMediaVersionRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, mediainfo
		   FROM media_versions
		  WHERE item_id = $1::uuid AND resolution IS NULL`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QualityMediaVersionRow
	for rows.Next() {
		var row QualityMediaVersionRow
		var miRaw *string
		if err := rows.Scan(&row.ID, &row.Name, &miRaw); err != nil {
			return nil, err
		}
		if miRaw != nil && *miRaw != "" {
			_ = json.Unmarshal([]byte(*miRaw), &row.MediaInfo)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) MarkMediaVersionQualityUnknown(ctx context.Context, mediaVersionID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE media_versions SET resolution = 'unknown' WHERE id = $1::uuid AND resolution IS NULL`, mediaVersionID)
	return err
}

func (r *BackgroundTaskRepository) UpdateMediaVersionQuality(ctx context.Context, mediaVersionID, resolution string, hdrFormat, videoCodec, audioCodec, source, qualityLabel *string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE media_versions
		    SET resolution = $1, hdr_format = $2, video_codec = $3, audio_codec = $4,
		        source = $5, quality_label = $6
		  WHERE id = $7::uuid AND resolution IS NULL`,
		resolution, hdrFormat, videoCodec, audioCodec, source, qualityLabel, mediaVersionID)
	return err
}

func (r *BackgroundTaskRepository) ListProbeTargets(ctx context.Context) ([]ProbeTarget, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT mv.id, mv.item_id, mv.file_path, mv.name
		 FROM media_versions mv
		 WHERE mv.mediainfo IS NULL
		    OR mv.runtime_ticks IS NULL
		    OR mv.size IS NULL
		    OR mv.bitrate IS NULL
		    OR NOT (mv.mediainfo ? 'RunTimeTicks')
		    OR NOT (mv.mediainfo ? 'Size')
		    OR NOT (mv.mediainfo ? 'Bitrate')
		    OR NOT (mv.mediainfo ? 'MediaStreams')
		    OR mv.chapters IS NULL
		 ORDER BY mv.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeTarget
	for rows.Next() {
		var row ProbeTarget
		if err := rows.Scan(&row.MediaVersionID, &row.ItemID, &row.FilePath, &row.Name); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *BackgroundTaskRepository) GetProbeOnPlayTarget(ctx context.Context, itemID, mediaSourceID string) (*ProbeTarget, error) {
	var row ProbeTarget
	err := r.pool.QueryRow(ctx,
		`SELECT id, item_id, file_path, COALESCE(name, '')
		 FROM media_versions
		 WHERE item_id = $1::uuid
		   AND (mediainfo IS NULL OR NOT (mediainfo ? 'MediaStreams') OR chapters IS NULL)
		 ORDER BY (id::text = $2) DESC, is_primary DESC, created_at ASC
		 LIMIT 1`,
		itemID, mediaSourceID).Scan(&row.MediaVersionID, &row.ItemID, &row.FilePath, &row.Name)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *BackgroundTaskRepository) UpdateProbeMediaVersion(ctx context.Context, mediaVersionID string, mediaInfoJSON string, runtimeTicks int64, bitrate, size *int64, chaptersJSON string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE media_versions
		 SET mediainfo = $1,
		     runtime_ticks = CASE WHEN $2 > 0 THEN $2 ELSE runtime_ticks END,
		     bitrate = COALESCE($3, bitrate),
		     size = COALESCE($4, size),
		     chapters = $6
		 WHERE id = $5::uuid`,
		mediaInfoJSON, runtimeTicks, bitrate, size, mediaVersionID, chaptersJSON)
	return err
}

func (r *BackgroundTaskRepository) FillItemRuntimeTicksIfEmpty(ctx context.Context, itemID string, runtimeTicks int64) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE items SET runtime_ticks = $1, updated_at = NOW() WHERE id = $2 AND (runtime_ticks IS NULL OR runtime_ticks = 0)",
		runtimeTicks, itemID)
	return err
}

func (r *BackgroundTaskRepository) GetMissingMediainfoCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM media_versions
		 WHERE mediainfo IS NULL
		    OR runtime_ticks IS NULL
		    OR size IS NULL
		    OR bitrate IS NULL
		    OR NOT (mediainfo ? 'RunTimeTicks')
		    OR NOT (mediainfo ? 'Size')
		    OR NOT (mediainfo ? 'Bitrate')
		    OR NOT (mediainfo ? 'MediaStreams')
		    OR chapters IS NULL`).Scan(&count)
	return count, err
}

func (r *BackgroundTaskRepository) GetTotalMediaVersionsCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, "SELECT count(*) FROM media_versions").Scan(&count)
	return count, err
}

func scanIdentifyCandidateRows(rows pgx.Rows) ([]IdentifyCandidateRow, error) {
	var out []IdentifyCandidateRow
	for rows.Next() {
		var rec IdentifyCandidateRow
		var payload []byte
		if err := rows.Scan(&rec.ID, &rec.ItemID, &rec.Provider, &rec.ExternalID, &rec.Title, &rec.Year, &rec.PosterURL, &rec.Score, &payload, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &rec.Payload)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanStringRows(rows pgx.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func mergeProviderIDBytes(dst map[string]string, raw []byte) {
	if len(raw) == 0 || dst == nil {
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	for key, value := range payload {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				dst[key] = s
			}
		case float64:
			if v > 0 {
				dst[key] = fmt.Sprintf("%.0f", v)
			}
		}
	}
}

const dirtyEpisodeNameWhereSQL = `
  e.type = 'Episode'
  AND e.index_number IS NOT NULL
  AND e.name IS NOT NULL
  AND NOT (
      e.name ~ '^Episode [0-9]+$'
      OR e.name ~ '^Special [0-9]+$'
      OR e.name ~ '^第[0-9]+[集话回]$'
      OR e.name ~ '^S[0-9]{1,2}E[0-9]{1,3}$'
  )
  AND (
      e.name ~* '\.(mkv|mp4|avi|ts|m2ts|rmvb|flv|mov|wmv|webm|m4v|iso)$'
      OR e.name ~* '(1080p|2160p|720p|480p|576p|1440p|4k|webrip|web-?dl|bluray|bdrip|hdtv|dvdrip|remux|x26[45]|hevc|atmos|truehd|dts-?hd|h\.?26[45])'
      OR e.name ~ '\[.+\]\[.+\]'
      OR e.name ~ '[._]{2,}'
  )
`
