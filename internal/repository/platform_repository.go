package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PlatformDimStudio    = "studio"
	PlatformDimNumPrefix = "num_prefix"
	PlatformDimActor     = "actor"
	PlatformDimLatest    = "latest"

	DefaultLatestItemLimit int64 = 200
)

const platformCatalogPrefixExpr = "regexp_replace(upper(catalog_number), '-[0-9]+$', '')"

type PlatformLibrary struct {
	ID             string
	PlatformName   string
	Enabled        bool
	CollectionType string
	IconURL        *string
	CreatedAt      time.Time
	ItemCount      int64
	SortOrder      int
	Dimension      string
	MatchValue     string
	MatchValues    []string
	CoverImagePath *string
	CoverImageTag  *string
	DisplayName    *string
	ItemLimit      *int64
}

func (p *PlatformLibrary) Values() []string {
	if len(p.MatchValues) > 0 {
		return p.MatchValues
	}
	return []string{p.MatchValue}
}

type PlatformDiscoveredValue struct {
	Value        string
	Count        int64
	AlreadyAdded bool
}

type PlatformScanItem struct {
	ID                 string
	ItemType           string
	Name               string
	ProductionYear     *int32
	TmdbID             *int32
	FilePath           *string
	Studio             *string
	PlatformScanStatus string
	PlatformScanSource *string
}

type platformDimensionSQL struct {
	MatchCondition string
	GroupExpr      string
	DiscoverFrom   string
	CountExpr      string
}

type PlatformFilenamePattern struct {
	Platform string
	SQL      string
}

type PlatformRepository struct {
	pool *pgxpool.Pool
}

func NewPlatformRepository(pool *pgxpool.Pool) *PlatformRepository {
	return &PlatformRepository{pool: pool}
}

func (r *PlatformRepository) ListLibraries(ctx context.Context, onlyEnabled bool, withCounts bool) ([]PlatformLibrary, error) {
	result, err := r.listLibrariesLite(ctx, onlyEnabled)
	if err != nil {
		return nil, err
	}
	if withCounts {
		for i := range result {
			result[i].ItemCount, _ = r.CountItemsForVirtualScoped(
				ctx, result[i].Dimension, result[i].Values(), result[i].ItemLimit, nil)
		}
	}
	return result, nil
}

