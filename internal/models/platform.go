package models

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PlatformLibrary struct {
	ID             string
	PlatformName   string
	Enabled        bool
	CollectionType string
	IconURL        *string
	CreatedAt      time.Time
	ItemCount      int64
}

type PlatformScanStatus string

const (
	PlatformScanPending      PlatformScanStatus = "pending"
	PlatformScanMatched      PlatformScanStatus = "matched"
	PlatformScanNoMatch      PlatformScanStatus = "no_match"
	PlatformScanUnidentified PlatformScanStatus = "unidentified"
	PlatformScanError        PlatformScanStatus = "error"
)

type PlatformScanSource string

const (
	PlatformScanSourceTMDB   PlatformScanSource = "tmdb"
	PlatformScanSourceSearch PlatformScanSource = "tmdb_search"
	PlatformScanSourceFile   PlatformScanSource = "filename"
	PlatformScanSourceNFO    PlatformScanSource = "nfo"
	PlatformScanSourceManual PlatformScanSource = "manual"
	PlatformScanSourceLegacy PlatformScanSource = "legacy"
)

type PlatformScanItem struct {
	ID                 string
	ItemType           string
	Name               string
	ProductionYear     *int32
	TmdbID             *int32
	FilePath           *string
	Studio             *string
	PlatformScanStatus PlatformScanStatus
	PlatformScanSource *string
}

func GetPlatformLibraries(ctx context.Context, pool *pgxpool.Pool) ([]PlatformLibrary, error) {
	rows, err := pool.Query(ctx,
		`SELECT p.id::text, p.platform_name, p.enabled, p.collection_type, p.icon_url, p.created_at,
		        COALESCE(c.cnt, 0) AS item_count
		 FROM platform_libraries p
		 LEFT JOIN (
		     SELECT studio, COUNT(*) AS cnt
		     FROM items
		     WHERE studio IS NOT NULL AND studio != '' AND type IN ('Movie','Series') AND merged_to_id IS NULL
		     GROUP BY studio
		 ) c ON c.studio = p.platform_name
		 ORDER BY p.platform_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlatformLibrary
	for rows.Next() {
		var p PlatformLibrary
		if err := rows.Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL, &p.CreatedAt, &p.ItemCount); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func GetEnabledPlatforms(ctx context.Context, pool *pgxpool.Pool) ([]PlatformLibrary, error) {
	rows, err := pool.Query(ctx,
		`SELECT p.id::text, p.platform_name, p.enabled, p.collection_type, p.icon_url, p.created_at,
		        COALESCE(c.cnt, 0) AS item_count
		 FROM platform_libraries p
		 LEFT JOIN (
		     SELECT studio, COUNT(*) AS cnt
		     FROM items
		     WHERE studio IS NOT NULL AND studio != '' AND type IN ('Movie','Series') AND merged_to_id IS NULL
		     GROUP BY studio
		 ) c ON c.studio = p.platform_name
		 WHERE p.enabled = true
		 ORDER BY p.platform_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlatformLibrary
	for rows.Next() {
		var p PlatformLibrary
		if err := rows.Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL, &p.CreatedAt, &p.ItemCount); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func SetPlatformEnabled(ctx context.Context, pool *pgxpool.Pool, platformName string, enabled bool) error {
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET enabled = $1 WHERE platform_name = $2`,
		enabled, platformName)
	return err
}

func AddPlatformLibrary(ctx context.Context, pool *pgxpool.Pool, platformName string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO platform_libraries (platform_name, enabled) VALUES ($1, false) ON CONFLICT (platform_name) DO NOTHING`,
		platformName)
	return err
}

func DeletePlatformLibrary(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM platform_libraries WHERE id = $1::uuid`, id)
	return err
}

// IsPlatformLibrariesEnabled checks the global toggle.
func IsPlatformLibrariesEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	var val *string
	_ = pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'platform_libraries_enabled'").Scan(&val)
	return val != nil && *val == "true"
}

// platformNamespace is a fixed UUID namespace for generating deterministic platform virtual IDs.
var platformNamespace = uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")

// PlatformVirtualID returns a deterministic UUID for a platform name,
// compatible with Emby clients that require valid UUIDs.
func PlatformVirtualID(platformName string) string {
	return uuid.NewSHA1(platformNamespace, []byte(platformName)).String()
}

// IsPlatformVirtualID checks whether a given ID belongs to any enabled platform.
func IsPlatformVirtualID(ctx context.Context, pool *pgxpool.Pool, id string) (string, bool) {
	if !IsPlatformLibrariesEnabled(ctx, pool) {
		return "", false
	}
	platforms, err := GetEnabledPlatforms(ctx, pool)
	if err != nil {
		return "", false
	}
	for _, p := range platforms {
		if PlatformVirtualID(p.PlatformName) == id {
			return p.PlatformName, true
		}
	}
	return "", false
}

// PlatformCollectionType returns the appropriate collection type based on item distribution.
func PlatformCollectionType(ctx context.Context, pool *pgxpool.Pool, studio string) string {
	var movieCount, seriesCount int64
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE studio = $1 AND type = 'Movie'", studio).Scan(&movieCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE studio = $1 AND type = 'Series'", studio).Scan(&seriesCount)
	if seriesCount > 0 && movieCount == 0 {
		return "tvshows"
	}
	return "movies"
}

// PlatformVirtualIDHash returns a deterministic emby-compatible numeric hash for a platform.
func PlatformVirtualIDHash(platformName string) string {
	h := sha256.Sum256([]byte("platform:" + platformName))
	return fmt.Sprintf("%x", h[:16])
}

// GetItemCountByStudio returns number of top-level items (Movie/Series) for a studio.
func GetItemCountByStudio(ctx context.Context, pool *pgxpool.Pool, studio string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items WHERE studio = $1 AND type IN ('Movie', 'Series') AND merged_to_id IS NULL`,
		studio).Scan(&count)
	return count, err
}