func (r *PlatformRepository) listLibrariesLite(ctx context.Context, onlyEnabled bool) ([]PlatformLibrary, error) {
	sql := `SELECT id::text, platform_name, enabled, collection_type, icon_url, created_at, sort_order,
	               dimension, COALESCE(match_value, platform_name), match_values, cover_image_path, cover_image_tag, display_name,
	               item_limit
	          FROM platform_libraries`
	if onlyEnabled {
		sql += ` WHERE enabled = true`
	}
	sql += ` ORDER BY sort_order, platform_name`
	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PlatformLibrary
	for rows.Next() {
		var p PlatformLibrary
		if err := rows.Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL,
			&p.CreatedAt, &p.SortOrder, &p.Dimension, &p.MatchValue, &p.MatchValues, &p.CoverImagePath, &p.CoverImageTag, &p.DisplayName,
			&p.ItemLimit); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func PlatformDimensionCondition(dimension string) (string, bool) {
	spec, ok := platformDimensionSpec(dimension)
	if !ok {
		return "", false
	}
	return spec.MatchCondition, true
}

func platformDimensionSpec(dimension string) (platformDimensionSQL, bool) {
	switch dimension {
	case PlatformDimStudio:
		return platformDimensionSQL{
			MatchCondition: "studio = ANY($1)",
			GroupExpr:      "studio",
			DiscoverFrom:   `FROM items WHERE studio IS NOT NULL AND studio != '' AND type IN ('Movie','Series') AND merged_to_id IS NULL`,
			CountExpr:      "COUNT(*)",
		}, true
	case PlatformDimNumPrefix:
		return platformDimensionSQL{
			MatchCondition: platformCatalogPrefixExpr + " = ANY($1)",
			GroupExpr:      platformCatalogPrefixExpr,
			DiscoverFrom:   `FROM items WHERE catalog_number IS NOT NULL AND type IN ('Movie','Series') AND merged_to_id IS NULL`,
			CountExpr:      "COUNT(*)",
		}, true
	case PlatformDimActor:
		return platformDimensionSQL{
			MatchCondition: "EXISTS (SELECT 1 FROM cast_members cm WHERE cm.item_id = items.id AND cm.name = ANY($1) AND cm.role = 'Actor')",
			GroupExpr:      "cm.name",
			DiscoverFrom:   `FROM cast_members cm JOIN items i ON i.id = cm.item_id WHERE cm.role = 'Actor' AND cm.name != '' AND i.type IN ('Movie','Series') AND i.merged_to_id IS NULL`,
			CountExpr:      "COUNT(DISTINCT cm.item_id)",
		}, true
	default:
		return platformDimensionSQL{}, false
	}
}

func (r *PlatformRepository) CountItemsForVirtual(ctx context.Context, dimension string, values []string) (int64, error) {
	return r.CountItemsForVirtualScoped(ctx, dimension, values, nil, nil)
}

func (r *PlatformRepository) CountItemsForVirtualScoped(ctx context.Context, dimension string, values []string, itemLimit *int64, allowedLibraryIDs []string) (int64, error) {
	if dimension == PlatformDimLatest {
		limit := DefaultLatestItemLimit
		if itemLimit != nil && *itemLimit > 0 {
			limit = *itemLimit
		}
		args := []any{}
		var allowedParam *int
		if allowedLibraryIDs != nil {
			if len(allowedLibraryIDs) == 0 {
				return 0, nil
			}
			param := 1
			allowedParam = &param
			args = append(args, allowedLibraryIDs)
		}
		args = append(args, limit)
		limitParam := len(args)
		var count int64
		err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (`+
			LatestVirtualMembersSQL(limitParam, allowedParam)+`) latest_items`, args...).Scan(&count)
		return count, err
	}
	cond, ok := PlatformDimensionCondition(dimension)
	if !ok || len(values) == 0 {
		return 0, nil
	}
	where := cond + " AND type IN ('Movie','Series') AND merged_to_id IS NULL"
	args := []any{values}
	if allowedLibraryIDs != nil {
		if len(allowedLibraryIDs) == 0 {
			return 0, nil
		}
		where += " AND library_id::text = ANY($2)"
		args = append(args, allowedLibraryIDs)
	}
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE `+where, args...).Scan(&count)
	return count, err
}

func (r *PlatformRepository) DiscoverDimensionValues(ctx context.Context, dimension, search string, minCount int64) ([]PlatformDiscoveredValue, error) {
	spec, ok := platformDimensionSpec(dimension)
	if !ok {
		return nil, fmt.Errorf("unknown dimension: %s", dimension)
	}
	args := []any{}
	idx := 1
	having := ""
	searchClause := ""
	search = strings.TrimSpace(search)
	if search != "" {
		searchClause = fmt.Sprintf(" AND %s ILIKE $%d", spec.GroupExpr, idx)
		args = append(args, "%"+search+"%")
		idx++
	}
	if minCount > 1 {
		having = fmt.Sprintf(" HAVING %s >= $%d", spec.CountExpr, idx)
		args = append(args, minCount)
	}
	sql := fmt.Sprintf(
		`SELECT %s AS v, %s AS cnt %s%s GROUP BY %s%s ORDER BY cnt DESC, v ASC LIMIT 2000`,
		spec.GroupExpr, spec.CountExpr, spec.DiscoverFrom, searchClause, spec.GroupExpr, having)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PlatformDiscoveredValue
	for rows.Next() {
		var v *string
		var cnt int64
		if err := rows.Scan(&v, &cnt); err != nil {
			return nil, err
		}
		if v == nil || strings.TrimSpace(*v) == "" {
			continue
		}
		result = append(result, PlatformDiscoveredValue{Value: *v, Count: cnt})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	added, _ := r.addedMatchValues(ctx, dimension)
	for i := range result {
		if _, ok := added[result[i].Value]; ok {
			result[i].AlreadyAdded = true
		}
	}
	return result, nil
}

func (r *PlatformRepository) addedMatchValues(ctx context.Context, dimension string) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT unnest(COALESCE(match_values, ARRAY[COALESCE(match_value, platform_name)]))
		   FROM platform_libraries WHERE dimension = $1`, dimension)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]struct{})
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err == nil {
			m[v] = struct{}{}
		}
	}
	return m, rows.Err()
}

func (r *PlatformRepository) SetEnabled(ctx context.Context, platformName string, enabled bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET enabled = $1 WHERE platform_name = $2`, enabled, platformName)
	return err
}

func (r *PlatformRepository) SetEnabledByID(ctx context.Context, id string, enabled bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET enabled = $1 WHERE id = $2::uuid`, enabled, id)
	return err
}

func (r *PlatformRepository) UpdateSortOrder(ctx context.Context, orderedIDs []string) error {
	for i, id := range orderedIDs {
		if _, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET sort_order = $1 WHERE id = $2::uuid`, i, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *PlatformRepository) AddLibrary(ctx context.Context, dimension, matchValue, displayName string, enabled bool) (bool, error) {
	dimension = strings.TrimSpace(dimension)
	if dimension == "" {
		dimension = PlatformDimStudio
	}
	matchValue = strings.TrimSpace(matchValue)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = matchValue
	}
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO platform_libraries (platform_name, dimension, match_value, match_values, enabled)
		 VALUES ($1, $2, $3, ARRAY[$3], $4)
		 ON CONFLICT (dimension, match_value) DO NOTHING`,
		displayName, dimension, matchValue, enabled)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PlatformRepository) GetByID(ctx context.Context, id string) (*PlatformLibrary, error) {
	var p PlatformLibrary
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, platform_name, enabled, collection_type, icon_url, created_at, sort_order,
		        dimension, COALESCE(match_value, platform_name), match_values, cover_image_path, cover_image_tag, display_name,
		        item_limit
		   FROM platform_libraries WHERE id = $1::uuid`, id).
		Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL,
			&p.CreatedAt, &p.SortOrder, &p.Dimension, &p.MatchValue, &p.MatchValues, &p.CoverImagePath, &p.CoverImageTag, &p.DisplayName,
			&p.ItemLimit)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PlatformRepository) SetCover(ctx context.Context, id, path, tag string) error {
	_, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET cover_image_path = $1, cover_image_tag = $2 WHERE id = $3::uuid`, path, tag, id)
	return err
}

func (r *PlatformRepository) ClearCover(ctx context.Context, id string) (string, error) {
	var oldPath *string
	_ = r.pool.QueryRow(ctx, `SELECT cover_image_path FROM platform_libraries WHERE id = $1::uuid`, id).Scan(&oldPath)
	_, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET cover_image_path = NULL, cover_image_tag = NULL WHERE id = $1::uuid`, id)
	if oldPath != nil {
		return *oldPath, err
	}
	return "", err
}

func (r *PlatformRepository) Rename(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	var val any
	if name != "" {
		val = name
	}
	_, err := r.pool.Exec(ctx, `UPDATE platform_libraries SET display_name = $1 WHERE id = $2::uuid`, val, id)
	return err
}

func (r *PlatformRepository) AddValues(ctx context.Context, id string, values []string) error {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE platform_libraries
		    SET match_values = ARRAY(
		      SELECT DISTINCT v
		        FROM unnest(COALESCE(match_values, ARRAY[match_value]) || $2::text[]) AS v
		       WHERE v IS NOT NULL AND v <> ''
		    )
		  WHERE id = $1::uuid`,
		id, clean)
	return err
}

func (r *PlatformRepository) RemoveValue(ctx context.Context, id, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE platform_libraries
		    SET match_values = array_remove(COALESCE(match_values, ARRAY[match_value]), $2)
		  WHERE id = $1::uuid AND match_value <> $2`,
		id, value)
	return err
}