// GradientColor defines a 3-stop gradient: [0%, 40%, 100%] as RGB.
type GradientColor [3][3]uint8

// PlatformLogoEntry maps keyword patterns to embedded logo filenames and gradient colors.
type PlatformLogoEntry struct {
	Keywords []string
	File     string
	Gradient GradientColor
}

var defaultGradient = GradientColor{{0x1a, 0x1a, 0x2e}, {0x1e, 0x29, 0x3b}, {0x33, 0x41, 0x55}}

// PlatformLogoMap is the list of all known platform → logo mappings.
var PlatformLogoMap = []PlatformLogoEntry{
	{[]string{"netflix"}, "logo/Netflix-iOS-1024x1024.png", GradientColor{{0x1a, 0x00, 0x00}, {0x3d, 0x00, 0x00}, {0x8b, 0x00, 0x00}}},
	{[]string{"disney"}, "logo/Disney+-iOS-1024x1024.png", GradientColor{{0x02, 0x00, 0x24}, {0x04, 0x0e, 0x50}, {0x0d, 0x1b, 0x63}}},
	{[]string{"amazon", "prime video"}, "logo/Amazon Prime Video-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x2e}, {0x00, 0x30, 0x50}, {0x00, 0x66, 0x8a}}},
	{[]string{"apple tv", "apple"}, "logo/Apple TV-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x1a, 0x1a}, {0x1d, 0x1d, 0x1f}}},
	{[]string{"hulu"}, "logo/Hulu_ Stream TV shows & movies-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x0a}, {0x00, 0x3c, 0x15}, {0x0a, 0x5c, 0x25}}},
	{[]string{"paramount"}, "logo/Paramount+-iOS-1024x1024.png", GradientColor{{0x00, 0x0a, 0x2e}, {0x00, 0x1b, 0x5e}, {0x00, 0x40, 0xb0}}},
	{[]string{"peacock"}, "logo/Peacock TV_ Stream TV & Movies-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x10, 0x20}, {0x2a, 0x1a, 0x3a}}},
	{[]string{"hbo"}, "logo/HBO Max_ Stream Movies & TV-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x1a, 0x2e}, {0x2d, 0x2d, 0x3f}}},
	{[]string{"bilibili", "哔哩哔哩"}, "logo/bilibili - All Your Fav Videos-iOS-1024x1024.png", GradientColor{{0x2e, 0x05, 0x10}, {0x50, 0x10, 0x20}, {0x8a, 0x20, 0x3a}}},
	{[]string{"iqiyi", "爱奇艺"}, "logo/iQIYI - Dramas, Anime, Shows-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x08}, {0x00, 0x40, 0x18}, {0x00, 0x80, 0x30}}},
	{[]string{"tencent", "腾讯"}, "logo/Tencent Video-iOS-1024x1024.png", GradientColor{{0x10, 0x18, 0x00}, {0x28, 0x38, 0x00}, {0x50, 0x68, 0x10}}},
	{[]string{"youku", "优酷"}, "logo/YOUKU-Drama, Film, Show, Anime-iOS-1024x1024.png", GradientColor{{0x10, 0x15, 0x2e}, {0x1a, 0x30, 0x50}, {0x30, 0x58, 0x80}}},
}

// PlatformLogoEntry lookup by name.
func findPlatformEntry(platformName string) *PlatformLogoEntry {
	lower := strings.ToLower(platformName)
	for i := range PlatformLogoMap {
		for _, kw := range PlatformLogoMap[i].Keywords {
			if strings.Contains(lower, kw) {
				return &PlatformLogoMap[i]
			}
		}
	}
	return nil
}

// PlatformLogoFile returns the embedded file path for a platform, or "" if none found.
func PlatformLogoFile(platformName string) string {
	if e := findPlatformEntry(platformName); e != nil {
		return e.File
	}
	return ""
}

// HasPlatformLogo returns true if a logo exists for the given platform name.
func HasPlatformLogo(platformName string) bool {
	return findPlatformEntry(platformName) != nil
}

// PlatformGradient returns the gradient colors for a platform.
func PlatformGradient(platformName string) GradientColor {
	if e := findPlatformEntry(platformName); e != nil {
		return e.Gradient
	}
	return defaultGradient
}

func normalizePlatformName(name string) string {
	if e := findPlatformEntry(name); e != nil && len(e.Keywords) > 0 {
		switch e.Keywords[0] {
		case "netflix":
			return "Netflix"
		case "disney":
			return "Disney+"
		case "amazon", "prime video":
			return "Amazon"
		case "apple tv", "apple":
			return "Apple TV+"
		case "hulu":
			return "Hulu"
		case "paramount":
			return "Paramount+"
		case "peacock":
			return "Peacock"
		case "hbo":
			return "HBO"
		case "bilibili", "哔哩哔哩":
			return "bilibili"
		case "iqiyi", "爱奇艺":
			return "iQIYI"
		case "tencent", "腾讯":
			return "Tencent Video"
		case "youku", "优酷":
			return "YOUKU"
		}
	}
	return strings.TrimSpace(name)
}

func CanonicalPlatformName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, entry := range PlatformLogoMap {
		for _, kw := range entry.Keywords {
			if strings.Contains(lower, kw) {
				return normalizePlatformName(kw)
			}
		}
	}
	return name
}

// UpdateItemStudio sets the studio field and unified platform scan state for an item.
func UpdateItemStudio(ctx context.Context, pool *pgxpool.Pool, itemID, studio string) error {
	studio = CanonicalPlatformName(studio)
	if studio == "" {
		return MarkPlatformScanNoMatch(ctx, pool, itemID, PlatformScanSourceTMDB, "")
	}
	return MarkPlatformScanMatched(ctx, pool, itemID, studio, PlatformScanSourceTMDB)
}

// PropagateStudioToChildren sets studio/state on child items (Season/Episode) of a Series.
func PropagateStudioToChildren(ctx context.Context, pool *pgxpool.Pool, seriesID, studio string) error {
	studio = CanonicalPlatformName(studio)
	_, err := pool.Exec(ctx,
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
	_, err = pool.Exec(ctx,
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

func MarkPlatformScanPending(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'pending',
		        platform_scan_error = NULL
		  WHERE id = $1::uuid`,
		itemID)
	return err
}

func MarkPlatformScanMatched(ctx context.Context, pool *pgxpool.Pool, itemID, studio string, source PlatformScanSource) error {
	studio = CanonicalPlatformName(studio)
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = $2,
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		studio, string(source), itemID)
	return err
}

func MarkPlatformScanNoMatch(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	var errorVal interface{}
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'no_match',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), errorVal, itemID)
	return err
}

func MarkPlatformScanUnidentified(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	var errorVal interface{}
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'unidentified',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), errorVal, itemID)
	return err
}

func MarkPlatformScanError(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'error',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), strings.TrimSpace(errMsg), itemID)
	return err
}

func GetItemsPendingPlatformScan(ctx context.Context, pool *pgxpool.Pool, limit int, requireTMDB bool, includeNoMatch bool) ([]PlatformScanItem, error) {
	statuses := []string{string(PlatformScanPending), string(PlatformScanUnidentified), string(PlatformScanError)}
	if includeNoMatch {
		statuses = append(statuses, string(PlatformScanNoMatch))
	}

	sql := `SELECT id::text, type, name, production_year, tmdb_id, file_path, studio, platform_scan_status, platform_scan_source
	          FROM items
	         WHERE type IN ('Movie', 'Series')
	           AND platform_scan_status = ANY($1)`
	args := []interface{}{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	sql += ` ORDER BY platform_scanned_at NULLS FIRST, updated_at DESC NULLS LAST`
	if limit > 0 {
		sql += ` LIMIT $2`
		args = append(args, limit)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlatformScanItem
	for rows.Next() {
		var item PlatformScanItem
		if err := rows.Scan(
			&item.ID,
			&item.ItemType,
			&item.Name,
			&item.ProductionYear,
			&item.TmdbID,
			&item.FilePath,
			&item.Studio,
			&item.PlatformScanStatus,
			&item.PlatformScanSource,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func CountItemsPendingPlatformScan(ctx context.Context, pool *pgxpool.Pool, requireTMDB bool, includeNoMatch bool) (int64, error) {
	statuses := []string{string(PlatformScanPending), string(PlatformScanUnidentified), string(PlatformScanError)}
	if includeNoMatch {
		statuses = append(statuses, string(PlatformScanNoMatch))
	}
	sql := `SELECT COUNT(*) FROM items WHERE type IN ('Movie', 'Series') AND platform_scan_status = ANY($1)`
	args := []interface{}{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	var count int64
	err := pool.QueryRow(ctx, sql, args...).Scan(&count)
	return count, err
}

func CountItemsPendingPlatformMetadataScrape(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items
		  WHERE type IN ('Movie', 'Series')
		    AND tmdb_id IS NULL
		    AND platform_scan_status IN ('pending', 'unidentified', 'error')`).Scan(&count)
	return count, err
}