func (r *PlatformRepository) DeleteLibrary(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM platform_libraries WHERE id = $1::uuid`, id)
	return err
}

func (r *PlatformRepository) IsGlobalEnabled(ctx context.Context) bool {
	var val *string
	_ = r.pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'platform_libraries_enabled'").Scan(&val)
	return val != nil && *val == "true"
}

func (r *PlatformRepository) CollectionType(ctx context.Context, dimension string, values []string) string {
	if dimension == PlatformDimLatest {
		return ""
	}
	cond, ok := PlatformDimensionCondition(dimension)
	if !ok || len(values) == 0 {
		return ""
	}
	var movieCount, seriesCount int64
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE `+cond+` AND type = 'Movie' AND merged_to_id IS NULL`, values).Scan(&movieCount)
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE `+cond+` AND type = 'Series' AND merged_to_id IS NULL`, values).Scan(&seriesCount)
	switch {
	case seriesCount > 0 && movieCount == 0:
		return "tvshows"
	case movieCount > 0 && seriesCount == 0:
		return "movies"
	default:
		return ""
	}
}

func (r *PlatformRepository) UpsertLatestLibrary(ctx context.Context, displayName string, itemLimit int64, enabled *bool) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO platform_libraries (
			platform_name, display_name, dimension, match_value, match_values,
			collection_type, item_limit, enabled
		) VALUES ('Latest Movies', $1, $2, 'Movie', ARRAY['Movie', 'Series'], 'mixed', $3, COALESCE($4, true))
		ON CONFLICT (dimension, match_value) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			match_values = EXCLUDED.match_values,
			collection_type = 'mixed',
			item_limit = EXCLUDED.item_limit,
			enabled = COALESCE($4, platform_libraries.enabled)`,
		displayName, PlatformDimLatest, itemLimit, enabled)
	return err
}

func (r *PlatformRepository) CountItemsByStudio(ctx context.Context, studio string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items WHERE studio = $1 AND type IN ('Movie', 'Series') AND merged_to_id IS NULL`,
		studio).Scan(&count)
	return count, err
}

func (r *PlatformRepository) PropagateStudioToChildren(ctx context.Context, seriesID, studio string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		   SET studio = $1,
		       platform_scan_status = 'matched',
		       platform_scan_source = COALESCE(platform_scan_source, 'tmdb'),
		       platform_scan_error = NULL,
		       platform_scanned_at = NOW()
		 WHERE (series_id = $2::uuid OR parent_id = $2::uuid)
		   AND type IN ('Season', 'Episode')
		   AND (platform_scan_status <> 'matched' OR studio IS DISTINCT FROM $1)`,
		studio, seriesID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE items
		   SET studio = $1,
		       platform_scan_status = 'matched',
		       platform_scan_source = COALESCE(platform_scan_source, 'tmdb'),
		       platform_scan_error = NULL,
		       platform_scanned_at = NOW()
		 WHERE parent_id IN (
		   SELECT id FROM items WHERE parent_id = $2::uuid AND type = 'Season'
		 ) AND type = 'Episode'
		   AND (platform_scan_status <> 'matched' OR studio IS DISTINCT FROM $1)`,
		studio, seriesID)
	return err
}

func (r *PlatformRepository) MarkScanPending(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'pending',
		        platform_scan_error = NULL
		  WHERE id = $1::uuid`,
		itemID)
	return err
}

func (r *PlatformRepository) MarkScanMatched(ctx context.Context, itemID, studio, source string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = $2,
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		studio, source, itemID)
	return err
}

func (r *PlatformRepository) MarkScanNoMatch(ctx context.Context, itemID, source, errMsg string) error {
	var errorVal any
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'no_match',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		source, errorVal, itemID)
	return err
}

func (r *PlatformRepository) MarkScanUnidentified(ctx context.Context, itemID, source, errMsg string) error {
	var errorVal any
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'unidentified',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		source, errorVal, itemID)
	return err
}

func (r *PlatformRepository) MarkScanError(ctx context.Context, itemID, source, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'error',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		source, strings.TrimSpace(errMsg), itemID)
	return err
}

func (r *PlatformRepository) ListPendingScanItems(ctx context.Context, limit int, requireTMDB bool, includeNoMatch bool) ([]PlatformScanItem, error) {
	statuses := []string{"pending", "unidentified", "error"}
	if includeNoMatch {
		statuses = append(statuses, "no_match")
	}
	sql := `SELECT id::text, type, name, production_year, tmdb_id, file_path, studio, platform_scan_status, platform_scan_source
	          FROM items
	         WHERE type IN ('Movie', 'Series')
	           AND platform_scan_status = ANY($1)`
	args := []any{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	sql += ` ORDER BY platform_scanned_at NULLS FIRST, updated_at DESC NULLS LAST`
	if limit > 0 {
		sql += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PlatformScanItem
	for rows.Next() {
		var item PlatformScanItem
		if err := rows.Scan(
			&item.ID, &item.ItemType, &item.Name, &item.ProductionYear, &item.TmdbID,
			&item.FilePath, &item.Studio, &item.PlatformScanStatus, &item.PlatformScanSource,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *PlatformRepository) CountPendingScanItems(ctx context.Context, requireTMDB bool, includeNoMatch bool) (int64, error) {
	statuses := []string{"pending", "unidentified", "error"}
	if includeNoMatch {
		statuses = append(statuses, "no_match")
	}
	sql := `SELECT COUNT(*) FROM items WHERE type IN ('Movie', 'Series') AND platform_scan_status = ANY($1)`
	args := []any{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	var count int64
	err := r.pool.QueryRow(ctx, sql, args...).Scan(&count)
	return count, err
}

func (r *PlatformRepository) CountPendingMetadataScrape(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items
		  WHERE type IN ('Movie', 'Series')
		    AND tmdb_id IS NULL
		    AND platform_scan_status IN ('pending', 'unidentified', 'error')`).Scan(&count)
	return count, err
}

func (r *PlatformRepository) ScanByFilename(ctx context.Context, patterns []PlatformFilenamePattern, canonical func(string) string) (int, error) {
	total := 0
	for _, p := range patterns {
		studio := canonical(p.Platform)
		tag, err := r.pool.Exec(ctx, fmt.Sprintf(
			`UPDATE items
			    SET studio = $1,
			        platform_scan_status = 'matched',
			        platform_scan_source = 'filename',
			        platform_scan_error = NULL,
			        platform_scanned_at = NOW()
			  WHERE type IN ('Movie', 'Series', 'Season', 'Episode')
			    AND platform_scan_status IN ('pending', 'unidentified', 'error', 'no_match')
			    AND (%s)`,
			p.SQL), studio)
		if err != nil {
			return total, fmt.Errorf("%s filename scan: %w", p.Platform, err)
		}
		total += int(tag.RowsAffected())
		if _, err = r.pool.Exec(ctx, `UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = 'filename',
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE type = 'Series' AND id IN (
			SELECT DISTINCT series_id FROM items WHERE studio = $1 AND series_id IS NOT NULL
		  )`, studio); err != nil {
			return total, fmt.Errorf("%s propagate series: %w", p.Platform, err)
		}
		if _, err = r.pool.Exec(ctx, `UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = 'filename',
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE type = 'Season' AND parent_id IN (
			SELECT id FROM items WHERE studio = $1 AND type = 'Series'
		  )`, studio); err != nil {
			return total, fmt.Errorf("%s propagate season: %w", p.Platform, err)
		}
	}
	return total, nil
}
